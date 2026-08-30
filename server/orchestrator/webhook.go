package orchestrator

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// AWSEventBridgeDetail represents the payload of an EC2 Spot Event.
type AWSEventBridgeEvent struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Account    string          `json:"account"`
	Time       string          `json:"time"`
	Region     string          `json:"region"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

type HeartbeatPayload struct {
	InstanceID string `json:"instance_id"`
	Region     string `json:"region"`
	IPAddress  string `json:"ip_address"`
	Status     string `json:"status"`
}

// HandleSpotEvent processes AWS EventBridge notifications for Spot instances.
func (o *Orchestrator) HandleSpotEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Handle AWS SNS Subscription Confirmation if wrapped
	var snsMessage struct {
		Type             string `json:"Type"`
		SubscribeURL     string `json:"SubscribeURL"`
		Message          string `json:"Message"`
	}
	if json.Unmarshal(body, &snsMessage) == nil && snsMessage.Type == "SubscriptionConfirmation" {
		log.Printf("[AWS SPOT WEBHOOK] Confirming SNS Subscription: %s", snsMessage.SubscribeURL)
		_, _ = http.Get(snsMessage.SubscribeURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Unwrap SNS Message payload if applicable
	eventPayload := body
	if snsMessage.Message != "" {
		eventPayload = []byte(snsMessage.Message)
	}

	var event AWSEventBridgeEvent
	if err := json.Unmarshal(eventPayload, &event); err != nil {
		log.Printf("[AWS SPOT WEBHOOK ERROR] Failed to parse EventBridge event: %v", err)
		http.Error(w, "Invalid event payload", http.StatusBadRequest)
		return
	}

	log.Printf("[AWS SPOT EVENT] Type: %q | Region: %s | Resources: %v", event.DetailType, event.Region, event.Resources)

	// Check for Rebalance Recommendation or Interruption Warning
	if strings.Contains(event.DetailType, "EC2 Instance Rebalance Recommendation") ||
		strings.Contains(event.DetailType, "EC2 Spot Instance Interruption Warning") {
		o.TriggerRebalance(r.Context(), event.DetailType)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"event_processed"}`))
}

// HandleWorkerHeartbeat receives boot completion and dynamic IP from the worker instance.
func (o *Orchestrator) HandleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload HeartbeatPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if payload.IPAddress == "" {
		// Try resolving remote addr
		payload.IPAddress = strings.Split(r.RemoteAddr, ":")[0]
	}

	if err := o.RegisterHeartbeat(payload.InstanceID, payload.Region, payload.IPAddress); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"heartbeat_acknowledged"}`))
}

// HandleAdminStatus returns JSON representation of orchestrator status.
func (o *Orchestrator) HandleAdminStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := o.GetStatus()
	_ = json.NewEncoder(w).Encode(status)
}
