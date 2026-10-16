package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMetadataCountsAndLatencyAreExplicit(t *testing.T) {
	var metrics telemetry
	request := httptest.NewRequest("GET", "/v1/realtime?diarize=true", nil)
	request.Header.Set("Upgrade", "websocket")
	s := metrics.begin(request)
	defer metrics.end(s)
	now := time.Now()
	metrics.audio(s, 17920, now)
	metrics.progress(s, []byte(`{"audio_processed":0.56}`), now.Add(100*time.Millisecond))
	m := metrics.snapshot()
	if m["active_transcription_requests"] != 1 || m["active_diarization_requests"] != 1 || m["active_streaming_connections"] != 1 {
		t.Fatal(m)
	}
	latency := m["latency"].(map[string]any)
	if latency["avg_chunk_response_ms"] != float64(100) || latency["avg_chunk_compute_ms"] != nil {
		t.Fatal(latency)
	}
	queue := m["queue"].(map[string]any)
	if queue["backend_queue_depth"] != nil || queue["audio_not_reported_processed_seconds"] != float64(0) {
		t.Fatal(queue)
	}
	if _, e := json.Marshal(m); e != nil {
		t.Fatal(e)
	}
}
func TestTelemetryHandlesNoProgressAndBoundsMemory(t *testing.T) {
	var metrics telemetry
	r := httptest.NewRequest("GET", "/v1/realtime", nil)
	r.Header.Set("Upgrade", "websocket")
	s := metrics.begin(r)
	for range 5000 {
		metrics.audio(s, 17920, time.Now())
	}
	if len(s.marks) != 4096 {
		t.Fatal("unbounded chunk bookkeeping")
	}
	m := metrics.snapshot()
	if m["latency"].(map[string]any)["avg_chunk_response_ms"] != nil {
		t.Fatal("missing samples invented as zero")
	}
	metrics.control(s, []byte(`{"type":"session.update","session":{"speaker_diarization":true}}`))
	if metrics.snapshot()["active_diarization_requests"] != 1 {
		t.Fatal("diarization update lost")
	}
	metrics.end(s)
	if metrics.snapshot()["active_streaming_connections"] != 0 {
		t.Fatal("active counter leak")
	}
}
