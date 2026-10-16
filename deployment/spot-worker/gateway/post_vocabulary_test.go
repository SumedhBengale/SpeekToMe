package main

import (
	"encoding/json"
	"speektome/vocabulary"
	"strings"
	"testing"
)

func TestWorkerFinalVocabularyRecovery(t *testing.T) {
	p := &postVocabulary{}
	config := map[string]any{"type": "session.update", "session": map[string]any{"speech_contexts": []speechContext{{Phrases: []string{"poison"}, Boost: 5}}, "prompt": "poison", "vocabulary_matching": vocabulary.Config{Terms: []string{"PostgreSQL", "SpeekToMe"}, Prefix: "We use postgrsq", Confirmed: 10, AudioOffset: 8}}}
	data, _ := json.Marshal(config)
	forward, e := p.control(data)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(forward), "poison") || strings.Contains(string(forward), "vocabulary_matching") {
		t.Fatalf("decoder biased: %s", forward)
	}
	p.audio()
	if _, e := p.control(data); e == nil {
		t.Fatal("midstream dictionary mutated")
	}
	if r := p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.delta","delta":"hallucination"}`)); r != nil {
		t.Fatal("matched partial")
	}
	if r := p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.completed","words":[{"word":"postgrsq","start":0,"end":1},{"word":"today.","start":3,"end":4}]}`)); r != nil {
		t.Fatal("matched at endpoint pause")
	}
	r := p.observe([]byte(`{"type":"input_audio_buffer.committed"}`))
	if r == nil || r.Raw != "We use postgrsq today." || r.Enhanced != "We use PostgreSQL today." {
		t.Fatalf("bad final: %+v", r)
	}
	if e := r.Validate(r.Raw, []string{"PostgreSQL", "SpeekToMe"}); e != nil {
		t.Fatal(e)
	}
	if p.observe([]byte(`{"type":"input_audio_buffer.committed"}`)) != nil {
		t.Fatal("duplicate final")
	}
}
