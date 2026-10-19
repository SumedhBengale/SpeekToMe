package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"speektome-server/accountlimits"
	"speektome-server/auth"
	"time"
)

var recordingPermitID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// A recording permit survives a mid-meeting network outage. Clients stop locally
// after four hours; five minutes of margin prevents overlap due to clock skew.
func (a *App) HandleStartMeetingSession(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, a.DB)
	if e != nil {
		http.Error(w, "Sign in before starting a meeting.", 401)
		return
	}
	if !a.accountRate(w, r, user.ID, "meeting_start", 10) {
		return
	}
	var tier string
	var active int
	var used float64
	if e = a.DB.GetDB().QueryRowContext(r.Context(), "SELECT COALESCE(plan_tier,'free'),COALESCE(plan_active,0),COALESCE(monthly_usage_seconds,0) FROM users WHERE id=?", user.ID).Scan(&tier, &active, &used); e != nil {
		accountlimits.Write(w, accountlimits.Unavailable())
		return
	}
	limit := configuredLimit("quota_free_monthly_seconds", 3600)
	if tier == "pro" && active == 1 {
		limit = configuredLimit("quota_paid_monthly_seconds", 2700000)
	}
	if used >= float64(limit) {
		accountlimits.Write(w, accountlimits.Denied("meeting_audio_quota_exhausted", "Your monthly transcription allowance is exhausted. You cannot start another meeting until it resets.", 60))
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	var request struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request); err != nil && err != io.EOF {
		http.Error(w, "Invalid recording permit request", 400)
		return
	}
	if request.ID == "" {
		request.ID = accountlimits.ID()
	}
	if !recordingPermitID.MatchString(request.ID) {
		http.Error(w, "Invalid recording permit id", 400)
		return
	}
	// Client-generated IDs make an interrupted start safe to reconcile: the
	// client knows the owner even if the successful HTTP response was lost.
	owner, e := a.limits().AcquireOwner(r.Context(), user.ID, "meeting recording", request.ID, 4*time.Hour+5*time.Minute)

	if e != nil {
		accountlimits.Write(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": owner, "max_duration_seconds": 14400})
}
func (a *App) HandleEndMeetingSession(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, a.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	if err := a.limits().Release(user.ID, "meeting recording", r.PathValue("id")); err != nil {
		accountlimits.Write(w, accountlimits.Unavailable())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
