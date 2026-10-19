package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"speektome-server/accountlimits"
	"speektome-server/config"
)

func configuredLimit(key string, fallback int64) int64 {
	if n, e := strconv.ParseInt(config.ConfigManager.GetRawAll()[key], 10, 64); e == nil && n > 0 {
		return n
	}
	return fallback
}
func (a *App) limits() accountlimits.Store { return accountlimits.Store{DB: a.DB.GetDB()} }
func (a *App) accountRate(w http.ResponseWriter, r *http.Request, user int64, operation string, rpm int) bool {
	if e := a.limits().Rate(r.Context(), user, operation, rpm); e != nil {
		accountlimits.Write(w, e)
		return false
	}
	return true
}

type commandBudget struct {
	store       accountlimits.Store
	user        int64
	period      string
	reset       time.Time
	reservation *accountlimits.Reservation
}

func (b *commandBudget) Reserve(ctx context.Context, system, user, provider, model string) (int64, error) {
	// UTF-8 byte count plus chat framing is a conservative reservation for the
	// configured tokenizers, not a word-to-token conversion. Actual usage refunds it.
	input := int64(len(system) + len(user) + 1024)
	r, e := b.store.Reserve(ctx, b.user, b.period, b.reset, input, configuredLimit("command_max_output_tokens", 32768), configuredLimit("quota_command_input_monthly_tokens", 5000000), configuredLimit("quota_command_output_monthly_tokens", 1000000), provider, model)
	if e != nil {
		return 0, e
	}
	b.reservation = r
	return r.Output, nil
}
func (b *commandBudget) Usage(input, output int64) {
	if b.reservation != nil {
		if e := b.reservation.Settle(input, output); e != nil {
			log.Printf("[COMMAND QUOTA] Failed to reconcile account %d: %v", b.user, e)
		}
	}
}
func limitStatus(err error) int {
	var f *accountlimits.Failure
	if errors.As(err, &f) {
		return f.Status
	}
	return http.StatusBadGateway
}

func (b *commandBudget) OutputLimitError() error {
	if b.reservation != nil && b.reservation.Output < configuredLimit("command_max_output_tokens", 32768) {
		return accountlimits.Denied("command_output_quota_exhausted", "Your monthly Command Mode output-token allowance is exhausted."+accountlimits.ResetNotice(b.reset), max(1, int(time.Until(b.reset).Seconds())))
	}
	return accountlimits.Denied("command_request_output_limit", "Command Mode reached its per-request output limit. Try transforming a shorter selection.", 0)
}

// commandUsage reads durable counters, including reservations for in-flight work.
// Never present unavailable accounting as an unused allowance.
func (a *App) commandUsage(ctx context.Context, userID int64) (map[string]interface{}, error) {
	var anchor, tier string
	var active int
	var start, end int64
	if err := a.DB.GetDB().QueryRowContext(ctx, `SELECT COALESCE(plan_activated_at,created_at,''),COALESCE(plan_tier,'free'),COALESCE(plan_active,0),COALESCE(razorpay_current_start,0),COALESCE(razorpay_current_end,0) FROM users WHERE id=?`, userID).Scan(&anchor, &tier, &active, &start, &end); err != nil {
		return nil, err
	}
	now := time.Now()
	period, reset := accountlimits.BillingPeriod(anchor, start, end, now)
	var input, output int64
	err := a.DB.GetDB().QueryRowContext(ctx, `SELECT input_tokens,output_tokens FROM command_usage WHERE user_id=? AND period=?`, userID, period).Scan(&input, &output)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	inputLimit := configuredLimit("quota_command_input_monthly_tokens", 5000000)
	outputLimit := configuredLimit("quota_command_output_monthly_tokens", 1000000)
	inputRemaining, outputRemaining := max(0, inputLimit-input), max(0, outputLimit-output)
	percent := math.Min(float64(inputRemaining)/float64(inputLimit), float64(outputRemaining)/float64(outputLimit)) * 100
	return map[string]interface{}{
		"enabled":    tier == "pro" && active == 1,
		"input_used": input, "input_limit": inputLimit, "input_remaining": inputRemaining,
		"output_used": output, "output_limit": outputLimit, "output_remaining": outputRemaining,
		"remaining_percent": percent, "period_start": period, "resets_at": reset.Format(time.RFC3339),
		"renewal_pending": !reset.After(now),
	}, nil
}
