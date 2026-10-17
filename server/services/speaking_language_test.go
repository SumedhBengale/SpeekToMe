package services

import (
	"strings"
	"testing"
)

func TestSpeakingLanguagePromptDoesNotBiasOtherSavedLanguages(t *testing.T) {
	for _, tc := range []struct {
		languages []string
		want      string
	}{
		{[]string{"en", "hi"}, "en"}, {[]string{"hi"}, "hi"}, {[]string{"auto"}, ""},
	} {
		got, prompt := BuildWhisperPrompt(tc.languages)
		if got != tc.want {
			t.Errorf("%v: language=%q", tc.languages, got)
		}
		if strings.Contains(prompt, "identified languages:") {
			t.Fatal(prompt)
		}
	}
}
