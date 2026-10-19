package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"speektome-server/accountlimits"
	"speektome-server/db"
	"speektome-server/handlers"
)

// Exercise the production router, persisted account state and the JSON contract
// consumed by the desktop, without starting audio or contacting inference.
func TestDesktopAccountAndRecordingPermitContract(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "desktop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	start, end := time.Now().Add(-24*time.Hour).Unix(), time.Now().Add(29*24*time.Hour).Unix()
	for id := 1; id <= 2; id++ {
		_, err = database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_current_start,razorpay_current_end)
            VALUES(?,?,'Desktop User','pro',1,?,?)`, id, fmt.Sprintf("desktop%d@test.invalid", id), start, end)
		if err != nil {
			t.Fatal(err)
		}
		if err = database.CreateSession(fmt.Sprintf("desktop%d", id), int64(id), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err = database.CreateSession("another-device", 1, 0); err != nil {
		t.Fatal(err)
	}
	period, _ := accountlimits.BillingPeriod("", start, end, time.Now())
	if _, err = database.GetDB().Exec(`INSERT INTO command_usage(user_id,period,input_tokens,output_tokens) VALUES(1,?,100,25)`, period); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux, err := newAPIMux(ctx, &handlers.App{DB: database})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(corsMiddleware(mux))
	t.Cleanup(server.Close)
	send := func(method, path, token, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var payload map[string]any
		if response.StatusCode != http.StatusNoContent {
			if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatalf("%s %s: non-JSON response (%d): %v", method, path, response.StatusCode, err)
			}
		}
		return response.StatusCode, payload
	}
	code, profile := send("GET", "/auth/me", "desktop1", "")
	if code != 200 {
		t.Fatal(code, profile)
	}
	usage, ok := profile["command_usage"].(map[string]any)
	if !ok || usage["enabled"] != true || usage["input_used"] != float64(100) || usage["output_used"] != float64(25) {
		t.Fatalf("desktop usage missing or stale: %#v", profile)
	}
	if _, ok := usage["remaining_percent"].(float64); !ok {
		t.Fatal("missing usage meter percentage")
	}
	reset, parseErr := time.Parse(time.RFC3339, fmt.Sprint(usage["resets_at"]))
	if parseErr != nil || !reset.Equal(time.Unix(end, 0)) {
		t.Fatal("missing confirmed reset date", usage)
	}
	first, next := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, token := range []string{"desktop1", "another-device"} {
		code, permit := send("POST", "/v1/meetings/session", token, `{"id":"`+first+`"}`)
		if code != 200 || permit["id"] != first || permit["max_duration_seconds"] != float64(14400) {
			t.Fatalf("desktop permit cannot start or reconcile: %d %#v", code, permit)
		}
	}
	if code, _ := send("POST", "/v1/meetings/session", "another-device", `{"id":"`+next+`"}`); code != 429 {
		t.Fatal("second device bypassed recording permit", code)
	}
	if code, _ := send("DELETE", "/v1/meetings/session/"+first, "desktop2", ""); code != 204 {
		t.Fatal(code)
	}
	if code, _ := send("POST", "/v1/meetings/session", "desktop1", `{"id":"`+next+`"}`); code != 429 {
		t.Fatal("another account released the recording permit", code)
	}
	if code, _ := send("DELETE", "/v1/meetings/session/"+first, "another-device", ""); code != 204 {
		t.Fatal("desktop could not release its persisted owner", code)
	}
	if code, permit := send("POST", "/v1/meetings/session", "desktop1", `{"id":"`+next+`"}`); code != 200 || permit["id"] != next {
		t.Fatal("recording did not recover after stop", code, permit)
	}
}
