package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// Define your Turso credentials here
const (
	TursoURL   = "libsql://YOUR-DATABASE.turso.io"
	TursoToken = "REDACTED_TURSO_TOKEN"
)

func main() {
	connectionString := fmt.Sprintf("%s?authToken=%s", TursoURL, TursoToken)
	log.Printf("Connecting to Turso: %s", TursoURL)

	db, err := sql.Open("libsql", connectionString)
	if err != nil {
		log.Fatalf("Failed to open database connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Successfully connected and pinged Turso database!")

	// Create tables if they do not exist
	log.Println("Initializing tables if empty...")
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE,
			name TEXT,
			google_id TEXT UNIQUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			plan_tier TEXT DEFAULT 'free',
			plan_active INTEGER DEFAULT 0,
			plan_activated_at TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			audio_duration REAL,
			whisper_time REAL,
			llm_time REAL,
			total_time REAL,
			status TEXT,
			audio_size_bytes INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS saas_config (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// Insert test data
	log.Println("Seeding test data...")
	email := fmt.Sprintf("bench_%d@example.com", rand.Intn(100000))
	token := fmt.Sprintf("session_token_%d", rand.Intn(100000))

	res, err := db.Exec("INSERT INTO users (email, name, google_id, plan_tier, plan_active) VALUES (?, ?, ?, ?, ?)",
		email, "Benchmark User", "google_"+email, "free", 1)
	if err != nil {
		log.Fatalf("Failed to insert test user: %v", err)
	}
	userID, _ := res.LastInsertId()

	expiresAt := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	_, err = db.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expiresAt)
	if err != nil {
		log.Fatalf("Failed to insert test session: %v", err)
	}

	configs := map[string]string{
		"free_rate_limit_rpm":          "3",
		"paid_rate_limit_rpm":          "15",
		"free_fup_limit_seconds_daily": "300",
		"free_monthly_limit_seconds":   "3600",
	}
	for k, v := range configs {
		_, _ = db.Exec("INSERT OR IGNORE INTO saas_config (key, value) VALUES (?, ?)", k, v)
	}

	for i := 0; i < 5; i++ {
		_, _ = db.Exec("INSERT INTO usage_logs (user_id, audio_duration, total_time, status) VALUES (?, ?, ?, ?)",
			userID, 15.5, 2.3, "success")
	}

	log.Println("Seeding complete. Beginning latency tests...")
	fmt.Println("\n=======================================================")
	fmt.Println("             TURSO DATABASE LATENCY BENCHMARK          ")
	fmt.Println("=======================================================")

	var totalDuration time.Duration

	// Query 1: Authenticate / Get User By Session
	q1Start := time.Now()
	var uID int64
	var uEmail, uPlanTier string
	var uPlanActive int
	err = db.QueryRow(`
		SELECT u.id, u.email, u.plan_tier, u.plan_active 
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		WHERE s.token = ? AND s.expires_at > ?
	`, token, time.Now().Format(time.RFC3339)).Scan(&uID, &uEmail, &uPlanTier, &uPlanActive)
	q1Dur := time.Since(q1Start)
	totalDuration += q1Dur
	if err != nil {
		fmt.Printf("Query 1 (Auth Check) Failed: %v\n", err)
	} else {
		fmt.Printf("1. Auth Check (GetUserBySession):         %7.2f ms\n", float64(q1Dur.Microseconds())/1000.0)
	}

	// Query 2: Get SaaS Configs
	q2Start := time.Now()
	rows, err := db.Query("SELECT key, value FROM saas_config")
	q2Dur := time.Since(q2Start)
	totalDuration += q2Dur
	if err != nil {
		fmt.Printf("Query 2 (GetSaasConfig) Failed: %v\n", err)
	} else {
		configMap := make(map[string]string)
		for rows.Next() {
			var k, v string
			_ = rows.Scan(&k, &v)
			configMap[k] = v
		}
		rows.Close()
		fmt.Printf("2. Fetch Configurations (GetSaasConfig):  %7.2f ms\n", float64(q2Dur.Microseconds())/1000.0)
	}

	// Query 3: Rate Limit Check
	q3Start := time.Now()
	lastMinute := time.Now().UTC().Add(-1 * time.Minute).Format("2006-01-02 15:04:05")
	var reqCount int
	err = db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE user_id = ? AND timestamp >= ?", uID, lastMinute).Scan(&reqCount)
	q3Dur := time.Since(q3Start)
	totalDuration += q3Dur
	if err != nil {
		fmt.Printf("Query 3 (Rate Limit Check) Failed: %v\n", err)
	} else {
		fmt.Printf("3. Rate Limit Check (Last Minute Count):  %7.2f ms\n", float64(q3Dur.Microseconds())/1000.0)
	}

	// Query 4: FUP Limit Check
	q4Start := time.Now()
	last24Hours := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	var dailyDuration float64
	err = db.QueryRow("SELECT COALESCE(SUM(audio_duration), 0.0) FROM usage_logs WHERE user_id = ? AND timestamp >= ? AND status = 'success'", uID, last24Hours).Scan(&dailyDuration)
	q4Dur := time.Since(q4Start)
	totalDuration += q4Dur
	if err != nil {
		fmt.Printf("Query 4 (Daily Limit Check) Failed: %v\n", err)
	} else {
		fmt.Printf("4. Daily Limit Check (24h Sum Duration):  %7.2f ms\n", float64(q4Dur.Microseconds())/1000.0)
	}

	// Query 5: Monthly Limit Check
	q5Start := time.Now()
	last30Days := time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	var monthlyDuration float64
	err = db.QueryRow("SELECT COALESCE(SUM(audio_duration), 0.0) FROM usage_logs WHERE user_id = ? AND timestamp >= ? AND status = 'success'", uID, last30Days).Scan(&monthlyDuration)
	q5Dur := time.Since(q5Start)
	totalDuration += q5Dur
	if err != nil {
		fmt.Printf("Query 5 (Monthly Limit Check) Failed: %v\n", err)
	} else {
		fmt.Printf("5. Monthly Limit Check (30d Sum Duration):%7.2f ms\n", float64(q5Dur.Microseconds())/1000.0)
	}

	// Query 6: Log Usage
	q6Start := time.Now()
	_, err = db.Exec("INSERT INTO usage_logs (user_id, audio_duration, whisper_time, llm_time, total_time, status) VALUES (?, ?, ?, ?, ?, ?)",
		uID, 12.0, 1.2, 0.8, 2.0, "success")
	q6Dur := time.Since(q6Start)
	totalDuration += q6Dur
	if err != nil {
		fmt.Printf("Query 6 (LogUsage Write) Failed: %v\n", err)
	} else {
		fmt.Printf("6. Write Log Entry (LogUsage):            %7.2f ms\n", float64(q6Dur.Microseconds())/1000.0)
	}

	fmt.Println("-------------------------------------------------------")
	fmt.Printf("TOTAL SEQUENTIAL LATENCY:                 %7.2f ms\n", float64(totalDuration.Microseconds())/1000.0)
	fmt.Println("=======================================================")
}
