package pipeline

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BreakerState defines the circuit breaker state machine
type BreakerState int32

const (
	StateClosed   BreakerState = 0 // Healthy, requests flow through
	StateOpen     BreakerState = 1 // Tripped, in cooldown, fast-fail
	StateHalfOpen BreakerState = 2 // Cooldown expired, exactly one probe allowed
)

func (s BreakerState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitState tracks the resilience metrics for a specific provider or model
type CircuitState struct {
	mu               sync.RWMutex
	state            BreakerState
	inCooldownUntil  time.Time
	consecutiveFails int
	probing          int32 // Atomic flag to guarantee single half-open probe
	lastError        string
	rollingLatencyMs float64
}

func newCircuitState() *CircuitState {
	return &CircuitState{
		state: StateClosed,
	}
}

// CircuitBreakerManager coordinates state across all providers and models
type CircuitBreakerManager struct {
	mu     sync.RWMutex
	states map[string]*CircuitState
}

func NewCircuitBreakerManager() *CircuitBreakerManager {
	return &CircuitBreakerManager{
		states: make(map[string]*CircuitState),
	}
}

// GlobalCircuitBreaker is the singleton manager persisting across config reloads
var GlobalCircuitBreaker = NewCircuitBreakerManager()

func (m *CircuitBreakerManager) getOrCreate(key string) *CircuitState {
	m.mu.RLock()
	st, exists := m.states[key]
	m.mu.RUnlock()
	if exists {
		return st
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if st, exists = m.states[key]; exists {
		return st
	}
	st = newCircuitState()
	m.states[key] = st
	return st
}

func providerKey(provider string) string {
	return "provider:" + strings.ToLower(strings.TrimSpace(provider))
}

func modelKey(provider, model string) string {
	return "model:" + strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.ToLower(strings.TrimSpace(model))
}

// DoneFunc is invoked upon completion of an attempt to record metrics & update circuit state
type DoneFunc func(success bool, statusCode int, err error, respHeader http.Header)

// CanExecute checks whether a candidate provider & model is permitted to execute.
// Returns (allowed, doneFunc). If allowed is false, the caller must fast-fail/skip immediately.
func (m *CircuitBreakerManager) CanExecute(provider, model string) (bool, DoneFunc) {
	pKey := providerKey(provider)
	mKey := modelKey(provider, model)

	pState := m.getOrCreate(pKey)
	mState := m.getOrCreate(mKey)

	now := time.Now()

	// 1. Check provider-level circuit
	pAllowed, pIsProbe := checkState(pState, now, pKey)
	if !pAllowed {
		return false, nil
	}

	// 2. Check model-level circuit
	mAllowed, _ := checkState(mState, now, mKey)
	if !mAllowed {
		if pIsProbe {
			// Release probing lock on provider if model check blocked
			atomic.StoreInt32(&pState.probing, 0)
		}
		return false, nil
	}

	done := func(success bool, statusCode int, err error, respHeader http.Header) {
		pState.recordResult(pKey, success, statusCode, err, respHeader, true)
		mState.recordResult(mKey, success, statusCode, err, respHeader, false)
	}

	return true, done
}

func checkState(st *CircuitState, now time.Time, key string) (allowed bool, isProbe bool) {
	st.mu.RLock()
	state := st.state
	cooldownUntil := st.inCooldownUntil
	st.mu.RUnlock()

	switch state {
	case StateClosed:
		return true, false

	case StateOpen:
		if now.Before(cooldownUntil) {
			// In cooldown: fast-fail with 0ms delay
			return false, false
		}
		// Cooldown expired: attempt single atomic probe
		if atomic.CompareAndSwapInt32(&st.probing, 0, 1) {
			st.mu.Lock()
			st.state = StateHalfOpen
			st.mu.Unlock()
			log.Printf("[CIRCUIT BREAKER] %s entered HALF_OPEN state; executing single probe request", key)
			return true, true
		}
		// Another concurrent request is already probing: fast-fail
		return false, false

	case StateHalfOpen:
		// While half-open, only the active probe is allowed through
		return false, false

	default:
		return true, false
	}
}

func parseCooldownDuration(header http.Header, consecutiveFails int) time.Duration {
	if header != nil {
		retryAfter := header.Get("Retry-After")
		if retryAfter != "" {
			if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
				if secs > 60 {
					secs = 60
				}
				return time.Duration(secs) * time.Second
			}
			if t, err := http.ParseTime(retryAfter); err == nil {
				d := time.Until(t)
				if d > 0 {
					if d > 60*time.Second {
						d = 60 * time.Second
					}
					return d
				}
			}
		}
	}

	// Exponential backoff: 15s -> 30s -> 60s
	multiplier := 1 << (consecutiveFails - 1)
	if multiplier < 1 {
		multiplier = 1
	}
	backoffSecs := 15 * multiplier
	if backoffSecs > 60 {
		backoffSecs = 60
	}
	return time.Duration(backoffSecs) * time.Second
}

func isTrippingError(statusCode int, err error, isProviderLevel bool) bool {
	if isProviderLevel {
		// Provider-level circuit trips only on 502/503/504 or network outage/timeout
		if statusCode == http.StatusBadGateway ||
			statusCode == http.StatusServiceUnavailable ||
			statusCode == http.StatusGatewayTimeout {
			return true
		}
		if err != nil {
			errStr := strings.ToLower(err.Error())
			return strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "deadline exceeded") ||
				strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "dial tcp")
		}
		return false
	}

	// Model-level circuit trips on 429 (rate-limit / capacity busy) as well as gateway / timeout errors
	if statusCode == http.StatusTooManyRequests || // 429
		statusCode == http.StatusBadGateway || // 502
		statusCode == http.StatusServiceUnavailable || // 503
		statusCode == http.StatusGatewayTimeout { // 504
		return true
	}
	if err != nil {
		errStr := strings.ToLower(err.Error())
		return strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "deadline exceeded") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "model busy") ||
			strings.Contains(errStr, "engine_overloaded") ||
			strings.Contains(errStr, "rate_limit_exceeded")
	}
	return false
}

func (st *CircuitState) recordResult(key string, success bool, statusCode int, err error, header http.Header, isProviderLevel bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	defer atomic.StoreInt32(&st.probing, 0)

	if success {
		if st.state != StateClosed {
			log.Printf("[CIRCUIT BREAKER] %s recovered! Transitioning to CLOSED.", key)
		}
		st.state = StateClosed
		st.consecutiveFails = 0
		st.lastError = ""
		return
	}

	// Only trip the circuit breaker on capacity / gateway / transient network errors
	if !isTrippingError(statusCode, err, isProviderLevel) {
		// Non-tripping error (e.g. 400 Bad Request or 401 Unauthorized, or 429 on provider-level)
		if st.state == StateHalfOpen {
			st.state = StateClosed
		}
		return
	}

	st.consecutiveFails++
	cooldown := parseCooldownDuration(header, st.consecutiveFails)
	st.inCooldownUntil = time.Now().Add(cooldown)
	st.state = StateOpen
	if err != nil {
		st.lastError = err.Error()
	} else {
		st.lastError = http.StatusText(statusCode)
	}

	log.Printf("[CIRCUIT BREAKER] %s TRIPPED (state: OPEN, cooldown: %v, fails: %d). Error: %s",
		key, cooldown, st.consecutiveFails, st.lastError)
}

// Reset clears all states (useful in hermetic tests)
func (m *CircuitBreakerManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = make(map[string]*CircuitState)
}
