package pipeline

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreakerStateTransitions(t *testing.T) {
	cb := NewCircuitBreakerManager()
	provider := "deepinfra"
	model := "deepseek-ai/DeepSeek-V4-Flash"

	// 1. Initially CLOSED (healthy)
	allowed, done := cb.CanExecute(provider, model)
	if !allowed || done == nil {
		t.Fatal("Expected CanExecute to allow healthy provider")
	}

	// 2. Report HTTP 429 failure -> should trip circuit to OPEN
	header := http.Header{}
	header.Set("Retry-After", "1") // 1 second cooldown for test speed
	done(false, http.StatusTooManyRequests, errors.New("rate limit exceeded"), header)

	// 3. Immediately after, CanExecute should fast-fail
	allowed, _ = cb.CanExecute(provider, model)
	if allowed {
		t.Error("Expected CanExecute to block request during cooldown")
	}

	// 4. Wait for 1 second cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	// 5. Half-Open probe: first request should be allowed as probe
	allowed, probeDone := cb.CanExecute(provider, model)
	if !allowed || probeDone == nil {
		t.Fatal("Expected CanExecute to allow single probe request after cooldown expiry")
	}

	// 6. Concurrent request during probe should be blocked
	concurrentAllowed, _ := cb.CanExecute(provider, model)
	if concurrentAllowed {
		t.Error("Expected concurrent request during HALF_OPEN probe to be blocked")
	}

	// 7. Probe succeeds -> should reset circuit to CLOSED
	probeDone(true, http.StatusOK, nil, nil)

	// 8. Subsequent requests should now be allowed
	allowedAfterReset, _ := cb.CanExecute(provider, model)
	if !allowedAfterReset {
		t.Error("Expected circuit to be CLOSED after successful probe")
	}
}

func TestCircuitBreakerConcurrentHalfOpenProbe(t *testing.T) {
	cb := NewCircuitBreakerManager()
	provider := "groq"
	model := "llama-3.3-70b-versatile"

	// Trip circuit with 1 second cooldown
	_, done := cb.CanExecute(provider, model)
	header := http.Header{}
	header.Set("Retry-After", "1")
	done(false, http.StatusTooManyRequests, errors.New("busy"), header)

	time.Sleep(1100 * time.Millisecond)

	// Launch 50 concurrent requests simultaneously
	const numGoroutines = 50
	var wg sync.WaitGroup
	allowedCount := 0
	var mu sync.Mutex

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			allowed, probeDone := cb.CanExecute(provider, model)
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
				// Simulate probe completion
				time.Sleep(50 * time.Millisecond)
				probeDone(true, http.StatusOK, nil, nil)
			}
		}()
	}
	wg.Wait()

	if allowedCount != 1 {
		t.Errorf("Expected exactly 1 request to be granted probe status in HALF_OPEN, got %d", allowedCount)
	}
}

func TestCircuitBreakerNonTrippingErrors(t *testing.T) {
	cb := NewCircuitBreakerManager()
	provider := "cerebras"
	model := "gpt-oss-120b"

	allowed, done := cb.CanExecute(provider, model)
	if !allowed {
		t.Fatal("Expected CanExecute to allow request")
	}

	// 400 Bad Request (bad prompt) should NOT trip the circuit breaker
	done(false, http.StatusBadRequest, errors.New("bad request"), nil)

	allowedAfter, _ := cb.CanExecute(provider, model)
	if !allowedAfter {
		t.Error("Circuit breaker should not trip on 400 Bad Request")
	}

	// 401 Unauthorized should NOT trip the circuit breaker
	_, done2 := cb.CanExecute(provider, model)
	done2(false, http.StatusUnauthorized, errors.New("unauthorized"), nil)

	allowedAfter2, _ := cb.CanExecute(provider, model)
	if !allowedAfter2 {
		t.Error("Circuit breaker should not trip on 401 Unauthorized")
	}
}

func TestModelLevelVsProviderLevelKeying(t *testing.T) {
	cb := NewCircuitBreakerManager()
	provider := "deepinfra"
	modelA := "deepseek-ai/DeepSeek-V4-Flash"
	modelB := "meta-llama/Llama-3.3-70B-Instruct"

	// Trip modelA on 429
	_, doneA := cb.CanExecute(provider, modelA)
	header := http.Header{}
	header.Set("Retry-After", "10")
	doneA(false, http.StatusTooManyRequests, errors.New("model busy"), header)

	// modelA should be blocked
	allowedA, _ := cb.CanExecute(provider, modelA)
	if allowedA {
		t.Error("Expected modelA to be blocked on 429")
	}

	// But modelB on the same provider should still be allowed if it wasn't a 503 gateway outage
	// (Since model-level 429 only trips modelKey)
	allowedB, doneB := cb.CanExecute(provider, modelB)
	if !allowedB || doneB == nil {
		t.Fatalf("Expected modelB to remain available when modelA trips on model-specific 429")
	}
	doneB(true, http.StatusOK, nil, nil)
}

func TestRetryAfterParsingFormats(t *testing.T) {
	// Test integer seconds
	h1 := http.Header{}
	h1.Set("Retry-After", "25")
	d1 := parseCooldownDuration(h1, 1)
	if d1 != 25*time.Second {
		t.Errorf("Expected 25s, got %v", d1)
	}

	// Test HTTP-date format
	h2 := http.Header{}
	future := time.Now().UTC().Add(40 * time.Second)
	h2.Set("Retry-After", future.Format(http.TimeFormat))
	d2 := parseCooldownDuration(h2, 1)
	if d2 < 38*time.Second || d2 > 42*time.Second {
		t.Errorf("Expected approx 40s from HTTP date, got %v", d2)
	}

	// Test exponential fallback when header missing
	d3 := parseCooldownDuration(nil, 1)
	if d3 != 15*time.Second {
		t.Errorf("Expected 15s for 1st fail without header, got %v", d3)
	}
	d4 := parseCooldownDuration(nil, 2)
	if d4 != 30*time.Second {
		t.Errorf("Expected 30s for 2nd fail without header, got %v", d4)
	}
	d5 := parseCooldownDuration(nil, 3)
	if d5 != 60*time.Second {
		t.Errorf("Expected 60s for 3rd fail without header, got %v", d5)
	}
}
