package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome/vocabulary"
	"sync/atomic"
	"testing"
)

func TestFinalVocabularyRPCAuthenticatesAndValidatesResult(t *testing.T) {
	terms := []string{"PostgreSQL"}
	raw := "We use postgrsq."
	var bad atomic.Bool
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/vocabulary/final" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("wrong matcher route or credentials")
		}
		var req vocabulary.Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.Raw != raw || len(req.Terms) != 1 {
			t.Error("missing final transcript or terms")
		}
		v, _ := vocabulary.Compile(req.Terms)
		result := vocabulary.Propose(r.Context(), req.Raw, v, vocabulary.TermsHash(req.Terms))
		if bad.Load() {
			result.TranscriptSHA = "wrong"
		}
		json.NewEncoder(w).Encode(result)
	}))
	defer worker.Close()
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("test-token"), 0600)
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	result, e := MatchFinalVocabulary(context.Background(), raw, terms)
	if e != nil || result.Enhanced != "We use PostgreSQL." {
		t.Fatalf("RPC result: %+v %v", result, e)
	}
	bad.Store(true)
	if _, e = MatchFinalVocabulary(context.Background(), raw, terms); e == nil {
		t.Fatal("mismatched transcript accepted")
	}
}
