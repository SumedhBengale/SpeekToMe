package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	log "speektome-server/privacylog"
	"strconv"
	"strings"
	"time"

	"speektome-server/auth"
	"speektome-server/pipeline"
)

// HandleRetiredMeetingUpload answers the old single-request upload, which
// wrote plaintext audio to server disk. Current desktops use encrypted,
// resumable meeting jobs.
func HandleRetiredMeetingUpload(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "This version of SpeekTo.Me can no longer upload meetings. Update the desktop app to continue.", http.StatusGone)
}

func (a *App) HandleGetMeetings(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !a.accountRate(w, r, user.ID, "meeting_read", 120) {
		return
	}

	meetings, err := a.DB.GetUserMeetings(user.ID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to retrieve meetings for user %d: %v", user.ID, err)
		http.Error(w, "Failed to retrieve meetings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	views := make([]meetingSpeakerView, 0, len(meetings))
	for _, meeting := range meetings {
		views = append(views, prepareMeetingSpeakerView(meeting, user.Name))
	}
	_ = json.NewEncoder(w).Encode(views)
}

func (a *App) HandleGetMeetingDetail(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !a.accountRate(w, r, user.ID, "meeting_read", 120) {
		return
	}

	idStr := r.PathValue("id")
	meetingID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid meeting ID format", http.StatusBadRequest)
		return
	}

	meeting, err := a.DB.GetMeeting(meetingID)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	if meeting.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prepareMeetingSpeakerView(*meeting, user.Name))
}

func (a *App) HandleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !a.accountRate(w, r, user.ID, "meeting_update", 30) {
		return
	}

	idStr := r.PathValue("id")
	meetingID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid meeting ID format", http.StatusBadRequest)
		return
	}

	meeting, err := a.DB.GetMeeting(meetingID)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	if meeting.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var admitted int
	if a.DB.GetDB().QueryRow("SELECT admitted FROM meeting_jobs WHERE meeting_id=? AND user_id=?", meetingID, user.ID).Scan(&admitted) == nil && admitted == 1 {
		http.Error(w, "This meeting is still being processed. Wait for processing to finish before deleting it.", http.StatusConflict)
		return
	}

	if a.DB.Privacy != nil && a.DB.Privacy.Objects != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		err := a.DB.Privacy.DeleteAudio(ctx, user.ID, meetingID, false)
		cancel()
		if err != nil {
			http.Error(w, "Cloud audio deletion is pending; retry shortly", 503)
			return
		}
	}

	var job string
	if a.DB.GetDB().QueryRow("SELECT job_key FROM meeting_jobs WHERE meeting_id=? AND user_id=?", meetingID, user.ID).Scan(&job) == nil {
		a.limits().Release(user.ID, "meeting processing", job)
	}
	_, _ = a.DB.GetDB().Exec(`UPDATE meeting_jobs SET admitted=0,status='cancelled',attempt='',lease_until=0 WHERE meeting_id=? AND user_id=?`, meetingID, user.ID)
	// Spooled audio of a cancelled job is deleted by maintenance if this fails.
	if job != "" && a.DB.Privacy != nil && a.DB.Privacy.Spool != nil {
		_ = a.DB.Privacy.Spool.Delete(r.Context(), job)
	}
	// Recordings from before the encrypted spool may still be on local disk.
	if meeting.AudioPath != "" && a.DB.Privacy != nil {
		_ = a.DB.Privacy.PurgeRecording(meeting.UserID, meeting.AudioPath)
	}

	err = a.DB.DeleteMeeting(meetingID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to delete meeting %d: %v", meetingID, err)
		http.Error(w, "Failed to delete meeting", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (a *App) HandleGetMeetingAudio(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if !a.accountRate(w, r, user.ID, "meeting_read", 120) {
		return
	}

	idStr := r.PathValue("id")
	meetingID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid meeting ID format", http.StatusBadRequest)
		return
	}

	meeting, err := a.DB.GetMeeting(meetingID)
	if err != nil {
		http.Error(w, "Meeting not found", http.StatusNotFound)
		return
	}

	if meeting.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if a.DB.Privacy == nil {
		http.Error(w, "Encrypted cloud audio is not available yet", 404)
		return
	}
	if r.Header.Get("Range") != "" && (len(r.Header.Get("Range")) > 128 || strings.Contains(r.Header.Get("Range"), ",")) {
		http.Error(w, "invalid audio range", 416)
		return
	}
	// Recordings are served only from the encrypted bucket, never local disk.
	reader, _, err := a.DB.Privacy.Audio(r.Context(), user.ID, meetingID)
	if err != nil {
		http.Error(w, "Encrypted cloud audio is not available yet", 404)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "recording.ogg", time.Time{}, reader)
}

func generateMeetingAIAnalysis(ctx context.Context, transcript string, pctx *pipeline.PipelineContext) (polished string, summary string, actionItems string, err error) {
	systemPrompt := `You are an expert meeting assistant. Analyze the provided meeting transcript.
The transcript contains speaker labels in square brackets (e.g. "[John Doe]: ...", "[Remote Speaker]: ...").
You must format the "polished_transcript" output as a clean dialogue script where each line is prefixed with the speaker's name (e.g. "John Doe: ...", "Remote Speaker: ...").
Polish all speech for punctuation, grammar, and typos, while keeping all original meaning.

You must return your response in JSON format with exactly three fields:
- "polished_transcript": The polished transcript formatted as dialogue script.
- "summary": A concise, professional markdown summary of the meeting, including key discussion points.
- "action_items": A markdown list of action items, tasks, and owners.
Use each speaker label from the input exactly when attributing owners or mentioning speakers in the summary; never abbreviate, renumber, or invent speaker labels.

Return ONLY the JSON object. Do not include any markdown fences (like ` + "```" + `json) or conversational text outside the JSON structure.`

	client := &http.Client{Timeout: 60 * time.Second}
	content, err := pipeline.RunDeepSeekChatCompletion(ctx, client, "", systemPrompt, transcript, pctx)
	if err != nil {
		return transcript, "", "", err
	}

	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var analysis struct {
		PolishedTranscript string      `json:"polished_transcript"`
		Summary            string      `json:"summary"`
		ActionItems        interface{} `json:"action_items"`
	}
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		log.Printf("[MEETING AI WARNING] Failed to unmarshal JSON analysis: %v. Raw content: %s", err, content)
		return transcript, content, "No action items extracted.", nil
	}

	actionItemsStr := "No action items extracted."
	if analysis.ActionItems != nil {
		switch v := analysis.ActionItems.(type) {
		case string:
			actionItemsStr = v
		case []interface{}:
			var items []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
			actionItemsStr = strings.Join(items, "\n")
		}
	}

	polishedStr := analysis.PolishedTranscript
	if polishedStr == "" {
		polishedStr = transcript
	}

	summaryStr := analysis.Summary
	if summaryStr == "" {
		summaryStr = "AI Summary not available."
	}

	return polishedStr, summaryStr, actionItemsStr, nil
}

type MeetingSegment struct {
	SpeakerID string  `json:"speaker_id,omitempty"`
	Channel   string  `json:"channel,omitempty"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	Speaker   string  `json:"speaker"`
}
