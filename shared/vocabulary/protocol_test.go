package vocabulary

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestFinalPassOffsetsAndContextIndependentChoices(t *testing.T) {
	terms := []string{"PostgreSQL", "SpeekToMe"}
	compiled, e := Compile(terms)
	if e != nil {
		t.Fatal(e)
	}
	raw := "é  postgrsq,\t" + strings.Repeat("ordinary words ", 20) + "why don't you speak to me? SpeekToMe."
	r := Propose(context.Background(), raw, compiled, TermsHash(terms))
	if e = r.Validate(raw, terms); e != nil {
		t.Fatal(e)
	}
	t.Logf("%+v", r.Choices)
	found := false
	for _, c := range r.Choices {
		if c.Original == "postgrsq" && c.Replacement == "PostgreSQL" {
			found = true
		}
	}
	if !found {
		t.Fatal("early fuzzy match lost outside trailing window")
	}
	selected := map[string]bool{}
	for _, c := range r.Choices {
		selected[c.ID] = c.Replacement == "PostgreSQL"
	}
	out, e := Apply(raw, r.Choices, selected)
	if e != nil {
		t.Fatal(e)
	}
	if out != strings.Replace(raw, "postgrsq", "PostgreSQL", 1) {
		t.Fatalf("whitespace or rejected phrase changed: %q", out)
	}
}
func TestInvalidAndOverlappingEdits(t *testing.T) {
	for _, c := range [][]Choice{{{ID: "a", StartByte: 0, EndByte: 1, Original: "x", Replacement: "y"}}, {{ID: "a", StartByte: 0, EndByte: 2, Original: "é", Replacement: "x"}, {ID: "b", StartByte: 0, EndByte: 2, Original: "é", Replacement: "y"}}} {
		if _, e := Apply("é", c, nil); e == nil {
			t.Fatal("invalid edit accepted")
		}
	}
}
func TestBoundsAndCancellation(t *testing.T) {
	if _, e := Compile(make([]string, 1001)); e == nil {
		t.Fatal("1001 accepted")
	}
	v, _ := Compile([]string{"PostgreSQL"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Propose(ctx, "postgrsq", v, "")
	if r.Error != "matcher_timeout" || r.Enhanced != "postgrsq" || len(r.Choices) != 0 {
		t.Fatalf("partial edits escaped cancellation: %+v", r)
	}
}
func BenchmarkFinalHour1000Terms(b *testing.B) {
	terms := []string{"PostgreSQL", "SpeekToMe"}
	for i := 2; i < 1000; i++ {
		terms = append(terms, fmt.Sprintf("AcmeService%d", i))
	}
	v, _ := Compile(terms)
	raw := strings.Repeat("We are using postgrsq as our database. Why don't you speak to me about the next meeting? ", 500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := Propose(context.Background(), raw, v, TermsHash(terms))
		if r.Error != "" {
			b.Fatal(r.Error)
		}
	}
}

func TestFinalMatchesDoNotCrossSentenceOrSpeakerBoundaries(t *testing.T) {
	terms := []string{"DailyMeeting"}
	v, _ := Compile(terms)
	for _, separator := range []string{"\n", ". ", "! ", "; "} {
		raw := "Daily" + separator + "Meeting"
		r := Propose(context.Background(), raw, v, TermsHash(terms))
		if !strings.Contains(r.Enhanced, separator) {
			t.Fatalf("lost boundary: %q -> %q", raw, r.Enhanced)
		}
		for _, c := range r.Choices {
			if c.Original == raw {
				t.Fatalf("choice crossed boundary: %+v", c)
			}
		}
	}
}
