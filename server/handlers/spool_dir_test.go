package handlers

import (
	"os"
	"testing"

	"speektome-server/config"
)

// Keep the encrypted meeting spool used by tests out of the source tree.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "speektome-spool-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("MEETING_SPOOL_DIR", dir)
	// Tests exercise the self-hosted streaming provider, which is only
	// enabled when a gateway address is configured.
	os.Setenv("SPEEKTOME_SELF_HOSTED_STREAM_URL", "ws://gpu.example.test:80/v1/stream")
	// The package-level default config was built before the variable existed.
	cfg := config.ConfigManager.Get()
	cfg.Models = config.DefaultModelsConfig()
	config.ConfigManager.Set(cfg, config.ConfigManager.GetRawAll())
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
