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
	queueTick(t, d)
	var remote, state string
	if err = j.db.QueryRow(`SELECT remote_id,status FROM meeting_diarization_queue WHERE job_key='python-gateway'`).Scan(&remote, &state); err != nil || len(remote) != 32 || state != "running" {
		t.Fatal("coordinator/gateway identity handshake failed", remote, state, err)
	}
	// Retry a possibly lost upload response using its saved identity. Production
	// Python admission must reattach without consuming or processing audio again.
	if err = d.submit(context.Background(), "python-gateway", remote); err != nil {
		t.Fatal(err)
	}
	// A fresh coordinator object recovers the persistent job and saves the result
	// before acknowledging it. No Python model or GPU/CPU inference runs here.
	restarted := &MeetingDiarization{jobs: j, url: d.url, token: d.token}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		queueTick(t, restarted)
		if err = j.db.QueryRow(`SELECT status FROM meeting_diarization_queue WHERE job_key='python-gateway'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var result string
	if err = j.db.QueryRow(`SELECT diar_result FROM meeting_jobs WHERE job_key='python-gateway'`).Scan(&result); err != nil || state != "completed" || !json.Valid([]byte(result)) {
		t.Fatal("production gateway result did not persist", state, result, err)
	}
	queueTick(t, restarted)
	var acknowledged int
	if err = j.db.QueryRow(`SELECT acknowledged FROM meeting_diarization_queue WHERE job_key='python-gateway'`).Scan(&acknowledged); err != nil || acknowledged != 1 {
		t.Fatal("saved result was not acknowledged", acknowledged, err)
	}
	if _, err = os.Stat(filepath.Join(data, remote)); !os.IsNotExist(err) {
		t.Fatal("production gateway did not delete the acknowledged job", err)
	}
	count, err := os.ReadFile(filepath.Join(data, "inference-count"))
	if err != nil || string(count) != "1" {
		t.Fatal("replayed upload duplicated inference", string(count), err)
	}
}
