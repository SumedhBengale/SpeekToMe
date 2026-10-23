package audiospool

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testSpool(t *testing.T) (*Spool, string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "spool.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE meeting_jobs(job_key TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	key := bytes.Repeat([]byte{7}, 32)
	s, err := New(db, DirStore{Root: root}, func(context.Context, int64, string) ([]byte, error) {
		return append([]byte(nil), key...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

const job = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSpoolStoresOnlyCiphertextAndRoundTrips(t *testing.T) {
	ctx := context.Background()
	s, root := testSpool(t)
	secret := []byte("private meeting audio bytes")
	next, err := s.Append(ctx, 1, job, "mic", 0, secret[:10])
	if err != nil || next != 10 {
		t.Fatalf("first append = %d, %v", next, err)
	}
	if size, err := s.Append(ctx, 1, job, "mic", 0, secret[10:]); err != ErrOffset || size != 10 {
		t.Fatalf("stale offset = %d, %v", size, err)
	}
	if next, err = s.Append(ctx, 1, job, "mic", 10, secret[10:]); err != nil || next != int64(len(secret)) {
		t.Fatalf("second append = %d, %v", next, err)
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, _ := os.ReadFile(path)
		if bytes.Contains(data, []byte("private")) || bytes.Contains(data, []byte("audio")) {
			t.Fatalf("plaintext reached storage in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, size, err := s.Open(ctx, 1, job, "mic")
	if err != nil || size != int64(len(secret)) {
		t.Fatalf("open = %d, %v", size, err)
	}
	plain, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(plain, secret) {
		t.Fatalf("round trip = %q, %v", plain, err)
	}
	if err = s.Delete(ctx, job); err != nil || s.Has(ctx, job) {
		t.Fatalf("delete = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "spool", "v1")); len(entries) != 0 {
		t.Fatalf("sealed chunks remain after delete: %d", len(entries))
	}
}

func TestSpoolRejectsTamperedChunk(t *testing.T) {
	ctx := context.Background()
	s, root := testSpool(t)
	if _, err := s.Append(ctx, 1, job, "system", 0, []byte("audio")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "spool", "v1", job, "system", "0000000000000000")
	data, _ := os.ReadFile(path)
	data[len(data)-1] ^= 1
	os.WriteFile(path, data, 0600)
	reader, _, err := s.Open(ctx, 1, job, "system")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(reader); err != ErrCorrupt {
		t.Fatalf("tampered chunk read error = %v", err)
	}
}

func TestSpoolOrphansListsChunksWithoutJob(t *testing.T) {
	ctx := context.Background()
	s, _ := testSpool(t)
	if _, err := s.Append(ctx, 1, job, "mixed", 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	orphans, err := s.Orphans(ctx, 10)
	if err != nil || len(orphans) != 1 || orphans[0] != job {
		t.Fatalf("orphans = %v, %v", orphans, err)
	}
}

func TestDirStoreRejectsEscapingKeys(t *testing.T) {
	store := DirStore{Root: t.TempDir()}
	for _, key := range []string{"../x", "spool/v1/../../x", "other/key"} {
		if err := store.Put(context.Background(), key, strings.NewReader("x")); err == nil {
			t.Fatalf("accepted %q", key)
		}
	}
}

func TestOggOpusDuration(t *testing.T) {
	duration, err := OggOpusDuration(bytes.NewReader(SyntheticOggOpus(12.5)))
	if err != nil || duration < 12.49 || duration > 12.51 {
		t.Fatalf("duration = %v, %v", duration, err)
	}
	corrupt := SyntheticOggOpus(3)
	corrupt[len(corrupt)-1] ^= 1
	if _, err = OggOpusDuration(bytes.NewReader(corrupt)); err != ErrInvalidAudio {
		t.Fatalf("corrupt page error = %v", err)
	}
	if _, err = OggOpusDuration(strings.NewReader("fake-audio")); err != ErrInvalidAudio {
		t.Fatalf("non-Ogg error = %v", err)
	}
}
