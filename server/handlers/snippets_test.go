package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"speektome-server/privatevault"
)

func TestSnippetsHTTPAndDictation(t *testing.T) {
	app, database, token, user, _, _ := setupTestAppForAck(t)
	defer database.Close()
	vault, err := privatevault.New(context.Background(), database.GetDB(), bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	database.Privacy = vault
	t.Setenv("TYPESAFE_API_KEY", "")

	call := func(method, path, id, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		if id != "" {
			req.SetPathValue("id", id)
		}
		w := httptest.NewRecorder()
		app.HandleSnippets(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	code, created := call("POST", "/v1/snippets", "", `{"name":"  My   address ","content":"12 Baker Street\nLondon\n"}`)
	if code != http.StatusCreated || created["name"] != "My address" || created["content"] != "12 Baker Street\nLondon" {
		t.Fatal(code, created)
	}
	id := created["id"].(string)
	for body, want := range map[string]string{
		`{"name":"","content":"x"}`:                                  "invalid_snippet",
		`{"name":"!!!","content":"x"}`:                               "invalid_snippet",
		`{"name":"one two three four five six seven","content":"x"}`: "invalid_snippet",
		`{"name":"empty","content":"   "}`:                           "invalid_snippet",
		`{"name":"my address","content":"x"}`:                        "snippet_name_taken",
		`not json`:                                                   "invalid_snippet",
	} {
		if _, out := call("POST", "/v1/snippets", "", body); out["code"] != want {
			t.Errorf("%s: %v", body, out)
		}
	}
	if code, out := call("GET", "/v1/snippets", "", ""); code != 200 || out["count"].(float64) != 1 || out["limit"].(float64) != 50 {
		t.Fatal(code, out)
	}
	if code, _ := call("PUT", "/v1/snippets/x", "not-an-id", `{"name":"a","content":"b"}`); code != http.StatusNotFound {
		t.Fatal(code)
	}

	// Dictation: without the decision model, a name said on its own inserts
	// the snippet and a name inside a sentence stays as words.
	ctx := context.Background()
	for raw, want := range map[string]string{
		"my address":                           "12 Baker Street\nLondon",
		"Here you go. My address.":             "Here you go. 12 Baker Street\nLondon",
		"I will send you my address by email.": "I will send you my address by email.",
	} {
		_, grammarInput, resolved, restore := app.finalizeDictation(ctx, user, raw, nil, nil, nil, app.dictationSnippets(ctx, user))
		if resolved != want {
			t.Errorf("%q: resolved %q", raw, resolved)
		}
		if strings.Contains(grammarInput, "Baker") {
			t.Errorf("%q: snippet text reached the grammar model: %q", raw, grammarInput)
		}
		if got := restore(grammarInput); got != want {
			t.Errorf("%q: restored %q", raw, got)
		}
	}

	if code, _ := call("PUT", "/v1/snippets/"+id, id, `{"name":"home","content":"Elsewhere"}`); code != 200 {
		t.Fatal(code)
	}
	_, _, resolved, _ := app.finalizeDictation(ctx, user, "home", nil, nil, nil, app.dictationSnippets(ctx, user))
	if resolved != "Elsewhere" {
		t.Fatal(resolved)
	}
	if code, _ := call("DELETE", "/v1/snippets/"+id, id, ""); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call("DELETE", "/v1/snippets/"+id, id, ""); code != http.StatusNotFound {
		t.Fatal(code)
	}
	if app.dictationSnippets(ctx, user) != nil {
		t.Fatal("deleted snippet still active")
	}
}
