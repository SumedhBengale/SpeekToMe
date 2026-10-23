package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/pipeline"
	"speektome-server/vocabularydecision"
	"speektome/vocabulary"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchPlanPolicyKeepsVocabularyAndJevButOnlyPaysForProEnhancement(t *testing.T) {
	database, err := db.NewDatabase(filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	app := &App{DB: database}
	orig, raw, diKey, llmKey := config.ConfigManager.Get(), config.ConfigManager.GetRawAll(), config.DeepInfraAPIKey, config.CerebrasAPIKey
	t.Cleanup(func() {
		config.ConfigManager.Set(orig, raw)
		config.DeepInfraAPIKey = diKey
		config.CerebrasAPIKey = llmKey
		pipeline.GlobalCircuitBreaker.Reset()
	})
	config.DeepInfraAPIKey = "test-only"
	config.CerebrasAPIKey = "test-only"
	t.Setenv("SPEEKTOME_ACK_SECRET", "synthetic-only")
	var llmCalls, workerCalls, jevCalls atomic.Int32
	var modelUsed atomic.Value
	asr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Error(e)
			return
		}
		defer r.MultipartForm.RemoveAll()
		modelUsed.Store(r.FormValue("model"))
		json.NewEncoder(w).Encode(map[string]string{"text": "We use postgrsq today."})
	}))
	defer asr.Close()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil || len(body.Messages) < 2 {
			t.Error("invalid enhancement payload", e)
			http.Error(w, "invalid", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": body.Messages[1].Content}}}})
	}))
	defer llm.Close()
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workerCalls.Add(1)
		var req vocabulary.Request
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		compiled, e := vocabulary.Compile(req.Terms)
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(vocabulary.Propose(r.Context(), req.Raw, compiled, vocabulary.TermsHash(req.Terms)))
	}))
	defer worker.Close()
	t.Setenv("SPEEKTOME_FLEET_URL", worker.URL)
	token := filepath.Join(t.TempDir(), "worker.token")
	if e := os.WriteFile(token, []byte("test-only"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SPEEKTOME_WORKER_TOKEN_FILE", token)
	jev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls.Add(1)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		answers := map[string]any{}
		for id := range body.Questions {
			answers[id] = map[string]any{"type": "choice", "choice": "use_vocabulary", "confidence": .99, "probabilities": map[string]float64{"keep_original": .01, "use_vocabulary": .99}}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers})
	}))
	defer jev.Close()
	client := vocabularydecision.Default()
	oldKey, oldURL, oldHTTP := client.Key, client.URL, client.HTTP
	client.Key = "test-only"
	client.URL = jev.URL
	client.HTTP = jev.Client()
	t.Cleanup(func() { client.Key = oldKey; client.URL = oldURL; client.HTTP = oldHTTP })
	cfg := orig
	cfg.Models.BatchSTTModels = []config.STTModelSpec{
		{Provider: "custom", Model: "paid-model", Enabled: true, EndpointOverride: asr.URL},
		{Provider: "deepinfra", Model: "openai/whisper-large-v3-turbo", Enabled: true, EndpointOverride: asr.URL},
	}
	cfg.Models.EnhancementModels = []config.ModelSpec{{Provider: config.ProviderCerebras, Model: "paid-grammar", Enabled: true, EndpointOverride: llm.URL, TimeoutMs: 2000}}
	config.ConfigManager.Set(cfg, raw)
	pipeline.GlobalCircuitBreaker.Reset()
	for i, tc := range []struct {
		tier     string
		active   int
		end      int64
		flag     string
		enhanced int
		model    string
	}{
		{"free", 0, 0, "true", 0, "openai/whisper-large-v3-turbo"},
		{"pro", 1, 0, "false", 1, "paid-model"},
		{"pro", 0, 0, "true", 0, "openai/whisper-large-v3-turbo"},
		{"pro", 1, time.Now().Add(-db.SubscriptionGracePeriod - time.Hour).Unix(), "true", 0, "openai/whisper-large-v3-turbo"},
	} {
		id := int64(21000 + i)
		_, e := database.GetDB().Exec(`INSERT INTO users(id,email,name,plan_tier,plan_active,razorpay_current_end) VALUES(?,?,'Test',?,?,?)`, id, fmt.Sprintf("plan%d@example.test", id), tc.tier, tc.active, tc.end)
		if e != nil {
			t.Fatal(e)
		}
		session := fmt.Sprintf("plan%d", id)
		if e = database.CreateSession(session, id, 0); e != nil {
			t.Fatal(e)
		}
		if _, e = database.AddVocabularyTerm(id, "PostgreSQL", "postgresql"); e != nil {
			t.Fatal(e)
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, e := form.CreateFormFile("file", "synthetic.ogg")
		if e != nil {
			t.Fatal(e)
		}
		file.Write([]byte("synthetic audio"))
		form.WriteField("enhance", tc.flag)
		form.WriteField("model", "expensive-client-selection")
		form.WriteField("language", "en")
		form.Close()
		req := httptest.NewRequest("POST", "/generate", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+session)
		before := llmCalls.Load()
		w := httptest.NewRecorder()
		app.HandleGenerate(w, req)
		if w.Code != 200 {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if modelUsed.Load() != tc.model || llmCalls.Load()-before != int32(tc.enhanced) {
			t.Fatalf("plan bypass: %+v model=%q enhancement calls=%d", tc, modelUsed.Load(), llmCalls.Load()-before)
		}
		var response struct {
			Text string `json:"text"`
		}
		if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil || !strings.Contains(response.Text, "PostgreSQL") {
			t.Fatal("vocabulary/Jev skipped", response, e)
		}
		var logged int
		if e = database.GetDB().QueryRow(`SELECT enhance_enabled FROM usage_logs WHERE user_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&logged); e != nil || logged != tc.enhanced {
			t.Fatal("incorrect cost accounting", logged, e)
		}
	}
	if workerCalls.Load() != 4 || jevCalls.Load() != 4 {
		t.Fatalf("vocabulary/Jev must run for both plans: worker=%d jev=%d", workerCalls.Load(), jevCalls.Load())
	}
}

func TestFreeCannotOpenTranscriptionStream(t *testing.T) {
	a := limitApp(t)
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET plan_tier='free',plan_active=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(a.HandleStreamTranscribe))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"?token=user1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var event StreamEventMessage
	if json.Unmarshal(data, &event) != nil || event.Type != "error" || !strings.Contains(event.Error, "Pro") {
		t.Fatalf("Free opened stream: %s", data)
	}
}

func TestVocabularyHTTPEnforcesLimitAndListsPreservedTerms(t *testing.T) {
	a := limitApp(t)
	for i := 0; i < 25; i++ {
		term := fmt.Sprintf("term%02d", i)
		if _, err := a.DB.AddVocabularyTerm(1, term, term); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET plan_tier='free',plan_active=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/auth/vocabulary", strings.NewReader(`{"canonical_term":"extra"}`))
	req.Header.Set("Authorization", "Bearer user1")
	w := httptest.NewRecorder()
	a.HandleAddVocabulary(w, req)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "vocabulary_limit") {
		t.Fatal(w.Code, w.Body.String())
	}
	get := httptest.NewRequest("GET", "/auth/vocabulary", nil)
	get.Header.Set("Authorization", "Bearer user1")
	w = httptest.NewRecorder()
	a.HandleGetVocabulary(w, get)
	var terms []db.VocabularyTerm
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &terms) != nil || len(terms) != 25 {
		t.Fatal("saved dictionary was hidden/deleted", w.Code, w.Body.String())
	}
	_, active, err := a.DB.GetUserAndVocabularyBySession("user1")
	if err != nil || len(active) != 20 {
		t.Fatal("matching snapshot exceeded Free limit", len(active), err)
	}
}

func TestFreeCannotBypassPlanThroughMeetingRoutesWithoutVault(t *testing.T) {
	a := limitApp(t)
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET plan_tier='free',plan_active=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, handler := range []http.HandlerFunc{a.HandleStartMeetingSession} {
		req := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer user1")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != 403 {
			t.Fatalf("Free bypassed meeting plan check: %d %s", w.Code, w.Body.String())
		}
	}
	jobs := &MeetingJobs{app: a}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer user1")
	w := httptest.NewRecorder()
	jobs.create(w, req)
	if w.Code != 403 {
		t.Fatalf("Free created durable job: %d %s", w.Code, w.Body.String())
	}
}
