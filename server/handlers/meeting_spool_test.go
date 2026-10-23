package handlers

import (
	"context"
	"testing"

	"speektome-server/audiospool"
)

// testMeetingSpool returns a sealed spool in a temporary directory.
func testMeetingSpool(t *testing.T, app *App) *audiospool.Spool {
	t.Helper()
	t.Setenv("MEETING_SPOOL_DIR", t.TempDir())
	spool, err := newMeetingSpool(app)
	if err != nil {
		t.Fatal(err)
	}
	return spool
}

func spoolAudio(t *testing.T, spool *audiospool.Spool, user int64, key string, channels map[string][]byte) {
	t.Helper()
	for channel, data := range channels {
		var offset int64
		for len(data) > 0 {
			n := min(len(data), audiospool.MaxChunk)
			next, err := spool.Append(context.Background(), user, key, channel, offset, data[:n])
			if err != nil {
				t.Fatal(err)
			}
			offset, data = next, data[n:]
		}
	}
}
