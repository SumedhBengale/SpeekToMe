package vocabularydecision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"speektome/vocabulary"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func proposal(t *testing.T) vocabulary.Result {
	t.Helper()
	v, _ := vocabulary.Compile([]string{"SpeekToMe", "PostgreSQL"})
	return vocabulary.Propose(context.Background(), "Why don't you speak to me? We use postgrsq as our database.", v, vocabulary.TermsHash([]string{"SpeekToMe", "PostgreSQL"}))
}
func TestOneRequestMultipleOccurrenceDecisions(t *testing.T) {
	r := proposal(t)
	if len(r.Choices) != 2 {
		t.Fatalf("expected two choices: %+v", r.Choices)
	}
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		calls.Add(1)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		json.NewDecoder(q.Body).Decode(&body)
		if len(body.Questions) != 2 {
			t.Error("questions not batched")
		}
		a := map[string]any{}
		for _, c := range r.Choices {
			choice := "keep_original"
			p := .99
			if c.Replacement == "PostgreSQL" {
				choice = "use_vocabulary"
				p = .01
			}
			a[c.ID] = map[string]any{"type": "choice", "choice": choice, "confidence": .99, "probabilities": map[string]float64{"keep_original": p, "use_vocabulary": 1 - p}}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": a})
	}))
	defer s.Close()
	c := FromEnv()
	c.Key = "test"
	c.URL = s.URL
	a := c.Evaluate(context.Background(), r, "worker")
	if a.Resolved != "Why don't you speak to me? We use PostgreSQL as our database." || calls.Load() != 1 {
		t.Fatalf("bad result: %+v", a)
	}
	input, restore := a.Protect()
	if strings.Contains(input, "PostgreSQL") || strings.Contains(input, "speak to me") {
		t.Fatal("unprotected choice")
	}
	if restore(input) != a.Resolved {
		t.Fatal("restore failed")
	}
	if restore("changed all tokens") != a.Resolved || !a.GrammarFallback {
		t.Fatal("grammar changed resolved vocabulary")
	}
}
func TestTimeoutRateLimitMissingAnswers(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		delay  time.Duration
		reason string
	}{{"timeout", 200, 80 * time.Millisecond, "timeout"}, {"rate", 429, 0, "http_429"}, {"overloaded", 529, 0, "http_529"}, {"invalid", 200, 0, "missing_or_invalid_answer"}} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				time.Sleep(test.delay)
				w.WriteHeader(test.status)
				w.Write([]byte(`{"answers":{}}`))
			}))
			defer s.Close()
			c := FromEnv()
			c.Key = "test"
			c.URL = s.URL
			c.Timeout = 20 * time.Millisecond
			r := proposal(t)
			start := time.Now()
			a := c.Evaluate(context.Background(), r, "worker")
			if time.Since(start) > 60*time.Millisecond {
				t.Error("fallback waited")
			}
			if a.Resolved != r.Enhanced || calls.Load() != 1 {
				t.Fatal("fallback lost worker choices or retried")
			}
			for _, d := range a.Decisions {
				if d.Fallback != test.reason {
					t.Fatalf("wrong fallback: %+v", d)
				}
			}
			if test.status == 429 || test.status == 529 {
				a = c.Evaluate(context.Background(), r, "worker")
				if calls.Load() != 1 || a.Decisions[0].Fallback != "rate_limit_cooldown" {
					t.Fatal("cooldown waited/retried")
				}
			}
		})
	}
}
func TestOversizedBatchFallsBackWithoutRequest(t *testing.T) {
	r := proposal(t)
	base := r.Choices[0]
	r.Choices = make([]vocabulary.Choice, 1000)
	for i := range r.Choices {
		r.Choices[i] = base
		r.Choices[i].ID = strings.Repeat("x", i+1)
	}
	c := FromEnv()
	c.Key = "test"
	a := c.Evaluate(context.Background(), r, "worker")
	if a.Decisions[0].Fallback != "context_budget" {
		t.Fatal("oversized request attempted")
	}
}

func TestAlreadyCorrectVocabularySkipsJevAndSurvivesGrammar(t *testing.T) {
	terms := []string{"PostgreSQL"}
	v, _ := vocabulary.Compile(terms)
	r := vocabulary.Propose(context.Background(), "PostgreSQL and PostgreSQL", v, vocabulary.TermsHash(terms))
	c := FromEnv()
	c.Key = "test"
	c.URL = ":invalid"
	a := c.Evaluate(context.Background(), r, "worker")
	if len(a.Decisions) != 2 || a.Decisions[0].Source != "asr" || a.LatencyMillis != 0 {
		t.Fatalf("identity matches queried Jev: %+v", a)
	}
	input, restore := a.Protect()
	if restore(input) != a.Resolved {
		t.Fatal("identity spelling lost")
	}
	words := strings.Fields(input)
	if restore(words[2]+" and "+words[0]) != a.Resolved || !a.GrammarFallback {
		t.Fatal("reordered choices accepted")
	}
}
func TestLocalCapacityFallsBackWithoutWaiting(t *testing.T) {
	c := FromEnv()
	c.Key = "test"
	c.slots = make(chan struct{}, 1)
	c.slots <- struct{}{}
	a := c.Evaluate(context.Background(), proposal(t), "worker")
	if a.Decisions[0].Fallback != "concurrency_limit" {
		t.Fatalf("blocked on local capacity: %+v", a)
	}
}
