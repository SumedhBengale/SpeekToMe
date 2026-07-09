package main

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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

var (
	DEEPINFRA_API_KEY     string
	GROQ_API_KEY          string
	CLOUDFLARE_API_TOKEN  string
	CLOUDFLARE_ACCOUNT_ID string
)

func getGeoIPLocation(ip string) string {
	if ip == "" {
		return "Unknown"
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://ip-api.com/json/"+ip, nil)
	if err != nil {
		return ip
	}

	client := &http.Client{Timeout: 3 * time.Second}
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
			if len(locParts) > 0 {
				return fmt.Sprintf("%s (%s)", strings.Join(locParts, ", "), ip)
			}
		}
	}
	return ip
}

func getHTTPResponseRouting(resp *http.Response, remoteIP string) string {
	if resp == nil {
		return "No response"
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
		return getGeoIPLocation(remoteIP)
	}
	if server := resp.Header.Get("Server"); server != "" {
		return fmt.Sprintf("Server (%s)", server)
	}
	return "Unknown"
}

type Result struct {
	Index    int
	Provider string
	Latency  time.Duration
	Routing  string
	Text     string
	Error    error
}

func runCloudflare(ctx context.Context, idx int, audioBytes []byte) Result {
	start := time.Now()
	urlStr := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", CLOUDFLARE_ACCOUNT_ID, "@cf/openai/whisper-large-v3-turbo")

	audioBase64 := base64.StdEncoding.EncodeToString(audioBytes)
	payload := map[string]interface{}{
		"audio": audioBase64,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return Result{Index: idx, Provider: "Cloudflare", Error: err}
	}
	body := bytes.NewReader(jsonPayload)

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
	ctxTrace := httptrace.WithClientTrace(ctx, trace)

	req, err := http.NewRequestWithContext(ctxTrace, "POST", urlStr, body)
	if err != nil {
		return Result{Index: idx, Provider: "Cloudflare", Error: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+CLOUDFLARE_API_TOKEN)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Index: idx, Provider: "Cloudflare", Error: err}
	}
	defer resp.Body.Close()

	routing := getHTTPResponseRouting(resp, remoteIP)
	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Result{Index: idx, Provider: "Cloudflare", Latency: latency, Routing: routing, Error: fmt.Errorf("status %d: %s", resp.StatusCode, string(b))}
	}

	var response struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
		Success bool  `json:"success"`
		Errors  []any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return Result{Index: idx, Provider: "Cloudflare", Latency: latency, Routing: routing, Error: err}
	}

	if !response.Success {
		return Result{Index: idx, Provider: "Cloudflare", Latency: latency, Routing: routing, Error: fmt.Errorf("success false: %v", response.Errors)}
	}

	return Result{Index: idx, Provider: "Cloudflare", Latency: latency, Routing: routing, Text: strings.TrimSpace(response.Result.Text)}
}

func runDeepInfra(ctx context.Context, idx int, audioBytes []byte) Result {
	start := time.Now()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("audio", "recording.ogg")
	if err != nil {
		return Result{Index: idx, Provider: "DeepInfra", Error: err}
	}
	_, _ = filePart.Write(audioBytes)
	_ = writer.Close()

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
	ctxTrace := httptrace.WithClientTrace(ctx, trace)

	urlStr := "https://api.deepinfra.com/v1/inference/openai/whisper-large-v3-turbo"
	req, err := http.NewRequestWithContext(ctxTrace, "POST", urlStr, body)
	if err != nil {
		return Result{Index: idx, Provider: "DeepInfra", Error: err}
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+DEEPINFRA_API_KEY)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Index: idx, Provider: "DeepInfra", Error: err}
	}
	defer resp.Body.Close()

	routing := getHTTPResponseRouting(resp, remoteIP)
	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Result{Index: idx, Provider: "DeepInfra", Latency: latency, Routing: routing, Error: fmt.Errorf("status %d: %s", resp.StatusCode, string(b))}
	}

	var res struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Result{Index: idx, Provider: "DeepInfra", Latency: latency, Routing: routing, Error: err}
	}

	return Result{Index: idx, Provider: "DeepInfra", Latency: latency, Routing: routing, Text: strings.TrimSpace(res.Text)}
}

func runGroq(ctx context.Context, idx int, audioBytes []byte) Result {
	start := time.Now()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("model", "whisper-large-v3-turbo")
	_ = writer.WriteField("response_format", "verbose_json")
	filePart, err := writer.CreateFormFile("file", "recording.ogg")
	if err != nil {
		return Result{Index: idx, Provider: "Groq", Error: err}
	}
	_, _ = filePart.Write(audioBytes)
	_ = writer.Close()

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
	ctxTrace := httptrace.WithClientTrace(ctx, trace)

	urlStr := "https://api.groq.com/openai/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctxTrace, "POST", urlStr, body)
	if err != nil {
		return Result{Index: idx, Provider: "Groq", Error: err}
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+GROQ_API_KEY)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{Index: idx, Provider: "Groq", Error: err}
	}
	defer resp.Body.Close()

	routing := getHTTPResponseRouting(resp, remoteIP)
	latency := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return Result{Index: idx, Provider: "Groq", Latency: latency, Routing: routing, Error: fmt.Errorf("status %d: %s", resp.StatusCode, string(b))}
	}

	var res struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return Result{Index: idx, Provider: "Groq", Latency: latency, Routing: routing, Error: err}
	}

	return Result{Index: idx, Provider: "Groq", Latency: latency, Routing: routing, Text: strings.TrimSpace(res.Text)}
}

func main() {
	_ = godotenv.Load()
	if os.Getenv("GROQ_API_KEY") == "" {
		_ = godotenv.Load("../.env")
		_ = godotenv.Load(".env")
	}
	DEEPINFRA_API_KEY = os.Getenv("DEEPINFRA_API_KEY")
	GROQ_API_KEY = os.Getenv("GROQ_API_KEY")
	CLOUDFLARE_API_TOKEN = os.Getenv("CLOUDFLARE_API_TOKEN")
	CLOUDFLARE_ACCOUNT_ID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")

	runCF := CLOUDFLARE_API_TOKEN != "" && CLOUDFLARE_ACCOUNT_ID != ""
	runDI := DEEPINFRA_API_KEY != ""
	runG := GROQ_API_KEY != ""

	if !runCF && !runDI && !runG {
		log.Fatalf("No transcription provider credentials configured in environment!")
	}

	audioPaths := []string{
		"../uploads/meetings/meeting_5_1783327344745226200.ogg",
		"uploads/meetings/meeting_5_1783327344745226200.ogg",
		"server/uploads/meetings/meeting_5_1783327344745226200.ogg",
	}
	var audioBytes []byte
	var err error
	for _, p := range audioPaths {
		audioBytes, err = os.ReadFile(p)
		if err == nil {
			log.Printf("Loaded audio file: %s", p)
			break
		}
	}
	if len(audioBytes) == 0 {
		log.Fatalf("Could not load test audio file. Error: %v", err)
	}

	totalRequests := 1
	if len(os.Args) > 1 {
		if val, err := strconv.Atoi(os.Args[1]); err == nil && val > 0 {
			totalRequests = val
		}
	}
	var activeProviders int
	if runCF {
		activeProviders++
	}
	if runDI {
		activeProviders++
	}
	if runG {
		activeProviders++
	}

	results := make(chan Result, totalRequests*activeProviders)
	var wg sync.WaitGroup

	log.Printf("Starting benchmarking: Sending %d simultaneous requests to configured providers...", totalRequests)
	for i := 1; i <= totalRequests; i++ {
		if runCF {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results <- runCloudflare(context.Background(), idx, audioBytes)
			}(i)
		}
		if runDI {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results <- runDeepInfra(context.Background(), idx, audioBytes)
			}(i)
		}
		if runG {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results <- runGroq(context.Background(), idx, audioBytes)
			}(i)
		}
	}

	wg.Wait()
	close(results)

	var cfLatencies []time.Duration
	var diLatencies []time.Duration
	var groqLatencies []time.Duration
	cfSuccess, diSuccess, groqSuccess := 0, 0, 0

	fmt.Println("\n" + strings.Repeat("=", 85))
	fmt.Printf("%-5s | %-10s | %-10s | %-32s | %s\n", "Index", "Provider", "Latency", "Routing Location", "Status / Text Snippet")
	fmt.Println(strings.Repeat("=", 85))

	for r := range results {
		statusStr := ""
		if r.Error != nil {
			statusStr = "ERROR: " + r.Error.Error()
		} else {
			statusStr = "SUCCESS: "
			if len(r.Text) > 25 {
				statusStr += "\"" + r.Text[:25] + "...\""
			} else {
				statusStr += "\"" + r.Text + "\""
			}
			if r.Provider == "Cloudflare" {
				cfLatencies = append(cfLatencies, r.Latency)
				cfSuccess++
			} else if r.Provider == "DeepInfra" {
				diLatencies = append(diLatencies, r.Latency)
				diSuccess++
			} else {
				groqLatencies = append(groqLatencies, r.Latency)
				groqSuccess++
			}
		}
		fmt.Printf("#%-4d | %-10s | %-10s | %-32s | %s\n", r.Index, r.Provider, r.Latency.Round(time.Millisecond), r.Routing, statusStr)
	}
	fmt.Println(strings.Repeat("=", 85))

	var cfAvg time.Duration
	if cfSuccess > 0 {
		var sum time.Duration
		for _, l := range cfLatencies {
			sum += l
		}
		cfAvg = sum / time.Duration(cfSuccess)
	}

	var diAvg time.Duration
	if diSuccess > 0 {
		var sum time.Duration
		for _, l := range diLatencies {
			sum += l
		}
		diAvg = sum / time.Duration(diSuccess)
	}

	var groqAvg time.Duration
	if groqSuccess > 0 {
		var sum time.Duration
		for _, l := range groqLatencies {
			sum += l
		}
		groqAvg = sum / time.Duration(groqSuccess)
	}

	fmt.Printf("\n=== BENCHMARK STATISTICS ===\n")
	if runCF {
		fmt.Printf("Cloudflare Success Rate: %d/%d | Average Latency: %v\n", cfSuccess, totalRequests, cfAvg.Round(time.Millisecond))
	}
	if runDI {
		fmt.Printf("DeepInfra Success Rate:  %d/%d | Average Latency: %v\n", diSuccess, totalRequests, diAvg.Round(time.Millisecond))
	}
	if runG {
		fmt.Printf("Groq Success Rate:       %d/%d | Average Latency: %v\n", groqSuccess, totalRequests, groqAvg.Round(time.Millisecond))
	}
	fmt.Println(strings.Repeat("=", 45))
}
