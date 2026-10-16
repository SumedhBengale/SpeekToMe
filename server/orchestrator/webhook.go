package orchestrator

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type AWSEventBridgeEvent struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	DetailType string          `json:"detail-type"`
	Source     string          `json:"source"`
	Account    string          `json:"account"`
	Time       time.Time       `json:"time"`
	Region     string          `json:"region"`
	Detail     json.RawMessage `json:"detail"`
}

func (o *Controller) ProcessEvent(event AWSEventBridgeEvent) error {
	if event.Source != "aws.ec2" || event.Account != o.cfg.AccountID {
		return errInvalidEvent
	}
	if _, e := regional(o.cfg, event.Region); e != nil {
		return errInvalidEvent
	}
	if event.Time.IsZero() || o.now().Sub(event.Time) > 30*time.Minute || event.Time.Sub(o.now()) > time.Minute {
		return errInvalidEvent
	}
	var detail struct {
		ID string `json:"instance-id"`
	}
	if json.Unmarshal(event.Detail, &detail) != nil || detail.ID == "" {
		return errInvalidEvent
	}
	return o.Signal(detail.ID, event.Region, event.DetailType)
}

var errInvalidEvent = &eventError{"invalid or stale event"}

type eventError struct{ message string }

func (e *eventError) Error() string { return e.message }
func (o *Controller) authenticated(r *http.Request) bool {
	token, e := os.ReadFile(o.cfg.TokenFile)
	if e != nil {
		return false
	}
	want := sha256.Sum256([]byte(strings.TrimSpace(string(token))))
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
func (o *Controller) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fleet/meetings/acquire", o.acquireMeeting)
	mux.HandleFunc("POST /fleet/meetings/release", o.releaseMeeting)
	mux.HandleFunc("GET /fleet/status", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(o.GetStatus()) })
	mux.HandleFunc("GET /fleet/workers", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(o.Candidates()) })
	mux.HandleFunc("POST /worker/heartbeat", o.handleHeartbeat)
	mux.HandleFunc("POST /webhooks/aws-spot-event", o.handleEvent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !o.authenticated(r) {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}
func (o *Controller) handleEvent(w http.ResponseWriter, r *http.Request) {
	var event AWSEventBridgeEvent
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&event) != nil {
		http.Error(w, "invalid event", 400)
		return
	}
	if e := o.ProcessEvent(event); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	w.WriteHeader(204)
}
func (o *Controller) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		InstanceID string      `json:"instance_id"`
		Region     string      `json:"region"`
		Signal     *SpotSignal `json:"spot_signal"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&payload) != nil {
		http.Error(w, "invalid heartbeat", 400)
		return
	}
	o.mu.RLock()
	worker, ok := o.state.Workers[payload.InstanceID]
	o.mu.RUnlock()
	if !ok || worker.Region != payload.Region {
		http.Error(w, "unmanaged worker", 404)
		return
	}
	// Do not trust callback-provided IPs/readiness. EC2 discovery and verified TLS
	// metadata determine routing. Heartbeats only expedite polling or warnings.
	if payload.Signal != nil {
		if payload.Signal.Time.IsZero() || o.now().Sub(payload.Signal.Time) > 30*time.Minute {
			http.Error(w, "stale signal", 400)
			return
		}
		if e := o.Signal(payload.InstanceID, payload.Region, payload.Signal.Type); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
	w.WriteHeader(204)
}
func (o *Orchestrator) guarded(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter, *http.Request)) {
	if o.Controller == nil {
		http.Error(w, "fleet controller not configured", 503)
		return
	}
	if !o.Controller.authenticated(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	next(w, r)
}
func (o *Orchestrator) HandleSpotEvent(w http.ResponseWriter, r *http.Request) {
	o.guarded(w, r, o.event)
}
func (o *Orchestrator) event(w http.ResponseWriter, r *http.Request) { o.Controller.handleEvent(w, r) }
func (o *Orchestrator) HandleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	o.guarded(w, r, o.heartbeat)
}
func (o *Orchestrator) heartbeat(w http.ResponseWriter, r *http.Request) {
	o.Controller.handleHeartbeat(w, r)
}
func (o *Orchestrator) HandleAdminStatus(w http.ResponseWriter, r *http.Request) {
	o.guarded(w, r, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(o.GetStatus()) })
}
