package privatevault

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type memoryObjects struct {
	mu                  sync.Mutex
	files               map[string][]byte
	failPut, failDelete bool
}

func (m *memoryObjects) Put(_ context.Context, k string, r io.Reader) error {
	b, e := io.ReadAll(r)
	if e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[k] = b
	if m.failPut {
		return errors.New("ambiguous upload")
	}
	return nil
}
func (m *memoryObjects) ReadAt(_ context.Context, k string, p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return bytes.NewReader(m.files[k]).ReadAt(p, off)
}
func (m *memoryObjects) Delete(_ context.Context, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete {
		return errors.New("delete failed")
	}
	delete(m.files, k)
	return nil
}
func fixture(t *testing.T) (*Vault, []byte, *memoryObjects) {
	t.Helper()
	d, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	for _, s := range []string{`CREATE TABLE users(id INTEGER PRIMARY KEY,plan_tier TEXT,plan_active INTEGER,razorpay_current_end INTEGER)`, `INSERT INTO users VALUES(1,'pro',1,0),(2,'pro',1,0)`, `CREATE TABLE meetings(id INTEGER PRIMARY KEY,user_id INTEGER,audio_path TEXT)`, `INSERT INTO meetings VALUES(1,1,''),(2,1,''),(3,2,'')`, `CREATE TABLE meeting_jobs(job_key TEXT,user_id INTEGER,meeting_id INTEGER,status TEXT,admitted INTEGER)`, `CREATE TABLE usage_logs(id INTEGER PRIMARY KEY,user_id INTEGER,input_transcript TEXT,final_output TEXT,vocabulary_audit TEXT)`} {
		if _, e = d.Exec(s); e != nil {
			t.Fatal(e)
		}
	}
	key := make([]byte, 32)
	rand.Read(key)
	v, e := New(context.Background(), d, key)
	if e != nil {
		t.Fatal(e)
	}
	m := &memoryObjects{files: make(map[string][]byte)}
	v.Objects = m
	return v, key, m
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestHistoryEncryptionIsolationAndIdempotency(t *testing.T) {
	v, key, _ := fixture(t)
	ctx := context.Background()
	id := "b448a06a-5aa5-4953-871f-a705361b12ba"
	text := "Private final output: नमस्ते 世界"
	must(t, v.SaveFinal(ctx, 1, id, "command", text))
	must(t, v.SaveFinal(ctx, 1, id, "command", text))
	if v.SaveFinal(ctx, 1, id, "command", "different") == nil {
		t.Fatal("conflicting identity accepted")
	}
	rows, e := v.History(ctx, 1, 0)
	must(t, e)
	if len(rows) != 1 || rows[0].Text != text || rows[0].Mode != "command" {
		t.Fatal("final roundtrip failed")
	}
	other, e := v.History(ctx, 2, 0)
	must(t, e)
	if len(other) != 0 {
		t.Fatal("cross-account history")
	}
	var sealed, wrapped string
	must(t, v.db.QueryRow(`SELECT ciphertext FROM transcription_history`).Scan(&sealed))
	must(t, v.db.QueryRow(`SELECT wrapped_key FROM user_encryption_keys WHERE user_id=1`).Scan(&wrapped))
	if bytes.Contains([]byte(sealed+wrapped), []byte(text)) || bytes.Contains([]byte(wrapped), key) {
		t.Fatal("plaintext storage")
	}
	if _, e = v.DecryptText(ctx, 2, "history-v1", id, sealed); e == nil {
		t.Fatal("wrong account decrypted")
	}
	if _, e = v.DecryptText(ctx, 1, "history-v1", "wrong-id", sealed); e == nil {
		t.Fatal("wrong identity decrypted")
	}
	wrong := bytes.Repeat([]byte{1}, 32)
	if _, e = New(ctx, v.db, wrong); e == nil {
		t.Fatal("wrong master accepted")
	}
	reopened, e := New(ctx, v.db, key)
	must(t, e)
	rows, e = reopened.History(ctx, 1, 0)
	must(t, e)
	if rows[0].Text != text {
		t.Fatal("restart lost key")
	}
	must(t, v.DeleteHistory(ctx, 2, id))
	rows, e = v.History(ctx, 1, 0)
	must(t, e)
	if len(rows) != 1 {
		t.Fatal("cross-account deletion")
	}
	must(t, v.DeleteHistory(ctx, 1, id))
	rows, e = v.History(ctx, 1, 0)
	must(t, e)
	if len(rows) != 0 {
		t.Fatal("deletion failed")
	}
}
func TestHistoryPaginationWithinSameSecond(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 61; i++ {
		must(t, v.SaveFinal(ctx, 1, fmt.Sprintf("00000000-0000-0000-0000-%012d", i), "dictation", "final"))
	}
	_, e := v.db.Exec(`UPDATE transcription_history SET created_at=100`)
	must(t, e)
	first, e := v.History(ctx, 1, 101)
	must(t, e)
	if len(first) != 50 {
		t.Fatal(len(first))
	}
	last := first[len(first)-1]
	next, e := v.History(ctx, 1, last.CreatedAt, last.ID)
	must(t, e)
	if len(next) != 11 {
		t.Fatal("cursor skipped tied timestamps", len(next))
	}
}
func audioFile(t *testing.T) (string, []byte) {
	t.Helper()
	b := make([]byte, 250123)
	rand.Read(b)
	p := filepath.Join(t.TempDir(), "mixed.ogg")
	must(t, os.WriteFile(p, b, 0600))
	return p, b
}
func TestAudioEncryptedRandomAccessTamperOwnership(t *testing.T) {
	v, _, objects := fixture(t)
	ctx := context.Background()
	path, plain := audioFile(t)
	must(t, v.Archive(ctx, 1, 1, path))
	a, e := v.Asset(ctx, 1, 1)
	must(t, e)
	if a.State != "ready" || a.CipherBytes <= a.PlainBytes {
		t.Fatal(a)
	}
	for _, data := range objects.files {
		if bytes.Equal(data, plain) || bytes.Contains(data, plain[:64]) {
			t.Fatal("audio unencrypted")
		}
	}
	reader, size, e := v.Audio(ctx, 1, 1)
	must(t, e)
	if size != int64(len(plain)) {
		t.Fatal(size)
	}
	for _, offset := range []int64{0, 65532, 170023, int64(len(plain) - 80)} {
		_, e = reader.Seek(offset, io.SeekStart)
		must(t, e)
		buf := make([]byte, 80)
		_, e = io.ReadFull(reader, buf)
		must(t, e)
		if !bytes.Equal(buf, plain[offset:offset+80]) {
			t.Fatal("range mismatch", offset)
		}
	}
	_, e = reader.Seek(0, io.SeekStart)
	must(t, e)
	all, e := io.ReadAll(reader)
	must(t, e)
	if !bytes.Equal(all, plain) {
		t.Fatal("full audio mismatch")
	}
	if _, _, e = v.Audio(ctx, 2, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-user read")
	}
	if e = v.Archive(ctx, 2, 1, path); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross-user archive")
	}
	for k, data := range objects.files {
		data[50] ^= 1
		objects.files[k] = data
	}
	reader, _, e = v.Audio(ctx, 1, 1)
	must(t, e)
	if _, e = io.ReadAll(reader); e == nil {
		t.Fatal("corruption not authenticated")
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("local original removed")
	}
}
func TestQuotaFailureRecoveryAndDeletion(t *testing.T) {
	v, _, objects := fixture(t)
	ctx := context.Background()
	path, _ := audioFile(t)
	objects.failPut = true
	if v.Archive(ctx, 1, 1, path) == nil {
		t.Fatal("failed PUT accepted")
	}
	u, e := v.Usage(ctx, 1)
	must(t, e)
	if u.Reserved <= 0 || u.Used != 0 {
		t.Fatal(u)
	}
	objects.failDelete = true
	if v.RecoverUploads(ctx) == nil {
		t.Fatal("delete failure ignored")
	}
	u2, e := v.Usage(ctx, 1)
	must(t, e)
	if u2 != u {
		t.Fatal("released before deletion")
	}
	objects.failDelete = false
	must(t, v.RecoverUploads(ctx))
	objects.failPut = false
	must(t, v.Archive(ctx, 1, 1, path))
	u, e = v.Usage(ctx, 1)
	must(t, e)
	if u.Reserved != 0 || u.Used <= 0 {
		t.Fatal(u)
	}
	_, e = v.db.Exec(`UPDATE user_cloud_storage SET limit_bytes=? WHERE user_id=1`, u.Used)
	must(t, e)
	if !errors.Is(v.Archive(ctx, 1, 2, path), ErrQuota) {
		t.Fatal("quota not enforced")
	}
	must(t, v.DeleteAudio(ctx, 1, 1, false))
	u, e = v.Usage(ctx, 1)
	must(t, e)
	if u.Used != 0 || u.Reserved != 0 {
		t.Fatal(u)
	}
	if !v.CloudDeleted(ctx, 1, 1) {
		t.Fatal("missing tombstone")
	}
	if v.Archive(ctx, 1, 1, path) == nil {
		t.Fatal("deleted object resurrected")
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("local original deleted")
	}
}
func TestRetentionThreeMonthsAndRenewal(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	path, _ := audioFile(t)
	must(t, v.Archive(ctx, 1, 1, path))
	now := time.Now().UTC()
	since := now.AddDate(0, -4, 0).Unix()
	_, e := v.db.Exec(`UPDATE users SET plan_tier='free',plan_active=0 WHERE id=1`)
	must(t, e)
	_, e = v.db.Exec(`UPDATE user_cloud_storage SET unpaid_since=? WHERE user_id=1`, since)
	must(t, e)
	must(t, v.Retention(ctx, now))
	if _, e = v.Asset(ctx, 1, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired audio kept")
	}
	must(t, v.Archive(ctx, 2, 3, path))
	if e = v.DeleteAudio(ctx, 2, 3, true); e == nil {
		t.Fatal("renewed account deleted")
	}
	if _, e = v.Asset(ctx, 2, 3); e != nil {
		t.Fatal("renewal lost audio")
	}
	want := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	if got := threeMonths(time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)); !got.Equal(want) {
		t.Fatal(got)
	}
}
func TestLegacyMigrationEncryptsAndRemovesIntermediates(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	_, e := v.db.Exec(`INSERT INTO usage_logs VALUES(1,1,'private ASR','legacy final','private terms')`)
	must(t, e)
	n, e := v.MigrateUsage(ctx)
	must(t, e)
	if n != 1 {
		t.Fatal(n)
	}
	var input, final, audit, sealed string
	must(t, v.db.QueryRow(`SELECT input_transcript,final_output,vocabulary_audit FROM usage_logs`).Scan(&input, &final, &audit))
	if input+final+audit != "" {
		t.Fatal("plaintext remained")
	}
	must(t, v.db.QueryRow(`SELECT ciphertext FROM encrypted_legacy_results`).Scan(&sealed))
	result, e := v.DecryptText(ctx, 1, "legacy-final-v1", "1", sealed)
	must(t, e)
	if result != "legacy final" {
		t.Fatal("migration lost data")
	}
	h, e := v.History(ctx, 1, 0)
	must(t, e)
	if len(h) != 0 {
		t.Fatal("unverified legacy ASR shown as delivered")
	}
	n, e = v.MigrateUsage(ctx)
	must(t, e)
	if n != 0 {
		t.Fatal("not idempotent")
	}
}
func TestAccountDeletionBlocksNewWrites(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	path, _ := audioFile(t)
	must(t, v.Archive(ctx, 1, 1, path))
	must(t, v.BeginAccountDeletion(ctx, 1))
	if v.SaveFinal(ctx, 1, "b448a06a-5aa5-4953-871f-a705361b12ba", "dictation", "late result") == nil {
		t.Fatal("write after deletion")
	}
	if v.Archive(ctx, 1, 2, path) == nil {
		t.Fatal("upload after deletion")
	}
}

func TestConcurrentQuotaReservation(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	path, plain := audioFile(t)
	u, e := v.Usage(ctx, 1)
	must(t, e)
	_, e = v.db.Exec(`UPDATE user_cloud_storage SET limit_bytes=? WHERE user_id=1`, len(plain)+1024)
	must(t, e)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int64{1, 2} {
		wg.Add(1)
		go func(id int64) { defer wg.Done(); results <- v.Archive(ctx, 1, id, path) }(id)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrQuota) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal("quota admitted overlapping uploads", success)
	}
	u, e = v.Usage(ctx, 1)
	must(t, e)
	if u.Used <= 0 || u.Reserved != 0 || u.Used > u.Limit {
		t.Fatal(u)
	}
}

func TestMigrationBatchesRollbackAndRetry(t *testing.T) {
	v, _, _ := fixture(t)
	ctx := context.Background()
	for i := 1; i <= 55; i++ {
		_, e := v.db.Exec(`INSERT INTO usage_logs VALUES(?,?,'raw ASR',?,'private audit')`, i, 1+i%2, fmt.Sprintf("final %d", i))
		must(t, e)
	}
	_, e := v.db.Exec(`CREATE TRIGGER abort_legacy_insert BEFORE INSERT ON encrypted_legacy_results WHEN NEW.usage_id=2 BEGIN SELECT RAISE(ABORT,'injected migration failure'); END`)
	must(t, e)
	if n, e := v.MigrateUsage(ctx); e == nil || n != 0 {
		t.Fatal("migration failure was not atomic", n)
	}
	var private int
	must(t, v.db.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE final_output<>''`).Scan(&private))
	if private != 55 {
		t.Fatal("rollback lost plaintext before encryption")
	}
	_, e = v.db.Exec(`DROP TRIGGER abort_legacy_insert`)
	must(t, e)
	n, e := v.MigrateUsage(ctx)
	must(t, e)
	if n != 55 {
		t.Fatal(n)
	}
	for _, id := range []int{1, 55} {
		var sealed string
		must(t, v.db.QueryRow(`SELECT ciphertext FROM encrypted_legacy_results WHERE usage_id=?`, id).Scan(&sealed))
		plain, e := v.DecryptText(ctx, int64(1+id%2), "legacy-final-v1", fmt.Sprint(id), sealed)
		must(t, e)
		if plain != fmt.Sprintf("final %d", id) {
			t.Fatal("batch encryption lost identity")
		}
	}
	must(t, v.db.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE input_transcript<>'' OR final_output<>'' OR vocabulary_audit<>''`).Scan(&private))
	if private != 0 {
		t.Fatal("private plaintext remained")
	}
}
