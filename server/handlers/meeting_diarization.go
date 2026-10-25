package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MeetingDiarization runs one meeting's diarization as a direct call: submit
// the system audio, poll, read the result. Nothing is persisted; after a
// restart the meeting's pipeline simply submits again, and the abandoned
// remote job's result is never read.
//
// Two backends share one HTTP job contract. The dedicated CPU gateway runs one
// job at a time over the private network. When a diarizer public key is
// configured, the external serverless GPU endpoint (deployment/modal-diarization)
// receives every recording sealed to that key, behind Modal proxy auth plus the
// bearer key, and up to `limit` jobs run at once.
type MeetingDiarization struct {
	jobs                  *MeetingJobs
	url, token            string
	recipient             *diarizationRecipient
	proxyKey, proxySecret string
	limit                 int
	label                 string
	slots                 chan struct{}
	poll                  time.Duration
	maxWait               time.Duration // caps a retry wait; tests shorten it
}

func newMeetingDiarization(j *MeetingJobs) (*MeetingDiarization, error) {
	endpoint := strings.TrimRight(os.Getenv("SPEEKTOME_DIARIZATION_URL"), "/")
	if endpoint == "" {
		endpoint = "http://DIARIZATION_PRIVATE_IP:8081"
	}
	token := os.Getenv("SPEEKTOME_DIARIZATION_TOKEN")
	if token == "" {
		tokenPath := os.Getenv("SPEEKTOME_DIARIZATION_TOKEN_FILE")
		if tokenPath == "" {
			tokenPath = "/etc/speektome-diarization/token"
		}
		data, _ := os.ReadFile(tokenPath) // Meetings wait while credentials are unavailable.
		token = string(data)
	}
	d := &MeetingDiarization{jobs: j, url: endpoint, token: strings.TrimSpace(token), limit: 1, label: "CPU diarization", poll: 2 * time.Second, maxWait: 5 * time.Minute,
		proxyKey: os.Getenv("SPEEKTOME_DIARIZATION_MODAL_KEY"), proxySecret: os.Getenv("SPEEKTOME_DIARIZATION_MODAL_SECRET")}
	var err error
	if encoded := strings.TrimSpace(os.Getenv("SPEEKTOME_DIARIZATION_PUBLIC_KEY")); encoded != "" {
		if d.recipient, err = parseDiarizationRecipient(encoded); err != nil {
			return nil, fmt.Errorf("SPEEKTOME_DIARIZATION_PUBLIC_KEY: %w", err)
		}
		d.limit, d.label = 8, "GPU diarization"
	}
	if n, err := strconv.Atoi(os.Getenv("SPEEKTOME_DIARIZATION_CONCURRENCY")); err == nil && n > 0 {
		d.limit = n
	}
	d.slots = make(chan struct{}, max(d.limit, 1))
	return d, nil
}

// Transient failures (busy, unreachable, lost jobs) are retried this many times
// before the meeting attempt fails and goes back to the job queue.
const diarizationAttempts = 3

// run diarizes the meeting's system audio and returns its speaker segments.
func (d *MeetingDiarization) run(ctx context.Context, key string) ([]jobSpeaker, error) {
	if len(d.token) < 24 {
		return nil, errors.New("waiting for " + d.label + " credentials")
	}
	select {
	case d.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-d.slots }()
	var last error
	for attempt := 0; attempt < diarizationAttempts; attempt++ {
		speakers, wait, err := d.once(ctx, key)
		if err == nil {
			return speakers, nil
		}
		var failure *meetingError
		if errors.As(err, &failure) && failure.permanent || ctx.Err() != nil {
			return nil, err
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(wait, d.maxWait)):
		}
	}
	return nil, last
}

// once submits one remote job and waits for it. On failure it also returns how
// long to wait before trying again.
func (d *MeetingDiarization) once(ctx context.Context, key string) ([]jobSpeaker, time.Duration, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, 0, err
	}
	remote := hex.EncodeToString(id)
	audio, size, err := d.jobs.openChannel(ctx, key, "system")
	if err != nil {
		return nil, 0, permanentError("system audio unavailable for " + d.label)
	}
	defer audio.Close()
	var body io.Reader = sizedBody{audio, size}
	var responseKey []byte
	if d.recipient != nil {
		body, responseKey, err = sealDiarizationRequest(audio, size, d.recipient, remote, time.Now(), nil)
		if err != nil {
			return nil, 0, permanentError("cannot seal audio for " + d.label)
		}
	}
	data, code, headers, err := d.request(ctx, http.MethodPost, "/v1/diarizations", body, remote)
	if err != nil {
		return nil, 15 * time.Second, fmt.Errorf("%s unavailable", d.label)
	}
	if code != 202 {
		return nil, retryAfter(headers), d.httpFailure(code)
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if json.Unmarshal(data, &accepted) != nil || accepted.JobID != remote {
		return nil, 0, permanentError(d.label + " must support coordinator job identities")
	}
	// Free the remote copy once this call is done with it, whatever the outcome.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_, _, _, _ = d.request(cleanup, http.MethodDelete, "/v1/diarizations/"+remote, nil, "")
	}()
	for {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-time.After(d.poll):
		}
		data, code, headers, err = d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote, nil, "")
		if err != nil {
			continue // Polling survives brief network loss; the 6-hour job bound still applies.
		}
		if code == 404 {
			return nil, 5 * time.Second, fmt.Errorf("%s lost the job", d.label)
		}
		if code != 200 {
			return nil, retryAfter(headers), d.httpFailure(code)
		}
		var state struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(data, &state) != nil {
			return nil, 15 * time.Second, fmt.Errorf("invalid %s status", d.label)
		}
		switch state.Status {
		case "uploading", "uploaded", "processing":
			continue
		case "failed":
			return nil, 15 * time.Second, fmt.Errorf("%s failed: %s", d.label, state.Error)
		case "completed":
		default:
			return nil, 15 * time.Second, fmt.Errorf("unrecognized %s status", d.label)
		}
		data, code, headers, err = d.request(ctx, http.MethodGet, "/v1/diarizations/"+remote+"/result", nil, "")
		if err != nil {
			return nil, 15 * time.Second, fmt.Errorf("%s result unavailable", d.label)
		}
		if code != 200 {
			return nil, retryAfter(headers), d.httpFailure(code)
		}
		if d.recipient != nil {
			if data, err = openDiarizationResponse(data, responseKey, remote); err != nil {
				return nil, 0, permanentError("unreadable " + d.label + " result")
			}
		}
		normalized, err := normalizeDiarizationResult(data)
		if err != nil {
			return nil, 0, permanentError("invalid " + d.label + " result: " + err.Error())
		}
		var result struct {
			Segments []jobSpeaker `json:"segments"`
		}
		if err = json.Unmarshal(normalized, &result); err != nil {
			return nil, 0, err
		}
		return result.Segments, 0, nil
	}
}

func retryAfter(headers http.Header) time.Duration {
	if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 300)) * time.Second
	}
	return 15 * time.Second
}

func (d *MeetingDiarization) httpFailure(code int) error {
	message := fmt.Sprintf("%s HTTP %d", d.label, code)
	if code == 400 || code == 401 || code == 403 || code == 413 || code == 422 {
		return permanentError(message)
	}
	return errors.New("waiting for " + message)
}

// sizedBody streams decrypted spool audio with a known length, so the gateway
// can reject oversized uploads before reading them.
type sizedBody struct {
	io.Reader
	size int64
}

func (s sizedBody) Len() int64 { return s.size }

func (d *MeetingDiarization) request(ctx context.Context, method, path string, body io.Reader, remote string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.url+path, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if d.proxyKey != "" {
		req.Header.Set("Modal-Key", d.proxyKey)
		req.Header.Set("Modal-Secret", d.proxySecret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "audio/ogg")
		if d.recipient != nil {
			req.Header.Set("Content-Type", "application/vnd.speektome.diarization-envelope")
		}
		req.Header.Set("X-SpeekToMe-Job-ID", remote)
		if sized, ok := body.(interface{ Len() int64 }); ok {
			req.ContentLength = sized.Len()
		}
	}
	response, err := d.jobs.client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (32<<20)+1))
	if len(data) > 32<<20 {
		return nil, response.StatusCode, response.Header, errors.New(d.label + " response exceeds limit")
	}
	return data, response.StatusCode, response.Header, err
}

var cpuSpeakerPattern = regexp.MustCompile(`^(?:speaker[_ -]?)?(\d+)$`)

// The dedicated runtime returns Start/End/Speaker, with numeric speaker IDs.
// Accept a string numeric ID too, but reject incomplete/malformed results instead
// of silently assigning every word to speaker zero. Empty segments are valid.
func normalizeDiarizationResult(data []byte) ([]byte, error) {
	var result struct {
		Segments json.RawMessage `json:"segments"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if len(result.Segments) == 0 || string(result.Segments) == "null" {
		return nil, errors.New("missing segments")
	}
	var raw []struct {
		Start   *float64        `json:"start"`
		End     *float64        `json:"end"`
		Speaker json.RawMessage `json:"speaker"`
	}
	if err := json.Unmarshal(result.Segments, &raw); err != nil {
		return nil, err
	}
	segments := make([]jobSpeaker, 0, len(raw))
	for _, s := range raw {
		if s.Start == nil || s.End == nil || math.IsNaN(*s.Start) || math.IsNaN(*s.End) || math.IsInf(*s.Start, 0) || math.IsInf(*s.End, 0) || *s.Start < 0 || *s.End < *s.Start {
			return nil, errors.New("invalid segment timing")
		}
		var speaker int
		if err := json.Unmarshal(s.Speaker, &speaker); err != nil {
			var label string
			if json.Unmarshal(s.Speaker, &label) != nil {
				return nil, errors.New("invalid speaker")
			}
			match := cpuSpeakerPattern.FindStringSubmatch(strings.ToLower(label))
			if match == nil {
				return nil, errors.New("invalid speaker label")
			}
			var err error
			speaker, err = strconv.Atoi(match[1])
			if err != nil {
				return nil, err
			}
		}
		if len(s.Speaker) == 0 || string(s.Speaker) == "null" || speaker < 0 {
			return nil, errors.New("missing or negative speaker")
		}
		segments = append(segments, jobSpeaker{Start: *s.Start, End: *s.End, Speaker: speaker})
	}
	return json.Marshal(struct {
		Segments []jobSpeaker `json:"segments"`
	}{segments})
}
