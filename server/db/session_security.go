package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
)

// Tokens supplied by clients are always hashed, even when they look like a stored digest.
func sessionHash(token string) string {
	if token == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(h[:])
}
func migrateSessionHashes(c *sql.DB) error {
	rows, e := c.Query("SELECT token FROM sessions WHERE token NOT LIKE 'sha256:%'")
	if e != nil {
		return e
	}
	var old []string
	for rows.Next() {
		var t string
		if e = rows.Scan(&t); e != nil {
			rows.Close()
			return e
		}
		old = append(old, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, t := range old {
		if _, e = c.Exec("UPDATE sessions SET token=? WHERE token=?", sessionHash(t), t); e != nil {
			return e
		}
	}
	// Any legacy temporary challenge has a long lifetime; invalidate it on upgrade.
	_, e = c.Exec("UPDATE sessions SET expires_at=strftime('%Y-%m-%dT%H:%M:%SZ','now','+10 minutes') WHERE is_temp=1 AND julianday(expires_at)>julianday('now','+10 minutes')")
	return e
}
