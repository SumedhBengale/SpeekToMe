package snippets

import (
	"context"
	"os"
	"testing"
	"time"

	"speektome-server/vocabularydecision"
)

// acceptanceCases are dictations as the recognizer may write them, with
// whether the snippet should be inserted. Most come from real test sessions.
var acceptanceCases = []struct {
	text   string
	insert bool
}{
	{"Please deliver the parcel here. My address.", true},
	{"Please deliver the parcel here my address", true},
	{"Hi Rahul, here are the details you asked for. My address.", true},
	{"Hi Rahul, here are the details you asked for my address.", true},
	{"Hi Rahul, here are the details you asked for, my address.", true},
	{"You can check out my profile here, portfolio website.", true},
	{"You can check out my profile here. Portfolio website.", true},
	{"You can check out my profile here portfolio website", true},
	{"Thanks for the help today, talk soon. Sign off.", true},
	{"Hi Priya, you can deliver it to insert my address", true},
	{"Sure, here you go: my address.", true},
	{"Looking forward to working together. Sign off.", true},
	{"Let me know if anything changes. Thanks, sign off", true},
	{"For the courier, my address", true},
	{"I will send you my address by email.", false},
	{"Please update my address in your system.", false},
	{"My portfolio website is down again.", false},
	{"Is my address on the form wrong?", false},
	{"Should I paste my address here or in the other box?", false},
	{"Did you get my address yesterday?", false},
	{"My address changed last month, so please update your records.", false},
	{"Can you confirm my address is correct on the invoice?", false},
	{"You can send it to my portfolio website.", false},
	{"You can try to enter my address in that field.", false},
	{"You can try to insert my address in that field.", false},
	{"You could do a command like, insert my address.", false},
	{"I need to sign off on the budget before Friday.", false},
	{"Have you seen my portfolio website yet?", false},
}

// TestJevAcceptance asks the real decision model about typical dictations.
// It runs only with SPEEKTOME_JEV_ACCEPTANCE=1 and TYPESAFE_API_KEY set.
func TestJevAcceptance(t *testing.T) {
	if os.Getenv("SPEEKTOME_JEV_ACCEPTANCE") != "1" {
		t.Skip("set SPEEKTOME_JEV_ACCEPTANCE=1 to call the decision model")
	}
	client := vocabularydecision.FromEnv()
	client.Timeout = 3 * time.Second
	snippets := []Snippet{{ID: "addr", Name: "my address"}, {ID: "site", Name: "portfolio website"}, {ID: "sig", Name: "sign off"}}
	wrong := 0
	for _, c := range acceptanceCases {
		decisions, _ := NewResolver(client, snippets).Finish(context.Background(), c.text)
		if len(decisions) != 1 {
			t.Fatalf("%q: %d occurrences", c.text, len(decisions))
		}
		d := decisions[0]
		mark := "ok"
		if d.Insert != c.insert {
			mark = "WRONG"
			wrong++
		}
		confidence := -1.0
		if d.Confidence != nil {
			confidence = *d.Confidence
		}
		t.Logf("%-5s insert=%-5v want=%-5v %s conf=%.2f p_insert=%.2f %q", mark, d.Insert, c.insert, d.Source, confidence, d.PInsert, c.text)
		if d.Source != "jev" && d.Source != "alone" {
			t.Errorf("%q: no model answer (%s)", c.text, d.Fallback)
		}
	}
	if wrong > 1 {
		t.Errorf("%d of %d decisions wrong", wrong, len(acceptanceCases))
	}
}
