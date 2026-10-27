package snippets

import (
	"context"
	"os"
	"testing"
	"time"

	"speektome-server/vocabularydecision"
)

// TestJevAcceptance asks the real decision model about typical dictations.
// It runs only with SPEEKTOME_JEV_ACCEPTANCE=1 and TYPESAFE_API_KEY set.
func TestJevAcceptance(t *testing.T) {
	if os.Getenv("SPEEKTOME_JEV_ACCEPTANCE") != "1" {
		t.Skip("set SPEEKTOME_JEV_ACCEPTANCE=1 to call the decision model")
	}
	client := vocabularydecision.FromEnv()
	client.Timeout = 3 * time.Second
	snippets := []Snippet{{ID: "addr", Name: "my address"}, {ID: "sig", Name: "sign off"}, {ID: "zoom", Name: "zoom link"}}
	cases := map[string]bool{
		"I will send you my address on email.":                           false,
		"Can you remind me what my address is on the form?":              false,
		"Please ship the package to the following. My address.":          true,
		"Thanks for the help today, talk soon. Sign off.":                true,
		"I need to sign off on the budget before Friday.":                false,
		"Here is the link for tomorrow's call zoom link":                 true,
		"The zoom link didn't work for me yesterday, can you resend it?": false,
		"Hi Priya, you can deliver it to insert my address":              true,
	}
	failures := 0
	for text, want := range cases {
		decisions, _ := NewResolver(client, snippets).Finish(context.Background(), text)
		if len(decisions) != 1 {
			t.Fatalf("%q: %d occurrences", text, len(decisions))
		}
		d := decisions[0]
		t.Logf("%-70q insert=%v source=%s fallback=%s", text, d.Insert, d.Source, d.Fallback)
		if d.Source != "jev" {
			t.Errorf("%q: no model answer (%s)", text, d.Fallback)
		}
		if d.Insert != want {
			failures++
		}
	}
	if failures > 1 {
		t.Errorf("%d of %d decisions wrong", failures, len(cases))
	}
}
