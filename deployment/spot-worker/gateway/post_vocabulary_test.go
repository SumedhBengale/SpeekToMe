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
	if r, _ := p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.delta","delta":"hallucination"}`)); r != nil {
		t.Fatal("matched partial")
	}
	if r, _ := p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.completed","words":[{"word":"postgrsq","start":0,"end":1},{"word":"today.","start":3,"end":4}]}`)); r != nil {
		t.Fatal("matched at endpoint pause")
	}
	event, final := p.observe([]byte(`{"type":"input_audio_buffer.committed"}`))
	var r vocabulary.Result
	if !final || json.Unmarshal(event, &r) != nil || r.Raw != "We use postgrsq today." || r.Enhanced != "We use PostgreSQL today." {
		t.Fatalf("bad final: %+v", r)
	}
	if e := r.Validate(r.Raw, []string{"PostgreSQL", "SpeekToMe"}); e != nil {
		t.Fatal(e)
	}
	if event, _ := p.observe([]byte(`{"type":"input_audio_buffer.committed"}`)); event != nil {
		t.Fatal("duplicate final")
	}
}

func TestWorkerLiveVocabularyUpdates(t *testing.T) {
	p := &postVocabulary{}
	terms := []string{"PostgreSQL", "SpeekToMe"}
	data, _ := json.Marshal(map[string]any{"type": "session.update", "session": map[string]any{"vocabulary_matching": vocabulary.Config{Terms: terms, Live: true}}})
	if _, e := p.control(data); e != nil {
		t.Fatal(e)
	}
	p.audio()
	completed := func(words string) ([]byte, bool) {
		return p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.completed","words":[` + words + `]}`))
	}
	event, final := completed(`{"word":"We","start":0,"end":0.2},{"word":"use","start":0.2,"end":0.4},{"word":"postgrsq","start":0.4,"end":1}`)
	var r vocabulary.Result
	if final || json.Unmarshal(event, &r) != nil || r.Type != vocabulary.UpdateType || len(r.Choices) != 1 || r.Choices[0].Original != "postgrsq" {
		t.Fatalf("missing live proposal: %s", event)
	}
	if e := r.ValidateAs(vocabulary.UpdateType, "We use postgrsq", terms); e != nil {
		t.Fatal(e)
	}
	if event, _ := completed(`{"word":"today.","start":1.5,"end":2}`); event != nil {
		t.Fatalf("unchanged proposals resent: %s", event)
	}
	event, _ = completed(`{"word":"speektome","start":2.5,"end":3}`)
	if json.Unmarshal(event, &r) != nil || len(r.Choices) != 2 || r.Choices[0].StartByte != 7 {
		t.Fatalf("new proposal must keep earlier offsets: %s", event)
	}
	event, final = p.observe([]byte(`{"type":"input_audio_buffer.committed"}`))
	if !final || json.Unmarshal(event, &r) != nil || r.Type != vocabulary.FinalType || r.Raw != "We use postgrsq today. speektome" {
		t.Fatalf("bad final after live updates: %s", event)
	}
}

func TestWorkerSendsNoLiveUpdatesUnlessAsked(t *testing.T) {
	p := &postVocabulary{}
	data, _ := json.Marshal(map[string]any{"type": "session.update", "session": map[string]any{"vocabulary_matching": vocabulary.Config{Terms: []string{"PostgreSQL"}}}})
	if _, e := p.control(data); e != nil {
		t.Fatal(e)
	}
	if event, _ := p.observe([]byte(`{"type":"conversation.item.input_audio_transcription.completed","words":[{"word":"postgrsq","start":0,"end":1}]}`)); event != nil {
		t.Fatalf("live update without opt-in: %s", event)
	}
}
