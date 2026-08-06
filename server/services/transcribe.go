package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"speektome-server/config"
	"speektome-server/ratelimit"
	"speektome-server/utils"
)

var (
	geoCache = make(map[string]string)
	geoMutex sync.RWMutex
)

func GetGeoIPLocation(ip string) string {
	if ip == "" {
		return "Unknown Location"
	}

	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	geoMutex.RLock()
	loc, exists := geoCache[ip]
	geoMutex.RUnlock()
	if exists {
		return loc
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://ip-api.com/json/"+ip, nil)
	if err != nil {
		return ip
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ip
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var geo struct {
			City        string `json:"city"`
			RegionName  string `json:"regionName"`
			CountryCode string `json:"countryCode"`
		}
		if json.NewDecoder(resp.Body).Decode(&geo) == nil {
			var locParts []string
			if geo.City != "" {
				locParts = append(locParts, geo.City)
			}
			if geo.RegionName != "" {
				locParts = append(locParts, geo.RegionName)
			}
			if geo.CountryCode != "" {
				locParts = append(locParts, geo.CountryCode)
			}

			locStr := ip
			if len(locParts) > 0 {
				locStr = fmt.Sprintf("%s (%s)", strings.Join(locParts, ", "), ip)
			}

			geoMutex.Lock()
			geoCache[ip] = locStr
			geoMutex.Unlock()
			return locStr
		}
	}

	return ip
}

func GetHTTPResponseRouting(resp *http.Response, remoteIP string) string {
	if resp == nil {
		return "Unknown Location (No response)"
	}

	if groqReg := resp.Header.Get("x-groq-region"); groqReg != "" {
		return fmt.Sprintf("Groq (%s)", groqReg)
	}

	if cfRay := resp.Header.Get("Cf-Ray"); cfRay != "" {
		parts := strings.Split(cfRay, "-")
		if len(parts) > 1 {
			return fmt.Sprintf("Cloudflare (%s)", parts[len(parts)-1])
		}
		return fmt.Sprintf("Cloudflare (%s)", cfRay)
	}

	if remoteIP != "" {
		return GetGeoIPLocation(remoteIP)
	}

	if server := resp.Header.Get("Server"); server != "" {
		return fmt.Sprintf("Server (%s)", server)
	}

	return "Unknown Location (No routing headers)"
}

type WhisperSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func BuildWhisperPrompt(userLanguages []string) (string, string) {
	valid, err := utils.ValidateLanguages(userLanguages)
	if err != nil || len(valid) == 0 {
		valid = []string{"en"}
	}

	langParam := ""
	promptStr := "Transcribe the audio accurately with full grammar and correct punctuation. Do not translate into other languages. Do not output 'Thank you', 'Thanks for watching', or subtitle credits during silence."

	if len(valid) == 1 {
		langParam = valid[0]
	} else {
		var names []string
		for _, code := range valid {
			names = append(names, utils.GetLanguageName(code))
		}
		promptStr = fmt.Sprintf("Transcribe the audio accurately with full grammar and correct punctuation in the identified spoken language. Spoken languages may include: %s. Do not translate into other languages. Do not output 'Thank you' or subtitle credits during silence.", strings.Join(names, ", "))
	}

	return langParam, promptStr
}

func RunCloudflareTranscribe(ctx context.Context, audioBytes []byte, model string, langParam, promptStr string) (string, []WhisperSegment, string, error) {
	urlStr := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", config.CloudflareAccountID, model)

	audioBase64 := base64.StdEncoding.EncodeToString(audioBytes)
	payload := map[string]interface{}{
		"audio": audioBase64,
	}
	if langParam != "" {
		payload["language"] = langParam
	}
	if promptStr != "" {
		payload["initial_prompt"] = promptStr
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", nil, "", err
	}
	body := bytes.NewReader(jsonPayload)

	ctxTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var remoteIP string
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			if host, _, err := net.SplitHostPort(addr); err == nil {
				remoteIP = host
			} else {
				remoteIP = addr
			}
		},
	}
	ctxTrace := httptrace.WithClientTrace(ctxTimeout, trace)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctxTrace, "POST", urlStr, body)
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.CloudflareAPIToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	routingLoc := GetHTTPResponseRouting(resp, remoteIP)

	if resp.StatusCode != http.StatusOK {
		bodyB, _ := io.ReadAll(resp.Body)
		return "", nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyB))
	}

	var response struct {
		Result struct {
			Text     string           `json:"text"`
			Segments []WhisperSegment `json:"segments"`
		} `json:"result"`
		Success bool  `json:"success"`
		Errors  []any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", nil, "", err
	}

	if !response.Success {
		return "", nil, "", fmt.Errorf("cloudflare API success false: %v", response.Errors)
	}

	rawText := strings.TrimSpace(response.Result.Text)
	hallucinations := map[string]bool{
		"thank you": true, "thank you.": true, "thanks for watching": true, "thanks for watching.": true,
		"thank you for watching.": true, "please subscribe": true, "subtitles by": true, "subtitles by amara": true,
		"thank you!": true, "thanks!": true,
	}
	checkText := strings.Trim(strings.ToLower(rawText), ".,!?\"'")
	if hallucinations[checkText] {
		rawText = ""
		response.Result.Segments = nil
	}
	if response.Result.Segments == nil {
		response.Result.Segments = make([]WhisperSegment, 0)
	}

	return rawText, response.Result.Segments, routingLoc, nil
}

func RunDeepInfraTranscribe(ctx context.Context, audioBytes []byte, urlStr string, langParam, promptStr string) (string, []WhisperSegment, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("audio", "recording.ogg")
	if err != nil {
		return "", nil, "", err
	}
	_, _ = filePart.Write(audioBytes)

	if langParam != "" {
		_ = writer.WriteField("language", langParam)
	}
	if promptStr != "" {
		_ = writer.WriteField("initial_prompt", promptStr)
		_ = writer.WriteField("prompt", promptStr)
	}
	_ = writer.Close()

	ctxTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var remoteIP string
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			if host, _, err := net.SplitHostPort(addr); err == nil {
				remoteIP = host
			} else {
				remoteIP = addr
			}
		},
	}
	ctxTrace := httptrace.WithClientTrace(ctxTimeout, trace)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctxTrace, "POST", urlStr, body)
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+config.DeepInfraAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	routingLoc := GetHTTPResponseRouting(resp, remoteIP)

	if resp.StatusCode != http.StatusOK {
		bodyB, _ := io.ReadAll(resp.Body)
		return "", nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyB))
	}

	var result struct {
		Text     string           `json:"text"`
		Segments []WhisperSegment `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, "", err
	}

	rawText := strings.TrimSpace(result.Text)
	hallucinations := map[string]bool{
		"thank you": true, "thank you.": true, "thanks for watching": true, "thanks for watching.": true,
		"thank you for watching.": true, "please subscribe": true, "subtitles by": true, "subtitles by amara": true,
		"thank you!": true, "thanks!": true,
	}
	checkText := strings.Trim(strings.ToLower(rawText), ".,!?\"'")
	if hallucinations[checkText] {
		rawText = ""
		result.Segments = nil
	}
	if result.Segments == nil {
		result.Segments = make([]WhisperSegment, 0)
	}

	return rawText, result.Segments, routingLoc, nil
}

func RunGroqTranscribe(ctx context.Context, audioBytes []byte, model string, langParam, promptStr string) (string, []WhisperSegment, string, error) {
	groqBody := &bytes.Buffer{}
	writer := multipart.NewWriter(groqBody)
	_ = writer.WriteField("model", model)
	_ = writer.WriteField("response_format", "verbose_json")
	if langParam != "" {
		_ = writer.WriteField("language", langParam)
	}
	if promptStr != "" {
		_ = writer.WriteField("prompt", promptStr)
	}
	filePart, err := writer.CreateFormFile("file", "recording.ogg")
	if err != nil {
		return "", nil, "", err
	}
	_, _ = filePart.Write(audioBytes)
	_ = writer.Close()

	ctxTimeout, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var remoteIP string
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			if host, _, err := net.SplitHostPort(addr); err == nil {
				remoteIP = host
			} else {
				remoteIP = addr
			}
		},
	}
	ctxTrace := httptrace.WithClientTrace(ctxTimeout, trace)

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctxTrace, "POST", config.GroqAPIURL, groqBody)
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+config.GroqAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, "", err
	}
	defer resp.Body.Close()

	routingLoc := GetHTTPResponseRouting(resp, remoteIP)

	if resp.StatusCode != http.StatusOK {
		bodyB, _ := io.ReadAll(resp.Body)
		return "", nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyB))
	}

	var result struct {
		Text     string           `json:"text"`
		Segments []WhisperSegment `json:"segments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, "", err
	}

	rawText := strings.TrimSpace(result.Text)
	hallucinations := map[string]bool{
		"thank you": true, "thank you.": true, "thanks for watching": true, "thanks for watching.": true,
		"thank you for watching.": true, "please subscribe": true, "subtitles by": true, "subtitles by amara": true,
		"thank you!": true, "thanks!": true,
	}
	checkText := strings.Trim(strings.ToLower(rawText), ".,!?\"'")
	if hallucinations[checkText] {
		rawText = ""
		result.Segments = nil
	}
	if result.Segments == nil {
		result.Segments = make([]WhisperSegment, 0)
	}

	return rawText, result.Segments, routingLoc, nil
}

func RunWhisperTranscribeWithModel(ctx context.Context, audioBytes []byte, model string, userLanguages []string) (string, []WhisperSegment, string, string, error) {
	cfg := config.ConfigManager.Get()
	langParam, promptStr := BuildWhisperPrompt(userLanguages)

	providers := []string{cfg.DefaultProvider}
	allProviders := []string{"groq", "deepinfra", "cloudflare"}
	for _, p := range allProviders {
		if p != cfg.DefaultProvider {
			providers = append(providers, p)
		}
	}

	var lastErr error
	for _, provider := range providers {
		switch provider {
		case "groq":
			if cfg.GroqEnabled && config.GroqAPIKey != "" {
				if ratelimit.GroqRateLimiter.Allow() {
					log.Printf("[GROQ] Attempting transcription using model %s (lang: %q)", model, langParam)
					text, segments, loc, err := RunGroqTranscribe(ctx, audioBytes, model, langParam, promptStr)
					if err == nil {
						return text, segments, "groq", loc, nil
					}
					log.Printf("[GROQ ERROR] Transcription failed: %v. Trying fallback.", err)
					lastErr = err
				} else {
					log.Printf("[GROQ LIMIT] Global rate limit reached (limit: %d RPM). Trying fallback.", cfg.GroqRateLimitRPM)
					lastErr = fmt.Errorf("groq rate limit reached")
				}
			}
		case "deepinfra":
			if cfg.DeepInfraEnabled && config.DeepInfraAPIKey != "" {
				diModel := "openai/whisper-large-v3-turbo"
				if model == "whisper-large-v3" {
					diModel = "openai/whisper-large-v3"
				}
				diURL := config.DeepInfraBaseURL + diModel

				log.Printf("[DI INFRA] Attempting transcription using model %s (lang: %q)", diModel, langParam)
				text, segments, loc, err := RunDeepInfraTranscribe(ctx, audioBytes, diURL, langParam, promptStr)
				if err == nil {
					return text, segments, "deepinfra", loc, nil
				}
				log.Printf("[DI INFRA ERROR] Transcription failed: %v. Trying fallback.", err)
				lastErr = err
			}
		case "cloudflare":
			if cfg.CloudflareEnabled && config.CloudflareAPIToken != "" && config.CloudflareAccountID != "" {
				cfModel := "@cf/openai/whisper-large-v3-turbo"
				if model == "whisper-large-v3" {
					cfModel = "@cf/openai/whisper"
				}
				log.Printf("[CLOUDFLARE AI] Attempting transcription using model %s (lang: %q)", cfModel, langParam)
				text, segments, loc, err := RunCloudflareTranscribe(ctx, audioBytes, cfModel, langParam, promptStr)
				if err == nil {
					return text, segments, "cloudflare", loc, nil
				}
				log.Printf("[CLOUDFLARE AI ERROR] Transcription failed: %v. Trying fallback.", err)
				lastErr = err
			}
		}
	}

	if lastErr != nil {
		return "", nil, "", "", fmt.Errorf("all enabled transcription providers failed. Last error: %v", lastErr)
	}
	return "", nil, "", "", fmt.Errorf("no transcription providers are enabled or configured")
}

func RunWhisperTranscribe(ctx context.Context, audioBytes []byte) (string, []WhisperSegment, error) {
	text, segments, _, _, err := RunWhisperTranscribeWithModel(ctx, audioBytes, "whisper-large-v3-turbo", []string{"en"})
	return text, segments, err
}
