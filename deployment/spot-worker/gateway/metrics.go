package main

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// This measures transport-visible progress, not CUDA execution or the model's
// internal scheduler. Missing backend instrumentation is explicitly null.
type audioMark struct {
	end float64
	at  time.Time
}
type streamStats struct {
	asr, diar, batch    bool
	received, processed float64
	marks               []audioMark
	progressAt          time.Time
}
type telemetry struct {
	mu       sync.Mutex
	sessions map[*streamStats]bool
	delays   []float64
	rejected uint64
	stages   map[string]*stageTiming
}

func (t *telemetry) begin(r *http.Request) *streamStats {
	s := &streamStats{asr: !strings.Contains(r.URL.Path, "diarization"), diar: strings.Contains(r.URL.Path, "diarization") || r.URL.Query().Get("diarize") == "true", batch: !isStreaming(r)}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessions == nil {
		t.sessions = make(map[*streamStats]bool)
	}
	t.sessions[s] = true
	return s
}
func isStreaming(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}
func (t *telemetry) end(s *streamStats) { t.mu.Lock(); defer t.mu.Unlock(); delete(t.sessions, s) }
func (t *telemetry) reject()            { t.mu.Lock(); t.rejected++; t.mu.Unlock() }
func (t *telemetry) control(s *streamStats, data []byte) bool {
	var e struct {
		Type    string `json:"type"`
		Session struct {
			Diar *bool `json:"speaker_diarization"`
		} `json:"session"`
	}
	if json.Unmarshal(data, &e) != nil || e.Type != "session.update" || e.Session.Diar == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	changed := s.diar != *e.Session.Diar
	s.diar = *e.Session.Diar
	return changed
}
func (t *telemetry) audio(s *streamStats, n int, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s.received += float64(n) / 32000
	// Bound bookkeeping even if a backend never reports progress.
	if len(s.marks) >= 4096 {
		copy(s.marks, s.marks[1:])
		s.marks = s.marks[:4095]
	}
	s.marks = append(s.marks, audioMark{s.received, now})
}
func (t *telemetry) progress(s *streamStats, data []byte, now time.Time) {
	var e struct {
		Audio *float64 `json:"audio_processed"`
	}
	if json.Unmarshal(data, &e) != nil || e.Audio == nil || math.IsNaN(*e.Audio) || math.IsInf(*e.Audio, 0) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if *e.Audio < s.processed || *e.Audio > s.received+0.1 {
		return
	}
	s.processed = *e.Audio
	s.progressAt = now
	n := 0
	for n < len(s.marks) && s.marks[n].end <= s.processed+0.001 {
		delay := now.Sub(s.marks[n].at)
		t.recordStageLocked("backend_progress_wait", delay, 0)
		t.delays = append(t.delays, float64(delay.Microseconds())/1000)
		n++
	}
	s.marks = append(s.marks[:0], s.marks[n:]...)
	if len(t.delays) > 256 {
		t.delays = append([]float64(nil), t.delays[len(t.delays)-256:]...)
	}
}
func (t *telemetry) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	asr, diar, batch, stream := 0, 0, 0, 0
	pending := 0.0
	reporting := 0
	for s := range t.sessions {
		if s.batch {
			batch++
		} else {
			stream++
		}
		if s.asr {
			asr++
		}
		if s.diar {
			diar++
		}
		pending += math.Max(0, s.received-s.processed)
		if !s.progressAt.IsZero() {
			reporting++
		}
	}
	var avg, p95 any
	if len(t.delays) > 0 {
		sum := 0.0
		d := append([]float64(nil), t.delays...)
		for _, v := range d {
			sum += v
		}
		sort.Float64s(d)
		avg = sum / float64(len(d))
		p95 = d[int(math.Ceil(.95*float64(len(d))))-1]
	}
	return map[string]any{
		"pipeline": t.pipelineLocked(),
		"schema":   2, "active_transcription_requests": asr, "active_diarization_requests": diar, "active_streaming_connections": stream, "active_batch_requests": batch,
		"rejected_capacity_total": t.rejected,
		"latency":                 map[string]any{"avg_chunk_compute_ms": nil, "avg_chunk_response_ms": avg, "p95_chunk_response_ms": p95, "samples": len(t.delays), "definition": "PCM chunk forwarded to backend until audio_processed covers its end; includes batching, endpointing and inference; last 256 samples"},
		"queue":                   map[string]any{"gateway_waiting_requests": 0, "backend_queue_depth": nil, "audio_not_reported_processed_seconds": pending, "streams_reporting_progress": reporting, "definition": "received audio minus reported audio_processed; includes buffered context and endpointing; not internal queue depth"},
	}
}

type stageTiming struct {
	Count   uint64
	Bytes   uint64
	TotalMS float64
	MaxMS   float64
	Samples []float64
}

func (t *telemetry) stage(name string, duration time.Duration, bytes int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordStageLocked(name, duration, bytes)
}
func (t *telemetry) recordStageLocked(name string, duration time.Duration, bytes int) {
	if t.stages == nil {
		t.stages = make(map[string]*stageTiming)
	}
	a := t.stages[name]
	if a == nil {
		a = &stageTiming{}
		t.stages[name] = a
	}
	ms := float64(duration.Nanoseconds()) / 1e6
	a.Count++
	a.Bytes += uint64(max(bytes, 0))
	a.TotalMS += ms
	a.MaxMS = math.Max(a.MaxMS, ms)
	if len(a.Samples) < 2048 {
		a.Samples = append(a.Samples, ms)
	} else {
		a.Samples[(a.Count-1)%2048] = ms
	}
}
func (t *telemetry) pipelineLocked() map[string]any {
	out := map[string]any{"gpu_execution_ms": nil, "definition": "Process-lifetime stage totals and last 2048 timings per stage; backend_progress_wait includes runtime buffering, preprocessing, scheduling, GPU execution and decoding, not isolated GPU time"}
	for name, a := range t.stages {
		samples := append([]float64(nil), a.Samples...)
		sort.Float64s(samples)
		out[name] = map[string]any{"count": a.Count, "bytes": a.Bytes, "total_ms": a.TotalMS, "avg_ms": a.TotalMS / float64(a.Count), "max_ms": a.MaxMS, "p50_ms": samples[len(samples)/2], "p95_ms": samples[int(math.Ceil(.95*float64(len(samples))))-1]}
	}
	return out
}
