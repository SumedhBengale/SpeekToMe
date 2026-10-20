package modelresult

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorContract(t *testing.T) {
	for _, text := range []string{`{"speektome_error":{"code":"refused","explanation":"PRIVATE"}}`, `{"speektome_error":{"code":"missing_input"}}`, `{"speektome_error":{"code":"unknown"}}`, `{"speektome_error":`, "```json\n{\"speektome_error\":{\"code\":\"refused\"}}\n```", ""} {
		err := Check(text, "command")
		var e *Error
		if !errors.As(err, &e) || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatal("unsafe or missing error", err)
		}
		w := httptest.NewRecorder()
		Write(w, err, "command")
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"stage":"command"`) || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal(w.Body.String())
		}
	}
	for _, text := range []string{"I’m sorry, but I can’t comply with that.", "मुझे खेद है, मैं ऐसा नहीं कर सकता।", `{"error":"example code"}`, "normal text"} {
		if e := Check(text, "llm"); e != nil {
			t.Fatalf("literal content misclassified: %s", text)
		}
	}
}

func TestCommandSuccessMustBeExplicit(t *testing.T) {
	for _, text := range []string{"I’m sorry, but I can’t comply with that.", "कृपया पाठ प्रदान करें।", `{"speektome_result":{}}`, `{"speektome_result":{"text":""}}`} {
		if _, err := DecodeCommand(text); err == nil {
			t.Fatal("unstructured response accepted", text)
		}
	}
	text, err := DecodeCommand(`{"speektome_result":{"text":"मुझे खेद है, मैं ऐसा नहीं कर सकता।"}}`)
	if err != nil || text != "मुझे खेद है, मैं ऐसा नहीं कर सकता।" {
		t.Fatal(text, err)
	}
}
