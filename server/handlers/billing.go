package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

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

	type RazorpaySubReq struct {
		PlanID         string            `json:"plan_id"`
		TotalCount     int               `json:"total_count"`
		Quantity       int               `json:"quantity"`
		CustomerNotify int               `json:"customer_notify"`
		StartAt        int64             `json:"start_at"`
		Notes          map[string]string `json:"notes"`
	}

	subReqBody := RazorpaySubReq{
		PlanID:         razorpayPlanID,
		TotalCount:     60,
		Quantity:       1,
		CustomerNotify: 1,
		StartAt:        time.Now().Add(5 * time.Second).Unix(),
		Notes: map[string]string{
			"user_id": strconv.FormatInt(user.ID, 10),
		},
	}

	jsonBytes, err := json.Marshal(subReqBody)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to encode subscription request"})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", "https://api.razorpay.com/v1/subscriptions", bytes.NewBuffer(jsonBytes))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create Razorpay HTTP request"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(razorpayKeyID, razorpayKeySecret)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Razorpay connection failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Payment provider connection failed"})
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read Razorpay response"})
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[RAZORPAY ERROR] Status %d for PlanID '%s': %s", resp.StatusCode, razorpayPlanID, string(bodyBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":           "Razorpay subscription initialization failed",
			"razorpay_status": resp.StatusCode,
			"details":         string(bodyBytes),
			"plan_id":         razorpayPlanID,
		})
		return
	}

	var rzResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyBytes, &rzResp); err != nil {
		http.Error(w, "Failed to parse Razorpay subscription details", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"subscription_id": rzResp.ID,
		"razorpay_key_id": razorpayKeyID,
		"amount":          200,
		"currency":        "USD",
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

	if razorpayKeyID == "" || razorpayKeySecret == "" || razorpayKeyID == "your_razorpay_key_id_here" {
		http.Error(w, "Billing service not configured", http.StatusServiceUnavailable)
		return
	}

	message := req.PaymentID + "|" + req.SubscriptionID

	mac := hmac.New(sha256.New, []byte(razorpayKeySecret))
	mac.Write([]byte(message))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(req.Signature)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid Razorpay signature. Verification failed.",
		})
		return
	}

	err = a.DB.UpdateUserPlan(user.ID, "pro", 1, req.SubscriptionID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to update user plan (%d): %v", user.ID, err)
		http.Error(w, "Failed to update user plan", http.StatusInternalServerError)
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

	type RazorpayCancelReq struct {
		CancelAtCycleEnd int `json:"cancel_at_cycle_end"`
	}
	cancelReqBody := RazorpayCancelReq{CancelAtCycleEnd: 0}
	jsonBytes, err := json.Marshal(cancelReqBody)
	if err != nil {
		http.Error(w, "Failed to encode cancel request", http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := fmt.Sprintf("https://api.razorpay.com/v1/subscriptions/%s/cancel", subID)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		http.Error(w, "Failed to create Razorpay HTTP request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(razorpayKeyID, razorpayKeySecret)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PAYMENT ERROR] Razorpay cancel connection failed: %v", err)
		http.Error(w, "Payment provider connection failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Razorpay Subscription %s cancelled successfully", subID)
	} else {
		log.Printf("Razorpay Cancel API returned status %d: %s. Proceeding with local downgrade to prevent lockout.", resp.StatusCode, string(bodyBytes))
	}

	err = a.DB.UpdateUserPlan(user.ID, "free", 0, "")
	if err != nil {
		log.Printf("[DB ERROR] Failed to update user plan (%d): %v", user.ID, err)
		http.Error(w, "Failed to update user plan", http.StatusInternalServerError)
		return
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
	mac := hmac.New(sha256.New, []byte(razorpayWebhookSecret))
	mac.Write(bodyBytes)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expectedSignature), []byte(signature)) != 1 {
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
					ID     string   `json:"id"`
					Status string   `json:"status"`
					Notes  NotesMap `json:"notes"`
				} `json:"entity"`
			} `json:"subscription"`
			Payment struct {
				Entity struct {
					ID             string   `json:"id"`
					SubscriptionID string   `json:"subscription_id"`
					Status         string   `json:"status"`
					Notes          NotesMap `json:"notes"`
				} `json:"entity"`
			} `json:"payment"`
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

	status := event.Payload.Subscription.Entity.Status
	if status == "" {
		status = event.Payload.Payment.Entity.Status
	}

	userIDStr := event.Payload.Subscription.Entity.Notes["user_id"]
	if userIDStr == "" {
		userIDStr = event.Payload.Payment.Entity.Notes["user_id"]
	}

	if subID == "" {
		log.Printf("[RAZORPAY WEBHOOK WARNING] Event %s missing subscription ID in payload", event.Event)
		_ = a.DB.InsertWebhookLog(eventID, event.Event, "", nil, "missing_sub", "No subscription ID in payload", rawPayload)
		http.Error(w, "Bad request: missing subscription details", http.StatusBadRequest)
		return
	}

	var userID int64
	if userIDStr != "" {
		if id, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
			userID = id
		}
	}
	if userID == 0 {
		userID, err = a.DB.GetUserBySubscriptionID(subID)
		if err != nil {
			log.Printf("[RAZORPAY WEBHOOK WARNING] Could not resolve user for subscription %s: %v", subID, err)
			_ = a.DB.InsertWebhookLog(eventID, event.Event, subID, nil, status, "user_not_found: "+err.Error(), rawPayload)
			http.Error(w, "User not found for subscription ID", http.StatusNotFound)
			return
		}
	}
	userIDPtr := &userID

	log.Printf("[RAZORPAY WEBHOOK EVENT] Processing %s for user ID %d (status: %s, sub: %s)", event.Event, userID, status, subID)

	switch event.Event {
	case "subscription.authenticated", "subscription.activated", "subscription.charged", "payment.captured", "payment.authorized", "subscription.resumed":
		log.Printf("[RAZORPAY WEBHOOK] Subscription active/charged for user ID %d (event: %s)", userID, event.Event)
		err = a.DB.UpdateUserPlan(userID, "pro", 1, subID)

	case "subscription.paused", "subscription.halted", "payment.failed":
		log.Printf("[RAZORPAY WEBHOOK] Subscription paused/halted for user ID %d (event: %s)", userID, event.Event)
		err = a.DB.UpdateUserPlan(userID, "pro", 0, subID)

	case "subscription.cancelled", "subscription.completed":
		log.Printf("[RAZORPAY WEBHOOK] Subscription cancelled/completed for user ID %d (event: %s)", userID, event.Event)
		err = a.DB.UpdateUserPlan(userID, "free", 0, "")

	case "subscription.updated":
		switch status {
		case "cancelled", "completed":
			err = a.DB.UpdateUserPlan(userID, "free", 0, "")
		case "halted", "paused":
			err = a.DB.UpdateUserPlan(userID, "pro", 0, subID)
		case "active", "authenticated":
			err = a.DB.UpdateUserPlan(userID, "pro", 1, subID)
		default:
			log.Printf("[RAZORPAY WEBHOOK INFO] subscription.updated status: %s ignored", status)
			_ = a.DB.InsertWebhookLog(eventID, event.Event, subID, userIDPtr, status, "ignored: status "+status, rawPayload)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ignored"}`))
			return
		}

	default:
		log.Printf("[RAZORPAY WEBHOOK WARNING] Unhandled event type: %s", event.Event)
		_ = a.DB.InsertWebhookLog(eventID, event.Event, subID, userIDPtr, status, "ignored: unhandled event type", rawPayload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored"}`))
		return
	}

	if err != nil {
		log.Printf("[RAZORPAY WEBHOOK ERROR] Failed to update user plan: %v", err)
		_ = a.DB.InsertWebhookLog(eventID, event.Event, subID, userIDPtr, status, "db_error: "+err.Error(), rawPayload)
		http.Error(w, "Failed to update user plan", http.StatusInternalServerError)
		return
	}

	log.Printf("[RAZORPAY WEBHOOK SUCCESS] %s applied for user ID %d", event.Event, userID)
	_ = a.DB.InsertWebhookLog(eventID, event.Event, subID, userIDPtr, status, "success", rawPayload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
