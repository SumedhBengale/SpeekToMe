package vocabularydecision

import (
	"strings"
	"testing"

	"speektome/vocabulary"
)

func TestProtectWithLocksSnippetsAndVocabulary(t *testing.T) {
	raw := "send it to jason at my address."
	a := Audit{Proposal: vocabulary.Result{Raw: raw}, Resolved: "send it to Jayson at my address."}
	a.Decisions = []Decision{{Choice: vocabulary.Choice{StartByte: 11, EndByte: 16, Original: "jason", Replacement: "Jayson"}, UseVocabulary: true}}
	address := "12 Baker Street\nLondon"
	protected, resolved, restore := a.ProtectWith([]Locked{{Start: 20, End: 31, Value: address}})
	if protected != "send it to STMLOCK0END at STMLOCK1END" {
		t.Fatal(protected)
	}
	if resolved != "send it to Jayson at "+address {
		t.Fatal(resolved)
	}
	if got := restore("Send it to STMLOCK0END at STMLOCK1END."); got != "Send it to Jayson at "+address+"." {
		t.Fatal(got)
	}
	if got := restore("Send it to Jayson at your address."); got != resolved || !a.GrammarFallback {
		t.Fatal("lost markers must fall back to the resolved text", got)
	}

	// A vocabulary term inside an inserted snippet's name gives way to it.
	a = Audit{Proposal: vocabulary.Result{Raw: "insert acme sign off"}, Resolved: "insert ACME sign off"}
	a.Decisions = []Decision{{Choice: vocabulary.Choice{StartByte: 7, EndByte: 11, Original: "acme", Replacement: "ACME"}, UseVocabulary: true}}
	protected, resolved, _ = a.ProtectWith([]Locked{{Start: 0, End: 20, Value: "Best,\nSam"}})
	if protected != "STMLOCK0END" || resolved != "Best,\nSam" {
		t.Fatal(protected, resolved)
	}

	// Without snippets it is exactly Protect.
	a = Audit{Proposal: vocabulary.Result{Raw: "hello"}, Resolved: "hello"}
	protected, resolved, restore = a.ProtectWith(nil)
	if protected != "hello" || resolved != "hello" || restore("Hello.") != "Hello." {
		t.Fatal(protected, resolved)
	}
	// Markers never collide with text the speaker or a snippet contains.
	a = Audit{Proposal: vocabulary.Result{Raw: "say my address"}, Resolved: "say my address"}
	protected, _, _ = a.ProtectWith([]Locked{{Start: 4, End: 14, Value: "STMLOCK0END"}})
	if !strings.HasPrefix(protected, "say STMLOCKX") {
		t.Fatal(protected)
	}
}
