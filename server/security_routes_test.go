package main

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"speektome-server/db"
	"speektome-server/handlers"
	"strings"
	"testing"
)

func TestSecurityProtectedRouteGates(t *testing.T) {
	d, e := db.NewDatabase(filepath.Join(t.TempDir(), "audit.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	_, e = d.GetDB().Exec(`INSERT INTO users(id,email,name) VALUES(1,'synthetic@example.invalid','Synthetic')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.CreateSession("synthetic-session", 1, 0); e != nil {
		t.Fatal(e)
	}
	t.Setenv("MEETING_AUDIO_DIR", t.TempDir())
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mux, e := newAPIMux(ctx, &handlers.App{DB: d})
	if e != nil {
		t.Fatal(e)
	}
	routes := [][2]string{{"GET", "/auth/me"}, {"GET", "/auth/profile/summary"}, {"DELETE", "/auth/account"}, {"POST", "/auth/delete-account"}, {"POST", "/auth/languages"}, {"GET", "/usage/history"}, {"POST", "/v1/transcriptions/final"}, {"GET", "/v1/transcriptions/history"}, {"DELETE", "/v1/transcriptions/history/synthetic"}, {"GET", "/v1/storage"}, {"POST", "/generate"}, {"POST", "/v1/transform"}, {"POST", "/transform"}, {"POST", "/v1/transcribe/ack"}, {"POST", "/validate-prompt"}, {"GET", "/auth/vocabulary"}, {"POST", "/auth/vocabulary"}, {"DELETE", "/auth/vocabulary"}, {"POST", "/v1/meetings/session"}, {"DELETE", "/v1/meetings/session/synthetic"}, {"GET", "/v1/meetings/live"}, {"GET", "/meetings"}, {"GET", "/meetings/synthetic"}, {"PUT", "/meetings/synthetic/speakers"}, {"PUT", "/meetings/synthetic/title"}, {"DELETE", "/meetings/synthetic"}, {"GET", "/meetings/synthetic/audio"}, {"POST", "/billing/create-subscription"}, {"POST", "/billing/verify-subscription"}, {"POST", "/billing/cancel-subscription"}, {"GET", "/admin/users"}, {"POST", "/admin/users/synthetic/plan"}, {"POST", "/admin/users/synthetic/rate-limit"}, {"GET", "/admin/config"}, {"POST", "/admin/config"}, {"GET", "/admin/webhook-logs"}, {"GET", "/admin/cache/rate-limits"}, {"DELETE", "/admin/cache/rate-limits"}, {"POST", "/v1/meetings/jobs"}, {"GET", "/v1/meetings/jobs/synthetic"}, {"PUT", "/v1/meetings/jobs/synthetic/audio/synthetic"}, {"POST", "/v1/meetings/jobs/synthetic/commit"}, {"POST", "/v1/meetings/jobs/synthetic/retry"}}
	for _, p := range routes {
		r := httptest.NewRequest(p[0], p[1], nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("anonymous %s %s got %d", p[0], p[1], w.Code)
		}
		if strings.HasPrefix(p[1], "/admin/") {
			r = httptest.NewRequest(p[0], p[1], nil)
			r.Header.Set("Authorization", "Bearer synthetic-session")
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Errorf("nonadmin %s %s got %d", p[0], p[1], w.Code)
			}
		}
	}
	t.Logf("Checked %d protected route registrations plus nonadmin rejection on all admin routes", len(routes))
}
