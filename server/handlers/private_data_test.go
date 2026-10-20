package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"speektome-server/privatevault"
	"testing"
)

func TestEncryptedFinalHistoryHTTPAndUsageRedaction(t *testing.T) {
	app, database, token, user, _, _ := setupTestAppForAck(t)
	defer database.Close()
	vault, err := privatevault.New(context.Background(), database.GetDB(), bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	database.Privacy = vault
	// The ordinary ASR/quota path must retain metadata without personal payloads.
	logID, err := database.LogUsageAndUpdateQuota(user, 1, 0, 0, 0, false, "model", "intermediate ASR", "intermediate ASR", "success", 10, "provider", "", `{"private":"vocabulary"}`)
	if err != nil {
		t.Fatal(err)
	}
	log, err := database.GetUsageLogByID(logID, user)
	if err != nil {
		t.Fatal(err)
	}
	if log.InputTranscript != "" || log.FinalOutput != "" {
		t.Fatal("intermediate data stored in usage logs")
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("id", "81e307f3-2948-4caf-885a-1ba53458034e")
		w := httptest.NewRecorder()
		if method == "POST" {
			app.HandleFinalTranscription(w, req)
		} else {
			app.HandlePrivateHistory(w, req)
		}
		return w
	}
	if w := call("GET", "/v1/transcriptions/history", ""); w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	payload := `{"request_id":"81e307f3-2948-4caf-885a-1ba53458034e","mode":"command","text":"Final transformed response"}`
	for i := 0; i < 2; i++ {
		if w := call("POST", "/v1/transcriptions/final", payload); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := call("GET", "/v1/transcriptions/history", "")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable private data")
	}
	var records []privatevault.History
	if err = json.Unmarshal(w.Body.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Text != "Final transformed response" || records[0].Mode != "command" {
		t.Fatal("wrong final result")
	}
	if w = call("DELETE", "/v1/transcriptions/history/81e307f3-2948-4caf-885a-1ba53458034e", ""); w.Code != http.StatusNoContent {
		t.Fatal(w.Code)
	}
	// A lost response retry after an explicit deletion cannot resurrect content.
	if w = call("POST", "/v1/transcriptions/final", payload); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = call("GET", "/v1/transcriptions/history", ""); w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatal("deleted entry resurrected", w.Body.String())
	}
}
