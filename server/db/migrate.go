package db

import (
	"database/sql"
	"fmt"
	"time"
)

// migration is one ordered, run-once schema change. The idempotent baseline in
// NewDatabase predates versioning; every change after it belongs here.
type migration struct {
	version int
	name    string
	up      func(*sql.Tx) error
}

var migrations = []migration{
	{1, "one-time sign-in codes and OAuth state", func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS auth_codes(code_hash TEXT PRIMARY KEY,user_id INTEGER NOT NULL,is_temp INTEGER NOT NULL,expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS oauth_states(state_hash TEXT PRIMARY KEY,cookie_hash TEXT NOT NULL,expires INTEGER NOT NULL);`)
		return err
	}},
	{2, "two-factor code replay protection", func(tx *sql.Tx) error {
		return addColumn(tx, "users", "totp_last_step", "INTEGER NOT NULL DEFAULT 0")
	}},
}

func addColumn(tx *sql.Tx, table, column, definition string) error {
	var n int
	if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name=?", table), column).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

// migrate applies pending migrations in order, each in its own transaction.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	for _, m := range migrations {
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, m.version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err = m.up(tx); err == nil {
			_, err = tx.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`, m.version, m.name, time.Now().Unix())
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			return fmt.Errorf("schema migration %d (%s): %w", m.version, m.name, err)
		}
	}
	return nil
}
