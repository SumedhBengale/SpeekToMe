package db

import (
	"context"
	"errors"
	"speektome-server/valkey"
	"time"
)

const FreeVocabularyLimit = 20
const ProVocabularyLimit = 1000

var ErrVocabularyLimit = errors.New("vocabulary limit reached")

func (u *UserProfile) VocabularyLimit() int {
	if u.PlanTier == "pro" && u.PlanActive == 1 {
		return ProVocabularyLimit
	}
	return FreeVocabularyLimit
}

// GetVocabularyTerms is the full management view. Only this bounded snapshot is
// sent to transcription. A downgrade never deletes the saved dictionary.
func (d *Database) GetActiveVocabularyTerms(userID int64) ([]VocabularyTerm, error) {
	var tier string
	var active int
	var end int64
	if err := d.db.QueryRow(`SELECT plan_tier,plan_active,COALESCE(razorpay_current_end,0) FROM users WHERE id=?`, userID).Scan(&tier, &active, &end); err != nil {
		return nil, err
	}
	if subscriptionExpired(tier, end, time.Now()) {
		active = 0
	}
	limit := (&UserProfile{PlanTier: tier, PlanActive: active}).VocabularyLimit()
	terms, err := d.GetVocabularyTerms(userID)
	if err != nil {
		return nil, err
	}
	if len(terms) > limit {
		terms = terms[:limit]
	}
	return terms, nil
}

// A single conditional write enforces the current plan and count atomically,
// including competing requests from different API processes. Re-adding an
// existing term remains idempotent even after a downgrade.
func (d *Database) AddVocabularyTerm(userID int64, canonical, normalized string) (int64, error) {
	res, err := d.db.Exec(`INSERT INTO vocabulary(user_id,canonical_term,normalized_term)
 SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM vocabulary WHERE user_id=? AND canonical_term=?)
 OR (SELECT COUNT(*) FROM vocabulary WHERE user_id=?) <
 (SELECT CASE WHEN plan_tier='pro' AND plan_active=1 AND (COALESCE(razorpay_current_end,0)<=0 OR razorpay_current_end>?) THEN ? ELSE ? END FROM users WHERE id=?)
 ON CONFLICT(user_id,canonical_term) DO UPDATE SET normalized_term=excluded.normalized_term,updated_at=CURRENT_TIMESTAMP`,
		userID, canonical, normalized, userID, canonical, userID, time.Now().Add(-SubscriptionGracePeriod).Unix(), ProVocabularyLimit, FreeVocabularyLimit, userID)
	if err != nil {
		return 0, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, ErrVocabularyLimit
	}
	if cache := valkey.GlobalClient; cache != nil && cache.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = cache.InvalidateCachedVocabulary(ctx, userID)
	}
	return res.LastInsertId()
}
