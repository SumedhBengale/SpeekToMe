// Package audiospool holds meeting audio only while a meeting job needs it.
//
// Every uploaded chunk is sealed with AES-256-GCM under a per-job key before it
// reaches any storage backend, so plaintext audio exists only in process
// memory. In production the backend is the private object bucket; the local
// directory backend exists for development and still stores ciphertext only.
package audiospool

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxChunk bounds a single sealed chunk; the desktop uploads 4 MiB pieces.
const MaxChunk = 4 << 20

const format byte = 1

var (
	ErrOffset      = errors.New("upload offset mismatch")
	ErrUnavailable = errors.New("encrypted audio spool unavailable")
	ErrCorrupt     = errors.New("encrypted audio spool is incomplete")
)

// Store is the subset of the private object store the spool needs. The R2
// client in privatevault satisfies it.
type Store interface {
	Put(context.Context, string, io.Reader) error
	ReadAt(context.Context, string, []byte, int64) (int, error)
	Delete(context.Context, string) error
}

// KeyFunc returns the 32-byte key for one job. Production keys are derived
// from the account key, so deleting an account also makes its spool unreadable.
type KeyFunc func(ctx context.Context, user int64, job string) ([]byte, error)

type Spool struct {
	db    *sql.DB
	store Store
	keys  KeyFunc
}

var (
	jobPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	channelPattern = regexp.MustCompile(`^[a-z]{1,16}$`)
)

func New(db *sql.DB, store Store, keys KeyFunc) (*Spool, error) {
	if db == nil || store == nil || keys == nil {
		return nil, ErrUnavailable
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS meeting_spool_chunks(job_key TEXT NOT NULL,channel TEXT NOT NULL,position INTEGER NOT NULL,size INTEGER NOT NULL,PRIMARY KEY(job_key,channel,position))`)
	if err != nil {
		return nil, err
	}
	return &Spool{db: db, store: store, keys: keys}, nil
}

func objectKey(job, channel string, offset int64) string {
	return fmt.Sprintf("spool/v1/%s/%s/%016d", job, channel, offset)
}

func aad(job, channel string, offset int64, size int) []byte {
	return []byte(fmt.Sprintf("speektome-meeting-spool-v1|%s|%s|%d|%d", job, channel, offset, size))
}

func valid(job, channel string) bool {
	return jobPattern.MatchString(job) && channelPattern.MatchString(channel)
}

func (s *Spool) aead(ctx context.Context, user int64, job string) (cipher.AEAD, error) {
	key, err := s.keys(ctx, user, job)
	if err != nil || len(key) != 32 {
		return nil, ErrUnavailable
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	return cipher.NewGCM(block)
}

// Size is the number of plaintext bytes accepted for one channel.
func (s *Spool) Size(ctx context.Context, job, channel string) (int64, error) {
	var size int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size),0) FROM meeting_spool_chunks WHERE job_key=? AND channel=?`, job, channel).Scan(&size)
	return size, err
}

// Append seals one chunk at the channel's current end. Callers serialize
// appends per job. A retried chunk at the same offset overwrites the same
// object, so an ambiguous network failure never leaves duplicate audio.
func (s *Spool) Append(ctx context.Context, user int64, job, channel string, offset int64, data []byte) (int64, error) {
	if !valid(job, channel) || len(data) == 0 || len(data) > MaxChunk {
		return 0, ErrUnavailable
	}
	size, err := s.Size(ctx, job, channel)
	if err != nil {
		return 0, ErrUnavailable
	}
	if size != offset {
		return size, ErrOffset
	}
	gcm, err := s.aead(ctx, user, job)
	if err != nil {
		return size, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return size, ErrUnavailable
	}
	sealed := make([]byte, 0, 1+len(nonce)+len(data)+gcm.Overhead())
	sealed = append(sealed, format)
	sealed = append(sealed, nonce...)
	sealed = gcm.Seal(sealed, nonce, data, aad(job, channel, offset, len(data)))
	if err = s.store.Put(ctx, objectKey(job, channel, offset), bytes.NewReader(sealed)); err != nil {
		return size, ErrUnavailable
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO meeting_spool_chunks(job_key,channel,position,size) VALUES(?,?,?,?)`, job, channel, offset, len(data)); err != nil {
		return size, ErrUnavailable
	}
	return offset + int64(len(data)), nil
}

type chunk struct {
	offset int64
	size   int
}

func (s *Spool) chunks(ctx context.Context, job, channel string) ([]chunk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT position,size FROM meeting_spool_chunks WHERE job_key=? AND channel=? ORDER BY position`, job, channel)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	var list []chunk
	var next int64
	for rows.Next() {
		var c chunk
		if err = rows.Scan(&c.offset, &c.size); err != nil {
			return nil, ErrUnavailable
		}
		if c.offset != next || c.size <= 0 || c.size > MaxChunk {
			return nil, ErrCorrupt
		}
		next += int64(c.size)
		list = append(list, c)
	}
	return list, rows.Err()
}

// Open returns a sequential plaintext reader for one channel and its size.
// Only one decrypted chunk is held in memory at a time.
func (s *Spool) Open(ctx context.Context, user int64, job, channel string) (io.ReadCloser, int64, error) {
	if !valid(job, channel) {
		return nil, 0, ErrUnavailable
	}
	list, err := s.chunks(ctx, job, channel)
	if err != nil {
		return nil, 0, err
	}
	if len(list) == 0 {
		return nil, 0, ErrCorrupt
	}
	gcm, err := s.aead(ctx, user, job)
	if err != nil {
		return nil, 0, err
	}
	last := list[len(list)-1]
	return &reader{ctx: ctx, spool: s, gcm: gcm, job: job, channel: channel, chunks: list}, last.offset + int64(last.size), nil
}

type reader struct {
	ctx     context.Context
	spool   *Spool
	gcm     cipher.AEAD
	job     string
	channel string
	chunks  []chunk
	current []byte
	closed  bool
}

func (r *reader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	for len(r.current) == 0 {
		if len(r.chunks) == 0 {
			return 0, io.EOF
		}
		c := r.chunks[0]
		r.chunks = r.chunks[1:]
		sealed := make([]byte, 1+r.gcm.NonceSize()+c.size+r.gcm.Overhead())
		n, err := r.spool.store.ReadAt(r.ctx, objectKey(r.job, r.channel, c.offset), sealed, 0)
		if n != len(sealed) || (err != nil && err != io.EOF) || sealed[0] != format {
			return 0, ErrCorrupt
		}
		nonce := sealed[1 : 1+r.gcm.NonceSize()]
		ciphertext := sealed[1+r.gcm.NonceSize():]
		plain, err := r.gcm.Open(ciphertext[:0], nonce, ciphertext, aad(r.job, r.channel, c.offset, c.size))
		if err != nil {
			return 0, ErrCorrupt
		}
		r.current = plain
	}
	n := copy(p, r.current)
	r.current = r.current[n:]
	return n, nil
}

func (r *reader) Close() error {
	clear(r.current)
	r.current = nil
	r.closed = true
	return nil
}

// Delete removes every sealed chunk for a job. Rows are removed only after the
// object deletion succeeds, so a failed delete is retried by maintenance.
func (s *Spool) Delete(ctx context.Context, job string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT channel,position FROM meeting_spool_chunks WHERE job_key=?`, job)
	if err != nil {
		return ErrUnavailable
	}
	type item struct {
		channel string
		offset  int64
	}
	var items []item
	for rows.Next() {
		var i item
		if rows.Scan(&i.channel, &i.offset) == nil {
			items = append(items, i)
		}
	}
	rows.Close()
	for _, i := range items {
		if err = s.store.Delete(ctx, objectKey(job, i.channel, i.offset)); err != nil {
			return ErrUnavailable
		}
		if _, err = s.db.ExecContext(ctx, `DELETE FROM meeting_spool_chunks WHERE job_key=? AND channel=? AND position=?`, job, i.channel, i.offset); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

// Orphans lists jobs whose chunks outlived their meeting job row, for example
// after an account deletion or an expired upload.
func (s *Spool) Orphans(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.job_key FROM meeting_spool_chunks c LEFT JOIN meeting_jobs j ON j.job_key=c.job_key WHERE j.job_key IS NULL LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []string
	for rows.Next() {
		var job string
		if rows.Scan(&job) == nil {
			jobs = append(jobs, job)
		}
	}
	return jobs, rows.Err()
}

// Has reports whether any sealed audio remains for a job.
func (s *Spool) Has(ctx context.Context, job string) bool {
	var n int
	return s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meeting_spool_chunks WHERE job_key=?`, job).Scan(&n) == nil && n > 0
}

// DirStore keeps sealed chunks in a local directory. It never receives
// plaintext; use it only where no object bucket is configured.
type DirStore struct{ Root string }

func (d DirStore) path(key string) (string, error) {
	if !strings.HasPrefix(key, "spool/v1/") || strings.Contains(key, "..") || strings.ContainsAny(key, "\\\x00") {
		return "", ErrUnavailable
	}
	return filepath.Join(d.Root, filepath.FromSlash(key)), nil
}

func (d DirStore) Put(_ context.Context, key string, body io.Reader) error {
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return ErrUnavailable
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return ErrUnavailable
	}
	_, err = io.Copy(file, body)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err != nil {
		_ = os.Remove(temporary)
		return ErrUnavailable
	}
	return nil
}

func (d DirStore) ReadAt(_ context.Context, key string, p []byte, off int64) (int, error) {
	path, err := d.path(key)
	if err != nil {
		return 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer file.Close()
	return file.ReadAt(p, off)
}

func (d DirStore) Delete(_ context.Context, key string) error {
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return ErrUnavailable
	}
	// Remove now-empty job and channel directories; failures are harmless.
	_ = os.Remove(filepath.Dir(path))
	_ = os.Remove(filepath.Dir(filepath.Dir(path)))
	return nil
}
