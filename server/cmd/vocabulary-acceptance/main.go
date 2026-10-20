// Runs the real application streaming handler against an isolated test database.
// Production configuration is read, but no production user/session/quota is changed.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"github.com/joho/godotenv"
	log "speektome-server/privacylog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"speektome-server/config"
	"speektome-server/db"
	"speektome-server/handlers"
	"speektome-server/ratelimit"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	termsPath := flag.String("terms", "", "JSON vocabulary array")
	directory := flag.String("dir", "/tmp/speektome-vocabulary-acceptance", "isolated artifacts")
	flag.Parse()
	if *termsPath == "" {
		log.Fatal("terms required")
	}
	_ = godotenv.Load(".env")
	config.DeepSeekAPIKey = os.Getenv("DEEPSEEK_API_KEY")
	config.CerebrasAPIKey = os.Getenv("CEREBRAS_API_KEY")
	config.WaferAPIKey = os.Getenv("WAFER_API_KEY")
	config.DeepInfraAPIKey = os.Getenv("DEEPINFRA_API_KEY")
	config.GroqAPIKey = os.Getenv("GROQ_API_KEY")
	config.OpenRouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
	config.CloudflareAPIToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	config.CloudflareAccountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	production, e := db.NewDatabase(os.Getenv("DATABASE_URL"))
	if e != nil {
		log.Fatal(e)
	}
	config.SyncConfigsFromValkeyOrDB(production, ratelimit.GroqRateLimiter.SetLimit)
	production.Close()
	if e = os.MkdirAll(*directory, 0700); e != nil {
		log.Fatal(e)
	}
	isolated, e := db.NewDatabase(filepath.Join(*directory, "test.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer isolated.Close()
	id, e := isolated.GetOrCreateUser("vocab-acceptance", "vocab-acceptance@invalid.example", "Vocabulary acceptance")
	if e != nil {
		log.Fatal(e)
	}
	_, e = isolated.GetDB().Exec(`UPDATE users SET plan_tier='pro',plan_active=1 WHERE id=?`, id)
	if e != nil {
		log.Fatal(e)
	}
	var terms []string
	b, e := os.ReadFile(*termsPath)
	if e != nil {
		log.Fatal(e)
	}
	if json.Unmarshal(b, &terms) != nil || len(terms) > 1000 {
		log.Fatal("invalid vocabulary")
	}
	tx, e := isolated.GetDB().Begin()
	if e != nil {
		log.Fatal(e)
	}
	for _, term := range terms {
		if _, e = tx.Exec(`INSERT INTO vocabulary(user_id,canonical_term,normalized_term) VALUES(?,?,?) ON CONFLICT(user_id,canonical_term) DO NOTHING`, id, term, strings.ToLower(term)); e != nil {
			log.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		log.Fatal(e)
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	token := hex.EncodeToString(secret)
	if e = isolated.CreateSession(token, id, 0); e != nil {
		log.Fatal(e)
	}
	os.WriteFile(filepath.Join(*directory, "session.token"), []byte(token), 0600)
	app := &handlers.App{DB: isolated}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/stream", app.HandleStreamTranscribe)
	mux.HandleFunc("GET /audit", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", 401)
			return
		}
		logs, e := isolated.GetUserUsageLogs(id)
		if e != nil {
			http.Error(w, "audit unavailable", 500)
			return
		}
		json.NewEncoder(w).Encode(logs)
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	server := &http.Server{Addr: "127.0.0.1:18001", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() { <-ctx.Done(); server.Close() }()
	log.Printf("isolated acceptance ready: user=%s vocabulary=%d", strconv.FormatInt(id, 10), len(terms))
	if e = server.ListenAndServe(); e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
