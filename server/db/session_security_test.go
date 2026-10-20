package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSessionHashMigrationPreservesInstalledClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.db")
	d, e := NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.db.Exec(`INSERT INTO users(id,email,name) VALUES(1,'synthetic@example.invalid','User');INSERT INTO sessions(token,user_id,expires_at,is_temp) VALUES('legacy-token',1,?,0)`, time.Now().Add(time.Hour).Format(time.RFC3339))
	if e != nil {
		t.Fatal(e)
	}
	d.Close()
	d, e = NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = d.GetUserBySession("legacy-token"); e != nil {
		t.Fatal("existing client logged out", e)
	}
	if _, e = d.GetUserBySession(sessionHash("legacy-token")); e == nil {
		t.Fatal("hash accepted as bearer")
	}
	if e = d.DeleteSession("legacy-token"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.GetUserBySession("legacy-token"); e == nil {
		t.Fatal("logout ineffective")
	}
}
func TestConfigCleanupPreservesConfiguredEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	d, e := NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.db.Exec(`UPDATE saas_config SET value='wss://configured.example.invalid/v1/stream' WHERE key='meeting_stream_url';INSERT INTO saas_config(key,value) VALUES('unified_stream_url','ws://obsolete.invalid:8002'),('inference_instance_enabled','false')`)
	if e != nil {
		t.Fatal(e)
	}
	d.Close()
	d, e = NewDatabase(path)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	cfg, e := d.GetSaasConfig()
	if e != nil {
		t.Fatal(e)
	}
	if cfg["meeting_stream_url"] != "wss://configured.example.invalid/v1/stream" {
		t.Fatal("configured endpoint overwritten")
	}
	for _, key := range []string{"unified_stream_url", "inference_instance_enabled", "quota_free_daily_fup_seconds", "quota_paid_daily_fup_seconds"} {
		if _, ok := cfg[key]; ok {
			t.Fatal("retired parameter reappeared", key)
		}
	}
}
