package db

import (
	"context"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"path/filepath"
	"speektome-server/valkey"
	"testing"
	"time"
)

func TestSubscriptionExpiryGraceAndIdempotency(t *testing.T) {
	d, err := NewDatabase(filepath.Join(t.TempDir(), "expiry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-SubscriptionGracePeriod).Unix()
	cases := []struct {
		id     int
		tier   string
		end    int64
		expire bool
	}{
		{1, "pro", cutoff - 1, true}, {2, "pro", cutoff, true}, {3, "pro", cutoff + 1, false},
		{4, "pro", now.Add(time.Hour).Unix(), false}, {5, "free", cutoff - 100, false}, {6, "pro", 0, false},
	}
	for _, c := range cases {
		_, err = d.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_current_end,razorpay_subscription_id,monthly_usage_seconds) VALUES(?,?,'Test',?,1,?,'sub_preserved',123)`, c.id, time.Unix(int64(c.id), 0).Format(time.RFC3339)+"@test.invalid", c.tier, c.end)
		if err != nil {
			t.Fatal(err)
		}
	}
	count, err := d.ExpireSubscriptions(context.Background(), now)
	if err != nil || count != 2 {
		t.Fatalf("sweep: %d %v", count, err)
	}
	for _, c := range cases {
		var tier, sub string
		var active int
		var used float64
		if err = d.db.QueryRow(`SELECT plan_tier,plan_active,monthly_usage_seconds,razorpay_subscription_id FROM users WHERE id=?`, c.id).Scan(&tier, &active, &used, &sub); err != nil {
			t.Fatal(err)
		}
		if c.expire && (tier != "free" || active != 0 || used != 0) {
			t.Fatalf("account %d not expired: %s %d %v", c.id, tier, active, used)
		}
		if !c.expire && (tier != c.tier || used != 123) {
			t.Fatalf("account %d changed unexpectedly", c.id)
		}
		if sub != "sub_preserved" {
			t.Fatal("subscription identity lost")
		}
	}
	if _, err = d.db.Exec(`UPDATE users SET monthly_usage_seconds=12 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	count, err = d.ExpireSubscriptions(context.Background(), now.Add(time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("later sweep: %d %v", count, err)
	} // account 3 just crossed its cutoff
	var used float64
	d.db.QueryRow(`SELECT monthly_usage_seconds FROM users WHERE id=1`).Scan(&used)
	if used != 12 {
		t.Fatal("daily sweep refilled Free quota")
	}
	// A confirmed renewal restores paid entitlement and starts its new allowance.
	_, err = d.ApplySuccessfulSubscriptionPayment(1, "sub_preserved", "pay_renew", "inv_renew", "payment:pay_renew", "evt", "subscription.charged", 200, "USD", "captured", now.Unix(), now.AddDate(0, 1, 0).Unix(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	count, err = d.expireSubscriptions(context.Background(), now, 1)
	if err != nil || count != 0 {
		t.Fatalf("renewal was expired: %d %v", count, err)
	}
}

func TestAuthenticationExpiresPaidAccessWithoutDailySweep(t *testing.T) {
	d, err := NewDatabase(filepath.Join(t.TempDir(), "expiry-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, err = d.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_current_end,monthly_usage_seconds) VALUES(1,'expiry@test.invalid','Test','pro',1,?,999)`, time.Now().Add(-SubscriptionGracePeriod-time.Minute).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CreateSession("expiry-test-token", 1, 0); err != nil {
		t.Fatal(err)
	}
	user, err := d.GetUserBySession("expiry-test-token")
	if err != nil {
		t.Fatal(err)
	}
	if user.PlanTier != "free" || user.PlanActive != 0 || user.MonthlyUsageSeconds != 0 {
		t.Fatalf("expired account retained access: %#v", user)
	}
}

func TestExpiredAndLegacySessionCachesCannotPreserveProAccess(t *testing.T) {
	cache := miniredis.RunT(t)
	t.Setenv("VALKEY_URL", "redis://"+cache.Addr())
	previous := valkey.GlobalClient
	client := valkey.NewClientFromEnv()
	valkey.GlobalClient = client
	t.Cleanup(func() { client.Close(); valkey.GlobalClient = previous })
	d, err := NewDatabase(filepath.Join(t.TempDir(), "cached-expiry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	end := time.Now().Add(-SubscriptionGracePeriod - time.Minute).Unix()
	for i, legacy := range []bool{false, true} {
		id := int64(i + 1)
		token := fmt.Sprintf("expiry-cache-%d", id)
		_, err = d.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_current_end,monthly_usage_seconds) VALUES(?,?,'Test','pro',1,?,999)`, id, token+"@test.invalid", end)
		if err != nil {
			t.Fatal(err)
		}
		if err = d.CreateSession(token, id, 0); err != nil {
			t.Fatal(err)
		}
		cached := &valkey.CachedSessionUser{ID: id, PlanTier: "pro", PlanActive: 1, MonthlyUsageSeconds: 999, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
		if !legacy {
			cached.BillingPeriodEnd = &end
		}
		if err = client.SetCachedSession(context.Background(), token, cached, time.Hour); err != nil {
			t.Fatal(err)
		}
		user, err := d.GetUserBySession(token)
		if err != nil {
			t.Fatal(err)
		}
		if user.PlanActive != 0 || user.PlanTier != "free" || user.MonthlyUsageSeconds != 0 {
			t.Fatalf("stale cache granted Pro access: %#v", user)
		}
	}
}
