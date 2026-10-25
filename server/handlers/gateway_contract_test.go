package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoordinatorWithProductionPythonGateway(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 is required for the cross-language gateway contract")
	}
	data := t.TempDir()
	fixture := filepath.Join("..", "..", "deployment", "diarization-server", "tests", "gateway_http_fixture.py")
	command := exec.Command(python, "-u", fixture, data)
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { command.Process.Kill(); command.Wait() })
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	var address string
	select {
	case address = <-ready:
		if !strings.HasPrefix(address, "http://127.0.0.1:") {
			t.Fatal("gateway fixture did not start", address)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("gateway fixture startup timed out")
	}
	j, d := diarizationFixture(t)
	d.url, d.token = address, "fixture-only-token-0123456789abcdef"
	addDiarizationMeeting(t, j, "python-gateway", 1)
	// The coordinator/gateway identity handshake, polling, result and deletion,
	// against the production Python gateway. No model inference runs here.
	speakers, err := d.run(context.Background(), "python-gateway")
	if err != nil {
		t.Fatal("production gateway job failed", err)
	}
	if _, err = json.Marshal(speakers); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) == 32 {
			t.Fatal("production gateway kept a finished job", entry.Name())
		}
	}
	count, err := os.ReadFile(filepath.Join(data, "inference-count"))
	if err != nil || string(count) != "1" {
		t.Fatal("expected exactly one inference", string(count), err)
	}
}
