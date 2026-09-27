package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/utils"
)

func (a *App) HandleUploadMeeting(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	limit := config.ConfigManager.Get().DefaultUserRateLimitRPM
	if user.RateLimitRPM.Valid {
		userOverride := int(user.RateLimitRPM.Int64)
		if userOverride < limit {
			limit = userOverride
		}
	}
	userLimiter := ratelimit.UserRateLimiterReg.GetLimiter(user.ID, limit)
	if !userLimiter.Allow() {
		http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 120<<20)
	err = r.ParseMultipartForm(100 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart form: audio payload exceeds limit (max 120MB)", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		title = fmt.Sprintf("Meeting on %s", time.Now().Format("2006-01-02 15:04"))
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var audioBuf bytes.Buffer
	if _, err := io.Copy(&audioBuf, file); err != nil {
		http.Error(w, "Failed to read audio file payload", http.StatusInternalServerError)
		return
	}
	audioBytes := audioBuf.Bytes()
	audioSizeBytes := len(audioBytes)

	duration := utils.GetOggOpusDuration(audioBytes)

	isPaid := user.PlanActive == 1 && user.PlanTier == "pro"
	var monthlyLimitSeconds float64
	cfg := config.ConfigManager.GetRawAll()
	if isPaid {
		monthlyLimitSeconds = 180000.0
		if val, ok := cfg["quota_paid_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		} else if val, ok := cfg["paid_monthly_limit_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	} else {
		monthlyLimitSeconds = 3600.0
		if val, ok := cfg["quota_free_monthly_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		} else if val, ok := cfg["free_monthly_limit_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	}

	if user.MonthlyUsageSeconds+duration > monthlyLimitSeconds {
		log.Printf("[MONTHLY LIMIT] Meeting Upload from %s denied: Exceeded monthly limit (%.1f + %.1f > %.1f seconds)", user.Email, user.MonthlyUsageSeconds, duration, monthlyLimitSeconds)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Monthly usage limit exceeded. Your plan is limited to %.1f hours of transcription per month. You have already used %.1f hours.", monthlyLimitSeconds/3600.0, user.MonthlyUsageSeconds/3600.0),
		})
		return
	}

	dir := "uploads/meetings"
	if err := os.MkdirAll(dir, 0700); err != nil {
		http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("meeting_%d_%d.ogg", user.ID, time.Now().UnixNano())
	audioPath := filepath.Join(dir, filename)
	if err := os.WriteFile(audioPath, audioBytes, 0600); err != nil {
		http.Error(w, "Failed to save audio file to disk", http.StatusInternalServerError)
		return
	}

	meetingID, err := a.DB.CreateMeeting(user.ID, title, duration, audioPath)
	if err != nil {
		log.Printf("[DB ERROR] Failed to record meeting for user %d: %v", user.ID, err)
		http.Error(w, "Failed to record meeting", http.StatusInternalServerError)
		return
	}

	var rawText string
	var segmentsJSON string = "[]"
	var whisperProvider string
	var whisperRouting string

	energyJSONStr := r.FormValue("energy_json")
	var energySamples []EnergySample
	if energyJSONStr != "" {
		_ = json.Unmarshal([]byte(energyJSONStr), &energySamples)
	}

	if config.AWSBearerTokenBedrock != "" || (config.CloudflareAPIToken != "" && config.CloudflareAccountID != "") || config.DeepInfraAPIKey != "" || config.GroqAPIKey != "" {
		var errWhisper error
		var segments []services.WhisperSegment
		var mixProvider, mixRouting string
		rawText, segments, mixProvider, mixRouting, errWhisper = services.RunWhisperTranscribeWithModel(r.Context(), audioBytes, "whisper-large-v3-turbo", user.Languages)
		if errWhisper == nil {
			whisperProvider = mixProvider
			whisperRouting = mixRouting

			userName := user.Name
			if userName == "" {
				userName = "Local Speaker"
			}

			merged, rawTextStr := diarizeSegmentsWithEnergy(segments, energySamples, userName)
			rawText = rawTextStr

			segBytes, _ := json.Marshal(merged)
			segmentsJSON = string(segBytes)
		} else {
			log.Printf("[GROQ ERROR] Transcription failed: %v", errWhisper)
		}
	}

	pctx := pipeline.NewPipelineContext(user.ID)
	var polished string = rawText
	var summary string = "AI Summary not available (no speech detected in recording)."
	var actionItems string = "No action items extracted."

	if rawText != "" {
		polished, summary, actionItems, err = generateMeetingAIAnalysis(r.Context(), rawText, pctx)
		if err != nil {
			log.Printf("[MEETING AI ERROR] Failed to generate AI analysis: %v", err)
			polished = rawText
			summary = fmt.Sprintf("AI Summary generation failed: %v", err)
		}
	}

	err = a.DB.UpdateMeetingResult(meetingID, polished, summary, actionItems, segmentsJSON)
	if err != nil {
		log.Printf("[DB ERROR] Failed to update meeting: %v", err)
	}

	log.Printf("[MEETING UPLOAD ROUTING] Transcription routing: %s | LLM routing: %s", whisperRouting, pctx.LLMRouting)
	_, _ = a.DB.LogUsageAndUpdateQuota(user.ID, duration, 0, 0, 0, false, "meeting-whisper", rawText, polished, "success", audioSizeBytes, whisperProvider, pctx.LLMProvider)

	updatedMeeting, err := a.DB.GetMeeting(meetingID)
	if err != nil {
		http.Error(w, "Failed to load updated meeting details", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updatedMeeting)
}

func (a *App) HandleGetMeetings(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	meetings, err := a.DB.GetUserMeetings(user.ID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to retrieve meetings for user %d: %v", user.ID, err)
		http.Error(w, "Failed to retrieve meetings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meetings)
}

func (a *App) HandleGetMeetingDetail(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
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
	_ = json.NewEncoder(w).Encode(meeting)
}

func (a *App) HandleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
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

	cleanedPath := filepath.Clean(meeting.AudioPath)
	normalizedPath := filepath.ToSlash(cleanedPath)
	if !strings.HasPrefix(normalizedPath, "uploads/meetings/") {
		http.Error(w, "Forbidden: Invalid audio file path", http.StatusForbidden)
		return
	}

	_ = os.Remove(meeting.AudioPath)

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

	cleanedPath := filepath.Clean(meeting.AudioPath)
	normalizedPath := filepath.ToSlash(cleanedPath)
	if !strings.HasPrefix(normalizedPath, "uploads/meetings/") {
		http.Error(w, "Forbidden: Invalid audio file path", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "audio/ogg")
	http.ServeFile(w, r, meeting.AudioPath)
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

type EnergySample struct {
	Time float64 `json:"time"`
	Mic  uint32  `json:"mic"`
	Lb   uint32  `json:"lb"`
}

type MeetingSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker"`
}

func diarizeSegmentsWithEnergy(segments []services.WhisperSegment, energySamples []EnergySample, localSpeakerName string) ([]MeetingSegment, string) {
	if localSpeakerName == "" {
		localSpeakerName = "Local Speaker"
	}

	type rawLabeled struct {
		Start   float64
		End     float64
		Text    string
		Speaker string
	}

	var labeled []rawLabeled

	for _, seg := range segments {
		cleanText := strings.TrimSpace(seg.Text)
		if cleanText == "" {
			continue
		}

		var totalMic, totalLb float64
		var count float64

		if len(energySamples) > 0 {
			for _, sample := range energySamples {
				if sample.Time >= seg.Start && sample.Time <= seg.End {
					totalMic += float64(sample.Mic)
					totalLb += float64(sample.Lb)
					count++
				}
			}
		}

		speaker := localSpeakerName
		if count > 0 {
			avgMic := totalMic / count
			avgLb := totalLb / count
			if avgLb > 15.0 && avgLb >= avgMic*0.75 {
				speaker = "Remote Speaker"
			}
		}

		labeled = append(labeled, rawLabeled{
			Start:   seg.Start,
			End:     seg.End,
			Text:    cleanText,
			Speaker: speaker,
		})
	}

	var merged []MeetingSegment
	for _, seg := range labeled {
		if len(merged) == 0 {
			merged = append(merged, MeetingSegment{
				Start:   seg.Start,
				End:     seg.End,
				Text:    seg.Text,
				Speaker: seg.Speaker,
			})
			continue
		}

		last := &merged[len(merged)-1]
		if last.Speaker == seg.Speaker && seg.Start <= last.End+1.5 {
			last.Text = strings.TrimSpace(last.Text + " " + seg.Text)
			if seg.End > last.End {
				last.End = seg.End
			}
		} else {
			merged = append(merged, MeetingSegment{
				Start:   seg.Start,
				End:     seg.End,
				Text:    seg.Text,
				Speaker: seg.Speaker,
			})
		}
	}

	var transcriptBuilder strings.Builder
	for _, s := range merged {
		transcriptBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", s.Speaker, s.Text))
	}

	return merged, transcriptBuilder.String()
}

func deduplicateMeetingSegments(micSegs []services.WhisperSegment, lbSegs []services.WhisperSegment, localSpeakerName string) ([]MeetingSegment, string) {
	if localSpeakerName == "" {
		localSpeakerName = "Local Speaker"
	}

	var candidates []MeetingSegment

	for _, s := range lbSegs {
		cleanText := strings.TrimSpace(s.Text)
		if cleanText == "" {
			continue
		}
		candidates = append(candidates, MeetingSegment{
			Start:   s.Start,
			End:     s.End,
			Text:    cleanText,
			Speaker: "Remote Speaker",
		})
	}

	for _, m := range micSegs {
		cleanMicText := strings.TrimSpace(m.Text)
		if cleanMicText == "" {
			continue
		}

		isBleed := false
		for _, lb := range lbSegs {
			cleanLbText := strings.TrimSpace(lb.Text)
			if cleanLbText == "" {
				continue
			}

			// Time overlap check (within +/- 2.0s window)
			timeOverlap := !(m.End < lb.Start-2.0 || m.Start > lb.End+2.0)
			if timeOverlap {
				similarity := calculateTextSimilarity(cleanMicText, cleanLbText)
				if similarity >= 0.45 {
					isBleed = true
					break
				}
			}
		}

		if !isBleed {
			candidates = append(candidates, MeetingSegment{
				Start:   m.Start,
				End:     m.End,
				Text:    cleanMicText,
				Speaker: localSpeakerName,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Start == candidates[j].Start {
			return candidates[i].End < candidates[j].End
		}
		return candidates[i].Start < candidates[j].Start
	})

	var merged []MeetingSegment
	for _, seg := range candidates {
		if len(merged) == 0 {
			merged = append(merged, seg)
			continue
		}

		last := &merged[len(merged)-1]
		if last.Speaker == seg.Speaker && seg.Start <= last.End+1.5 {
			last.Text = strings.TrimSpace(last.Text + " " + seg.Text)
			if seg.End > last.End {
				last.End = seg.End
			}
		} else {
			merged = append(merged, seg)
		}
	}

	var transcriptBuilder strings.Builder
	for _, s := range merged {
		transcriptBuilder.WriteString(fmt.Sprintf("[%s]: %s\n", s.Speaker, s.Text))
	}

	return merged, transcriptBuilder.String()
}

func calculateTextSimilarity(s1, s2 string) float64 {
	words1 := tokenizeString(s1)
	words2 := tokenizeString(s2)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	var common int
	for _, w := range words2 {
		if set1[w] {
			common++
		}
	}

	maxWords := len(words1)
	if len(words2) > maxWords {
		maxWords = len(words2)
	}

	return float64(common) / float64(maxWords)
}

func tokenizeString(s string) []string {
	s = strings.ToLower(s)
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.Fields(sb.String())
}
