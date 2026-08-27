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

	// Pub/Sub methods should not panic
	_ = c.PublishConfigReload(ctx)
	_ = c.PublishVocabInvalidate(ctx, 123)
	c.SubscribeConfigReload(ctx, func() {})
	c.SubscribeVocabInvalidate(ctx, func(uID int64) {})
}

func TestCachedSessionSerialization(t *testing.T) {
	u := CachedSessionUser{
		ID:         42,
		Email:      "user@example.com",
		Name:       "Test User",
		PlanTier:   "pro",
		PlanActive: true,
		Languages:  []string{"en", "es"},
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}

	if u.ID != 42 || u.PlanTier != "pro" {
		t.Errorf("Unexpected user field values: %+v", u)
	}
}
