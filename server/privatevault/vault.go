// Package privatevault owns encrypted personal data. Keys are never serialized
// unwrapped; production bootstrap fetches the root from Secrets Manager once.
package privatevault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"speektome-server/audiospool"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("encrypted personal storage unavailable")
var ErrQuota = errors.New("cloud audio storage is full; the local recording is preserved")
var ErrNotFound = errors.New("recording unavailable")
var ErrNotPaid = errors.New("paid access is required for cloud audio")

const DefaultQuota int64 = 2_000_000_000

type Vault struct {
	db           *sql.DB
	master       cipher.AEAD
	Objects      Objects
	Spool        *audiospool.Spool
	Bucket       string
	historySlots chan struct{}
}
type History struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
	Text      string `json:"text"`
	Mode      string `json:"mode"`
}

func New(ctx context.Context, db *sql.DB, master []byte) (*Vault, error) {
	if len(master) != 32 {
		return nil, errors.New("master key must contain exactly 32 bytes")
	}
	block, e := aes.NewCipher(master)
	if e != nil {
		return nil, ErrUnavailable
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrUnavailable
	}
	v := &Vault{db: db, master: a, historySlots: make(chan struct{}, 2)}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS transcription_history_tombstones(user_id INTEGER NOT NULL,request_id TEXT NOT NULL,deleted_at INTEGER NOT NULL,PRIMARY KEY(user_id,request_id))`,
		`CREATE TABLE IF NOT EXISTS meeting_archive_status(meeting_id INTEGER PRIMARY KEY,user_id INTEGER NOT NULL,state TEXT NOT NULL,next_attempt INTEGER NOT NULL DEFAULT 0,processing_purged INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS encrypted_legacy_results(user_id INTEGER NOT NULL,usage_id INTEGER NOT NULL,ciphertext TEXT NOT NULL,PRIMARY KEY(user_id,usage_id))`,
		`CREATE TABLE IF NOT EXISTS private_account_deletions(user_id INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS meeting_audio_tombstones(user_id INTEGER NOT NULL,meeting_id INTEGER NOT NULL,deleted_at INTEGER NOT NULL,PRIMARY KEY(user_id,meeting_id))`,
		`CREATE TABLE IF NOT EXISTS private_vault_settings(name TEXT PRIMARY KEY,value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS user_encryption_keys(user_id INTEGER PRIMARY KEY,wrapped_key TEXT NOT NULL,version INTEGER NOT NULL DEFAULT 1,created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS transcription_history(user_id INTEGER NOT NULL,request_id TEXT NOT NULL,created_at INTEGER NOT NULL,mode TEXT NOT NULL,ciphertext TEXT NOT NULL,PRIMARY KEY(user_id,request_id))`,
		`CREATE INDEX IF NOT EXISTS history_user_time ON transcription_history(user_id,created_at DESC,request_id DESC)`,
		`CREATE TABLE IF NOT EXISTS user_cloud_storage(user_id INTEGER PRIMARY KEY,limit_bytes INTEGER NOT NULL DEFAULT 2000000000,used_bytes INTEGER NOT NULL DEFAULT 0,reserved_bytes INTEGER NOT NULL DEFAULT 0,unpaid_since INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS meeting_audio_assets(id TEXT PRIMARY KEY,user_id INTEGER NOT NULL,meeting_id INTEGER NOT NULL UNIQUE,object_key TEXT NOT NULL UNIQUE,wrapped_key TEXT NOT NULL,plain_bytes INTEGER NOT NULL,cipher_bytes INTEGER NOT NULL,state TEXT NOT NULL,created_at INTEGER NOT NULL,last_error TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS audio_user_state ON meeting_audio_assets(user_id,state)`,
	}
	for _, s := range statements {
		if _, e = db.ExecContext(ctx, s); e != nil {
			return nil, fmt.Errorf("personal storage schema: %w", e)
		}
	}
	marker, e := v.seal(a, []byte("SpeekToMe vault v1"), "master-verification")
	if e != nil {
		return nil, e
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO private_vault_settings(name,value) VALUES('master-check',?) ON CONFLICT(name) DO NOTHING`, marker); e != nil {
		return nil, e
	}
	var stored string
	if e = db.QueryRowContext(ctx, `SELECT value FROM private_vault_settings WHERE name='master-check'`).Scan(&stored); e != nil {
		return nil, e
	}
	p, e := v.open(a, stored, "master-verification")
	if e != nil || string(p) != "SpeekToMe vault v1" {
		return nil, errors.New("master key does not match this database; refusing startup")
	}
	return v, nil
}
func (v *Vault) seal(a cipher.AEAD, p []byte, aad string) (string, error) {
	n := make([]byte, a.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return "", ErrUnavailable
	}
	return base64.StdEncoding.EncodeToString(a.Seal(n, n, p, []byte(aad))), nil
}
func (v *Vault) open(a cipher.AEAD, s, aad string) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil || len(b) < a.NonceSize()+a.Overhead() {
		return nil, ErrUnavailable
	}
	p, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(aad))
	if e != nil {
		return nil, ErrUnavailable
	}
	return p, nil
}
func (v *Vault) UserKey(ctx context.Context, user int64) ([]byte, error) {
	var allowed int
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=? AND NOT EXISTS(SELECT 1 FROM private_account_deletions WHERE user_id=?)`, user, user).Scan(&allowed); err != nil || allowed != 1 {
		return nil, ErrUnavailable
	}
	var wrapped string
	e := v.db.QueryRowContext(ctx, `SELECT wrapped_key FROM user_encryption_keys WHERE user_id=?`, user).Scan(&wrapped)
	if errors.Is(e, sql.ErrNoRows) {
		key := make([]byte, 32)
		if _, e = io.ReadFull(rand.Reader, key); e != nil {
			return nil, ErrUnavailable
		}
		wrapped, e = v.seal(v.master, key, "user-key-v1:"+strconv.FormatInt(user, 10))
		clear(key)
		if e != nil {
			return nil, e
		}
		_, e = v.db.ExecContext(ctx, `INSERT INTO user_encryption_keys(user_id,wrapped_key,created_at) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM users WHERE id=?) AND NOT EXISTS(SELECT 1 FROM private_account_deletions WHERE user_id=?) ON CONFLICT(user_id) DO NOTHING`, user, wrapped, time.Now().Unix(), user, user)
		if e != nil {
			return nil, ErrUnavailable
		}
		e = v.db.QueryRowContext(ctx, `SELECT wrapped_key FROM user_encryption_keys WHERE user_id=?`, user).Scan(&wrapped)
	}
	if e != nil {
		return nil, ErrUnavailable
	}
	return v.open(v.master, wrapped, "user-key-v1:"+strconv.FormatInt(user, 10))
}
func aead(key []byte) (cipher.AEAD, error) {
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, ErrUnavailable
	}
	return cipher.NewGCM(b)
}
func (v *Vault) EncryptText(ctx context.Context, user int64, kind, id, text string) (string, error) {
	key, e := v.UserKey(ctx, user)
	if e != nil {
		return "", e
	}
	defer clear(key)
	a, e := aead(key)
	if e != nil {
		return "", e
	}
	return v.seal(a, []byte(text), fmt.Sprintf("%s:%d:%s", kind, user, id))
}
func (v *Vault) DecryptText(ctx context.Context, user int64, kind, id, text string) (string, error) {
	key, e := v.UserKey(ctx, user)
	if e != nil {
		return "", e
	}
	defer clear(key)
	a, e := aead(key)
	if e != nil {
		return "", e
	}
	p, e := v.open(a, text, fmt.Sprintf("%s:%d:%s", kind, user, id))
	return string(p), e
}
func (v *Vault) SaveFinal(ctx context.Context, user int64, id, mode, text string) error {
	if user <= 0 || len(id) < 16 || len(id) > 96 || strings.TrimSpace(text) == "" || len(text) > 1<<20 || (mode != "dictation" && mode != "command") {
		return errors.New("invalid final transcription")
	}
	sealed, e := v.EncryptText(ctx, user, "history-v1", id, text)
	if e != nil {
		return e
	}
	// Idempotent retries never replace another result under an existing identity.
	result, e := v.db.ExecContext(ctx, `INSERT INTO transcription_history(user_id,request_id,created_at,mode,ciphertext) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM users WHERE id=?) AND NOT EXISTS(SELECT 1 FROM private_account_deletions WHERE user_id=?) AND NOT EXISTS(SELECT 1 FROM transcription_history_tombstones WHERE user_id=? AND request_id=?) ON CONFLICT(user_id,request_id) DO NOTHING`, user, id, time.Now().Unix(), mode, sealed, user, user, user, id)
	if e != nil {
		return ErrUnavailable
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var deleted int
		if v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transcription_history_tombstones WHERE user_id=? AND request_id=?`, user, id).Scan(&deleted) == nil && deleted > 0 {
			return nil
		}
		var existing, existingMode string
		if e = v.db.QueryRowContext(ctx, `SELECT ciphertext,mode FROM transcription_history WHERE user_id=? AND request_id=?`, user, id).Scan(&existing, &existingMode); e != nil {
			return ErrUnavailable
		}
		plain, err := v.DecryptText(ctx, user, "history-v1", id, existing)
		if err != nil || plain != text || existingMode != mode {
			return errors.New("final transcription identity conflict")
		}
	}
	return nil
}
func (v *Vault) History(ctx context.Context, user int64, before int64, beforeID ...string) ([]History, error) {
	select {
	case v.historySlots <- struct{}{}:
		defer func() { <-v.historySlots }()
	case <-ctx.Done():
		return nil, ErrUnavailable
	}
	key, err := v.UserKey(ctx, user)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer clear(key)
	accountCipher, err := aead(key)
	if err != nil {
		return nil, ErrUnavailable
	}

	if before <= 0 {
		before = time.Now().Unix() + 1
	}
	cursor := ""
	if len(beforeID) > 0 {
		cursor = beforeID[0]
	}
	rows, e := v.db.QueryContext(ctx, `SELECT request_id,created_at,mode,ciphertext FROM transcription_history WHERE user_id=? AND (created_at<? OR (created_at=? AND request_id<?)) ORDER BY created_at DESC,request_id DESC LIMIT 50`, user, before, before, cursor)
	if e != nil {
		return nil, ErrUnavailable
	}
	type record struct {
		h History
		c string
	}
	var records []record
	for rows.Next() {
		var r record
		if e = rows.Scan(&r.h.ID, &r.h.CreatedAt, &r.h.Mode, &r.c); e != nil {
			rows.Close()
			return nil, ErrUnavailable
		}
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, ErrUnavailable
	}
	out := make([]History, 0, len(records))
	for _, r := range records {
		plain, err := v.open(accountCipher, r.c, fmt.Sprintf("history-v1:%d:%s", user, r.h.ID))
		e = err
		r.h.Text = string(plain)
		if e != nil {
			return nil, e
		}
		out = append(out, r.h)
	}
	return out, nil
}
func (v *Vault) DeleteHistory(ctx context.Context, user int64, id string) error {
	tx, e := v.db.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO transcription_history_tombstones(user_id,request_id,deleted_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, user, id, time.Now().Unix()); e != nil {
		return ErrUnavailable
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM transcription_history WHERE user_id=? AND request_id=?`, user, id); e != nil {
		return ErrUnavailable
	}
	return tx.Commit()
}
