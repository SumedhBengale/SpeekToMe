package ratelimit

import (
	"testing"
	"time"
)

func TestRollingLimiterHonoursLimitAndWindow(t *testing.T) {
	limiter := NewRollingRateLimiter(2, 50*time.Millisecond)
	if !limiter.Allow() || !limiter.Allow() || limiter.Allow() {
		t.Fatal("limit of two was not enforced")
	}
	time.Sleep(60 * time.Millisecond)
	if !limiter.Allow() {
		t.Fatal("window did not expire")
	}
	limiter.SetLimit(0)
	if limiter.Allow() {
		t.Fatal("zero limit admitted a request")
	}
}

func TestSimpleLimiterCountsPerUser(t *testing.T) {
	limiter := NewSimpleRateLimiter()
	for i := 0; i < 3; i++ {
		if !limiter.Allow(1, 3) {
			t.Fatalf("request %d denied", i)
		}
	}
	if limiter.Allow(1, 3) {
		t.Fatal("fourth request admitted")
	}
	if !limiter.Allow(2, 3) {
		t.Fatal("another user's budget was shared")
	}
	limiter.ResetUser(1)
	if !limiter.Allow(1, 3) {
		t.Fatal("reset did not restore the budget")
	}
}

func TestRegistryUpdatesLimitForExistingUser(t *testing.T) {
	registry := &UserRateLimiterRegistry{limiters: map[int64]*RollingRateLimiter{}}
	first := registry.GetLimiter(7, 1)
	if !first.Allow() || first.Allow() {
		t.Fatal("initial limit not applied")
	}
	if registry.GetLimiter(7, 2) != first || !first.Allow() {
		t.Fatal("raised limit not applied to the same limiter")
	}
}

func TestAuthLimiterSeparatesKeysAndExpires(t *testing.T) {
	limiter := NewAuthRateLimiter()
	if !limiter.Allow("login_ip:1", 1, 40*time.Millisecond) || limiter.Allow("login_ip:1", 1, 40*time.Millisecond) {
		t.Fatal("per-key limit not enforced")
	}
	if !limiter.Allow("login_ip:2", 1, 40*time.Millisecond) {
		t.Fatal("keys share a budget")
	}
	time.Sleep(50 * time.Millisecond)
	if !limiter.Allow("login_ip:1", 1, 40*time.Millisecond) {
		t.Fatal("window did not expire")
	}
}
