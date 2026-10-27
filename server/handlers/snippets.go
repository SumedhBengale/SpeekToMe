package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"speektome-server/auth"
	"speektome-server/db"
	log "speektome-server/privacylog"
	"speektome-server/snippets"
	"speektome-server/vocabularydecision"
	"speektome/vocabulary"
)

const (
	snippetNameMaxChars    = 60
	snippetNameMaxWords    = 6
	snippetContentMaxChars = 5000
)

var snippetIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type snippetRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// validSnippet trims a request and returns why it cannot be saved, if it
// cannot. Names are spoken, so they need at least one word and few of them.
func validSnippet(req *snippetRequest) string {
	req.Name = strings.Join(strings.Fields(req.Name), " ")
	req.Content = strings.TrimRightFunc(strings.TrimLeft(req.Content, "\r\n"), unicode.IsSpace)
	switch {
	case !utf8.ValidString(req.Name) || !utf8.ValidString(req.Content):
		return "Snippets must be valid text"
	case snippets.WordCount(req.Name) == 0:
		return "Give the snippet a name you can say, such as \"my address\""
	case utf8.RuneCountInString(req.Name) > snippetNameMaxChars:
		return fmt.Sprintf("Snippet names can be at most %d characters", snippetNameMaxChars)
	case snippets.WordCount(req.Name) > snippetNameMaxWords:
		return fmt.Sprintf("Snippet names can be at most %d words", snippetNameMaxWords)
	case strings.TrimSpace(req.Content) == "":
		return "Add the text this snippet inserts"
	case utf8.RuneCountInString(req.Content) > snippetContentMaxChars:
		return fmt.Sprintf("Snippet text can be at most %d characters", snippetContentMaxChars)
	case strings.ContainsRune(req.Content, 0) || strings.ContainsRune(req.Name, 0):
		return "Snippets cannot contain null characters"
	}
	return ""
}

func writeSnippetJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func snippetError(w http.ResponseWriter, status int, code, message string) {
	writeSnippetJSON(w, status, map[string]any{"code": code, "error": message})
}

// HandleSnippets serves the account's snippets:
//
//	GET    /v1/snippets       list, with the plan's limit
//	POST   /v1/snippets       create {name, content}
//	PUT    /v1/snippets/{id}  replace {name, content}
//	DELETE /v1/snippets/{id}
func (a *App) HandleSnippets(w http.ResponseWriter, r *http.Request) {
	user, err := auth.Authenticate(r, a.DB)
	if err != nil {
		snippetError(w, http.StatusUnauthorized, "unauthorized", "Sign in to manage snippets")
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	if id != "" && !snippetIDPattern.MatchString(id) {
		snippetError(w, http.StatusNotFound, "snippet_not_found", "Snippet not found")
		return
	}
	if r.Method == http.MethodGet {
		list, limit, err := a.DB.ListSnippets(ctx, user.ID)
		if err != nil {
			log.Printf("[SNIPPETS] list failed for user %d: %v", user.ID, err)
			snippetError(w, http.StatusInternalServerError, "snippets_unavailable", "Snippets are unavailable right now")
			return
		}
		writeSnippetJSON(w, http.StatusOK, map[string]any{"snippets": list, "limit": limit, "count": len(list)})
		return
	}
	if r.Method == http.MethodDelete {
		err := a.DB.DeleteSnippet(ctx, user.ID, id)
		if errors.Is(err, db.ErrSnippetNotFound) {
			snippetError(w, http.StatusNotFound, "snippet_not_found", "Snippet not found")
			return
		}
		if err != nil {
			log.Printf("[SNIPPETS] delete failed for user %d: %v", user.ID, err)
			snippetError(w, http.StatusInternalServerError, "snippets_unavailable", "The snippet could not be deleted")
			return
		}
		writeSnippetJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req snippetRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		snippetError(w, http.StatusBadRequest, "invalid_snippet", "Invalid snippet")
		return
	}
	if message := validSnippet(&req); message != "" {
		snippetError(w, http.StatusBadRequest, "invalid_snippet", message)
		return
	}
	var saved db.SnippetRecord
	status := http.StatusOK
	if id == "" {
		saved, err = a.DB.CreateSnippet(ctx, user.ID, req.Name, req.Content)
		status = http.StatusCreated
	} else {
		saved, err = a.DB.UpdateSnippet(ctx, user.ID, id, req.Name, req.Content)
	}
	switch {
	case errors.Is(err, db.ErrSnippetNameTaken):
		snippetError(w, http.StatusConflict, "snippet_name_taken", fmt.Sprintf("You already have a snippet called %q", req.Name))
	case errors.Is(err, db.ErrSnippetLimit):
		writeSnippetJSON(w, http.StatusForbidden, map[string]any{"code": "snippet_limit", "error": fmt.Sprintf("Your plan allows %d snippets. Delete one or upgrade to add more.", user.SnippetLimit()), "limit": user.SnippetLimit()})
	case errors.Is(err, db.ErrSnippetNotFound):
		snippetError(w, http.StatusNotFound, "snippet_not_found", "Snippet not found")
	case err != nil:
		log.Printf("[SNIPPETS] save failed for user %d: %v", user.ID, err)
		snippetError(w, http.StatusInternalServerError, "snippets_unavailable", "The snippet could not be saved")
	default:
		writeSnippetJSON(w, status, saved)
	}
}

// dictationSnippets returns a resolver for the account's snippets, or nil
// when it has none or they cannot be loaded; dictation then works as before.
func (a *App) dictationSnippets(ctx context.Context, user int64) *snippets.Resolver {
	list, err := a.DB.ActiveSnippets(ctx, user)
	if err != nil {
		log.Printf("[SNIPPETS] could not load snippets for user %d: %v", user, err)
		return nil
	}
	return snippets.NewResolver(vocabularydecision.Default(), list)
}

// finalizeDictation resolves vocabulary and snippets for a finished
// transcript, side by side, and returns the grammar model's input, the text
// to use without it, and the function that restores the grammar output.
func (a *App) finalizeDictation(ctx context.Context, user int64, raw string, terms []db.VocabularyTerm, worker *vocabulary.Result, live *vocabularydecision.Live, resolver *snippets.Resolver) (audit vocabularydecision.Audit, grammarInput, resolved string, restore func(string) string) {
	var decisions []snippets.Decision
	var jevMillis float64
	var wg sync.WaitGroup
	if resolver != nil && raw != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decisions, jevMillis = resolver.Finish(ctx, raw)
		}()
	}
	audit = finalizeVocabulary(ctx, raw, terms, worker, live)
	wg.Wait()
	locks := a.snippetLocks(ctx, user, raw, audit.Proposal.Raw, decisions)
	if len(decisions) > 0 {
		inserted, live := 0, 0
		for _, d := range decisions {
			if d.Insert {
				inserted++
			}
			if d.Live {
				live++
			}
		}
		log.Printf("[SNIPPETS] user %d: %d spoken, %d inserted, %d decided live, decision model %.0fms", user, len(decisions), len(locks), live, jevMillis)
	}
	grammarInput, resolved, restore = audit.ProtectWith(locks)
	return audit, grammarInput, resolved, restore
}

// snippetLocks turns inserted snippets into locked spans of the transcript.
func (a *App) snippetLocks(ctx context.Context, user int64, raw, proposalRaw string, decisions []snippets.Decision) []vocabularydecision.Locked {
	var ids []string
	for _, d := range decisions {
		if d.Insert {
			ids = append(ids, d.SnippetID)
		}
	}
	// Spans are offsets into the raw transcript, which the vocabulary
	// proposal must share for both to apply to one text.
	if len(ids) == 0 || raw != proposalRaw {
		return nil
	}
	contents, err := a.DB.SnippetContents(ctx, user, ids)
	if err != nil {
		log.Printf("[SNIPPETS] could not open snippet text for user %d: %v", user, err)
		return nil
	}
	var locks []vocabularydecision.Locked
	for _, d := range decisions {
		if content, ok := contents[d.SnippetID]; ok && d.Insert {
			locks = append(locks, vocabularydecision.Locked{Start: d.CommandStart, End: d.End, Value: content})
		}
	}
	return locks
}
