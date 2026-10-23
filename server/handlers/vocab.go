package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"speektome-server/db"
	log "speektome-server/privacylog"
	"strings"

	"speektome-server/auth"
)

func (a *App) HandleGetVocabulary(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	terms, err := a.DB.GetVocabularyTerms(user.ID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to load vocabulary for user %d: %v", user.ID, err)
		http.Error(w, "Failed to load vocabulary", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(terms)
}

func (a *App) HandleAddVocabulary(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		Canonical string `json:"canonical_term"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: invalid payload", http.StatusBadRequest)
		return
	}

	canonical := strings.TrimSpace(req.Canonical)
	if canonical == "" || len(canonical) > 256 {
		http.Error(w, "Bad request: term must contain 1 to 256 bytes", http.StatusBadRequest)
		return
	}

	normalized := strings.ToLower(canonical)

	_, err = a.DB.AddVocabularyTerm(user.ID, canonical, normalized)
	if errors.Is(err, db.ErrVocabularyLimit) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "vocabulary_limit", "error": fmt.Sprintf("Your plan allows %d vocabulary terms. Existing terms are preserved; remove terms or upgrade before adding more.", user.VocabularyLimit()), "limit": user.VocabularyLimit()})
		return
	}
	if err != nil {
		log.Printf("[DB ERROR] Failed to add vocabulary term for user %d: %v", user.ID, err)
		http.Error(w, "Failed to add vocabulary term", http.StatusInternalServerError)
		return
	}

	if a.VocabCache != nil {
		a.VocabCache.Invalidate(user.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (a *App) HandleDeleteVocabulary(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var req struct {
		Canonical string `json:"canonical_term"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: invalid payload", http.StatusBadRequest)
		return
	}

	canonical := strings.TrimSpace(req.Canonical)
	if canonical == "" {
		http.Error(w, "Bad request: term cannot be empty", http.StatusBadRequest)
		return
	}

	err = a.DB.DeleteVocabularyTerm(user.ID, canonical)
	if err != nil {
		log.Printf("[DB ERROR] Failed to delete vocabulary term for user %d: %v", user.ID, err)
		http.Error(w, "Failed to delete vocabulary term", http.StatusInternalServerError)
		return
	}

	if a.VocabCache != nil {
		a.VocabCache.Invalidate(user.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
