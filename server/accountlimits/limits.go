// Package accountlimits provides account-scoped admission and durable token budgets.
// Valkey accelerates rate checks; SQL remains authoritative for quotas and leases.
package accountlimits

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"speektome-server/valkey"
)

const Schema = `
CREATE TABLE IF NOT EXISTS account_rate_windows(user_id INTEGER NOT NULL,operation TEXT NOT NULL,window INTEGER NOT NULL,count INTEGER NOT NULL,PRIMARY KEY(user_id,operation));
CREATE TABLE IF NOT EXISTS account_leases(user_id INTEGER NOT NULL,operation TEXT NOT NULL,owner TEXT NOT NULL,expires INTEGER NOT NULL,PRIMARY KEY(user_id,operation));
CREATE TABLE IF NOT EXISTS command_usage(user_id INTEGER NOT NULL,period TEXT NOT NULL,input_tokens INTEGER NOT NULL DEFAULT 0,output_tokens INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(user_id,period));
CREATE TABLE IF NOT EXISTS command_attempts(id TEXT PRIMARY KEY,user_id INTEGER NOT NULL,period TEXT NOT NULL,input_tokens INTEGER NOT NULL,output_tokens INTEGER NOT NULL,settled INTEGER NOT NULL DEFAULT 0,provider TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '');`

type Failure struct {
	Code       string `json:"code"`
	Message    string `json:"error"`
	RetryAfter int    `json:"retry_after_seconds,omitempty"`
	Status     int    `json:"-"`
}

func (f *Failure) Error() string    { return f.Message }
func (f *Failure) NoFallback() bool { return true }
func Denied(code, message string, retry int) *Failure {
	return &Failure{Code: code, Message: message, RetryAfter: retry, Status: http.StatusTooManyRequests}
}
func Unavailable() *Failure {
	return &Failure{Code: "limit_service_unavailable", Message: "Account limits are temporarily unavailable. Please try again shortly.", RetryAfter: 10, Status: http.StatusServiceUnavailable}
}
func Write(w http.ResponseWriter, err error) {
	var failure *Failure
	if !errors.As(err, &failure) {
		failure = Unavailable()
	}
	w.Header().Set("Content-Type", "application/json")
	if failure.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(failure.RetryAfter))
	}
	w.WriteHeader(failure.Status)
	_ = json.NewEncoder(w).Encode(failure)
}
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

type Store struct{ DB *sql.DB }

// Rate is independent for each operation, shared by all sessions for this user.
func (s Store) Rate(ctx context.Context, user int64, operation string, limit int) error {
	now := time.Now().UTC()
	retry := 60 - now.Second()
	if cache := valkey.GlobalClient; cache != nil && cache.IsConnected() {
		check, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		count, err := cache.Raw().Eval(check, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end; return n`, []string{fmt.Sprintf("ratelimit:account:%d:%s:%d", user, operation, now.Unix()/60)}, 120).Int64()
		cancel()
		if err == nil {
			if count > int64(limit) {
				return Denied(operation+"_rate_limit", fmt.Sprintf("Too many %s requests. Your account allows %d per minute. Please try again shortly.", operation, limit), retry)
			}
		}
	}
	// Durable fallback stays account-wide even across Go processes or cache outages.
	var n int
	err := s.DB.QueryRowContext(ctx, `INSERT INTO account_rate_windows(user_id,operation,window,count) VALUES(?,?,?,1) ON CONFLICT(user_id,operation) DO UPDATE SET count=CASE WHEN window=excluded.window THEN count+1 ELSE 1 END,window=excluded.window RETURNING count`, user, operation, now.Unix()/60).Scan(&n)
	if err != nil {
		return Unavailable()
	}
	if n > limit {
		return Denied(operation+"_rate_limit", fmt.Sprintf("Too many %s requests. Your account allows %d per minute. Please try again shortly.", operation, limit), retry)
	}
	return nil
}

func (s Store) Acquire(ctx context.Context, user int64, operation string, ttl time.Duration) (string, error) {
	return s.AcquireOwner(ctx, user, operation, ID(), ttl)
}

func (s Store) AcquireOwner(ctx context.Context, user int64, operation, owner string, ttl time.Duration) (string, error) {
	now := time.Now().Unix()
	result, err := s.DB.ExecContext(ctx, `INSERT INTO account_leases(user_id,operation,owner,expires) VALUES(?,?,?,?) ON CONFLICT(user_id,operation) DO UPDATE SET owner=excluded.owner,expires=excluded.expires WHERE expires<=? OR owner=excluded.owner`, user, operation, owner, now+int64(ttl.Seconds()), now)
	if err != nil {
		return "", Unavailable()
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", Unavailable()
	}
	if n != 1 {
		return "", Denied(operation+"_busy", "Your account already has an active "+operation+". Wait for it to finish before starting another.", 5)
	}
	return owner, nil
}
func (s Store) Release(user int64, operation, owner string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.DB.ExecContext(ctx, `DELETE FROM account_leases WHERE user_id=? AND operation=? AND owner=?`, user, operation, owner)
	return err
}
func (s Store) Renew(ctx context.Context, user int64, operation, owner string, ttl time.Duration) bool {
	r, e := s.DB.ExecContext(ctx, `UPDATE account_leases SET expires=? WHERE user_id=? AND operation=? AND owner=? AND expires>?`, time.Now().Add(ttl).Unix(), user, operation, owner, time.Now().Unix())
	if e != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n == 1
}

// Period follows the subscription anniversary, clamping month-end dates.
func Period(anchor string, now time.Time) (string, time.Time) {
	now = now.UTC()
	date := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var a time.Time
	for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, e := time.Parse(format, anchor); e == nil {
			a = t.UTC()
			break
		}
	}
	if a.IsZero() {
		return date.Format(time.RFC3339), date.AddDate(0, 1, 0)
	}
	anniversary := func(month time.Time) time.Time {
		last := month.AddDate(0, 1, 0).AddDate(0, 0, -1).Day()
		day := a.Day()
		if day > last {
			day = last
		}
		return time.Date(month.Year(), month.Month(), day, a.Hour(), a.Minute(), a.Second(), 0, time.UTC)
	}
	start := anniversary(date)
	if now.Before(start) {
		date = date.AddDate(0, -1, 0)
		start = anniversary(date)
	}
	return start.Format(time.RFC3339), anniversary(date.AddDate(0, 1, 0))
}

// BillingPeriod is shared by quota enforcement, account display, and renewal.
// Keep a confirmed period even after its end until renewal advances the record.
func BillingPeriod(anchor string, start, end int64, now time.Time) (string, time.Time) {
	if start > 0 && start <= now.Unix() && end > start {
		return time.Unix(start, 0).UTC().Format(time.RFC3339), time.Unix(end, 0).UTC()
	}
	return Period(anchor, now)
}

// A known billing period only changes when the payment record changes. An
// expired period with a delayed webhook must not open a second anniversary quota.
func ResetNotice(reset time.Time) string {
	if reset.After(time.Now()) {
		return " It resets on " + reset.UTC().Format("2 Jan 2006") + "."
	}
	return " It refreshes when your subscription renewal is confirmed."
}

type Reservation struct {
	Store         Store
	ID            string
	User          int64
	Period        string
	Input, Output int64
}

func (s Store) Reserve(ctx context.Context, user int64, period string, reset time.Time, input, maxOutput, inputLimit, outputLimit int64, metadata ...string) (*Reservation, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, Unavailable()
	}
	defer tx.Rollback()
	// A write before reading serializes concurrent reservations in SQLite/libSQL.
	if _, e = tx.ExecContext(ctx, `INSERT INTO command_usage(user_id,period) VALUES(?,?) ON CONFLICT(user_id,period) DO UPDATE SET input_tokens=input_tokens`, user, period); e != nil {
		return nil, Unavailable()
	}
	var usedIn, usedOut int64
	if e = tx.QueryRowContext(ctx, `SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=? AND period=?`, user, period).Scan(&usedIn, &usedOut); e != nil {
		return nil, Unavailable()
	}
	retry := int(time.Until(reset).Seconds())
	if retry < 1 {
		retry = 1
	}
	if input > inputLimit-usedIn {
		return nil, Denied("command_input_quota_exhausted", "Your monthly Command Mode input-token allowance is exhausted or insufficient for this request."+ResetNotice(reset), retry)
	}
	output := min(maxOutput, outputLimit-usedOut)
	if output <= 0 {
		return nil, Denied("command_output_quota_exhausted", "Your monthly Command Mode output-token allowance is exhausted."+ResetNotice(reset), retry)
	}
	reservation := &Reservation{Store: s, ID: ID(), User: user, Period: period, Input: input, Output: output}
	if _, e = tx.ExecContext(ctx, `UPDATE command_usage SET input_tokens=input_tokens+?,output_tokens=output_tokens+? WHERE user_id=? AND period=?`, input, output, user, period); e != nil {
		return nil, Unavailable()
	}
	provider, model := "", ""
	if len(metadata) > 1 {
		provider, model = metadata[0], metadata[1]
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO command_attempts(id,user_id,period,input_tokens,output_tokens,provider,model) VALUES(?,?,?,?,?,?,?)`, reservation.ID, user, period, input, output, provider, model); e != nil {
		return nil, Unavailable()
	}
	if e = tx.Commit(); e != nil {
		return nil, Unavailable()
	}
	s.cacheUsage(user, period)
	return reservation, nil
}

// Missing usage (disconnect/timeout/crash) keeps the conservative reservation.
// Only provider-reported usage can refund it; settlement is idempotent.
func (r *Reservation) Settle(input, output int64) error {
	if input < 0 || output < 0 {
		return errors.New("invalid provider usage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, e := r.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE command_attempts SET settled=1,input_tokens=?,output_tokens=? WHERE id=? AND settled=0`, input, output, r.ID)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil
	}
	if _, e = tx.ExecContext(ctx, `UPDATE command_usage SET input_tokens=MAX(0,input_tokens+?),output_tokens=MAX(0,output_tokens+?) WHERE user_id=? AND period=?`, input-r.Input, output-r.Output, r.User, r.Period); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		r.Store.cacheUsage(r.User, r.Period)
	}
	return e
}
func (s Store) cacheUsage(user int64, period string) {
	c := valkey.GlobalClient
	if c == nil || !c.IsConnected() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var input, output int64
	if s.DB.QueryRowContext(ctx, `SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=? AND period=?`, user, period).Scan(&input, &output) != nil {
		return
	}
	data, _ := json.Marshal(map[string]int64{"input_tokens": input, "output_tokens": output})
	_ = c.Raw().Set(ctx, fmt.Sprintf("quota:command:%d:%s", user, period), data, 35*24*time.Hour).Err()
}

// ResetRates preserves monthly usage and active work. Used by the existing admin
// rate-limit reset action; clearing a rate counter must never refill token quotas.
func (s Store) ResetRates(ctx context.Context, user int64) error {
	query := "DELETE FROM account_rate_windows"
	var args []any
	if user > 0 {
		query += " WHERE user_id=?"
		args = append(args, user)
	}
	if _, e := s.DB.ExecContext(ctx, query, args...); e != nil {
		return e
	}
	c := valkey.GlobalClient
	if c == nil || !c.IsConnected() {
		return nil
	}
	pattern := "ratelimit:account:*"
	if user > 0 {
		pattern = fmt.Sprintf("ratelimit:account:%d:*", user)
	}
	iter := c.Raw().Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		if e := c.Raw().Del(ctx, iter.Val()).Err(); e != nil {
			return e
		}
	}
	return iter.Err()
}
