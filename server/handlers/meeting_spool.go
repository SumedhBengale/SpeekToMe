package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"speektome-server/audiospool"
	log "speektome-server/privacylog"
	"speektome-server/privatevault"
	"time"
)

// newMeetingSpool seals meeting audio under per-account keys. Production uses
// the private bucket; without one (development) sealed chunks go to a local
// directory, still never in plaintext.
func newMeetingSpool(a *App) (*audiospool.Spool, error) {
	vault := a.DB.Privacy
	if vault != nil && vault.Spool != nil {
		return vault.Spool, nil
	}
	var store audiospool.Store
	if vault != nil && vault.Objects != nil {
		store = vault.Objects
	} else {
		root := os.Getenv("MEETING_SPOOL_DIR")
		if root == "" {
			root = "uploads/meetings/spool"
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(absolute, 0700); err != nil {
			return nil, err
		}
		store = audiospool.DirStore{Root: absolute}
	}
	var keys audiospool.KeyFunc
	if vault != nil {
		keys = vault.SpoolKey
	} else {
		// Without the vault (tests and local tools) a process key still keeps
		// the spool sealed; it simply does not survive a restart.
		process := make([]byte, 32)
		if _, err := rand.Read(process); err != nil {
			return nil, err
		}
		keys = func(_ context.Context, _ int64, job string) ([]byte, error) {
			sum := sha256.Sum256(append(append([]byte(nil), process...), job...))
			return sum[:], nil
		}
	}
	spool, err := audiospool.New(a.DB.GetDB(), store, keys)
	if err != nil {
		return nil, err
	}
	if vault != nil {
		vault.Spool = spool
	}
	return spool, nil
}

func (j *MeetingJobs) jobUser(ctx context.Context, key string) (int64, error) {
	var user int64
	err := j.db.QueryRowContext(ctx, `SELECT user_id FROM meeting_jobs WHERE job_key=?`, key).Scan(&user)
	return user, err
}

func (j *MeetingJobs) openChannel(ctx context.Context, key, channel string) (io.ReadCloser, int64, error) {
	user, err := j.jobUser(ctx, key)
	if err != nil {
		return nil, 0, audiospool.ErrCorrupt
	}
	return j.spool.Open(ctx, user, key, channel)
}

// verifyChannel streams one sealed channel once, computing its checksum and
// Ogg Opus duration together.
func (j *MeetingJobs) verifyChannel(ctx context.Context, user int64, key, channel string) (float64, int64, string, error) {
	reader, size, err := j.spool.Open(ctx, user, key, channel)
	if err != nil {
		return 0, 0, "", err
	}
	defer reader.Close()
	hash := sha256.New()
	verifyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-verifyCtx.Done():
			reader.Close()
		case <-done:
		}
	}()
	seconds, err := audiospool.OggOpusDuration(io.TeeReader(reader, hash))
	close(done)
	<-stopped // the watcher must not close the reader alongside the deferred Close
	if errors.Is(err, audiospool.ErrInvalidAudio) {
		// Distinguish a damaged spool from a client that uploaded non-Opus data.
		if _, drainErr := io.Copy(io.Discard, reader); drainErr != nil {
			return 0, 0, "", audiospool.ErrCorrupt
		}
	}
	if err != nil {
		return 0, 0, "", err
	}
	return seconds, size, hex.EncodeToString(hash.Sum(nil)), nil
}

// checksumChannel streams one sealed file that is not audio, such as the live
// transcript, and returns its size and checksum.
func (j *MeetingJobs) checksumChannel(ctx context.Context, user int64, key, channel string) (int64, string, error) {
	reader, size, err := j.spool.Open(ctx, user, key, channel)
	if err != nil {
		return 0, "", err
	}
	defer reader.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, reader); err != nil {
		return 0, "", audiospool.ErrCorrupt
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

// readChannel opens one sealed file into memory, up to limit bytes.
func (j *MeetingJobs) readChannel(ctx context.Context, key, channel string, limit int64) ([]byte, error) {
	reader, size, err := j.openChannel(ctx, key, channel)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if size > limit {
		return nil, audiospool.ErrCorrupt
	}
	return io.ReadAll(io.LimitReader(reader, limit))
}

// channelDuration reads one sealed recording's duration from its Ogg pages.
func (j *MeetingJobs) channelDuration(ctx context.Context, key, channel string) (float64, error) {
	reader, _, err := j.openChannel(ctx, key, channel)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	return audiospool.OggOpusDuration(reader)
}

// maintainSpool archives finished recordings to the bucket and deletes every
// spool the job no longer needs, so audio never outlives its processing.
func (j *MeetingJobs) maintainSpool(ctx context.Context) {
	now := time.Now()
	type item struct {
		key, status string
		user        int64
		meeting     int64
		admitted    int
		created     int64
	}
	rows, err := j.db.QueryContext(ctx, `SELECT DISTINCT j.job_key,j.status,j.user_id,COALESCE(j.meeting_id,0),j.admitted,j.created_at FROM meeting_jobs j JOIN meeting_spool_chunks c ON c.job_key=j.job_key WHERE j.status IN ('completed','failed','cancelled') OR j.created_at<? LIMIT 32`, now.Add(-meetingSpoolRetention).Unix())
	if err != nil {
		return
	}
	var items []item
	for rows.Next() {
		var i item
		if rows.Scan(&i.key, &i.status, &i.user, &i.meeting, &i.admitted, &i.created) == nil {
			items = append(items, i)
		}
	}
	rows.Close()
	for _, i := range items {
		age := now.Sub(time.Unix(i.created, 0))
		expired := age > meetingSpoolRetention && i.admitted == 0 || age > meetingSpoolHardLimit
		switch {
		case i.status == "completed":
			if !j.archive(ctx, i.user, i.meeting, i.key) && !expired {
				continue
			}
		case i.status == "failed" && i.admitted == 0 && !expired:
			continue // Still retryable without a new upload.
		case i.status != "cancelled" && i.status != "failed" && !expired:
			continue
		}
		if j.spool.Delete(ctx, i.key) != nil {
			continue
		}
		if i.status != "completed" {
			_, _ = j.db.ExecContext(ctx, `UPDATE meeting_jobs SET status=CASE WHEN status='cancelled' THEN status ELSE 'failed' END,admitted=0,spool_purged=1,error=CASE WHEN status='cancelled' THEN error ELSE 'The server no longer holds this recording. Retry to upload it again from this device.' END WHERE job_key=?`, i.key)
			j.releaseProcessing(i.key)
		}
	}
	if orphans, err := j.spool.Orphans(ctx, 16); err == nil {
		for _, key := range orphans {
			_ = j.spool.Delete(ctx, key)
		}
	}
}

// archive copies the mixed recording into the private bucket. It reports true
// once nothing further can be done: archived, not entitled, or impossible.
func (j *MeetingJobs) archive(ctx context.Context, user, meeting int64, key string) bool {
	vault := j.app.DB.Privacy
	if vault == nil || vault.Objects == nil || meeting <= 0 {
		return true
	}
	reader, size, err := j.spool.Open(ctx, user, key, "mixed")
	if err != nil {
		return true
	}
	defer reader.Close()
	archiveCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	err = vault.ArchiveReader(archiveCtx, user, meeting, reader, size)
	state := "ready"
	switch {
	case err == nil:
	case errors.Is(err, privatevault.ErrQuota):
		state = "storage_full"
	case errors.Is(err, privatevault.ErrNotPaid), errors.Is(err, privatevault.ErrNotFound):
		state = "not_archived"
	default:
		log.Printf("[MEETINGS] encrypted archive pending for meeting %d", meeting)
		return false
	}
	_, _ = j.db.ExecContext(ctx, `INSERT INTO meeting_archive_status(meeting_id,user_id,state,processing_purged) VALUES(?,?,?,1) ON CONFLICT(meeting_id) DO UPDATE SET state=excluded.state,processing_purged=1`, meeting, user, state)
	return true
}

// migrateLegacyAudio moves plaintext job audio written before the spool
// existed into sealed chunks and deletes the plaintext. Completed jobs are
// archived and purged by the vault's maintenance instead.
func (j *MeetingJobs) migrateLegacyAudio(ctx context.Context) {
	entries, err := os.ReadDir(j.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		key := entry.Name()
		if !entry.IsDir() || !jobKeyPattern.MatchString(key) {
			continue
		}
		var status string
		var user int64
		if j.db.QueryRowContext(ctx, `SELECT status,user_id FROM meeting_jobs WHERE job_key=?`, key).Scan(&status, &user) != nil || status == "completed" {
			continue // The vault removes job-less and completed directories.
		}
		dir := filepath.Join(j.root, key)
		unlock := j.lock(key)
		if status != "uploading" && status != "cancelled" && !j.spool.Has(ctx, key) {
			if err = j.importLegacyJob(ctx, user, key, dir); err != nil {
				_ = j.spool.Delete(ctx, key)
				unlock()
				log.Printf("[MEETINGS] legacy recording migration pending")
				continue
			}
		}
		// Uploading jobs resume from offset zero against the empty spool.
		if os.RemoveAll(dir) != nil {
			log.Printf("[MEETINGS] legacy recording removal pending")
		}
		unlock()
	}
}

func (j *MeetingJobs) importLegacyJob(ctx context.Context, user int64, key, dir string) error {
	buffer := make([]byte, audiospool.MaxChunk)
	defer clear(buffer)
	for _, channel := range []string{"mic", "system", "mixed"} {
		file, err := os.Open(filepath.Join(dir, channel+".ogg"))
		if err != nil {
			return err
		}
		var offset int64
		for {
			n, readErr := io.ReadFull(file, buffer)
			if n > 0 {
				if offset, err = j.spool.Append(ctx, user, key, channel, offset, buffer[:n]); err != nil {
					file.Close()
					return err
				}
			}
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			if readErr != nil {
				file.Close()
				return readErr
			}
		}
		file.Close()
	}
	return nil
}
