package privatevault

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MigrateUsage removes historical plaintext and intermediate ASR from usage
// records. Existing final fields are retained encrypted as legacy data, not
// presented as verified client-delivered history.
func (v *Vault) MigrateUsage(ctx context.Context) (int, error) {
	if _, e := v.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS encrypted_legacy_results(user_id INTEGER NOT NULL,usage_id INTEGER NOT NULL,ciphertext TEXT NOT NULL,PRIMARY KEY(user_id,usage_id))`); e != nil {
		return 0, e
	}
	total := 0
	for {
		rows, e := v.db.QueryContext(ctx, `SELECT id,user_id,COALESCE(final_output,'') FROM usage_logs WHERE COALESCE(input_transcript,'')<>'' OR COALESCE(final_output,'')<>'' OR COALESCE(vocabulary_audit,'')<>'' LIMIT 50`)
		if e != nil {
			return total, e
		}
		type old struct {
			id, user int64
			text     string
		}
		var batch []old
		for rows.Next() {
			var r old
			if e = rows.Scan(&r.id, &r.user, &r.text); e != nil {
				rows.Close()
				return total, e
			}
			batch = append(batch, r)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return total, e
		}
		if len(batch) == 0 {
			return total, nil
		}
		// Encrypt each account's batch in memory, then commit ciphertext and
		// plaintext removal together. Bulk statements avoid thousands of Turso
		// round trips while old API writers are stopped for migration.
		ciphers := make(map[int64]cipher.AEAD)
		values := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*3)
		ids := make([]string, 0, len(batch))
		idArgs := make([]any, 0, len(batch))
		for _, r := range batch {
			ids = append(ids, "?")
			idArgs = append(idArgs, r.id)
			if r.text == "" {
				continue
			}
			accountCipher := ciphers[r.user]
			if accountCipher == nil {
				key, err := v.UserKey(ctx, r.user)
				if err != nil {
					return total, err
				}
				accountCipher, err = aead(key)
				clear(key)
				if err != nil {
					return total, err
				}
				ciphers[r.user] = accountCipher
			}
			sealed, err := v.seal(accountCipher, []byte(r.text), fmt.Sprintf("legacy-final-v1:%d:%d", r.user, r.id))
			if err != nil {
				return total, err
			}
			values = append(values, "(?,?,?)")
			args = append(args, r.user, r.id, sealed)
		}
		tx, e := v.db.BeginTx(ctx, nil)
		if e != nil {
			return total, e
		}
		if len(values) > 0 {
			_, e = tx.ExecContext(ctx, `INSERT INTO encrypted_legacy_results(user_id,usage_id,ciphertext) VALUES `+strings.Join(values, ",")+` ON CONFLICT(user_id,usage_id) DO NOTHING`, args...)
		}
		if e == nil {
			_, e = tx.ExecContext(ctx, `UPDATE usage_logs SET input_transcript='',final_output='',vocabulary_audit='' WHERE id IN (`+strings.Join(ids, ",")+`)`, idArgs...)
		}
		if e != nil {
			tx.Rollback()
			return total, e
		}
		if e = tx.Commit(); e != nil {
			return total, e
		}
		total += len(batch)

	}
}

// Recover failed uploads before making a fresh encrypted attempt. Reservations
// are released only after object deletion, and old uploading leases expire.
func (v *Vault) RecoverUploads(ctx context.Context) error {
	if v.Objects == nil {
		return nil
	}
	rows, e := v.db.QueryContext(ctx, `SELECT user_id,meeting_id FROM meeting_audio_assets WHERE state='failed' OR (state='uploading' AND created_at<?) LIMIT 10`, time.Now().Add(-30*time.Minute).Unix())
	if e != nil {
		return e
	}
	var ids [][2]int64
	for rows.Next() {
		var id [2]int64
		if e = rows.Scan(&id[0], &id[1]); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if e = v.discardFailed(ctx, id[0], id[1]); e != nil {
			return e
		}
	}
	return nil
}
func (v *Vault) discardFailed(ctx context.Context, user, meeting int64) error {
	tx, e := v.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id, key, state string
	var size int64
	e = tx.QueryRowContext(ctx, `SELECT id,object_key,state,cipher_bytes FROM meeting_audio_assets WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&id, &key, &state, &size)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if state == "ready" {
		return nil
	}
	if _, e = tx.ExecContext(ctx, `UPDATE meeting_audio_assets SET state='failed' WHERE id=?`, id); e != nil {
		return e
	}
	if e = v.Objects.Delete(ctx, key); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE user_cloud_storage SET reserved_bytes=MAX(0,reserved_bytes-?) WHERE user_id=?`, size, user); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM meeting_audio_assets WHERE id=?`, id); e != nil {
		return e
	}
	return tx.Commit()
}

// ArchivePending only reads files already owned by a database meeting. It never
// accepts a client-provided server path. Old recordings with no verified owner
// are not guessed or assigned to another account.
func (v *Vault) ArchivePending(ctx context.Context) error {
	if v.Objects == nil {
		return nil
	}
	if e := v.RecoverUploads(ctx); e != nil {
		return e
	}
	rows, e := v.db.QueryContext(ctx, `SELECT m.id,m.user_id,m.audio_path FROM meetings m JOIN users u ON u.id=m.user_id LEFT JOIN meeting_audio_assets a ON a.meeting_id=m.id LEFT JOIN meeting_audio_tombstones t ON t.meeting_id=m.id AND t.user_id=m.user_id LEFT JOIN meeting_archive_status s ON s.meeting_id=m.id WHERE a.id IS NULL AND t.meeting_id IS NULL AND u.plan_tier='pro' AND u.plan_active=1 AND COALESCE(s.next_attempt,0)<=? AND NOT EXISTS(SELECT 1 FROM private_account_deletions d WHERE d.user_id=m.user_id) ORDER BY COALESCE(s.next_attempt,0),m.id DESC LIMIT 10`, time.Now().Unix())
	if e != nil {
		return e
	}
	type candidate struct {
		id, user int64
		path     string
	}
	var batch []candidate
	for rows.Next() {
		var r candidate
		if e = rows.Scan(&r.id, &r.user, &r.path); e != nil {
			rows.Close()
			return e
		}
		batch = append(batch, r)
	}
	rows.Close()
	for _, r := range batch {
		state, next := "ready", time.Now().Add(time.Hour).Unix()
		if !v.trustedRecording(ctx, r.user, r.id, r.path) {
			e = ErrNotFound
		} else {
			e = v.Archive(ctx, r.user, r.id, r.path)
		}
		switch {
		case errors.Is(e, ErrQuota):
			state = "storage_full"
		case errors.Is(e, ErrNotFound):
			state = "local_file_unavailable"
			next = time.Now().Add(24 * time.Hour).Unix()
		case e != nil:
			state = "pending"
			next = time.Now().Add(5 * time.Minute).Unix()
		}
		if _, err := v.db.ExecContext(ctx, `INSERT INTO meeting_archive_status(meeting_id,user_id,state,next_attempt) VALUES(?,?,?,?) ON CONFLICT(meeting_id) DO UPDATE SET state=excluded.state,next_attempt=excluded.next_attempt`, r.id, r.user, state, next); err != nil {
			return ErrUnavailable
		}
	}
	// Process each completed staging directory once. Missing files count as purged.
	rows, e = v.db.QueryContext(ctx, `SELECT m.id,m.user_id,j.job_key FROM meetings m JOIN meeting_audio_assets a ON a.meeting_id=m.id JOIN meeting_jobs j ON j.meeting_id=m.id LEFT JOIN meeting_archive_status s ON s.meeting_id=m.id WHERE a.state='ready' AND j.status='completed' AND COALESCE(s.processing_purged,0)=0 LIMIT 100`)
	if e != nil {
		return e
	}
	type cleanup struct {
		id, user int64
		key      string
	}
	var clean []cleanup
	for rows.Next() {
		var r cleanup
		if e = rows.Scan(&r.id, &r.user, &r.key); e != nil {
			rows.Close()
			return e
		}
		clean = append(clean, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range clean {
		if e = v.purgeProcessing(r.key); e != nil {
			return e
		}
		if _, e = v.db.ExecContext(ctx, `INSERT INTO meeting_archive_status(meeting_id,user_id,state,processing_purged) VALUES(?,?,'ready',1) ON CONFLICT(meeting_id) DO UPDATE SET processing_purged=1`, r.id, r.user); e != nil {
			return e
		}
	}

	legacy, e := v.db.QueryContext(ctx, `SELECT m.id,m.user_id,m.audio_path FROM meetings m JOIN meeting_audio_assets a ON a.meeting_id=m.id LEFT JOIN meeting_jobs j ON j.meeting_id=m.id LEFT JOIN meeting_archive_status s ON s.meeting_id=m.id WHERE a.state='ready' AND j.job_key IS NULL AND COALESCE(s.processing_purged,0)=0 AND (COALESCE(m.transcript,'')<>'' OR datetime(m.timestamp)<datetime('now','-24 hours')) LIMIT 100`)
	if e != nil {
		return e
	}
	var old []candidate
	for legacy.Next() {
		var r candidate
		if e = legacy.Scan(&r.id, &r.user, &r.path); e != nil {
			legacy.Close()
			return e
		}
		old = append(old, r)
	}
	e = legacy.Err()
	legacy.Close()
	if e != nil {
		return e
	}
	for _, r := range old {
		if e = v.purgeLegacy(r.user, r.path); e != nil {
			continue
		}
		if _, e = v.db.ExecContext(ctx, `INSERT INTO meeting_archive_status(meeting_id,user_id,state,processing_purged) VALUES(?,?,'ready',1) ON CONFLICT(meeting_id) DO UPDATE SET processing_purged=1`, r.id, r.user); e != nil {
			return e
		}
	}

	return nil
}
func legacyPath(user int64, path string) (string, bool) {
	root, e := filepath.Abs("uploads/meetings")
	if e != nil {
		return "", false
	}
	abs, e := filepath.Abs(path)
	if e != nil || filepath.Dir(abs) != root {
		return "", false
	}
	name := filepath.Base(abs)
	prefix := fmt.Sprintf("meeting_%d_", user)
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".ogg") {
		return "", false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".ogg")
	if number == "" {
		return "", false
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return abs, true
}
func (v *Vault) trustedRecording(ctx context.Context, user, meeting int64, path string) bool {
	var key, client string
	e := v.db.QueryRowContext(ctx, `SELECT job_key,client_id FROM meeting_jobs WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&key, &client)
	var expected string
	if errors.Is(e, sql.ErrNoRows) {
		var ok bool
		expected, ok = legacyPath(user, path)
		if !ok {
			return false
		}
		var copies int
		if v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meetings WHERE audio_path=?`, path).Scan(&copies) != nil || copies != 1 {
			return false
		}
	} else if e == nil {
		if key != fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d/%s", user, client)))) {
			return false
		}
		root := os.Getenv("MEETING_AUDIO_DIR")
		if root == "" {
			root = "uploads/meetings/jobs"
		}
		expected, e = filepath.Abs(filepath.Join(root, key, "mixed.ogg"))
		if e != nil {
			return false
		}
	} else {
		return false
	}
	actual, e := filepath.Abs(path)
	if e != nil || actual != expected {
		return false
	}
	// Directory links may be configured, but a recording must never resolve to
	// another recording or a file outside its server-owned directory.
	parent, e := filepath.EvalSymlinks(filepath.Dir(expected))
	if e != nil {
		return false
	}
	canonical, e := filepath.EvalSymlinks(expected)
	return e == nil && canonical == filepath.Join(parent, filepath.Base(expected))
}
func (v *Vault) purgeLegacy(user int64, path string) error {
	p, ok := legacyPath(user, path)
	if !ok {
		return ErrNotFound
	}
	if e := os.Remove(p); e != nil && !os.IsNotExist(e) {
		return ErrUnavailable
	}
	return nil
}

func (v *Vault) purgeProcessing(key string) error {
	if len(key) != 64 {
		return ErrUnavailable
	}
	for _, c := range key {
		if !((c >= 'a' && c <= 'f') || (c >= '0' && c <= '9')) {
			return ErrUnavailable
		}
	}
	root := os.Getenv("MEETING_AUDIO_DIR")
	if root == "" {
		root = "uploads/meetings/jobs"
	}
	for _, name := range []string{"mixed.ogg", "mic.ogg", "system.ogg"} {
		if e := os.Remove(filepath.Join(root, key, name)); e != nil && !os.IsNotExist(e) {
			return ErrUnavailable
		}
	}
	return nil
}
func (v *Vault) CloudState(ctx context.Context, user, meeting int64) string {
	if v.Objects == nil {
		return "unavailable"
	}
	if v.CloudDeleted(ctx, user, meeting) {
		return "deleted"
	}
	var state string
	if v.db.QueryRowContext(ctx, `SELECT state FROM meeting_audio_assets WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&state) == nil {
		if state == "ready" {
			return "ready"
		}
		return "pending"
	}
	if v.db.QueryRowContext(ctx, `SELECT state FROM meeting_archive_status WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&state) == nil {
		return state
	}
	return "pending"
}

func threeMonths(t time.Time) time.Time {
	day := t.Day()
	first := time.Date(t.Year(), t.Month()+3, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}
func (v *Vault) Retention(ctx context.Context, now time.Time) error {
	if v.Objects == nil {
		return nil
	}
	// New/legacy non-paying accounts start a conservative clock when observed;
	// unknown historical billing is never backdated into immediate deletion.
	_, e := v.db.ExecContext(ctx, `UPDATE user_cloud_storage SET unpaid_since=CASE WHEN EXISTS(SELECT 1 FROM users u WHERE u.id=user_cloud_storage.user_id AND u.plan_tier='pro' AND u.plan_active=1 AND (COALESCE(u.razorpay_current_end,0)=0 OR u.razorpay_current_end+172800>?)) THEN 0 WHEN unpaid_since=0 THEN ? ELSE unpaid_since END`, now.Unix(), now.Unix())
	if e != nil {
		return e
	}
	rows, e := v.db.QueryContext(ctx, `SELECT user_id,unpaid_since FROM user_cloud_storage WHERE unpaid_since>0`)
	if e != nil {
		return e
	}
	var expired []int64
	for rows.Next() {
		var user, since int64
		if rows.Scan(&user, &since) == nil && !now.Before(threeMonths(time.Unix(since, 0).UTC())) {
			expired = append(expired, user)
		}
	}
	rows.Close()
	for _, user := range expired {
		if paid(ctx, v.db, user) {
			continue
		}
		staging, e := v.db.QueryContext(ctx, `SELECT job_key FROM meeting_jobs WHERE user_id=? AND status IN ('completed','failed','cancelled') AND admitted=0`, user)
		if e != nil {
			return e
		}
		var keys []string
		for staging.Next() {
			var key string
			if staging.Scan(&key) == nil {
				keys = append(keys, key)
			}
		}
		staging.Close()
		for _, key := range keys {
			if e = v.purgeProcessing(key); e != nil {
				return e
			}
		}
		legacy, e := v.db.QueryContext(ctx, `SELECT m.audio_path FROM meetings m LEFT JOIN meeting_jobs j ON j.meeting_id=m.id WHERE m.user_id=? AND j.job_key IS NULL`, user)
		if e != nil {
			return e
		}
		var legacyPaths []string
		for legacy.Next() {
			var p string
			if legacy.Scan(&p) == nil {
				legacyPaths = append(legacyPaths, p)
			}
		}
		legacy.Close()
		for _, p := range legacyPaths {
			_ = v.purgeLegacy(user, p)
		}
		objects, e := v.db.QueryContext(ctx, `SELECT meeting_id FROM meeting_audio_assets WHERE user_id=? LIMIT 10`, user)
		if e != nil {
			return e
		}
		var ids []int64
		for objects.Next() {
			var id int64
			if objects.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		objects.Close()
		for _, id := range ids {
			deleteCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			e = v.DeleteAudio(deleteCtx, user, id, true)
			cancel()
			if e != nil {
				return e
			}
		}
	}
	return nil
}
func (v *Vault) Run(ctx context.Context, onError func(string)) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			work, cancel := context.WithTimeout(ctx, 20*time.Minute)
			if e := v.ArchivePending(work); e != nil {
				onError("encrypted audio archive pending")
			}
			if e := v.Retention(work, time.Now().UTC()); e != nil {
				onError("cloud audio cleanup pending")
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
