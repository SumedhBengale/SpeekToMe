package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEveryPooledConnectionHasBusyTimeout(t *testing.T) {
	database, err := NewDatabase(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	var held []interface{ Close() error }
	for i := 0; i < 3; i++ {
		conn, err := database.GetDB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, conn)
		var timeout int
		var mode string
		if err = conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
			t.Fatalf("connection %d busy_timeout = %d, %v", i, timeout, err)
		}
		if err = conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Fatalf("connection %d journal_mode = %q, %v", i, mode, err)
		}
	}
	for _, conn := range held {
		conn.Close()
	}
}

func TestMigrationsRunOnceAndRecordVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	for i := 0; i < 2; i++ {
		database, err := NewDatabase(path)
		if err != nil {
			t.Fatal(err)
		}
		var versions int
		if err = database.GetDB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil || versions != len(migrations) {
			t.Fatalf("recorded %d migrations, %v", versions, err)
		}
		for _, table := range []string{"auth_codes", "oauth_states"} {
			var n int
			database.GetDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
			if n != 1 {
				t.Fatalf("%s missing after migration", table)
			}
		}
		database.Close()
	}
}
