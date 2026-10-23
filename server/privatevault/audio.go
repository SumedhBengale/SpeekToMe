package privatevault

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/minio/sio"
	"io"
	"os"
	"time"
)

type AudioAsset struct {
	ID          string `json:"id"`
	MeetingID   int64  `json:"meeting_id"`
	PlainBytes  int64  `json:"bytes"`
	CipherBytes int64  `json:"stored_bytes"`
	State       string `json:"state"`
}
type StorageUsage struct {
	Limit      int64 `json:"limit_bytes"`
	Used       int64 `json:"used_bytes"`
	Reserved   int64 `json:"reserved_bytes"`
	Enabled    bool  `json:"enabled"`
	PurgeAfter int64 `json:"purge_after,omitempty"`
}

func streamConfig(key []byte) sio.Config {
	return sio.Config{Key: key, MinVersion: sio.Version20, MaxVersion: sio.Version20, CipherSuites: []byte{sio.AES_GCM}}
}
func (v *Vault) Usage(ctx context.Context, user int64) (StorageUsage, error) {
	var u StorageUsage
	_, e := v.db.ExecContext(ctx, `INSERT INTO user_cloud_storage(user_id) VALUES(?) ON CONFLICT(user_id) DO NOTHING`, user)
	if e != nil {
		return u, ErrUnavailable
	}
	e = v.db.QueryRowContext(ctx, `SELECT limit_bytes,used_bytes,reserved_bytes FROM user_cloud_storage WHERE user_id=?`, user).Scan(&u.Limit, &u.Used, &u.Reserved)
	u.Enabled = v.Objects != nil
	var since int64
	if e == nil && v.db.QueryRowContext(ctx, `SELECT unpaid_since FROM user_cloud_storage WHERE user_id=?`, user).Scan(&since) == nil && since > 0 {
		u.PurgeAfter = threeMonths(time.Unix(since, 0).UTC()).Unix()
	}
	return u, e
}
func paid(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, user int64) bool {
	var active int
	var tier string
	var end int64
	e := q.QueryRowContext(ctx, `SELECT COALESCE(plan_tier,'free'),COALESCE(plan_active,0),COALESCE(razorpay_current_end,0) FROM users WHERE id=?`, user).Scan(&tier, &active, &end)
	return e == nil && tier == "pro" && active == 1 && (end == 0 || end+48*3600 > time.Now().Unix())
}

// Archive uploads a legacy plaintext recording that predates the encrypted
// spool. Migration archives it regardless of plan so it can leave local disk;
// the normal unpaid-retention clock still applies afterwards.
func (v *Vault) Archive(ctx context.Context, user, meeting int64, path string) error {
	file, e := os.Open(path)
	if e != nil {
		return ErrNotFound
	}
	defer file.Close()
	st, e := file.Stat()
	if e != nil || st.Size() <= 0 {
		return ErrNotFound
	}
	return v.archive(ctx, user, meeting, file, st.Size(), true)
}

// ArchiveReader encrypts a recording streamed from the meeting spool into the
// private bucket. Plaintext is never written to local storage.
func (v *Vault) ArchiveReader(ctx context.Context, user, meeting int64, body io.Reader, plainSize int64) error {
	return v.archive(ctx, user, meeting, body, plainSize, false)
}

func (v *Vault) archive(ctx context.Context, user, meeting int64, body io.Reader, plainSize int64, migration bool) error {
	if v.Objects == nil {
		return ErrUnavailable
	}
	var owner int64
	if v.db.QueryRowContext(ctx, `SELECT user_id FROM meetings WHERE id=? AND NOT EXISTS(SELECT 1 FROM private_account_deletions WHERE user_id=?)`, meeting, user).Scan(&owner) != nil || owner != user {
		return ErrNotFound
	}
	if !migration && !paid(ctx, v.db, user) {
		return ErrNotPaid
	}
	if plainSize <= 0 {
		return ErrNotFound
	}
	size, e := sio.EncryptedSize(uint64(plainSize))
	if e != nil || size > uint64(DefaultQuota) {
		return ErrQuota
	}
	var existingState string
	e = v.db.QueryRowContext(ctx, `SELECT state FROM meeting_audio_assets WHERE meeting_id=? AND user_id=?`, meeting, user).Scan(&existingState)
	if e == nil {
		if existingState == "ready" || existingState == "uploading" {
			return nil
		}
		return errors.New("cloud audio asset is not available for upload")
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return ErrUnavailable
	}
	accountKey, e := v.UserKey(ctx, user)
	if e != nil {
		return e
	}
	defer clear(accountKey)
	random := make([]byte, 16)
	if _, e = rand.Read(random); e != nil {
		return ErrUnavailable
	}
	id := hex.EncodeToString(random)
	dataKey := make([]byte, 32)
	if _, e = rand.Read(dataKey); e != nil {
		return ErrUnavailable
	}
	defer clear(dataKey)
	a, e := aead(accountKey)
	if e != nil {
		return e
	}
	wrapped, e := v.seal(a, dataKey, fmt.Sprintf("audio-key-v1:%d:%d:%s:%d", user, meeting, id, plainSize))
	if e != nil {
		return e
	}
	tx, e := v.db.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO user_cloud_storage(user_id) VALUES(?) ON CONFLICT(user_id) DO NOTHING`, user)
	if e != nil {
		return ErrUnavailable
	}
	// Migration moves existing recordings off local disk even past the quota;
	// new recordings must fit.
	quotaClause := ` AND used_bytes+reserved_bytes+?<=limit_bytes`
	args := []any{int64(size), user, int64(size), user, meeting, user}
	if migration {
		quotaClause = ``
		args = []any{int64(size), user, user, meeting, user}
	}
	reservation, e := tx.ExecContext(ctx, `UPDATE user_cloud_storage SET reserved_bytes=reserved_bytes+? WHERE user_id=?`+quotaClause+` AND NOT EXISTS(SELECT 1 FROM meeting_audio_tombstones WHERE user_id=? AND meeting_id=?) AND NOT EXISTS(SELECT 1 FROM private_account_deletions WHERE user_id=?)`, args...)
	if e != nil {
		return ErrUnavailable
	}
	n, _ := reservation.RowsAffected()
	if n != 1 {
		return ErrQuota
	}
	key := "audio/v1/" + id
	_, e = tx.ExecContext(ctx, `INSERT INTO meeting_audio_assets(id,user_id,meeting_id,object_key,wrapped_key,plain_bytes,cipher_bytes,state,created_at) VALUES(?,?,?,?,?,?,?,'uploading',?)`, id, user, meeting, key, wrapped, plainSize, int64(size), time.Now().Unix())
	if e != nil {
		return ErrUnavailable
	}
	if e = tx.Commit(); e != nil {
		return ErrUnavailable
	}
	counted := &countingReader{r: body}
	encrypted, e := sio.EncryptReader(counted, streamConfig(dataKey))
	if e == nil {
		e = v.Objects.Put(ctx, key, encrypted)
	}
	if e == nil && counted.n != plainSize {
		e = ErrUnavailable
	}
	if e != nil {
		// Keep the reservation and a durable deletion marker until R2 cleanup is
		// confirmed. A failed/ambiguous PUT must not create uncharged orphan objects.
		_, _ = v.db.ExecContext(context.Background(), `UPDATE meeting_audio_assets SET state='failed',last_error='cloud upload failed' WHERE id=? AND state='uploading'`, id)
		return ErrUnavailable
	}
	tx, e = v.db.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE meeting_audio_assets SET state='ready',last_error='' WHERE id=? AND state='uploading'`, id)
	if e != nil {
		return ErrUnavailable
	}
	n, _ = result.RowsAffected()
	if n != 1 {
		return ErrUnavailable
	}
	result, e = tx.ExecContext(ctx, `UPDATE user_cloud_storage SET used_bytes=used_bytes+?,reserved_bytes=reserved_bytes-? WHERE user_id=? AND reserved_bytes>=?`, int64(size), int64(size), user, int64(size))
	if e != nil {
		return ErrUnavailable
	}
	n, _ = result.RowsAffected()
	if n != 1 {
		return ErrUnavailable
	}
	return tx.Commit()
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, e := c.r.Read(p)
	c.n += int64(n)
	return n, e
}

func (v *Vault) Asset(ctx context.Context, user, meeting int64) (AudioAsset, error) {
	var a AudioAsset
	e := v.db.QueryRowContext(ctx, `SELECT id,meeting_id,plain_bytes,cipher_bytes,state FROM meeting_audio_assets WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&a.ID, &a.MeetingID, &a.PlainBytes, &a.CipherBytes, &a.State)
	if e != nil {
		return a, ErrNotFound
	}
	return a, nil
}

type objectReader struct {
	ctx     context.Context
	objects Objects
	key     string
}

func (r objectReader) ReadAt(p []byte, off int64) (int, error) {
	return r.objects.ReadAt(r.ctx, r.key, p, off)
}
func (v *Vault) Audio(ctx context.Context, user, meeting int64) (io.ReadSeeker, int64, error) {
	if v.Objects == nil {
		return nil, 0, ErrUnavailable
	}
	var id, key, wrapped string
	var size int64
	e := v.db.QueryRowContext(ctx, `SELECT id,object_key,wrapped_key,plain_bytes FROM meeting_audio_assets WHERE user_id=? AND meeting_id=? AND state='ready'`, user, meeting).Scan(&id, &key, &wrapped, &size)
	if e != nil {
		return nil, 0, ErrNotFound
	}
	accountKey, e := v.UserKey(ctx, user)
	if e != nil {
		return nil, 0, e
	}
	defer clear(accountKey)
	a, e := aead(accountKey)
	if e != nil {
		return nil, 0, e
	}
	dataKey, e := v.open(a, wrapped, fmt.Sprintf("audio-key-v1:%d:%d:%s:%d", user, meeting, id, size))
	if e != nil {
		return nil, 0, e
	}
	defer clear(dataKey)
	reader, e := sio.DecryptReaderAt(objectReader{ctx, v.Objects, key}, streamConfig(dataKey))
	if e != nil {
		return nil, 0, ErrUnavailable
	}
	return io.NewSectionReader(reader, 0, size), size, nil
}

// DeleteAudio is a restart-safe idempotent transition. Metadata and quota only
// disappear after the exact R2 object is confirmed deleted.
func (v *Vault) DeleteAudio(ctx context.Context, user, meeting int64, retentionOnly bool) error {
	if v.Objects == nil {
		return ErrUnavailable
	}
	tx, e := v.db.BeginTx(ctx, nil)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	var id, key, state string
	var bytes, created int64
	e = tx.QueryRowContext(ctx, `SELECT id,object_key,state,cipher_bytes,created_at FROM meeting_audio_assets WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&id, &key, &state, &bytes, &created)
	if errors.Is(e, sql.ErrNoRows) {
		if _, e = tx.ExecContext(ctx, `INSERT INTO meeting_audio_tombstones(user_id,meeting_id,deleted_at) VALUES(?,?,?) ON CONFLICT DO NOTHING`, user, meeting, time.Now().Unix()); e != nil {
			return ErrUnavailable
		}
		return tx.Commit()
	}
	if e != nil {
		return ErrUnavailable
	}
	// Do not race an in-flight PUT: deleting its key before completion could
	// leave an orphan after the uploader finishes. Upload contexts expire in 20m.
	if state == "uploading" && created > time.Now().Add(-30*time.Minute).Unix() {
		return ErrUnavailable
	}
	// Acquire the write lock before rechecking billing; renewal commits and this
	// irreversible deletion are serialized. The caller imposes a short deadline.
	if _, e = tx.ExecContext(ctx, `UPDATE meeting_audio_assets SET state='deleting' WHERE id=?`, id); e != nil {
		return ErrUnavailable
	}
	if retentionOnly && paid(ctx, tx, user) {
		return errors.New("retention cancelled: account renewed")
	}
	if e = v.Objects.Delete(ctx, key); e != nil {
		return ErrUnavailable
	}
	field := "reserved_bytes"
	if state == "ready" {
		field = "used_bytes"
	}
	if _, e = tx.ExecContext(ctx, `UPDATE user_cloud_storage SET `+field+`=MAX(0,`+field+`-?) WHERE user_id=?`, bytes, user); e != nil {
		return ErrUnavailable
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM meeting_audio_assets WHERE id=?`, id); e != nil {
		return ErrUnavailable
	}
	// Tombstone prevents the background archiver re-uploading a purged recording.
	if _, e = tx.ExecContext(ctx, `INSERT INTO meeting_audio_tombstones(user_id,meeting_id,deleted_at) VALUES(?,?,?) ON CONFLICT(user_id,meeting_id) DO NOTHING`, user, meeting, time.Now().Unix()); e != nil {
		return ErrUnavailable
	}
	return tx.Commit()
}
func (v *Vault) CloudDeleted(ctx context.Context, user, meeting int64) bool {
	var n int
	_ = v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meeting_audio_tombstones WHERE user_id=? AND meeting_id=?`, user, meeting).Scan(&n)
	return n > 0
}
