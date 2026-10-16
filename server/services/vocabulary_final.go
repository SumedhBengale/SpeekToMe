package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"speektome/vocabulary"
	"strings"
	"time"
)

var vocabularyHTTP = &http.Client{Timeout: 1500 * time.Millisecond}

func MatchFinalVocabulary(ctx context.Context, raw string, terms []string) (*vocabulary.Result, error) {
	base := strings.TrimRight(os.Getenv("SPEEKTOME_FLEET_URL"), "/")
	if base == "" {
		return nil, fmt.Errorf("fleet not configured")
	}
	tokenPath := os.Getenv("SPEEKTOME_WORKER_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "/etc/speektome-fleet/worker.token"
	}
	token, e := os.ReadFile(tokenPath)
	if e != nil {
		return nil, e
	}
	body, _ := json.Marshal(vocabulary.Request{Raw: raw, Terms: terms})
	req, e := http.NewRequestWithContext(ctx, "POST", base+"/v1/vocabulary/final", bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	resp, e := vocabularyHTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("worker matcher status %d", resp.StatusCode)
	}
	var result vocabulary.Result
	if e = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); e != nil {
		return nil, e
	}
	if e = result.Validate(raw, terms); e != nil {
		return nil, e
	}
	return &result, nil
}
