package config

import (
	"os"
	"testing"
)

// The self-hosted streaming provider is only enabled when a gateway address
// is configured; tests assume it is.
func TestMain(m *testing.M) {
	os.Setenv("SPEEKTOME_SELF_HOSTED_STREAM_URL", "ws://gpu.example.test:80/v1/stream")
	os.Exit(m.Run())
}
