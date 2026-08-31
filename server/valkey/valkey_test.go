package valkey

import (
	"context"
	"testing"
	"time"
)

func TestValkeyClientFallbackWhenDisconnected(t *testing.T) {
	// Client pointing to an invalid port / non-existent server
	c := &Client{connected: false}

	if c.IsConnected() {
		t.Error("Expected client to report disconnected")
	}

	ctx := context.Background()

	// Rate limiting should gracefully allow with error indicator
	allowed, _, err := c.AllowUserRPM(ctx, 123, 10)
	if !allowed || err == nil {
		t.Errorf("Expected fallback allow=true with error; got allowed=%v, err=%v", allowed, err)
	}

	// GeoIP should gracefully return error
	_, err = c.GetGeoIP(ctx, "1.1.1.1")
	if err == nil {
		t.Error("Expected error from GetGeoIP when disconnected")
	}

	// Session cache should return error
	_, err = c.GetCachedSession(ctx, "dummy_token")
	if err == nil {
		t.Error("Expected error from GetCachedSession when disconnected")
	}

	// Invalidate methods should gracefully handle disconnected states
	_ = c.InvalidateSession(ctx, "dummy_token")
	_ = c.InvalidateUserSessions(ctx, 42)

	// Invalidate vocab should gracefully return nil
	_ = c.InvalidateCachedVocabulary(ctx, 42)
}

func TestCachedSessionSerialization(t *testing.T) {
	rpm := int64(60)
	u := CachedSessionUser{
		ID:                     42,
		Email:                  "user@example.com",
		Name:                   "Test User",
		PlanTier:               "pro",
		PlanActive:             1,
		PlanActivatedAt:        "2026-08-31T00:00:00Z",
		RazorpaySubscriptionID: "sub_12345",
		RateLimitRPM:           &rpm,
		IsAdmin:                0,
		IsTempSession:          0,
		MonthlyUsageSeconds:    120.5,
		IsLocked:               0,
		Languages:              []string{"en", "es"},
		ExpiresAt:              time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	}

	if u.ID != 42 || u.PlanTier != "pro" || u.PlanActive != 1 || *u.RateLimitRPM != 60 {
		t.Errorf("Unexpected user field values: %+v", u)
	}
}
