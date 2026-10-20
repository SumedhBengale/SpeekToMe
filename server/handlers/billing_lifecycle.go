package handlers

import (
	"fmt"
	"os"
	"speektome-server/auth"
	"strings"
	"time"
)

func (a *App) lockBilling(user int64) (func(), error) {
	owner := auth.GenerateNonce()
	r, e := a.DB.GetDB().Exec(`INSERT INTO billing_operations(user_id,owner,expires) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET owner=excluded.owner,expires=excluded.expires WHERE billing_operations.expires<?`, user, owner, time.Now().Add(5*time.Minute).Unix(), time.Now().Unix())
	if e != nil {
		return nil, e
	}
	n, e := r.RowsAffected()
	if e != nil || n != 1 {
		return nil, fmt.Errorf("billing operation in progress")
	}
	return func() {
		_, _ = a.DB.GetDB().Exec("DELETE FROM billing_operations WHERE user_id=? AND owner=?", user, owner)
	}, nil
}
func (a *App) cancelProviderSubscription(id string) error {
	if id == "" {
		return nil
	}
	key, secret := os.Getenv("RAZORPAY_KEY_ID"), os.Getenv("RAZORPAY_KEY_SECRET")
	if key == "" || secret == "" {
		return fmt.Errorf("billing unavailable")
	}
	client := newRazorpayClient(key, secret)
	data, e := client.Subscription.Fetch(id, nil, nil)
	var sub razorpaySubscription
	if e != nil || decodeMap(data, &sub) != nil || sub.ID != id {
		return fmt.Errorf("subscription verification unavailable")
	}
	if sub.Status == "cancelled" || sub.Status == "completed" || sub.Status == "expired" {
		return nil
	}
	_, e = client.Subscription.Cancel(id, map[string]interface{}{"cancel_at_cycle_end": 0}, nil)
	if e != nil {
		return fmt.Errorf("provider cancellation unavailable")
	}
	return nil
}

func verifyWebhookCharge(id, planID string, amount int64, currency, status string) error {
	configured := os.Getenv("RAZORPAY_PLAN_ID")
	key, secret := os.Getenv("RAZORPAY_KEY_ID"), os.Getenv("RAZORPAY_KEY_SECRET")
	if configured == "" || key == "" || secret == "" {
		return fmt.Errorf("billing unavailable")
	}
	client := newRazorpayClient(key, secret)
	if planID == "" {
		data, e := client.Subscription.Fetch(id, nil, nil)
		var sub razorpaySubscription
		if e != nil || decodeMap(data, &sub) != nil || sub.ID != id {
			return fmt.Errorf("subscription unavailable")
		}
		planID = sub.PlanID
	}
	if planID != configured {
		return fmt.Errorf("wrong plan")
	}
	data, e := client.Plan.Fetch(configured, nil, nil)
	var plan razorpayPlan
	if e != nil || decodeMap(data, &plan) != nil || plan.Item.Amount <= 0 || amount != plan.Item.Amount || !strings.EqualFold(currency, plan.Item.Currency) || (status != "captured" && status != "paid") {
		return fmt.Errorf("unverified payment")
	}
	return nil
}
