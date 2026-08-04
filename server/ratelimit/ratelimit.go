package ratelimit

import (
	"sync"
	"time"
)

type RollingRateLimiter struct {
	mu         sync.Mutex
	timestamps []time.Time
	limit      int
	window     time.Duration
}

func NewRollingRateLimiter(limit int, window time.Duration) *RollingRateLimiter {
	return &RollingRateLimiter{
		limit:  limit,
		window: window,
	}
}

func (r *RollingRateLimiter) SetLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit = limit
}

func (r *RollingRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Filter out old timestamps
	valid := r.timestamps[:0]
	for _, t := range r.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	r.timestamps = valid

	if len(r.timestamps) < r.limit {
		r.timestamps = append(r.timestamps, now)
		return true
	}
	return false
}

var GroqRateLimiter = NewRollingRateLimiter(20, 60*time.Second) // Starts at 20 RPM (synchronized from DB)

type SimpleRateLimiter struct {
	mu    sync.Mutex
	store map[int64]int
}

func NewSimpleRateLimiter() *SimpleRateLimiter {
	limiter := &SimpleRateLimiter{
		store: make(map[int64]int),
	}
	// Background cleanup ticker running every 60 seconds to completely wipe counts
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			limiter.mu.Lock()
			limiter.store = make(map[int64]int)
			limiter.mu.Unlock()
		}
	}()
	return limiter
}

func (rl *SimpleRateLimiter) Allow(userID int64, limit int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	count := rl.store[userID]
	if count >= limit {
		return false
	}
	rl.store[userID] = count + 1
	return true
}

func (rl *SimpleRateLimiter) GetCounts() map[int64]int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	copyMap := make(map[int64]int, len(rl.store))
	for k, v := range rl.store {
		copyMap[k] = v
	}
	return copyMap
}

func (rl *SimpleRateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.store = make(map[int64]int)
}

func (rl *SimpleRateLimiter) ResetUser(userID int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.store, userID)
}

var GlobalUserRateLimiter = NewSimpleRateLimiter()

type UserRateLimiterRegistry struct {
	mu       sync.Mutex
	limiters map[int64]*RollingRateLimiter
}

func (r *UserRateLimiterRegistry) GetLimiter(userID int64, limit int) *RollingRateLimiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	limiter, exists := r.limiters[userID]
	if !exists {
		limiter = NewRollingRateLimiter(limit, 60*time.Second)
		r.limiters[userID] = limiter
		return limiter
	}

	limiter.mu.Lock()
	limiter.limit = limit
	limiter.mu.Unlock()

	return limiter
}

var UserRateLimiterReg = &UserRateLimiterRegistry{
	limiters: make(map[int64]*RollingRateLimiter),
}

type AuthRateLimiter struct {
	mu    sync.Mutex
	store map[string][]time.Time
}

func NewAuthRateLimiter() *AuthRateLimiter {
	arl := &AuthRateLimiter{
		store: make(map[string][]time.Time),
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			arl.mu.Lock()
			now := time.Now()
			cutoff := now.Add(-15 * time.Minute)
			for k, times := range arl.store {
				var valid []time.Time
				for _, t := range times {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(arl.store, k)
				} else {
					arl.store[k] = valid
				}
			}
			arl.mu.Unlock()
		}
	}()
	return arl
}

func (arl *AuthRateLimiter) Allow(key string, limit int, window time.Duration) bool {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	times := arl.store[key]
	var valid []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		arl.store[key] = valid
		return false
	}

	valid = append(valid, now)
	arl.store[key] = valid
	return true
}

var AuthRateLimiterInst = NewAuthRateLimiter()
