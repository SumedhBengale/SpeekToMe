package accountlimits_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"path/filepath"
	"speektome-server/accountlimits"
	"speektome-server/db"
	"speektome-server/valkey"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) accountlimits.Store {
	t.Helper()
	d, e := db.NewDatabase(filepath.Join(t.TempDir(), "limits.db"))
	if e != nil {
		t.Fatal(e)
	}
	d.GetDB().SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return accountlimits.Store{DB: d.GetDB()}
}
func TestRateAndLeaseAreAccountScopedAcrossStores(t *testing.T) {
	s := fixture(t)
	other := accountlimits.Store{DB: s.DB}
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if e := s.Rate(ctx, 1, "transform", 10); e != nil {
			t.Fatal(e)
		}
	}
	if e := other.Rate(ctx, 1, "transform", 10); e == nil {
		t.Fatal("other session bypassed rate limit")
	}
	if e := other.Rate(ctx, 2, "transform", 10); e != nil {
		t.Fatal(e)
	}
	owner, e := s.Acquire(ctx, 1, "transform", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Acquire(ctx, 1, "transform", time.Minute); e == nil {
		t.Fatal("simultaneous transform admitted")
	}
	if _, e = other.Acquire(ctx, 2, "transform", time.Minute); e != nil {
		t.Fatal(e)
	}
	other.Release(1, "transform", "wrong-owner")
	if _, e = other.Acquire(ctx, 1, "transform", time.Minute); e == nil {
		t.Fatal("wrong owner released lease")
	}
	s.Release(1, "transform", owner)
	if _, e = other.Acquire(ctx, 1, "transform", time.Minute); e != nil {
		t.Fatal(e)
	}
}
func TestOnlyOneConcurrentAccountLeaseWins(t *testing.T) {
	s := fixture(t)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Acquire(context.Background(), 7, "transform", time.Minute); e == nil {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("admitted %d simultaneous requests", admitted.Load())
	}
}
func TestQuotaReservationSettlementPersistenceAndIndependentMonths(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	reset := time.Now().Add(time.Hour)
	r, e := s.Reserve(ctx, 1, "first", reset, 80, 50, 100, 60)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Reserve(ctx, 1, "first", reset, 30, 50, 100, 60); e == nil {
		t.Fatal("reserved input was overspent")
	}
	if e = r.Settle(20, 10); e != nil {
		t.Fatal(e)
	}
	if e = r.Settle(20, 10); e != nil {
		t.Fatal(e)
	}
	r, e = (accountlimits.Store{DB: s.DB}).Reserve(ctx, 1, "first", reset, 80, 100, 100, 60)
	if e != nil {
		t.Fatal(e)
	}
	if r.Output != 50 {
		t.Fatalf("output budget not clamped: %d", r.Output)
	}
	if _, e = s.Reserve(ctx, 1, "first", reset, 0, 50, 100, 60); e == nil {
		t.Fatal("reserved output was overspent")
	}
	// Missing usage deliberately keeps the reservation across restart/cache loss.
	var input, output int64
	if e = s.DB.QueryRow("SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=1 AND period='first'").Scan(&input, &output); e != nil {
		t.Fatal(e)
	}
	if input != 100 || output != 60 {
		t.Fatalf("usage %d/%d", input, output)
	}
	if _, e = s.Reserve(ctx, 1, "second", reset, 80, 50, 100, 60); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Reserve(ctx, 2, "first", reset, 80, 50, 100, 60); e != nil {
		t.Fatal(e)
	}
}
func TestQuotaSurvivesDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limits.db")
	d, e := db.NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = (accountlimits.Store{DB: d.GetDB()}).Reserve(context.Background(), 1, "month", time.Now().Add(time.Hour), 100, 50, 100, 50)
	if e != nil {
		t.Fatal(e)
	}
	d.Close()
	d, e = db.NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	_, e = (accountlimits.Store{DB: d.GetDB()}).Reserve(context.Background(), 1, "month", time.Now().Add(time.Hour), 1, 1, 100, 50)
	var failure *accountlimits.Failure
	if !errors.As(e, &failure) || failure.Code != "command_input_quota_exhausted" {
		t.Fatalf("lost durable quota: %v", e)
	}
}
func TestBillingPeriodClampsMonthEnds(t *testing.T) {
	anchor := "2026-01-31T12:00:00Z"
	for _, tc := range []struct{ now, start, end string }{
		{"2026-02-28T11:00:00Z", "2026-01-31T12:00:00Z", "2026-02-28T12:00:00Z"},
		{"2026-02-28T12:00:00Z", "2026-02-28T12:00:00Z", "2026-03-31T12:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, end := accountlimits.Period(anchor, now)
		if start != tc.start || end.Format(time.RFC3339) != tc.end {
			t.Fatalf("period: %s to %s", start, end)
		}
	}
}

func TestValkeyRateChecksAndQuotaCacheCannotResetDurableLimits(t *testing.T) {
	mini := miniredis.RunT(t)
	t.Setenv("VALKEY_URL", "redis://"+mini.Addr())
	previous := valkey.GlobalClient
	cache := valkey.NewClientFromEnv()
	valkey.GlobalClient = cache
	t.Cleanup(func() { valkey.GlobalClient = previous; cache.Close() })
	s := fixture(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if e := s.Rate(ctx, 1, "transform", 10); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Rate(ctx, 1, "transform", 10); e == nil {
		t.Fatal("Valkey admitted request eleven")
	}
	key := fmt.Sprintf("ratelimit:account:1:transform:%d", time.Now().Unix()/60)
	if !mini.Exists(key) {
		t.Fatal("account rate counter was not cached")
	}
	reservation, e := s.Reserve(ctx, 1, "month", time.Now().Add(time.Hour), 100, 50, 100, 50)
	if e != nil {
		t.Fatal(e)
	}
	if !mini.Exists("quota:command:1:month") {
		t.Fatal("monthly usage was not cached")
	}
	mini.FlushAll()
	if e = s.Rate(ctx, 1, "transform", 10); e == nil {
		t.Fatal("flushing Valkey reset rate allowance")
	}
	if _, e = s.Reserve(ctx, 1, "month", time.Now().Add(time.Hour), 1, 1, 100, 50); e == nil {
		t.Fatal("flushing Valkey reset token allowance")
	}
	if e = reservation.Settle(50, 25); e != nil {
		t.Fatal(e)
	}
	if e = s.ResetRates(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.Rate(ctx, 1, "transform", 10); e != nil {
		t.Fatal(e)
	}
	var input, output int64
	s.DB.QueryRow("SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=1 AND period='month'").Scan(&input, &output)
	if input != 50 || output != 25 {
		t.Fatal("admin rate reset changed monthly quota", input, output)
	}
	mini.Close()
	if e = s.Rate(ctx, 2, "transform", 10); e != nil {
		t.Fatal("durable rate fallback failed", e)
	}
}

func TestConfirmedBillingPeriodDoesNotRefillAfterExpiry(t *testing.T) {
	now := time.Now().UTC()
	start, end := now.AddDate(0, -2, 0), now.AddDate(0, -1, 0)
	period, reset := accountlimits.BillingPeriod("", start.Unix(), end.Unix(), now)
	if period != start.Format(time.RFC3339) || reset.Unix() != end.Unix() {
		t.Fatalf("expired confirmed period rolled forward: %s %v", period, reset)
	}
}
