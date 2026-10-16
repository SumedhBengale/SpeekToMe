package handlers

import (
	"context"
	"encoding/json"
	"speektome-server/db"
	"speektome-server/services"
	"speektome-server/vocabularydecision"
	"speektome/vocabulary"
	"time"
)

func finalizeVocabulary(ctx context.Context, raw string, terms []db.VocabularyTerm, worker *vocabulary.Result) vocabularydecision.Audit {
	phrases := make([]string, 0, len(terms))
	for _, t := range terms {
		phrases = append(phrases, t.Canonical)
	}
	if worker == nil && len(phrases) > 0 && raw != "" {
		worker, _ = services.MatchFinalVocabulary(ctx, raw, phrases)
	}
	source := "inference_worker"
	var proposal vocabulary.Result
	if worker != nil && worker.Validate(raw, phrases) == nil {
		proposal = *worker
	} else {
		source = "main_fallback"
		compiled, err := vocabulary.Compile(phrases)
		if err != nil {
			proposal = vocabulary.Result{Type: "vocabulary.final", SchemaVersion: 1, Raw: raw, Enhanced: raw, TranscriptSHA: vocabulary.Hash(raw), VocabularySHA: vocabulary.TermsHash(phrases), MatcherVersion: vocabulary.Version, Error: "invalid_vocabulary", Choices: []vocabulary.Choice{}}
		} else {
			matchCtx, cancel := context.WithTimeout(ctx, time.Second)
			proposal = vocabulary.Propose(matchCtx, raw, compiled, vocabulary.TermsHash(phrases))
			cancel()
		}
	}
	return vocabularydecision.Default().Evaluate(ctx, proposal, source)
}
func auditJSON(a vocabularydecision.Audit) string { b, _ := json.Marshal(a); return string(b) }
