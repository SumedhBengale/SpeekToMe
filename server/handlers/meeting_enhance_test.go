package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"speektome-server/db"
)

func lockedTarget() map[string]protectedTurn {
	turn := &pipelineTurn{Words: []pipelineWord{{Word: "we"}, {Word: "ship"}, {Word: "Kubernetes", Locked: true}, {Word: "on"}, {Word: "march"}, {Word: "3"}}}
	counter := 0
	return map[string]protectedTurn{"t0": protectTurn(turn, "STMLOCK", &counter)}
}

func TestEnhancementValidationAcceptsPolishAndRestoresTerms(t *testing.T) {
	targets := lockedTarget()
	if targets["t0"].text != "we ship STMLOCK0END on march 3" {
		t.Fatal(targets["t0"].text)
	}
	got, err := validateEnhancement("```json\n"+`{"turns":[{"id":"t0","text":"We ship STMLOCK0END on March 3."}]}`+"\n```", targets, "STMLOCK")
	if err != nil || got["t0"] != "We ship Kubernetes on March 3." {
		t.Fatal(got, err)
	}
}

func TestEnhancementValidationRejectsUnsafeReplies(t *testing.T) {
	for name, reply := range map[string]string{
		"malformed":        `{"turns":[{"id":"t0","text":"We ship`,
		"missing turn":     `{"turns":[]}`,
		"unknown turn":     `{"turns":[{"id":"t9","text":"We ship STMLOCK0END on March 3."}]}`,
		"repeated turn":    `{"turns":[{"id":"t0","text":"We ship STMLOCK0END on March 3."},{"id":"t0","text":"x"}]}`,
		"empty text":       `{"turns":[{"id":"t0","text":"  "}]}`,
		"term dropped":     `{"turns":[{"id":"t0","text":"We ship Kubernetes on March 3."}]}`,
		"term duplicated":  `{"turns":[{"id":"t0","text":"We ship STMLOCK0END STMLOCK0END on March 3."}]}`,
		"term invented":    `{"turns":[{"id":"t0","text":"We ship STMLOCK0END on March 3 with STMLOCK1END."}]}`,
		"number changed":   `{"turns":[{"id":"t0","text":"We ship STMLOCK0END on March 4."}]}`,
		"content replaced": `{"turns":[{"id":"t0","text":"Sure, here is a summary STMLOCK0END 3 of the meeting you asked about."}]}`,
		"answer appended":  `{"turns":[{"id":"t0","text":"We ship STMLOCK0END on March 3. That sounds like a great plan, and here are five more ideas for you."}]}`,
	} {
		if _, err := validateEnhancement(reply, lockedTarget(), "STMLOCK"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEnhancementRetriesThenKeepsOriginalText(t *testing.T) {
	previous := meetingLLM
	defer func() { meetingLLM = previous }()
	var calls atomic.Int32
	meetingLLM = func(context.Context, int64, string, string) (string, error) {
		if calls.Add(1) == 1 {
			return "", errors.New("provider unavailable")
		}
		return `not json`, nil
	}
	run := &meetingRun{llmSlots: make(chan struct{}, 4)}
	turns := buildTurns("mic", []pipelineWord{{Word: "keep", Start: 0, End: 0.3}, {Word: "this", Start: 0.4, End: 0.6}}, nil, "Owner")
	run.enhance(context.Background(), turns)
	if turns[0].Text != "keep this" || calls.Load() != enhanceAttempts || run.llmFallbacks.Load() != 1 {
		t.Fatal(turns[0].Text, calls.Load(), run.llmFallbacks.Load())
	}
}

func TestEnhancementChunksWholeTurnsWithReadOnlyContext(t *testing.T) {
	previous := meetingLLM
	defer func() { meetingLLM = previous }()
	var mu sync.Mutex
	var requests []map[string][]map[string]string
	meetingLLM = func(_ context.Context, _ int64, _, prompt string) (string, error) {
		var request map[string][]map[string]string
		json.Unmarshal([]byte(prompt), &request)
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		var reply struct {
			Turns []map[string]string `json:"turns"`
		}
		for _, turn := range request["turns"] {
			reply.Turns = append(reply.Turns, map[string]string{"id": turn["id"], "text": turn["text"]})
		}
		data, _ := json.Marshal(reply)
		return string(data), nil
	}
	// Ten turns of 100 words each, separated by pauses.
	var words []pipelineWord
	for turn := 0; turn < 10; turn++ {
		for i := 0; i < 100; i++ {
			start := float64(turn*200) + float64(i)*0.5
			words = append(words, pipelineWord{Word: "word", Start: start, End: start + 0.3})
		}
	}
	turns := buildTurns("mic", words, nil, "Owner")
	if len(turns) != 10 {
		t.Fatal(len(turns))
	}
	run := &meetingRun{llmSlots: make(chan struct{}, 4)}
	run.enhance(context.Background(), turns)
	edited := 0
	for _, r := range requests {
		n := len(r["turns"])
		edited += n
		if n < 1 || n > 3 || len(r["context_before"]) > enhanceContextTurns || len(r["context_after"]) > enhanceContextTurns {
			t.Fatalf("chunk of %d turns, context %d/%d", n, len(r["context_before"]), len(r["context_after"]))
		}
		for _, c := range append(r["context_before"], r["context_after"]...) {
			if c["id"] != "" {
				t.Fatal("context turns must not carry editable ids")
			}
		}
	}
	if edited != 10 || len(requests) < 4 {
		t.Fatalf("%d turns edited across %d chunks", edited, len(requests))
	}
}

func TestBuildTurnsSplitsLongMonologues(t *testing.T) {
	var words []pipelineWord
	for i := 0; i < 600; i++ {
		words = append(words, pipelineWord{Word: "w", Start: float64(i) * 0.3, End: float64(i)*0.3 + 0.2})
	}
	turns := buildTurns("mic", words, nil, "Owner")
	total := 0
	for _, turn := range turns {
		if len(turn.Words) > turnMaxWords {
			t.Fatal(len(turn.Words))
		}
		total += len(turn.Words)
	}
	if total != 600 || len(turns) < 3 {
		t.Fatal(total, len(turns))
	}
}

func TestVocabularyResolutionKeepsWordTimesAndLocksTerms(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "") // the matcher's choice stands without Jev
	vocab := newMeetingVocabulary([]db.VocabularyTerm{{Canonical: "Kubernetes"}})
	words := []pipelineWord{{"we", 0, 0.2, false}, {"deploy", 0.3, 0.6, false}, {"to", 0.7, 0.8, false}, {"kubernetes", 0.9, 1.6, false}, {"today", 1.7, 2.0, false}}
	resolved, audits := vocab.resolve(context.Background(), &meetingRun{}, words)
	if len(audits) != 1 {
		t.Fatalf("expected one audited section, got %d", len(audits))
	}
	var term *pipelineWord
	for i := range resolved {
		if resolved[i].Locked {
			term = &resolved[i]
		}
	}
	if term == nil || term.Word != "Kubernetes" || term.Start != 0.9 || term.End != 1.6 || len(resolved) != len(words) {
		t.Fatalf("%+v", resolved)
	}
}

func TestMicEnhancementDoesNotWaitForDiarization(t *testing.T) {
	t.Setenv("SPEEKTOME_LIVE_TRANSCRIPT_KEY", testLiveKey)
	transcript := liveLines(t, 1,
		liveBatch{JobID: "m1", Channel: "mic", Session: "a", From: 0, To: 20, Words: []liveWord{{"hello", 0.5, 0.9}}},
		liveBatch{JobID: "m1", Channel: "system", Session: "b", From: 0, To: 20, Words: []liveWord{{"welcome", 1.0, 1.5}}},
	)
	p := newPipelineFixture(t, transcript)
	// The diarizer accepts the upload only after the microphone's turns were
	// enhanced. A pipeline that waited for diarization first would stall until
	// the escape timer, and fail below.
	hold := make(chan struct{})
	p.diarizer.hold = hold
	var micFirst atomic.Bool
	var once sync.Once
	inner := meetingLLM
	meetingLLM = func(ctx context.Context, user int64, system, prompt string) (string, error) {
		if system == enhancePrompt && strings.Contains(prompt, "hello") {
			once.Do(func() { micFirst.Store(true); close(hold) })
		}
		return inner(ctx, user, system, prompt)
	}
	go func() {
		time.Sleep(5 * time.Second)
		once.Do(func() { close(hold) })
	}()
	segments, _ := p.run(t)
	if !micFirst.Load() {
		t.Fatal("microphone enhancement waited for diarization")
	}
	if len(segments) != 2 {
		t.Fatal(describe(segments))
	}
}

func TestLongMeetingsAreSummarizedInSections(t *testing.T) {
	previous := meetingLLM
	defer func() { meetingLLM = previous }()
	var mu sync.Mutex
	prompts := map[string]int{}
	meetingLLM = func(_ context.Context, _ int64, system, _ string) (string, error) {
		mu.Lock()
		prompts[system]++
		mu.Unlock()
		return `{"summary":"notes","action_items":"- Owner: task"}`, nil
	}
	// About 90 minutes of continuous speech: 15,000 words.
	var turns []*pipelineTurn
	for i := 0; i < 150; i++ {
		start := float64(i) * 36
		words := make([]pipelineWord, 100)
		turns = append(turns, &pipelineTurn{SpeakerName: "Owner", Start: start, End: start + 30, Words: words, Text: "text"})
	}
	run := &meetingRun{}
	summary, actions := run.summarize(context.Background(), turns)
	if summary != "notes" || actions != "- Owner: task" {
		t.Fatal(summary, actions)
	}
	if prompts[sectionSummaryPrompt] != 3 || prompts[combinePrompt] != 1 || prompts[summaryPrompt] != 0 {
		t.Fatal(prompts)
	}
}
