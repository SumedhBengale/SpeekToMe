package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPipelineStagesAreBoundedAndSeparateFromGPUTime(t *testing.T) {
	var stats telemetry
	for i := 0; i < 3000; i++ {
		stats.stage("opus_decode_packet", 2*time.Millisecond, 100)
	}
	stats.mu.Lock()
	if len(stats.stages["opus_decode_packet"].Samples) != 2048 {
		t.Fatal("unbounded samples")
	}
	stats.mu.Unlock()
	snapshot := stats.snapshot()
	p := snapshot["pipeline"].(map[string]any)
	if p["gpu_execution_ms"] != nil {
		t.Fatal("cannot claim GPU timing")
	}
	d := p["opus_decode_packet"].(map[string]any)
	if d["count"] != uint64(3000) || d["bytes"] != uint64(300000) || d["avg_ms"] != float64(2) {
		t.Fatal(d)
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatal(err)
	}
}
func TestPipelineProgressWait(t *testing.T) {
	var stats telemetry
	s := stats.begin(httptest.NewRequest("GET", "/v1/realtime", nil))
	now := time.Now()
	stats.audio(s, 17920, now)
	stats.progress(s, []byte(`{"audio_processed":0.56}`), now.Add(100*time.Millisecond))
	p := stats.snapshot()["pipeline"].(map[string]any)
	if p["backend_progress_wait"].(map[string]any)["avg_ms"] != float64(100) {
		t.Fatal(p)
	}
}
