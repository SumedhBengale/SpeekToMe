package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"speektome-server/audiospool"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// fakeFleet records every controller call a meeting makes. Gap ASR returns one
// word inside the requested range and one outside it, which must be dropped.
type fakeFleet struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeFleet) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.URL.Path+"?"+r.URL.Query().Get("ranges"))
	f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/fleet/meetings/") {
		w.WriteHeader(204)
		return
	}
	w.Write([]byte(`{"text":"gapword outside","words":[{"word":"gapword","start":9.0,"end":9.4},{"word":"outside","start":3.0,"end":3.2}]}`))
}

func (f *fakeFleet) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeMeetingLLM capitalizes and punctuates every turn, and returns a fixed
// summary for the summary prompts.
func fakeMeetingLLM(t *testing.T) {
	t.Helper()
	previous := meetingLLM
	t.Cleanup(func() { meetingLLM = previous })
	meetingLLM = func(_ context.Context, _ int64, system, prompt string) (string, error) {
		if system != enhancePrompt {
			return `{"summary":"Discussed the plan.","action_items":["Owner: send the notes"]}`, nil
		}
		var request struct {
			Turns []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"turns"`
		}
		if err := json.Unmarshal([]byte(prompt), &request); err != nil {
			return "", err
		}
		for i := range request.Turns {
			text := []rune(request.Turns[i].Text)
			text[0] = unicode.ToUpper(text[0])
			request.Turns[i].Text = string(text) + "."
		}
		reply, _ := json.Marshal(request)
		return string(reply), nil
	}
}

type pipelineFixture struct {
	j        *MeetingJobs
	fleet    *fakeFleet
	diarizer *fakeCPU
	meeting  int64
	key      string
}

// newPipelineFixture spools a committed 20-second meeting. transcript is the
// signed live transcript file, or nil for a desktop that had none.
func newPipelineFixture(t *testing.T, transcript []byte) *pipelineFixture {
	t.Helper()
	j, d := diarizationFixture(t)
	diarizer := fakeCPUServer(t, d)
	diarizer.audio = string(audiospool.SyntheticOggOpus(20))
	fleet := &fakeFleet{}
	server := httptest.NewServer(http.HandlerFunc(fleet.serve))
	t.Cleanup(server.Close)
	j.fleet, j.token, j.client = server.URL, "01234567890123456789012345678901", server.Client()
	if _, err := j.db.Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active) VALUES(1,'owner@example.com','Owner','pro',1)`); err != nil {
		t.Fatal(err)
	}
	meeting, err := j.app.DB.CreateMeeting(1, "meeting", 20, "")
	if err != nil {
		t.Fatal(err)
	}
	audio := audiospool.SyntheticOggOpus(20)
	files := map[string]MeetingFile{"mic": {Size: 1}, "system": {Size: 1}, "mixed": {Size: 1}}
	spooled := map[string][]byte{"mic": audio, "system": audio}
	if transcript != nil {
		files["transcript"] = MeetingFile{Size: int64(len(transcript))}
		spooled["transcript"] = transcript
	}
	manifest, _ := json.Marshal(files)
	key := jobKey(1, "m1")
	if _, err = j.db.Exec(`INSERT INTO meeting_jobs(job_key,user_id,client_id,title,files_json,status,meeting_id,created_at,attempt,admitted,language) VALUES(?,1,'m1','meeting',?,'processing',?,0,'attempt',1,'en')`, key, string(manifest), meeting); err != nil {
		t.Fatal(err)
	}
	spoolAudio(t, j.spool, 1, key, spooled)
	fakeMeetingLLM(t)
	return &pipelineFixture{j, fleet, diarizer, meeting, key}
}

func (p *pipelineFixture) run(t *testing.T) ([]MeetingSegment, string) {
	t.Helper()
	p.j.process(context.Background(), p.key, "attempt")
	var status, lastError string
	p.j.db.QueryRow(`SELECT status,error FROM meeting_jobs WHERE job_key=?`, p.key).Scan(&status, &lastError)
	if status != "completed" {
		t.Fatal(status, lastError)
	}
	meeting, err := p.j.app.DB.GetMeeting(p.meeting)
	if err != nil {
		t.Fatal(err)
	}
	var segments []MeetingSegment
	if err = json.Unmarshal([]byte(meeting.SegmentsJSON), &segments); err != nil {
		t.Fatal(err)
	}
	if meeting.Summary != "Discussed the plan." || meeting.ActionItems != "- Owner: send the notes" {
		t.Fatalf("summary not saved: %q %q", meeting.Summary, meeting.ActionItems)
	}
	return segments, meeting.Transcript
}

func describe(segments []MeetingSegment) string {
	var parts []string
	for _, s := range segments {
		parts = append(parts, fmt.Sprintf("%s/%s:%s", s.Channel, s.SpeakerID, s.Text))
	}
	return strings.Join(parts, " | ")
}

func TestMeetingPipelineUsesLiveTranscriptAndTranscribesOnlyTheGap(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	// The microphone is fully covered. The system channel lost 8-12 s, as if
	// the network dropped for four seconds.
	transcript := liveLines(t, 1,
		liveBatch{JobID: "m1", Channel: "mic", Session: "a", From: 0, To: 20, Words: []liveWord{{"hello", 0.5, 0.9}, {"there", 1.0, 1.3}}},
		liveBatch{JobID: "m1", Channel: "system", Session: "b", From: 0, To: 8, Words: []liveWord{{"welcome", 1.0, 1.5}}},
		liveBatch{JobID: "m1", Channel: "system", Session: "c", From: 12, To: 20},
	)
	p := newPipelineFixture(t, transcript)
	segments, text := p.run(t)
	// Microphone words are the user's; diarization put speaker 3 at 0.25-2.5 s;
	// the gap's word overlaps no speaker.
	want := "mic/speaker_local:Hello there. | system/spk_3:Welcome. | system/unknown:Gapword."
	if got := describe(segments); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !strings.Contains(text, "Owner: Hello there.") {
		t.Fatal(text)
	}
	calls := p.fleet.paths()
	asr := 0
	for _, call := range calls {
		if strings.HasPrefix(call, "/v1/meeting/mic") {
			t.Fatal("a fully covered channel was transcribed again")
		}
		if strings.HasPrefix(call, "/v1/meeting/system") {
			asr++
			if call != "/v1/meeting/system?8.00-12.00" {
				t.Fatal("wrong gap range", call)
			}
		}
	}
	if asr != 1 || calls[0] != "/fleet/meetings/acquire?" || calls[len(calls)-1] != "/fleet/meetings/release?" {
		t.Fatal(calls)
	}
}

func TestMeetingPipelineWithoutGapsNeverCallsTheFleet(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	transcript := liveLines(t, 1,
		liveBatch{JobID: "m1", Channel: "mic", Session: "a", From: 0, To: 20, Words: []liveWord{{"hello", 0.5, 0.9}}},
		liveBatch{JobID: "m1", Channel: "system", Session: "b", From: 0, To: 20, Words: []liveWord{{"welcome", 1.0, 1.5}}},
	)
	p := newPipelineFixture(t, transcript)
	segments, _ := p.run(t)
	if len(segments) != 2 {
		t.Fatal(describe(segments))
	}
	if calls := p.fleet.paths(); len(calls) != 0 {
		t.Fatal("a fully covered meeting used a GPU worker", calls)
	}
}

func TestMeetingPipelineTranscribesWholeChannelsWithoutALiveTranscript(t *testing.T) {
	p := newPipelineFixture(t, nil) // an older desktop, or live never started
	segments, _ := p.run(t)
	// Every word the worker returns belongs to the whole-channel range.
	if len(segments) != 4 {
		t.Fatal(describe(segments))
	}
	stages := map[string]bool{}
	for _, call := range p.fleet.paths() {
		if strings.HasPrefix(call, "/v1/meeting/") {
			path, ranges, _ := strings.Cut(call, "?")
			stages[path] = true
			if !strings.HasPrefix(ranges, "0.00-") || strings.Contains(ranges, ",") {
				t.Fatal("expected one whole-channel range", call)
			}
		}
	}
	if !stages["/v1/meeting/mic"] || !stages["/v1/meeting/system"] {
		t.Fatal(p.fleet.paths())
	}
}

func TestMeetingPipelineForgedTranscriptCountsAsGaps(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	transcript := liveLines(t, 1, liveBatch{JobID: "m1", Channel: "mic", Session: "a", From: 0, To: 20, Words: []liveWord{{"hello", 0.5, 0.9}}})
	forged := []byte(strings.Replace(string(transcript), "hello", "forged", 1))
	p := newPipelineFixture(t, forged)
	segments, _ := p.run(t)
	for _, s := range segments {
		if strings.Contains(strings.ToLower(s.Text), "forged") {
			t.Fatal("forged text reached the meeting", describe(segments))
		}
	}
	mic := false
	for _, call := range p.fleet.paths() {
		mic = mic || strings.HasPrefix(call, "/v1/meeting/mic?0.00-")
	}
	if !mic {
		t.Fatal("the channel with only forged coverage was not re-transcribed", p.fleet.paths())
	}
}

func TestMeetingDescriptionInformsOnlyTheSummary(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	transcript := liveLines(t, 1,
		liveBatch{JobID: "m1", Channel: "mic", Session: "a", From: 0, To: 20, Words: []liveWord{{"hello", 0.5, 0.9}}},
		liveBatch{JobID: "m1", Channel: "system", Session: "b", From: 0, To: 20, Words: []liveWord{{"welcome", 1.0, 1.5}}},
	)
	p := newPipelineFixture(t, transcript)
	if _, err := p.j.db.Exec(`UPDATE meeting_jobs SET description='Q4 planning with the design team' WHERE job_key=?`, p.key); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var summaryInputs, enhanceInputs []string
	inner := meetingLLM
	meetingLLM = func(ctx context.Context, user int64, system, prompt string) (string, error) {
		mu.Lock()
		if system == enhancePrompt {
			enhanceInputs = append(enhanceInputs, prompt)
		} else {
			summaryInputs = append(summaryInputs, prompt)
		}
		mu.Unlock()
		return inner(ctx, user, system, prompt)
	}
	p.run(t)
	if len(summaryInputs) != 1 || !strings.HasPrefix(summaryInputs[0], "Meeting description (written by the organiser before the meeting; background only):\nQ4 planning with the design team\n\nTranscript:\n") {
		t.Fatal(summaryInputs)
	}
	for _, input := range enhanceInputs {
		if strings.Contains(input, "Q4 planning") {
			t.Fatal("the description reached transcript enhancement")
		}
	}
	meeting, err := p.j.app.DB.GetMeeting(p.meeting)
	if err != nil || meeting.Description != "Q4 planning with the design team" {
		t.Fatal("description not saved with the meeting", meeting, err)
	}
}
