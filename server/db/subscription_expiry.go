package db

import (
	"context"
	"time"
)

const SubscriptionGracePeriod = 48 * time.Hour

func subscriptionExpired(tier string, periodEnd int64, now time.Time) bool {
	return tier == "pro" && periodEnd > 0 && periodEnd <= now.Add(-SubscriptionGracePeriod).Unix()
}

// ExpireSubscriptions is idempotent. Only an expired Pro account is reset;
// a Free account's subsequent usage is left for the existing monthly reset.
func (d *Database) ExpireSubscriptions(ctx context.Context, now time.Time) (int, error) {
	return d.expireSubscriptions(ctx, now, 0)
}

func (d *Database) expireSubscriptions(ctx context.Context, now time.Time, userID int64) (int, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `UPDATE users SET plan_tier='free',plan_active=0,monthly_usage_seconds=0.0
 WHERE plan_tier='pro' AND razorpay_current_end>0 AND razorpay_current_end<=?
 AND (?=0 OR id=?) RETURNING id`, now.Add(-SubscriptionGracePeriod).Unix(), userID, userID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		invalidateCachedUserSessions(id)
	}
	return len(ids), nil
}
