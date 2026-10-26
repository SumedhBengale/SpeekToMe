package vocabularydecision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"speektome/vocabulary"
	"sync/atomic"
	"testing"
	"time"
)

var liveTerms = []string{"SpeekToMe", "PostgreSQL"}

func liveProposal(text string) vocabulary.Result {
	v, _ := vocabulary.Compile(liveTerms)
	return vocabulary.Propose(context.Background(), text, v, vocabulary.TermsHash(liveTerms))
}

// liveJev answers use_vocabulary for PostgreSQL and keep_original otherwise,
// counting calls and the questions in each.
func liveJev(t *testing.T, status int, delay time.Duration) (*Client, *atomic.Int32, chan int) {
	t.Helper()
	var calls atomic.Int32
	sizes := make(chan int, 16)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls.Add(1)
		time.Sleep(delay)
		var body struct {
			Questions map[string]struct {
				Instructions struct {
					Candidate string `json:"vocabulary_candidate"`
				} `json:"instructions"`
			} `json:"questions"`
		}
		json.NewDecoder(q.Body).Decode(&body)
		sizes <- len(body.Questions)
		if status != 200 {
			http.Error(w, "unavailable", status)
			return
		}
		a := map[string]any{}
		for id, question := range body.Questions {
			choice, p := "keep_original", .99
			if question.Instructions.Candidate == "PostgreSQL" {
				choice, p = "use_vocabulary", .01
			}
			a[id] = map[string]any{"type": "choice", "choice": choice, "confidence": .99, "probabilities": map[string]float64{"keep_original": p, "use_vocabulary": 1 - p}}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": a})
	}))
	t.Cleanup(s.Close)
	c := FromEnv()
	c.Key = "test"
	c.URL = s.URL
	c.Timeout = time.Second
	return c, &calls, sizes
}

func newTestLive(c *Client, words int, wait time.Duration) (*Live, chan struct{}) {
	changed := make(chan struct{}, 16)
	l := NewLive(c, func() { changed <- struct{}{} })
	l.contextWords, l.maxWait = words, wait
	return l, changed
}

func waitChange(t *testing.T, changed chan struct{}) {
	t.Helper()
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("no live decision")
	}
}

func TestLiveDecisionWaitsForContextThenUpdatesPreview(t *testing.T) {
	c, calls, sizes := liveJev(t, 200, 0)
	l, changed := newTestLive(c, 3, time.Hour)

	text := "We use postgrsq"
	l.Text(text)
	l.Propose(liveProposal(text))
	if l.Display() != text || calls.Load() != 0 {
		t.Fatalf("decided without context: %q, %d calls", l.Display(), calls.Load())
	}

	text = "We use postgrsq as our database. Why don't you"
	l.Text(text + " speak") // partial words count as context but are never matched
	waitChange(t, changed)
	if got := l.Display(); got != "We use PostgreSQL as our database. Why don't you speak" {
		t.Fatalf("preview not updated: %q", got)
	}
	if got, spans := l.Preview(); len(spans) != 1 || got[spans[0].Start:spans[0].End] != "PostgreSQL" {
		t.Fatalf("replacement span not reported: %+v in %q", spans, got)
	}

	text += " speak to me?"
	l.Text(text)
	l.Propose(liveProposal(text))
	if l.Display() != "We use PostgreSQL as our database. Why don't you speak to me?" {
		t.Fatalf("undecided proposal must show the ASR words: %q", l.Display())
	}

	a := l.Finish(context.Background(), liveProposal(text), "inference_worker")
	if a.Resolved != "We use PostgreSQL as our database. Why don't you speak to me?" || a.LiveDecisions != 1 {
		t.Fatalf("bad final audit: %+v", a)
	}
	if calls.Load() != 2 || <-sizes != 1 || <-sizes != 1 {
		t.Fatalf("each proposal must be asked exactly once: %d calls", calls.Load())
	}
	for _, d := range a.Decisions {
		if d.Source != "jev" {
			t.Fatalf("decision not from Jev: %+v", d)
		}
	}
}

func TestLiveDecisionAfterMaxWaitWithoutMoreWords(t *testing.T) {
	c, calls, _ := liveJev(t, 200, 0)
	l, changed := newTestLive(c, 50, 50*time.Millisecond)
	text := "We use postgrsq"
	l.Text(text)
	l.Propose(liveProposal(text))
	waitChange(t, changed)
	if l.Display() != "We use PostgreSQL" || calls.Load() != 1 {
		t.Fatalf("max wait did not decide: %q", l.Display())
	}
}

func TestLiveFinishWaitsForCallInFlight(t *testing.T) {
	c, calls, _ := liveJev(t, 200, 100*time.Millisecond)
	l, _ := newTestLive(c, 1, time.Hour)
	text := "We use postgrsq today"
	l.Propose(liveProposal(text))
	l.Text(text)
	time.Sleep(10 * time.Millisecond)
	a := l.Finish(context.Background(), liveProposal(text), "inference_worker")
	if a.Resolved != "We use PostgreSQL today" || a.LiveDecisions != 1 || calls.Load() != 1 {
		t.Fatalf("finish must reuse the in-flight decision: %+v, %d calls", a, calls.Load())
	}
}

func TestLiveFailureFallsBackWithoutRetry(t *testing.T) {
	c, calls, _ := liveJev(t, 500, 0)
	l, changed := newTestLive(c, 1, time.Hour)
	text := "We use postgrsq today"
	l.Propose(liveProposal(text))
	l.Text(text)
	waitChange(t, changed)
	a := l.Finish(context.Background(), liveProposal(text), "inference_worker")
	if a.Resolved != "We use PostgreSQL today" || calls.Load() != 1 || a.Decisions[0].Fallback != "http_500" {
		t.Fatalf("failed live call must fall back once: %+v, %d calls", a, calls.Load())
	}
}

func TestLiveNewProposalsAtFinishAreAskedTogether(t *testing.T) {
	c, calls, sizes := liveJev(t, 200, 0)
	l, _ := newTestLive(c, 50, time.Hour)
	text := "Why don't you speak to me? We use postgrsq"
	l.Text(text)
	l.Propose(liveProposal(text))
	a := l.Finish(context.Background(), liveProposal(text), "inference_worker")
	if a.Resolved != "Why don't you speak to me? We use PostgreSQL" || calls.Load() != 1 || <-sizes != 2 || a.LiveDecisions != 0 {
		t.Fatalf("pending proposals must be decided in one final call: %+v", a)
	}
}
