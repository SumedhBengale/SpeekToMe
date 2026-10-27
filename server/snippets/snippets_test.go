package snippets

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"speektome-server/vocabularydecision"
)

var list = []Snippet{{ID: "addr", Name: "my address"}, {ID: "work", Name: "My work address"}, {ID: "sig", Name: "sign-off"}}

func found(text string) []string {
	var out []string
	for _, o := range NewMatcher(list).Find(text) {
		out = append(out, o.SnippetID+"="+text[o.CommandStart:o.End])
	}
	return out
}

func TestFindMatchesWholeWordsAndLongestName(t *testing.T) {
	cases := map[string][]string{
		"My address.":                           {"addr=My address."},
		"Ship it to my work address, please":    {"work=my work address"},
		"insert my address":                     {"addr=insert my address"},
		"I'm at my addresses":                   nil,
		"Tell my. Address book":                 nil,
		"Thanks. Sign off.":                     {"sig=Sign off."},
		"thanks\nsign-off":                      {"sig=sign-off"},
		"my address and my address":             {"addr=my address", "addr=my address"},
		"MY ADDRESS?":                           {"addr=MY ADDRESS?"},
		"paste my work address then my address": {"work=paste my work address", "addr=my address"},
	}
	for text, want := range cases {
		got := found(text)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

func TestNormalizeAndSentenceRules(t *testing.T) {
	if Normalize("  My   Work-Address! ") != "my work address" || WordCount("it's mine") != 2 {
		t.Fatal(Normalize("  My   Work-Address! "), WordCount("it's mine"))
	}
	m := NewMatcher(list)
	for text, want := range map[string][2]bool{
		"My address.":                          {true, true},
		"Hi Sam. My address.":                  {false, true},
		"Hi Sam, here it is: my address":       {false, true},
		"I will send you my address by email.": {false, false},
		"Please send it to my address":         {false, false},
		"Insert my address.":                   {true, true},
	} {
		o := m.Find(text)[0]
		if Alone(text, o) != want[0] || Standalone(text, o) != want[1] {
			t.Errorf("%q: alone=%v standalone=%v, want %v", text, Alone(text, o), Standalone(text, o), want)
		}
	}
}

// fakeJev answers with a fixed choice per snippet name in the question, and
// records how many questions it was asked.
type fakeJev struct {
	mu     sync.Mutex
	insert map[string]bool
	fail   string
	asked  int
	block  chan struct{}
}

func (f *fakeJev) Choose(ctx context.Context, state, questions map[string]any, keep, use string) (map[string]vocabularydecision.Answer, float64, string) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked += len(questions)
	if keep != "keep_words" || use != "insert_snippet" {
		panic("wrong options")
	}
	if f.fail != "" {
		return nil, 1, f.fail
	}
	out := map[string]vocabularydecision.Answer{}
	for id, q := range questions {
		instructions := q.(map[string]any)["instructions"].(map[string]any)
		if !strings.Contains(instructions["context"].(string), "[TARGET: ") {
			panic("no target")
		}
		out[id] = vocabularydecision.Answer{Use: f.insert[instructions["words_reading"].(string)]}
	}
	return out, 5, ""
}

func TestFinishAsksOnlyWhenNeededAndFallsBackSafely(t *testing.T) {
	jev := &fakeJev{insert: map[string]bool{"Here you go. my address": true}}
	r := NewResolver(jev, list)
	decisions, _ := r.Finish(context.Background(), "my address")
	if len(decisions) != 1 || !decisions[0].Insert || decisions[0].Source != "alone" || jev.asked != 0 {
		t.Fatalf("alone: %+v asked=%d", decisions, jev.asked)
	}
	r = NewResolver(jev, list)
	decisions, _ = r.Finish(context.Background(), "Here you go. my address")
	if len(decisions) != 1 || !decisions[0].Insert || decisions[0].Source != "jev" {
		t.Fatalf("jev insert: %+v", decisions)
	}
	r = NewResolver(jev, list)
	decisions, _ = r.Finish(context.Background(), "I will send you my address by email")
	if len(decisions) != 1 || decisions[0].Insert {
		t.Fatalf("jev keep: %+v", decisions)
	}

	down := &fakeJev{fail: "timeout"}
	for text, want := range map[string]bool{
		"I will send you my address by email": false,
		"Here you go. My address.":            true,
		"Here you go, insert my address":      true,
	} {
		decisions, _ = NewResolver(down, list).Finish(context.Background(), text)
		if len(decisions) != 1 || decisions[0].Insert != want || decisions[0].Source != "sentence_rule" || decisions[0].Fallback != "timeout" {
			t.Errorf("fallback %q: %+v", text, decisions)
		}
	}
	if NewResolver(jev, nil) != nil {
		t.Fatal("resolver without snippets")
	}
	var none *Resolver
	none.Text("my address")
	if d, _ := none.Finish(context.Background(), "my address"); d != nil {
		t.Fatal("nil resolver decided")
	}
}

func TestLiveDecisionsAreReusedAtFinish(t *testing.T) {
	jev := &fakeJev{insert: map[string]bool{}}
	r := NewResolver(jev, list)
	text := "I will send you my address by email when I get home tonight"
	r.Text("I will send you my address")
	r.Text("I will send you my address by email when I get home")
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.decided)
		r.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	decisions, _ := r.Finish(context.Background(), text)
	if len(decisions) != 1 || decisions[0].Insert || !decisions[0].Live || jev.asked != 1 {
		t.Fatalf("%+v asked=%d", decisions, jev.asked)
	}
	// A revised transcript asks again rather than reusing a stale decision.
	r = NewResolver(jev, list)
	r.Text("I will send you my address by email when I get home")
	time.Sleep(50 * time.Millisecond)
	decisions, _ = r.Finish(context.Background(), "I will send you, my address! by email when I get home")
	if len(decisions) != 1 || decisions[0].Live {
		t.Fatalf("stale reuse: %+v", decisions)
	}
}

func TestFinishWaitsForLiveCallInFlight(t *testing.T) {
	jev := &fakeJev{insert: map[string]bool{}, block: make(chan struct{})}
	r := NewResolver(jev, list)
	r.Text("I will send you my address by email when I get home")
	time.Sleep(20 * time.Millisecond)
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(jev.block)
	}()
	decisions, _ := r.Finish(context.Background(), "I will send you my address by email when I get home")
	if len(decisions) != 1 || !decisions[0].Live || jev.asked != 1 {
		t.Fatalf("%+v asked=%d", decisions, jev.asked)
	}
}
