package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	razorpay "github.com/razorpay/razorpay-go"
	"github.com/razorpay/razorpay-go/utils"
)

var razorpayAPIBaseURL = "https://api.razorpay.com"

type razorpaySubscription struct {
	ID           string   `json:"id"`
	PlanID       string   `json:"plan_id"`
	Status       string   `json:"status"`
	PaidCount    int64    `json:"paid_count"`
	ChargeAt     int64    `json:"charge_at"`
	StartAt      int64    `json:"start_at"`
	CurrentStart int64    `json:"current_start"`
	CurrentEnd   int64    `json:"current_end"`
	Notes        NotesMap `json:"notes"`
}

type razorpayPayment struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id"`
	InvoiceID      string `json:"invoice_id"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	Captured       bool   `json:"captured"`
}

type razorpayPlan struct {
	ID   string `json:"id"`
	Item struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	} `json:"item"`
}

func newRazorpayClient(keyID, keySecret string) *razorpay.Client {
	client := razorpay.NewClient(keyID, keySecret)
	if razorpayAPIBaseURL != "" && razorpayAPIBaseURL != "https://api.razorpay.com" {
		client.Request.BaseURL = strings.TrimRight(razorpayAPIBaseURL, "/")
	}
	return client
}

func decodeMap(src map[string]interface{}, target interface{}) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func (a *App) HandleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Unauthorized: missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := a.DB.GetUserBySession(token)
	if err != nil {
		http.Error(w, "Unauthorized: invalid or expired session", http.StatusUnauthorized)
		return
	}

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")
	razorpayPlanID := os.Getenv("RAZORPAY_PLAN_ID")

	if razorpayKeyID == "" || razorpayKeySecret == "" || razorpayKeyID == "your_razorpay_key_id_here" || razorpayPlanID == "" {
		log.Printf("[PAYMENT ERROR] Billing service not configured: KEY_ID='%s', PLAN_ID='%s'", razorpayKeyID, razorpayPlanID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Billing service not configured"})
		return
	}

	client := newRazorpayClient(razorpayKeyID, razorpayKeySecret)

	planData, err := client.Plan.Fetch(razorpayPlanID, nil, nil)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Failed to verify configured plan %s: %v", razorpayPlanID, err)
		http.Error(w, "Could not verify billing plan", http.StatusBadGateway)
		return
	}
	var configuredPlan razorpayPlan
	if err := decodeMap(planData, &configuredPlan); err != nil || configuredPlan.Item.Amount <= 0 || configuredPlan.Item.Currency == "" {
		http.Error(w, "Configured Razorpay plan has invalid amount or currency", http.StatusServiceUnavailable)
		return
	}
	log.Printf("[RAZORPAY PLAN] plan_id=%s amount=%d currency=%s", razorpayPlanID, configuredPlan.Item.Amount, configuredPlan.Item.Currency)

	subReqParams := map[string]interface{}{
		"plan_id":     razorpayPlanID,
		"total_count": 60,
		"notes": map[string]string{
			"user_id": strconv.FormatInt(user.ID, 10),
		},
	}

	subData, err := client.Subscription.Create(subReqParams, nil)
	if err != nil {
		log.Printf("[RAZORPAY ERROR] Subscription initialization failed for PlanID '%s': %v", razorpayPlanID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "Razorpay subscription initialization failed",
			"details": err.Error(),
			"plan_id": razorpayPlanID,
		})
		return
	}

	var rzResp struct {
		ID string `json:"id"`
	}
	if err := decodeMap(subData, &rzResp); err != nil || rzResp.ID == "" {
		http.Error(w, "Razorpay returned an empty subscription ID", http.StatusBadGateway)
		return
	}

	if err := a.DB.SetPendingSubscription(user.ID, rzResp.ID); err != nil {
		log.Printf("[DB ERROR] Failed to save pending subscription (%d): %v", user.ID, err)
		http.Error(w, "Failed to save subscription", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"subscription_id": rzResp.ID,
		"razorpay_key_id": razorpayKeyID,
		"amount":          configuredPlan.Item.Amount,
		"currency":        configuredPlan.Item.Currency,
		"mock_mode":       false,
	})
}

func (a *App) HandleVerifySubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Unauthorized: missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := a.DB.GetUserBySession(token)
	if err != nil {
		http.Error(w, "Unauthorized: invalid or expired session", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		PaymentID      string `json:"razorpay_payment_id"`
		SubscriptionID string `json:"razorpay_subscription_id"`
		Signature      string `json:"razorpay_signature"`
	}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")
	razorpayPlanID := os.Getenv("RAZORPAY_PLAN_ID")

	if razorpayKeyID == "" || razorpayKeySecret == "" || razorpayKeyID == "your_razorpay_key_id_here" || razorpayPlanID == "" {
		http.Error(w, "Billing service not configured", http.StatusServiceUnavailable)
		return
	}

	if req.PaymentID == "" || req.SubscriptionID == "" || req.Signature == "" {
		http.Error(w, "Missing Razorpay payment verification fields", http.StatusBadRequest)
		return
	}

	verificationParams := map[string]interface{}{
		"razorpay_payment_id":      req.PaymentID,
		"razorpay_subscription_id": req.SubscriptionID,
	}

	if !utils.VerifySubscriptionSignature(verificationParams, req.Signature, razorpayKeySecret) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid Razorpay signature. Verification failed.",
		})
		return
	}

	if user.RazorpaySubscriptionID == "" || user.RazorpaySubscriptionID != req.SubscriptionID {
		http.Error(w, "Subscription does not belong to the current user", http.StatusForbidden)
		return
	}

	client := newRazorpayClient(razorpayKeyID, razorpayKeySecret)

	subData, err := client.Subscription.Fetch(req.SubscriptionID, nil, nil)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Failed to fetch subscription %s: %v", req.SubscriptionID, err)
		http.Error(w, "Could not verify Razorpay subscription", http.StatusBadGateway)
		return
	}
	var subscription razorpaySubscription
	if err := decodeMap(subData, &subscription); err != nil || subscription.ID != req.SubscriptionID || subscription.PlanID != razorpayPlanID {
		http.Error(w, "Razorpay subscription does not match the configured plan", http.StatusBadRequest)
		return
	}

	payData, err := client.Payment.Fetch(req.PaymentID, nil, nil)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Failed to fetch payment %s: %v", req.PaymentID, err)
		http.Error(w, "Could not verify Razorpay payment", http.StatusBadGateway)
		return
	}
	var payment razorpayPayment
	if err := decodeMap(payData, &payment); err != nil || payment.ID != req.PaymentID || (payment.SubscriptionID != "" && payment.SubscriptionID != req.SubscriptionID) || (!payment.Captured && payment.Status != "captured") {
		http.Error(w, "Razorpay payment is not captured for this subscription", http.StatusBadRequest)
		return
	}

	planData, err := client.Plan.Fetch(razorpayPlanID, nil, nil)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Failed to fetch plan %s: %v", razorpayPlanID, err)
		http.Error(w, "Could not verify Razorpay plan", http.StatusBadGateway)
		return
	}
	var plan razorpayPlan
	if err := decodeMap(planData, &plan); err != nil {
		http.Error(w, "Could not verify Razorpay plan details", http.StatusBadGateway)
		return
	}
	if plan.Item.Amount > 0 && payment.Amount != plan.Item.Amount {
		http.Error(w, "Razorpay payment amount does not match the subscription plan", http.StatusBadRequest)
		return
	}
	if plan.Item.Currency != "" && !strings.EqualFold(payment.Currency, plan.Item.Currency) {
		http.Error(w, "Razorpay payment currency does not match the subscription plan", http.StatusBadRequest)
		return
	}

	dedupeKey := "payment:" + payment.ID
	if payment.ID == "" && payment.InvoiceID != "" {
		dedupeKey = "invoice:" + payment.InvoiceID
	}
	_, err = a.DB.ApplySuccessfulSubscriptionPayment(user.ID, req.SubscriptionID, payment.ID, payment.InvoiceID, dedupeKey, "", "checkout.payment", payment.Amount, payment.Currency, payment.Status, subscription.CurrentStart, subscription.CurrentEnd, "")
	if err != nil {
		log.Printf("[DB ERROR] Failed to record payment (%d): %v", user.ID, err)
		http.Error(w, "Failed to record payment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
	})
}

func (a *App) HandleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Unauthorized: missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := a.DB.GetUserBySession(token)
	if err != nil {
		http.Error(w, "Unauthorized: invalid or expired session", http.StatusUnauthorized)
		return
	}

	if user.PlanTier != "pro" || user.PlanActive != 1 {
		http.Error(w, "Bad request: no active paid subscription found", http.StatusBadRequest)
		return
	}

	subID := user.RazorpaySubscriptionID
	if subID == "" {
		err = a.DB.UpdateUserPlan(user.ID, "free", 0, "")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to update user plan: %v", err), http.StatusInternalServerError)
			return
		}
		inactive := 0
		if err := a.DB.UpdateSubscriptionState(user.ID, "", "cancelled", 0, 0, &inactive); err != nil {
			log.Printf("[DB ERROR] Failed to record local cancellation (%d): %v", user.ID, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Subscription cancelled locally"})
		return
	}

	razorpayKeyID := os.Getenv("RAZORPAY_KEY_ID")
	razorpayKeySecret := os.Getenv("RAZORPAY_KEY_SECRET")

	if razorpayKeyID == "" || razorpayKeySecret == "" || razorpayKeyID == "your_razorpay_key_id_here" {
		http.Error(w, "Billing service not configured", http.StatusServiceUnavailable)
		return
	}

	client := newRazorpayClient(razorpayKeyID, razorpayKeySecret)
	cancelParams := map[string]interface{}{
		"cancel_at_cycle_end": 0,
	}

	_, err = client.Subscription.Cancel(subID, cancelParams, nil)
	if err != nil {
		log.Printf("Razorpay Cancel API returned error: %v. Proceeding with local downgrade to prevent lockout.", err)
	} else {
		log.Printf("Razorpay Subscription %s cancelled successfully", subID)
	}

	err = a.DB.UpdateUserPlan(user.ID, "free", 0, "")
	if err != nil {
		log.Printf("[DB ERROR] Failed to update user plan (%d): %v", user.ID, err)
		http.Error(w, "Failed to update user plan", http.StatusInternalServerError)
		return
	}
	inactive := 0
	if err := a.DB.UpdateSubscriptionState(user.ID, "", "cancelled", 0, 0, &inactive); err != nil {
		log.Printf("[DB ERROR] Failed to record cancellation state (%d): %v", user.ID, err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}

type NotesMap map[string]string

func (n *NotesMap) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "[]" || trimmed == "null" || trimmed == "" {
		*n = make(NotesMap)
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		*n = make(NotesMap)
		return nil
	}
	*n = m
	return nil
}

func (a *App) HandleRazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 5*1024*1024)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	rawPayload := string(bodyBytes)

	razorpayWebhookSecret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")
	if razorpayWebhookSecret == "" {
		log.Printf("[RAZORPAY WEBHOOK ERROR] Webhook secret not configured")
		http.Error(w, "Internal Server Error: Webhook configuration missing", http.StatusInternalServerError)
		return
	}

	eventID := r.Header.Get("X-Razorpay-Event-Id")
	signature := r.Header.Get("X-Razorpay-Signature")

	if !utils.VerifyWebhookSignature(rawPayload, signature, razorpayWebhookSecret) {
		log.Printf("[RAZORPAY WEBHOOK] Signature verification FAILED for incoming payload")
		_ = a.DB.InsertWebhookLog(eventID, "unknown", "", nil, "signature_failed", "Invalid HMAC-SHA256 signature", rawPayload)
		http.Error(w, "Unauthorized: Invalid webhook signature", http.StatusBadRequest)
		return
	}

	type WebhookEvent struct {
		ID      string `json:"id"`
		Event   string `json:"event"`
		Payload struct {
			Subscription struct {
				Entity struct {
					ID           string   `json:"id"`
					PlanID       string   `json:"plan_id"`
					Status       string   `json:"status"`
					Notes        NotesMap `json:"notes"`
					CurrentStart int64    `json:"current_start"`
					CurrentEnd   int64    `json:"current_end"`
				} `json:"entity"`
			} `json:"subscription"`
			Payment struct {
				Entity struct {
					ID             string   `json:"id"`
					SubscriptionID string   `json:"subscription_id"`
					InvoiceID      string   `json:"invoice_id"`
					Amount         int64    `json:"amount"`
					Currency       string   `json:"currency"`
					Status         string   `json:"status"`
					Captured       bool     `json:"captured"`
					Notes          NotesMap `json:"notes"`
				} `json:"entity"`
			} `json:"payment"`
			Invoice struct {
				Entity struct {
					ID             string `json:"id"`
					SubscriptionID string `json:"subscription_id"`
					PaymentID      string `json:"payment_id"`
					Amount         int64  `json:"amount"`
					Currency       string `json:"currency"`
					Status         string `json:"status"`
				} `json:"entity"`
			} `json:"invoice"`
		} `json:"payload"`
	}

	var event WebhookEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		_ = a.DB.InsertWebhookLog(eventID, "unknown", "", nil, "parse_error", "Failed to parse JSON payload", rawPayload)
		http.Error(w, "Bad request: invalid event JSON payload", http.StatusBadRequest)
		return
	}

	if eventID == "" {
		eventID = event.ID
	}
	if eventID == "" {
		digest := sha256.Sum256(bodyBytes)
		eventID = "payload:" + hex.EncodeToString(digest[:])
	}

	if processed, _ := a.DB.IsWebhookEventProcessed(eventID); processed {
		log.Printf("[RAZORPAY WEBHOOK IDEMPOTENT] Event ID %s already processed successfully. Skipping.", eventID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"already_processed"}`))
		return
	}

	subID := event.Payload.Subscription.Entity.ID
	if subID == "" {
		subID = event.Payload.Payment.Entity.SubscriptionID
	}
	if subID == "" {
		subID = event.Payload.Invoice.Entity.SubscriptionID
	}

	status := event.Payload.Subscription.Entity.Status
	if status == "" {
		status = event.Payload.Payment.Entity.Status
	}
	if status == "" {
		status = event.Payload.Invoice.Entity.Status
	}

	paymentID := event.Payload.Payment.Entity.ID
	if paymentID == "" {
		paymentID = event.Payload.Invoice.Entity.PaymentID
	}
	invoiceID := event.Payload.Payment.Entity.InvoiceID
	if invoiceID == "" {
		invoiceID = event.Payload.Invoice.Entity.ID
	}
	amount := event.Payload.Payment.Entity.Amount
	if amount == 0 {
		amount = event.Payload.Invoice.Entity.Amount
	}
	currency := event.Payload.Payment.Entity.Currency
	if currency == "" {
		currency = event.Payload.Invoice.Entity.Currency
	}

	userIDStr := event.Payload.Subscription.Entity.Notes["user_id"]
	if userIDStr == "" {
		userIDStr = event.Payload.Payment.Entity.Notes["user_id"]
	}

	if subID == "" {
		log.Printf("[RAZORPAY WEBHOOK WARNING] Event %s missing subscription ID in payload", event.Event)
		_ = a.DB.InsertWebhookLogDetailed(eventID, event.Event, "", paymentID, invoiceID, nil, status, "missing_sub: No subscription ID in payload", rawPayload)
		http.Error(w, "Bad request: missing subscription details", http.StatusBadRequest)
		return
	}

	var userID int64
	mappedUserID, lookupErr := a.DB.GetUserBySubscriptionID(subID)
	if lookupErr == nil {
		userID = mappedUserID
	}
	if userIDStr != "" {
		if noteUserID, parseErr := strconv.ParseInt(userIDStr, 10, 64); parseErr == nil {
			if userID != 0 && userID != noteUserID {
				_ = a.DB.InsertWebhookLogDetailed(eventID, event.Event, subID, paymentID, invoiceID, nil, status, "user_mismatch", rawPayload)
				http.Error(w, "Subscription user mismatch", http.StatusForbidden)
				return
			}
			if userID == 0 {
				if a.DB.UserExists(noteUserID) {
					userID = noteUserID
				} else {
					log.Printf("[RAZORPAY WEBHOOK WARNING] notes.user_id %d does not exist in users table", noteUserID)
				}
			}
		}
	}
	if userID == 0 {
		log.Printf("[RAZORPAY WEBHOOK WARNING] Could not resolve verified user for subscription %s: %v", subID, lookupErr)
		_ = a.DB.InsertWebhookLogDetailed(eventID, event.Event, subID, paymentID, invoiceID, nil, status, "user_not_found", rawPayload)
		http.Error(w, "User not found for subscription ID", http.StatusNotFound)
		return
	}
	userIDPtr := &userID

	log.Printf("[RAZORPAY WEBHOOK EVENT] Processing %s for user ID %d (status: %s, sub: %s)", event.Event, userID, status, subID)

	successfulPaymentEvent := event.Event == "subscription.charged" || event.Event == "invoice.paid" || event.Event == "payment.captured"
	switch {
	case successfulPaymentEvent:
		dedupeKey := "event:" + eventID
		if paymentID != "" {
			dedupeKey = "payment:" + paymentID
		} else if invoiceID != "" {
			dedupeKey = "invoice:" + invoiceID
		}
		_, err = a.DB.ApplySuccessfulSubscriptionPayment(userID, subID, paymentID, invoiceID, dedupeKey, eventID, event.Event, amount, currency, status, event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, rawPayload)
	case event.Event == "subscription.pending" || event.Event == "payment.failed":
		log.Printf("[RAZORPAY WEBHOOK] Payment pending/failed for user ID %d (event: %s)", userID, event.Event)
		err = a.DB.UpdateSubscriptionState(userID, subID, "pending", event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, nil)
	case event.Event == "subscription.halted" || event.Event == "subscription.paused":
		log.Printf("[RAZORPAY WEBHOOK] Subscription restricted for user ID %d (event: %s)", userID, event.Event)
		inactive := 0
		err = a.DB.UpdateSubscriptionState(userID, subID, status, event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, &inactive)
	case event.Event == "subscription.cancelled" || event.Event == "subscription.completed":
		log.Printf("[RAZORPAY WEBHOOK] Subscription ended for user ID %d (event: %s)", userID, event.Event)
		inactive := 0
		err = a.DB.UpdateSubscriptionState(userID, "", event.Event, 0, 0, &inactive)
	case event.Event == "subscription.updated":
		if status == "cancelled" || status == "completed" {
			inactive := 0
			err = a.DB.UpdateSubscriptionState(userID, "", status, 0, 0, &inactive)
		} else if status == "halted" || status == "paused" {
			inactive := 0
			err = a.DB.UpdateSubscriptionState(userID, subID, status, event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, &inactive)
		} else {
			err = a.DB.UpdateSubscriptionState(userID, subID, status, event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, nil)
		}
	case event.Event == "subscription.authenticated" || event.Event == "subscription.activated" || event.Event == "subscription.resumed" || event.Event == "payment.authorized":
		log.Printf("[RAZORPAY WEBHOOK] Recorded non-payment subscription event for user ID %d: %s", userID, event.Event)
		err = a.DB.UpdateSubscriptionState(userID, subID, status, event.Payload.Subscription.Entity.CurrentStart, event.Payload.Subscription.Entity.CurrentEnd, nil)
	default:
		log.Printf("[RAZORPAY WEBHOOK WARNING] Unhandled event type: %s", event.Event)
	}

	if err != nil {
		log.Printf("[RAZORPAY WEBHOOK ERROR] Failed to update user plan: %v", err)
		_ = a.DB.InsertWebhookLogDetailed(eventID, event.Event, subID, paymentID, invoiceID, userIDPtr, status, "db_error: "+err.Error(), rawPayload)
		http.Error(w, "Failed to update user plan", http.StatusInternalServerError)
		return
	}

	log.Printf("[RAZORPAY WEBHOOK SUCCESS] %s applied for user ID %d", event.Event, userID)
	_ = a.DB.InsertWebhookLogDetailed(eventID, event.Event, subID, paymentID, invoiceID, userIDPtr, status, "success", rawPayload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
