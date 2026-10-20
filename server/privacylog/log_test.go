package privacylog

import (
	"bytes"
	"errors"
	stdlog "log"
	"strings"
	"testing"
)

func TestPrivateValuesNeverReachLogs(t *testing.T) {
	var output bytes.Buffer
	previous := stdlog.Writer()
	stdlog.SetOutput(&output)
	defer stdlog.SetOutput(previous)
	secret := "unique-private-transcript-command-token"
	Printf("[ASR] result=%s error=%v object=%+v bytes=%d ok=%t", secret, errors.New(secret), map[string]string{"text": secret}, 123, true)
	Print(secret)
	Println(secret)
	HTTPLogger().Println(secret)
	result := output.String()
	if strings.Contains(result, secret) {
		t.Fatal("private data reached logs")
	}
	for _, value := range []string{"[ASR]", "bytes=123", "ok=true"} {
		if !strings.Contains(result, value) {
			t.Fatalf("missing diagnostic %s", value)
		}
	}
}
