package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"speektome-server/db"
)

func signedWebhookRequest(t *testing.T, database *db.Database, eventID, secret, payload string) *httptest.ResponseRecorder {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/razorpay", strings.NewReader(payload))
	req.Header.Set("X-Razorpay-Event-Id", eventID)
	req.Header.Set("X-Razorpay-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	(&App{DB: database}).HandleRazorpayWebhook(rec, req)
	return rec
}

func TestRazorpayWebhookPaymentIsIdempotentAndAuthenticationDoesNotActivate(t *testing.T) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "billing.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer database.Close()
	t.Setenv("RAZORPAY_WEBHOOK_SECRET", "webhook-test-secret")

	if _, err := database.GetDB().Exec("INSERT INTO users (email, name, google_id) VALUES (?, ?, ?)", "billing@example.com", "Billing User", "billing-google"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := database.SetPendingSubscription(1, "sub_test"); err != nil {
		t.Fatalf("set pending subscription: %v", err)
	}
	if _, err := database.GetDB().Exec("UPDATE users SET monthly_usage_seconds = 42.0 WHERE id = 1"); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	secret := "webhook-test-secret"
	authenticated := `{"id":"evt_auth","event":"subscription.authenticated","payload":{"subscription":{"entity":{"id":"sub_test","status":"authenticated","notes":{"user_id":"1"}}}}}`
	if rec := signedWebhookRequest(t, database, "evt_auth", secret, authenticated); rec.Code != http.StatusOK {
		t.Fatalf("authenticated webhook status = %d, want 200", rec.Code)
	}
	var active int
	if err := database.GetDB().QueryRow("SELECT plan_active FROM users WHERE id = 1").Scan(&active); err != nil {
		t.Fatalf("read plan status: %v", err)
	}
	if active != 0 {
		t.Fatalf("authentication webhook activated plan; want inactive")
	}

	charged := `{"id":"evt_charged","event":"subscription.charged","payload":{"subscription":{"entity":{"id":"sub_test","status":"active","current_start":1700000000,"current_end":1702592000,"notes":{"user_id":"1"}}},"payment":{"entity":{"id":"pay_test","invoice_id":"inv_test","amount":200,"currency":"USD","status":"captured","captured":true}}}}`
	if rec := signedWebhookRequest(t, database, "evt_charged", secret, charged); rec.Code != http.StatusOK {
		t.Fatalf("charged webhook status = %d, want 200", rec.Code)
	}
	if err := database.GetDB().QueryRow("SELECT plan_active, monthly_usage_seconds FROM users WHERE id = 1").Scan(&active, new(float64)); err != nil {
		t.Fatalf("read active plan: %v", err)
	}
	if active != 1 {
		t.Fatalf("charged webhook did not activate plan")
	}

	if _, err := database.GetDB().Exec("UPDATE users SET monthly_usage_seconds = 17.0 WHERE id = 1"); err != nil {
		t.Fatalf("seed post-payment usage: %v", err)
	}
	invoicePaid := `{"id":"evt_invoice","event":"invoice.paid","payload":{"subscription":{"entity":{"id":"sub_test","status":"active","notes":{"user_id":"1"}}},"invoice":{"entity":{"id":"inv_test","subscription_id":"sub_test","payment_id":"pay_test","amount":200,"currency":"USD","status":"paid"}}}}`
	if rec := signedWebhookRequest(t, database, "evt_invoice", secret, invoicePaid); rec.Code != http.StatusOK {
		t.Fatalf("invoice webhook status = %d, want 200", rec.Code)
	}
	var usage float64
	if err := database.GetDB().QueryRow("SELECT monthly_usage_seconds FROM users WHERE id = 1").Scan(&usage); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if usage != 17.0 {
		t.Fatalf("duplicate invoice reset usage to %v; want 17", usage)
	}

	pending := `{"id":"evt_pending","event":"subscription.pending","payload":{"subscription":{"entity":{"id":"sub_test","status":"pending","notes":{"user_id":"1"}}}}}`
	if rec := signedWebhookRequest(t, database, "evt_pending", secret, pending); rec.Code != http.StatusOK {
		t.Fatalf("pending webhook status = %d, want 200", rec.Code)
	}
	if err := database.GetDB().QueryRow("SELECT plan_active, monthly_usage_seconds, razorpay_subscription_status FROM users WHERE id = 1").Scan(&active, &usage, new(string)); err != nil {
		t.Fatalf("read pending state: %v", err)
	}
	if active != 1 || usage != 17.0 {
		t.Fatalf("pending payment changed access/usage: active=%d usage=%v", active, usage)
	}

	halted := `{"id":"evt_halted","event":"subscription.halted","payload":{"subscription":{"entity":{"id":"sub_test","status":"halted","notes":{"user_id":"1"}}}}}`
	if rec := signedWebhookRequest(t, database, "evt_halted", secret, halted); rec.Code != http.StatusOK {
		t.Fatalf("halted webhook status = %d, want 200", rec.Code)
	}
	if err := database.GetDB().QueryRow("SELECT plan_active, monthly_usage_seconds FROM users WHERE id = 1").Scan(&active, &usage); err != nil {
		t.Fatalf("read halted state: %v", err)
	}
	if active != 0 || usage != 17.0 {
		t.Fatalf("halted payment state incorrect: active=%d usage=%v", active, usage)
	}
}

func TestVerifySubscriptionReconcilesCapturedPlanPayment(t *testing.T) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "verify.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer database.Close()

	if _, err := database.GetDB().Exec("INSERT INTO users (email, name, google_id) VALUES (?, ?, ?)", "verify@example.com", "Verify User", "verify-google"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := database.SetPendingSubscription(1, "sub_verify"); err != nil {
		t.Fatalf("set pending subscription: %v", err)
	}
	if err := database.CreateSession("verify-session", 1, 0); err != nil {
		t.Fatalf("create session: %v", err)
	}

	mockRazorpay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/subscriptions/sub_verify":
		_, _ = w.Write([]byte(`{"id":"sub_verify","plan_id":"plan_verify","status":"active","current_start":1700000000,"current_end":1702592000}`))
	case "/v1/payments/pay_verify":
		_, _ = w.Write([]byte(`{"id":"pay_verify","subscription_id":"sub_verify","invoice_id":"inv_verify","amount":200,"currency":"USD","status":"captured","captured":true}`))
	case "/v1/plans/plan_verify":
		_, _ = w.Write([]byte(`{"id":"plan_verify","item":{"amount":200,"currency":"USD"}}`))
	default:
		http.NotFound(w, r)
	}
	}))
	defer mockRazorpay.Close()
	previousBaseURL := razorpayAPIBaseURL
	razorpayAPIBaseURL = mockRazorpay.URL
	defer func() { razorpayAPIBaseURL = previousBaseURL }()

	t.Setenv("RAZORPAY_KEY_ID", "key_test")
	t.Setenv("RAZORPAY_KEY_SECRET", "secret_test")
	t.Setenv("RAZORPAY_PLAN_ID", "plan_verify")

	verificationPayload := map[string]string{
		"razorpay_payment_id":      "pay_verify",
		"razorpay_subscription_id": "sub_verify",
	}
	mac := hmac.New(sha256.New, []byte("secret_test"))
	_, _ = mac.Write([]byte("pay_verify|sub_verify"))
	verificationPayload["razorpay_signature"] = hex.EncodeToString(mac.Sum(nil))
	body, _ := json.Marshal(verificationPayload)
	req := httptest.NewRequest(http.MethodPost, "/billing/verify-subscription", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer verify-session")
	rec := httptest.NewRecorder()
	(&App{DB: database}).HandleVerifySubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verification status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var active int
	if err := database.GetDB().QueryRow("SELECT plan_active FROM users WHERE id = 1").Scan(&active); err != nil {
		t.Fatalf("read active plan: %v", err)
	}
	if active != 1 {
		t.Fatalf("successful captured payment did not activate plan")
	}
	var paymentCount int
	if err := database.GetDB().QueryRow("SELECT COUNT(*) FROM subscription_payments WHERE payment_id = 'pay_verify'").Scan(&paymentCount); err != nil {
		t.Fatalf("read recorded payment: %v", err)
	}
	if paymentCount != 1 {
		t.Fatalf("recorded payment count = %d, want 1", paymentCount)
	}
}

func TestCreateSubscriptionStartsImmediatelyWithoutTrial(t *testing.T) {
	databasePath := t.TempDir() + string(os.PathSeparator) + "create.db"
	database, err := db.NewDatabase(databasePath)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer database.Close()

	if _, err := database.GetDB().Exec("INSERT INTO users (email, name, google_id) VALUES (?, ?, ?)", "create@example.com", "Create User", "create-google"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := database.CreateSession("create-session", 1, 0); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var subscriptionRequest map[string]interface{}
	mockRazorpay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/plans/plan_create" {
			_, _ = w.Write([]byte(`{"id":"plan_create","item":{"amount":200,"currency":"USD"}}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions" {
			if err := json.NewDecoder(r.Body).Decode(&subscriptionRequest); err != nil {
				http.Error(w, "invalid body", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"id":"sub_create"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockRazorpay.Close()
	previousBaseURL := razorpayAPIBaseURL
	razorpayAPIBaseURL = mockRazorpay.URL
	defer func() { razorpayAPIBaseURL = previousBaseURL }()

	t.Setenv("RAZORPAY_KEY_ID", "key_test")
	t.Setenv("RAZORPAY_KEY_SECRET", "secret_test")
	t.Setenv("RAZORPAY_PLAN_ID", "plan_create")

	req := httptest.NewRequest(http.MethodPost, "/billing/create-subscription", nil)
	req.Header.Set("Authorization", "Bearer create-session")
	rec := httptest.NewRecorder()
	(&App{DB: database}).HandleCreateSubscription(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create subscription status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, exists := subscriptionRequest["start_at"]; exists {
		t.Fatalf("create request included start_at; expected immediate subscription")
	}
	if notify, ok := subscriptionRequest["customer_notify"].(bool); !ok || !notify {
		t.Fatalf("customer_notify = %#v, want true", subscriptionRequest["customer_notify"])
	}
	if subscriptionRequest["plan_id"] != "plan_create" {
		t.Fatalf("plan_id = %#v, want plan_create", subscriptionRequest["plan_id"])
	}

	var savedSubscription string
	if err := database.GetDB().QueryRow("SELECT razorpay_subscription_id FROM users WHERE id = 1").Scan(&savedSubscription); err != nil {
		t.Fatalf("read saved subscription: %v", err)
	}
	if savedSubscription != "sub_create" {
		t.Fatalf("saved subscription = %q, want sub_create", savedSubscription)
	}
}
