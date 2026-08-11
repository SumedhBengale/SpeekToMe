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
	"sync"
	"time"

	"speektome-server/auth"
	"speektome-server/config"
	"speektome-server/pipeline"
	"speektome-server/ratelimit"
	"speektome-server/services"
	"speektome-server/utils"
)

func (a *App) getOrCompileUserVocabulary(userID int64) (*pipeline.UserVocabulary, error) {
	if a.VocabCache != nil {
		vocab := a.VocabCache.Get(userID)
		if vocab != nil {
			return vocab, nil
		}
	}

	terms, err := a.DB.GetVocabularyTerms(userID)
	if err != nil {
		return nil, err
	}

	vocab := pipeline.CompileUserVocabulary(terms)
	if a.VocabCache != nil {
		a.VocabCache.Set(userID, vocab)
	}
	return vocab, nil
}

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

	err = r.ParseMultipartForm(100 << 20)
	if err != nil {
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
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
		if val, ok := cfg["paid_monthly_limit_seconds"]; ok {
			if limit, err := strconv.ParseFloat(val, 64); err == nil {
				monthlyLimitSeconds = limit
			}
		}
	} else {
		monthlyLimitSeconds = 3600.0
		if val, ok := cfg["free_monthly_limit_seconds"]; ok {
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "Failed to create uploads directory", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("meeting_%d_%d.ogg", user.ID, time.Now().UnixNano())
	audioPath := filepath.Join(dir, filename)
	if err := os.WriteFile(audioPath, audioBytes, 0644); err != nil {
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

	fileMic, _, errMic := r.FormFile("file_mic")
	fileLoopback, _, errLb := r.FormFile("file_loopback")

	var parallelSuccess bool
	if errMic == nil && errLb == nil {
		defer fileMic.Close()
		defer fileLoopback.Close()

		var micBuf, lbBuf bytes.Buffer
		_, err1 := io.Copy(&micBuf, fileMic)
		_, err2 := io.Copy(&lbBuf, fileLoopback)

		if err1 == nil && err2 == nil && ((config.CloudflareAPIToken != "" && config.CloudflareAccountID != "") || config.DeepInfraAPIKey != "" || config.GroqAPIKey != "") {
			var micSegs, lbSegs []services.WhisperSegment
			var errMicTrans, errLbTrans error
			var micProvider, micRouting string

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				_, micSegs, micProvider, micRouting, errMicTrans = services.RunWhisperTranscribeWithModel(r.Context(), micBuf.Bytes(), "whisper-large-v3-turbo", user.Languages)
			}()

			go func() {
				defer wg.Done()
				_, lbSegs, _, _, errLbTrans = services.RunWhisperTranscribeWithModel(r.Context(), lbBuf.Bytes(), "whisper-large-v3-turbo", user.Languages)
			}()

			wg.Wait()

			if errMicTrans == nil && errLbTrans == nil {
				whisperProvider = micProvider
				whisperRouting = micRouting

				userName := user.Name
				if userName == "" {
					userName = "Local Speaker"
				}

				merged, rawTextStr := deduplicateMeetingSegments(micSegs, lbSegs, userName)
				rawText = rawTextStr

				segBytes, _ := json.Marshal(merged)
				segmentsJSON = string(segBytes)
				parallelSuccess = true
			} else {
				log.Printf("[DIARIZATION ERROR] Parallel transcription failed (mic_err=%v, lb_err=%v). Falling back to mixed audio.", errMicTrans, errLbTrans)
			}
		}
	}

	if !parallelSuccess && ((config.CloudflareAPIToken != "" && config.CloudflareAccountID != "") || config.DeepInfraAPIKey != "" || config.GroqAPIKey != "") {
		var errWhisper error
		var segments []services.WhisperSegment
		var mixProvider, mixRouting string
		rawText, segments, mixProvider, mixRouting, errWhisper = services.RunWhisperTranscribeWithModel(r.Context(), audioBytes, "whisper-large-v3-turbo", user.Languages)
		if errWhisper == nil {
			whisperProvider = mixProvider
			whisperRouting = mixRouting
			type Segment struct {
				Start   float64 `json:"start"`
				End     float64 `json:"end"`
				Text    string  `json:"text"`
				Speaker string  `json:"speaker,omitempty"`
			}
			var segList []Segment
			for _, s := range segments {
				segList = append(segList, Segment{
					Start: s.Start,
					End:   s.End,
					Text:  s.Text,
				})
			}
			segBytes, _ := json.Marshal(segList)
			segmentsJSON = string(segBytes)
		} else {
			log.Printf("[GROQ ERROR] Transcription failed: %v", errWhisper)
		}
	}

	vocab, err := a.getOrCompileUserVocabulary(user.ID)
	if err != nil {
		vocab = &pipeline.UserVocabulary{
			NormalizedLookup: make(map[string]string),
			TermsByCount:     make(map[int][]pipeline.CompiledTerm),
		}
	}

	pctx := pipeline.NewPipelineContext(user.ID, vocab)
	normalizer := pipeline.NewVocabNormalizerTransform("Vocab Normalizer Pass 1", true, 0.88)
	normalizedTranscript, _ := normalizer.Transform(r.Context(), rawText, pctx)

	var polished string = normalizedTranscript
	var summary string = "AI Summary not available (DeepSeek key missing or request failed)."
	var actionItems string = "No action items extracted."

	if config.DeepSeekAPIKey != "" && normalizedTranscript != "" {
		polished, summary, actionItems, err = generateMeetingAIAnalysis(r.Context(), normalizedTranscript, pctx)
		if err != nil {
			log.Printf("[DEEPSEEK ERROR] Failed to generate AI analysis: %v", err)
			polished = normalizedTranscript
		}
	}

	err = a.DB.UpdateMeetingResult(meetingID, polished, summary, actionItems, segmentsJSON)
	if err != nil {
		log.Printf("[DB ERROR] Failed to update meeting: %v", err)
	}

	log.Printf("[MEETING UPLOAD ROUTING] Transcription routing: %s | LLM routing: %s", whisperRouting, pctx.LLMRouting)
	_ = a.DB.LogUsageAndUpdateQuota(user.ID, duration, 0, 0, 0, false, "meeting-whisper", rawText, polished, "success", audioSizeBytes, whisperProvider, pctx.LLMProvider)

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

	var matchedTerms []string
	uniqueMatched := make(map[string]bool)
	for _, m := range pctx.Matches {
		uniqueMatched[m.Canonical] = true
	}
	for term := range uniqueMatched {
		matchedTerms = append(matchedTerms, term)
	}
	sort.Strings(matchedTerms)

	if len(matchedTerms) > 0 {
		vocabLines := make([]string, len(matchedTerms))
		for i, term := range matchedTerms {
			vocabLines[i] = fmt.Sprintf("- %s", term)
		}
		systemPrompt += "\n\nThe following user-specific vocabulary terms are present in the text and MUST remain unchanged and spelled exactly as shown:\n" + strings.Join(vocabLines, "\n")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	content, err := pipeline.RunDeepSeekChatCompletion(ctx, client, "deepseek-v4-flash", systemPrompt, transcript, pctx)
	if err != nil {
		return transcript, "", "", err
	}

	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var analysis struct {
		PolishedTranscript string `json:"polished_transcript"`
		Summary            string `json:"summary"`
		ActionItems        string `json:"action_items"`
	}
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return transcript, content, "No action items extracted.", nil
	}

	return analysis.PolishedTranscript, analysis.Summary, analysis.ActionItems, nil
}

type MeetingSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Speaker string  `json:"speaker"`
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
