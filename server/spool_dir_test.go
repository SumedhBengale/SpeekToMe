package main

import (
	"os"
	"testing"
)

// Keep the encrypted meeting spool used by tests out of the source tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "speektome-spool-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("MEETING_SPOOL_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
