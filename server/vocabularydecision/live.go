package vocabularydecision

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"speektome/vocabulary"
)

// Live decides a stream's vocabulary proposals while the user is still
// speaking. A proposal is shown as its original text until Jev decides it,
// which happens once enough words follow it or it has waited long enough.
// Finish then only waits for the proposals that are left.
type Live struct {
	client       *Client
	contextWords int
	maxWait      time.Duration
	onChange     func()

	mu       sync.Mutex
	text     string
	current  map[string]bool // proposals in the worker's latest update
	pending  map[string]pendingChoice
	decided  map[string]Decision
	inflight chan struct{} // closed when the current Jev call returns
	asked    []vocabulary.Choice // proposals in the current Jev call
	timer    *time.Timer
	closed   bool
	millis   float64
	model    string
}

type pendingChoice struct {
	vocabulary.Choice
	seen time.Time
}

// NewLive starts a resolver. onChange runs after each batch of decisions,
// outside the resolver's lock, so the caller can refresh the preview.
func NewLive(client *Client, onChange func()) *Live {
	words := envInt("TYPESAFE_LIVE_CONTEXT_WORDS", 6)
	if words > 50 {
		words = 6
	}
	wait := envInt("TYPESAFE_LIVE_MAX_WAIT_MS", 2000)
	if wait > 10000 {
		wait = 2000
	}
	return &Live{client: client, contextWords: words, maxWait: time.Duration(wait) * time.Millisecond, onChange: onChange, pending: map[string]pendingChoice{}, decided: map[string]Decision{}}
}

// Propose replaces the live proposals with the worker's latest set. Its
// offsets refer to the confirmed transcript, which only grows.
func (l *Live) Propose(r vocabulary.Result) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.current = map[string]bool{}
	for _, c := range r.Choices {
		key := choiceKey(c)
		l.current[key] = true
		if c.Original == c.Replacement {
			continue
		}
		if _, done := l.decided[key]; done {
			continue
		}
		if _, waiting := l.pending[key]; !waiting {
			l.pending[key] = pendingChoice{c, time.Now()}
		}
	}
	for key := range l.pending {
		if !l.current[key] {
			delete(l.pending, key)
		}
	}
	l.schedule()
}

// Text records the latest transcript preview: confirmed words, then any
// unconfirmed partial words.
func (l *Live) Text(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.text = text
	l.schedule()
}

// Span is a UTF-8 byte range in preview text.
type Span struct {
	Start int `json:"start_byte"`
	End   int `json:"end_byte"`
}

// Display returns the latest preview with every decided replacement applied.
// Undecided proposals keep the words the speaker's ASR produced.
func (l *Live) Display() string {
	text, _, _ := l.Preview()
	return text
}

// Preview is Display plus where each applied replacement sits in the text,
// and where the words still waiting for a decision sit, so clients can mark
// the words a decision is changing and has changed.
func (l *Live) Preview() (text string, replaced, deciding []Span) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var use []vocabulary.Choice
	for key, d := range l.decided {
		if d.UseVocabulary && l.current[key] && fits(l.text, d.Choice) {
			use = append(use, d.Choice)
		}
	}
	sortWithIDs(use)
	text, e := vocabulary.Apply(l.text, use, nil)
	if e != nil {
		return l.text, nil, nil
	}
	// at maps a byte offset in l.text outside every replacement to the preview.
	at := func(offset int) int {
		shift := 0
		for _, c := range use {
			if offset >= c.EndByte {
				shift += len(c.Replacement) - len(c.Original)
			}
		}
		return offset + shift
	}
	for _, c := range use {
		start := at(c.StartByte)
		replaced = append(replaced, Span{start, start + len(c.Replacement)})
	}
	applied := func(p vocabulary.Choice) bool {
		for _, c := range use {
			if p.StartByte < c.EndByte && c.StartByte < p.EndByte {
				return true
			}
		}
		return false
	}
	var waiting []vocabulary.Choice
	for _, p := range l.pending {
		waiting = append(waiting, p.Choice)
	}
	for _, c := range l.asked {
		if l.current[choiceKey(c)] {
			waiting = append(waiting, c)
		}
	}
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].StartByte < waiting[j].StartByte })
	for _, c := range waiting {
		if !fits(l.text, c) || applied(c) {
			continue
		}
		start, end := at(c.StartByte), at(c.EndByte)
		if overlaps(deciding, start) {
			continue
		}
		deciding = append(deciding, Span{start, end})
	}
	return text, replaced, deciding
}

func overlaps(spans []Span, start int) bool {
	return len(spans) > 0 && start < spans[len(spans)-1].End
}

// Close stops starting Jev calls, for a stream that ends without Finish.
func (l *Live) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.timer != nil {
		l.timer.Stop()
	}
}

// Finish stops live work, waits for a call already in flight, and decides
// the final proposals, reusing every live decision for the same span.
func (l *Live) Finish(ctx context.Context, r vocabulary.Result, source string) Audit {
	l.Close()
	l.mu.Lock()
	inflight := l.inflight
	l.mu.Unlock()
	if inflight != nil {
		select {
		case <-inflight:
		case <-ctx.Done():
		}
	}
	l.mu.Lock()
	known := make(map[string]Decision, len(l.decided))
	for key, d := range l.decided {
		known[key] = d
	}
	millis, model := l.millis, l.model
	l.mu.Unlock()
	a := l.client.evaluate(ctx, r, source, known)
	a.LiveMillis = millis
	if a.Model == "" && a.LiveDecisions > 0 {
		a.Model = model
	}
	return a
}

func sortWithIDs(choices []vocabulary.Choice) {
	sort.Slice(choices, func(i, j int) bool { return choices[i].StartByte < choices[j].StartByte })
	for i := range choices {
		choices[i].ID = "live" + strconv.Itoa(i)
	}
}

func fits(text string, c vocabulary.Choice) bool {
	return c.EndByte <= len(text) && text[c.StartByte:c.EndByte] == c.Original
}

// schedule sends every ready proposal to Jev in one call, or arms a timer
// for the earliest one still waiting. One call runs at a time. Requires l.mu.
func (l *Live) schedule() {
	if l.closed || l.inflight != nil || len(l.pending) == 0 {
		return
	}
	var ready []vocabulary.Choice
	var wait time.Duration
	for key, p := range l.pending {
		if !fits(l.text, p.Choice) {
			continue
		}
		left := l.maxWait - time.Since(p.seen)
		if left <= 0 || len(strings.Fields(l.text[p.EndByte:])) >= l.contextWords {
			ready = append(ready, p.Choice)
			delete(l.pending, key)
		} else if wait == 0 || left < wait {
			wait = left
		}
	}
	if len(ready) == 0 {
		if wait > 0 && l.timer == nil {
			l.timer = time.AfterFunc(wait, func() {
				l.mu.Lock()
				defer l.mu.Unlock()
				l.timer = nil
				l.schedule()
			})
		}
		return
	}
	// Worker IDs restart with each update, so a batch gets its own.
	sortWithIDs(ready)
	done := make(chan struct{})
	l.inflight = done
	l.asked = ready
	text := l.text
	go l.decide(text, ready, done)
}

func (l *Live) decide(text string, ready []vocabulary.Choice, done chan struct{}) {
	verdicts, model, _, latency, reason := l.client.judge(context.Background(), text, ready)
	l.mu.Lock()
	for _, c := range ready {
		d := Decision{Choice: c, UseVocabulary: true, Source: "jev"}
		if v, ok := verdicts[c.ID]; ok {
			d.UseVocabulary, d.Confidence = v.use, v.confidence
		} else {
			d.Source = "matcher"
			d.Fallback = reason
			if reason == "" {
				d.Fallback = "missing_or_invalid_answer"
			}
		}
		l.decided[choiceKey(c)] = d
	}
	l.millis += latency
	if model != "" {
		l.model = model
	}
	l.inflight = nil
	l.asked = nil
	close(done)
	l.schedule()
	l.mu.Unlock()
	if l.onChange != nil {
		l.onChange()
	}
}
