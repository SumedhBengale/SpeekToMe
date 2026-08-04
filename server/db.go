package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(connStr string) (*Database, error) {
	driver := "sqlite"
	if strings.HasPrefix(connStr, "libsql://") || strings.HasPrefix(connStr, "http://") || strings.HasPrefix(connStr, "https://") {
		driver = "libsql"
		token := os.Getenv("DATABASE_TOKEN")
		if token != "" {
			if strings.Contains(connStr, "?") {
				connStr = connStr + "&authToken=" + token
			} else {
				connStr = connStr + "?authToken=" + token
			}
		}
	}

	db, err := sql.Open(driver, connStr)
	if err != nil {
		return nil, err
	}
	
	// Configure connection pooling
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if driver == "sqlite" {
		// Enable WAL journal mode and busy timeout (Group A Optimization)
		_, _ = db.Exec("PRAGMA journal_mode=WAL;")
		_, _ = db.Exec("PRAGMA synchronous=NORMAL;")
		_, _ = db.Exec("PRAGMA busy_timeout = 5000;")
	}
	
	// Create tables and indices (Group A Optimization)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE,
			name TEXT,
			google_id TEXT UNIQUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			plan_tier TEXT DEFAULT 'free',
			plan_active INTEGER DEFAULT 0,
			plan_activated_at TIMESTAMP,
			password_hash TEXT,
			is_verified INTEGER DEFAULT 0,
			verification_token TEXT,
			razorpay_subscription_id TEXT,
			rate_limit_rpm INTEGER DEFAULT NULL,
			is_admin INTEGER DEFAULT 0,
			totp_secret TEXT DEFAULT NULL,
			monthly_usage_seconds REAL DEFAULT 0.0,
			is_locked INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			is_temp INTEGER DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			audio_duration REAL,
			whisper_time REAL,
			llm_time REAL,
			total_time REAL,
			enhance_enabled INTEGER,
			enhance_model TEXT,
			input_transcript TEXT,
			final_output TEXT,
			status TEXT,
			audio_size_bytes INTEGER DEFAULT 0,
			transcription_provider TEXT DEFAULT '',
			llm_provider TEXT DEFAULT '',
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
		);
		CREATE TABLE IF NOT EXISTS saas_config (
			"key" TEXT PRIMARY KEY,
			value TEXT
		);
		CREATE TABLE IF NOT EXISTS vocabulary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			canonical_term TEXT NOT NULL,
			normalized_term TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
			UNIQUE(user_id, canonical_term)
		);
		CREATE INDEX IF NOT EXISTS idx_usage_logs_user_status_timestamp ON usage_logs(user_id, status, timestamp);
		CREATE INDEX IF NOT EXISTS idx_sessions_token_expires ON sessions(token, expires_at);
		CREATE INDEX IF NOT EXISTS idx_vocabulary_user ON vocabulary(user_id);

		CREATE TABLE IF NOT EXISTS meetings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			duration REAL NOT NULL,
			audio_path TEXT NOT NULL,
			transcript TEXT,
			summary TEXT,
			action_items TEXT,
			segments_json TEXT,
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_meetings_user ON meetings(user_id);

		CREATE TABLE IF NOT EXISTS webhook_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			event_id TEXT,
			event_type TEXT NOT NULL,
			subscription_id TEXT,
			user_id INTEGER,
			status TEXT,
			result TEXT,
			raw_payload TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_webhook_logs_event ON webhook_logs(event_type);
		CREATE INDEX IF NOT EXISTS idx_webhook_logs_user ON webhook_logs(user_id);
		CREATE INDEX IF NOT EXISTS idx_webhook_logs_event_id ON webhook_logs(event_id);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	
	_, _ = db.Exec("ALTER TABLE webhook_logs ADD COLUMN event_id TEXT DEFAULT ''")
	
	// Dynamic migrations for existing databases to avoid breaking changes
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_tier TEXT DEFAULT 'free'")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_active INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE usage_logs ADD COLUMN audio_size_bytes INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_activated_at TIMESTAMP")
	_, _ = db.Exec("UPDATE users SET plan_activated_at = created_at WHERE plan_activated_at IS NULL")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN password_hash TEXT")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN is_verified INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN verification_token TEXT")
	_, _ = db.Exec("ALTER TABLE usage_logs ADD COLUMN transcription_provider TEXT DEFAULT ''")
	_, _ = db.Exec("ALTER TABLE usage_logs ADD COLUMN llm_provider TEXT DEFAULT ''")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN razorpay_subscription_id TEXT")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN rate_limit_rpm INTEGER DEFAULT NULL")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT NULL")
	_, _ = db.Exec("ALTER TABLE sessions ADD COLUMN is_temp INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN monthly_usage_seconds REAL DEFAULT 0.0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN is_locked INTEGER DEFAULT 0")
	_, _ = db.Exec("CREATE TABLE IF NOT EXISTS webhook_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, event_type TEXT NOT NULL, subscription_id TEXT, user_id INTEGER, status TEXT, result TEXT, raw_payload TEXT)")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_webhook_logs_event ON webhook_logs(event_type)")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_webhook_logs_user ON webhook_logs(user_id)")

	// Set default configurations
	configs := map[string]string{
		"free_rate_limit_rpm":          "3",
		"paid_rate_limit_rpm":          "15",
		"free_fup_limit_seconds_daily": "300",
		"paid_fup_limit_seconds_daily": "3600",
		"free_monthly_limit_seconds":   "3600",
		"paid_monthly_limit_seconds":   "180000",
		"whisper_default_provider":     "groq",
		"groq_enabled":                 "true",
		"deepinfra_enabled":            "true",
		"cloudflare_enabled":           "true",
		"groq_rate_limit_rpm":          "20",
		"default_user_rate_limit_rpm":  "10",
	}
	for k, v := range configs {
		_, _ = db.Exec("INSERT OR IGNORE INTO saas_config (\"key\", value) VALUES (?, ?)", k, v)
	}

	return &Database{db: db}, nil
}

// User operations
func (d *Database) GetOrCreateUser(googleID, email, name string) (int64, error) {
	var id int64
	err := d.db.QueryRow("SELECT id FROM users WHERE google_id = ?", googleID).Scan(&id)
	if err == nil {
		return id, nil
	}
	
	res, err := d.db.Exec("INSERT INTO users (google_id, email, name, is_verified) VALUES (?, ?, ?, 1)", googleID, email, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Session operations
func (d *Database) CreateSession(token string, userID int64, isTemp int) error {
	expiresAt := time.Now().AddDate(0, 0, 30).Format(time.RFC3339)
	_, err := d.db.Exec("INSERT INTO sessions (token, user_id, expires_at, is_temp) VALUES (?, ?, ?, ?)", token, userID, expiresAt, isTemp)
	return err
}

type UserProfile struct {
	ID                     int64
	Email                  string
	Name                   string
	PlanTier               string
	PlanActive             int
	PlanActivatedAt        string
	RazorpaySubscriptionID string
	RateLimitRPM           sql.NullInt64
	IsAdmin                int
	IsTempSession          int
	MonthlyUsageSeconds    float64
	IsLocked               int
}

func (d *Database) GetUserBySession(token string) (*UserProfile, error) {
	now := time.Now().Format(time.RFC3339)
	var u UserProfile
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, COALESCE(u.plan_activated_at, u.created_at), COALESCE(u.razorpay_subscription_id, ''), u.rate_limit_rpm, u.is_admin, s.is_temp, COALESCE(u.monthly_usage_seconds, 0.0), u.is_locked
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		WHERE s.token = ? AND s.expires_at > ?
	`, token, now).Scan(&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, &u.PlanActivatedAt, &u.RazorpaySubscriptionID, &u.RateLimitRPM, &u.IsAdmin, &u.IsTempSession, &u.MonthlyUsageSeconds, &u.IsLocked)
	if err != nil {
		return nil, err
	}
	if u.IsLocked == 1 {
		return nil, fmt.Errorf("ACCOUNT_LOCKED")
	}
	if u.IsTempSession == 1 {
		return nil, fmt.Errorf("2FA_REQUIRED")
	}
	return &u, nil
}

func (d *Database) GetUserAndVocabularyBySession(token string) (*UserProfile, []VocabularyTerm, error) {
	now := time.Now().Format(time.RFC3339)
	rows, err := d.db.Query(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, 
		       COALESCE(u.plan_activated_at, u.created_at), COALESCE(u.razorpay_subscription_id, ''), 
		       u.rate_limit_rpm, u.is_admin, s.is_temp, COALESCE(u.monthly_usage_seconds, 0.0), u.is_locked,
		       v.canonical_term, v.normalized_term
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		LEFT JOIN vocabulary v ON u.id = v.user_id
		WHERE s.token = ? AND s.expires_at > ?
	`, token, now)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var u UserProfile
	var terms []VocabularyTerm
	userParsed := false

	for rows.Next() {
		var canonical, normalized sql.NullString
		err := rows.Scan(
			&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, 
			&u.PlanActivatedAt, &u.RazorpaySubscriptionID, &u.RateLimitRPM, 
			&u.IsAdmin, &u.IsTempSession, &u.MonthlyUsageSeconds, &u.IsLocked,
			&canonical, &normalized,
		)
		if err != nil {
			return nil, nil, err
		}
		userParsed = true
		if canonical.Valid {
			terms = append(terms, VocabularyTerm{
				Canonical:  canonical.String,
				Normalized: normalized.String,
			})
		}
	}

	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	if !userParsed {
		return nil, nil, fmt.Errorf("invalid or expired session token")
	}

	if u.IsLocked == 1 {
		return nil, nil, fmt.Errorf("ACCOUNT_LOCKED")
	}

	if u.IsTempSession == 1 {
		return nil, nil, fmt.Errorf("2FA_REQUIRED")
	}

	return &u, terms, nil
}

func (d *Database) LogUsageAndUpdateQuota(userID int64, duration, whisperTime, llmTime, totalTime float64, enhanceEnabled bool, model, input, output, status string, audioSizeBytes int, transcriptionProvider, llmProvider string) error {
	enhanceVal := 0
	if enhanceEnabled {
		enhanceVal = 1
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO usage_logs (
			user_id, audio_duration, whisper_time, llm_time, total_time, 
			enhance_enabled, enhance_model, input_transcript, final_output, status, audio_size_bytes,
			transcription_provider, llm_provider
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, duration, whisperTime, llmTime, totalTime, enhanceVal, model, input, output, status, audioSizeBytes, transcriptionProvider, llmProvider)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE users 
		SET monthly_usage_seconds = COALESCE(monthly_usage_seconds, 0.0) + ? 
		WHERE id = ?
	`, duration, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}


func (d *Database) GetUserByTempSession(token string) (*UserProfile, string, error) {
	now := time.Now().Format(time.RFC3339)
	var u UserProfile
	var totpSecret sql.NullString
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, COALESCE(u.plan_activated_at, u.created_at), COALESCE(u.razorpay_subscription_id, ''), u.rate_limit_rpm, u.is_admin, u.totp_secret, u.is_locked
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		WHERE s.token = ? AND s.expires_at > ? AND s.is_temp = 1
	`, token, now).Scan(&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, &u.PlanActivatedAt, &u.RazorpaySubscriptionID, &u.RateLimitRPM, &u.IsAdmin, &totpSecret, &u.IsLocked)
	if err != nil {
		return nil, "", err
	}
	if u.IsLocked == 1 {
		return nil, "", fmt.Errorf("ACCOUNT_LOCKED")
	}
	decryptedSecret, err := decrypt(totpSecret.String)
	if err != nil {
		decryptedSecret = totpSecret.String
	}
	return &u, decryptedSecret, nil
}

func (d *Database) PromoteSession(token string) error {
	_, err := d.db.Exec("UPDATE sessions SET is_temp = 0 WHERE token = ?", token)
	return err
}

func (d *Database) GetUserAdminStatus(userID int64) (bool, bool, error) {
	var isAdmin int
	var totpSecret sql.NullString
	err := d.db.QueryRow("SELECT is_admin, totp_secret FROM users WHERE id = ?", userID).Scan(&isAdmin, &totpSecret)
	if err != nil {
		return false, false, err
	}
	return isAdmin == 1, totpSecret.String != "", nil
}

func (d *Database) SetUserTOTPSecret(userID int64, encryptedSecret string) error {
	_, err := d.db.Exec("UPDATE users SET totp_secret = ? WHERE id = ?", encryptedSecret, userID)
	return err
}


func (d *Database) DeleteSession(token string) error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// Usage logging
func (d *Database) LogUsage(userID int64, duration, whisperTime, llmTime, totalTime float64, enhanceEnabled bool, model, input, output, status string, audioSizeBytes int, transcriptionProvider, llmProvider string) error {
	enhanceVal := 0
	if enhanceEnabled {
		enhanceVal = 1
	}
	_, err := d.db.Exec(`
		INSERT INTO usage_logs (
			user_id, audio_duration, whisper_time, llm_time, total_time, 
			enhance_enabled, enhance_model, input_transcript, final_output, status, audio_size_bytes,
			transcription_provider, llm_provider
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, duration, whisperTime, llmTime, totalTime, enhanceVal, model, input, output, status, audioSizeBytes, transcriptionProvider, llmProvider)
	return err
}

// SaaS checks
func (d *Database) GetUserRequestCountLastMinute(userID int64) (int, error) {
	lastMinute := time.Now().UTC().Add(-1 * time.Minute).Format("2006-01-02 15:04:05")
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) 
		FROM usage_logs 
		WHERE user_id = ? AND timestamp >= ? AND status NOT IN ('failed_rate_limit', 'failed_fup_limit')
	`, userID, lastMinute).Scan(&count)
	return count, err
}

func (d *Database) GetUserDurationLast24Hours(userID int64) (float64, error) {
	last24Hours := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	var duration float64
	err := d.db.QueryRow(`
		SELECT COALESCE(SUM(audio_duration), 0.0) 
		FROM usage_logs 
		WHERE user_id = ? AND timestamp >= ? AND status = 'success'
	`, userID, last24Hours).Scan(&duration)
	return duration, err
}

// Admin helpers
func (d *Database) UpdateUserPlan(userID int64, planTier string, planActive int, subID string) error {
	_, err := d.db.Exec("UPDATE users SET plan_tier = ?, plan_active = ?, plan_activated_at = CURRENT_TIMESTAMP, razorpay_subscription_id = ?, monthly_usage_seconds = 0.0 WHERE id = ?", planTier, planActive, subID, userID)
	return err
}

func (d *Database) IsUserPlanActive(userID int64) (bool, error) {
	var planActive int
	err := d.db.QueryRow("SELECT plan_active FROM users WHERE id = ?", userID).Scan(&planActive)
	if err != nil {
		return false, err
	}
	return planActive == 1, nil
}

func (d *Database) GetUserDurationSincePlanActivation(userID int64) (float64, error) {
	var duration float64
	err := d.db.QueryRow(`
		SELECT COALESCE(SUM(audio_duration), 0.0) 
		FROM usage_logs 
		WHERE user_id = ? AND timestamp >= (SELECT COALESCE(plan_activated_at, created_at) FROM users WHERE id = ?) AND status = 'success'
	`, userID, userID).Scan(&duration)
	return duration, err
}

func (d *Database) GetUserDurationLast30Days(userID int64) (float64, error) {
	last30Days := time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	var duration float64
	err := d.db.QueryRow(`
		SELECT COALESCE(SUM(audio_duration), 0.0) 
		FROM usage_logs 
		WHERE user_id = ? AND timestamp >= ? AND status = 'success'
	`, userID, last30Days).Scan(&duration)
	return duration, err
}

func (d *Database) GetSaasConfig() (map[string]string, error) {
	rows, err := d.db.Query("SELECT \"key\", value FROM saas_config")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	config := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			config[k] = v
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return config, nil
}

func (d *Database) UpdateSaasConfig(key, value string) error {
	_, err := d.db.Exec("INSERT INTO saas_config (\"key\", value) VALUES (?, ?) ON CONFLICT(\"key\") DO UPDATE SET value = excluded.value", key, value)
	return err
}

func (d *Database) ListUsersWithStats() ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, u.created_at,
		       COALESCE(COUNT(l.id), 0) as total_requests,
		       COALESCE(SUM(l.audio_duration), 0.0) as total_duration,
		       COALESCE(SUM(l.audio_size_bytes), 0) as total_bytes
		FROM users u
		LEFT JOIN usage_logs l ON u.id = l.user_id AND l.status = 'success'
		GROUP BY u.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id int64
		var email, name, planTier, createdAt string
		var planActive int
		var totalRequests int
		var totalDuration float64
		var totalBytes int64
		err := rows.Scan(&id, &email, &name, &planTier, &planActive, &createdAt, &totalRequests, &totalDuration, &totalBytes)
		if err == nil {
			result = append(result, map[string]interface{}{
				"id":             id,
				"email":          email,
				"name":           name,
				"plan_tier":      planTier,
				"plan_active":    planActive,
				"created_at":     createdAt,
				"total_requests": totalRequests,
				"total_duration": totalDuration,
				"total_bytes":    totalBytes,
			})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateEmailUser creates a new user with an email and password hash
func (d *Database) CreateEmailUser(email, password, name, verificationToken string) (int64, error) {
	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}

	// Insert into DB (Google ID is NULL for email/password users)
	res, err := d.db.Exec("INSERT INTO users (email, password_hash, name, is_verified, verification_token) VALUES (?, ?, ?, 0, ?)", email, string(hashedPassword), name, verificationToken)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AuthenticateEmailUser checks email/password credentials and returns the User ID if valid
func (d *Database) AuthenticateEmailUser(email, password string) (int64, error) {
	var id int64
	var hashedPassword string
	var isVerified int
	var isLocked int

	err := d.db.QueryRow("SELECT id, password_hash, is_verified, is_locked FROM users WHERE email = ? AND password_hash IS NOT NULL", email).Scan(&id, &hashedPassword, &isVerified, &isLocked)
	if err != nil {
		return 0, fmt.Errorf("user not found or invalid login method")
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return 0, fmt.Errorf("invalid password")
	}

	if isLocked == 1 {
		return 0, fmt.Errorf("ACCOUNT_LOCKED")
	}

	if isVerified == 0 {
		return 0, fmt.Errorf("EMAIL_UNVERIFIED")
	}

	return id, nil
}

// VerifyUserEmail checks the verification token, marks user as verified, and returns user email
func (d *Database) VerifyUserEmail(token string) (string, error) {
	var id int64
	var email string
	err := d.db.QueryRow("SELECT id, email FROM users WHERE verification_token = ?", token).Scan(&id, &email)
	if err != nil {
		return "", fmt.Errorf("invalid or expired verification token")
	}

	_, err = d.db.Exec("UPDATE users SET is_verified = 1, verification_token = NULL WHERE id = ?", id)
	if err != nil {
		return "", fmt.Errorf("failed to update verification status")
	}

	return email, nil
}

type UsageLog struct {
	Timestamp       string  `json:"timestamp"`
	AudioDuration   float64 `json:"audio_duration"`
	TotalTime       float64 `json:"total_time"`
	Status          string  `json:"status"`
	InputTranscript string  `json:"input_transcript"`
	FinalOutput     string  `json:"final_output"`
}

func (d *Database) GetUserUsageLogs(userID int64) ([]UsageLog, error) {
	rows, err := d.db.Query(`
		SELECT timestamp, audio_duration, total_time, status, COALESCE(input_transcript, ''), COALESCE(final_output, '')
		FROM usage_logs
		WHERE user_id = ?
		ORDER BY timestamp DESC
		LIMIT 50
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]UsageLog, 0)
	for rows.Next() {
		var l UsageLog
		err := rows.Scan(&l.Timestamp, &l.AudioDuration, &l.TotalTime, &l.Status, &l.InputTranscript, &l.FinalOutput)
		if err == nil {
			logs = append(logs, l)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

type VocabularyTerm struct {
	Canonical  string `json:"canonical_term"`
	Normalized string `json:"normalized_term"`
}

func (d *Database) AddVocabularyTerm(userID int64, canonical, normalized string) (int64, error) {
	res, err := d.db.Exec(`
		INSERT INTO vocabulary (user_id, canonical_term, normalized_term)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, canonical_term) DO UPDATE SET
			normalized_term = excluded.normalized_term,
			updated_at = CURRENT_TIMESTAMP
	`, userID, canonical, normalized)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *Database) GetVocabularyTerms(userID int64) ([]VocabularyTerm, error) {
	rows, err := d.db.Query(`
		SELECT canonical_term, normalized_term
		FROM vocabulary
		WHERE user_id = ?
		ORDER BY canonical_term ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	terms := make([]VocabularyTerm, 0)
	for rows.Next() {
		var vt VocabularyTerm
		if err := rows.Scan(&vt.Canonical, &vt.Normalized); err == nil {
			terms = append(terms, vt)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return terms, nil
}

func (d *Database) DeleteVocabularyTerm(userID int64, canonical string) error {
	_, err := d.db.Exec(`
		DELETE FROM vocabulary
		WHERE user_id = ? AND canonical_term = ?
	`, userID, canonical)
	return err
}

type Meeting struct {
	ID           int64   `json:"id"`
	UserID       int64   `json:"user_id"`
	Title        string  `json:"title"`
	Timestamp    string  `json:"timestamp"`
	Duration     float64 `json:"duration"`
	AudioPath    string  `json:"audio_path"`
	Transcript   string  `json:"transcript"`
	Summary      string  `json:"summary"`
	ActionItems  string  `json:"action_items"`
	SegmentsJSON string  `json:"segments_json"`
}

func (d *Database) CreateMeeting(userID int64, title string, duration float64, audioPath string) (int64, error) {
	res, err := d.db.Exec(`
		INSERT INTO meetings (user_id, title, duration, audio_path, transcript, summary, action_items, segments_json)
		VALUES (?, ?, ?, ?, '', '', '', '[]')
	`, userID, title, duration, audioPath)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *Database) UpdateMeetingResult(meetingID int64, transcript string, summary string, actionItems string, segmentsJSON string) error {
	_, err := d.db.Exec(`
		UPDATE meetings
		SET transcript = ?, summary = ?, action_items = ?, segments_json = ?
		WHERE id = ?
	`, transcript, summary, actionItems, segmentsJSON, meetingID)
	return err
}

func (d *Database) GetUserMeetings(userID int64) ([]Meeting, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, title, timestamp, duration, audio_path, transcript, summary, action_items, segments_json
		FROM meetings
		WHERE user_id = ?
		ORDER BY timestamp DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meetings := make([]Meeting, 0)
	for rows.Next() {
		var m Meeting
		err := rows.Scan(&m.ID, &m.UserID, &m.Title, &m.Timestamp, &m.Duration, &m.AudioPath, &m.Transcript, &m.Summary, &m.ActionItems, &m.SegmentsJSON)
		if err == nil {
			meetings = append(meetings, m)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return meetings, nil
}

func (d *Database) GetMeeting(meetingID int64) (*Meeting, error) {
	var m Meeting
	err := d.db.QueryRow(`
		SELECT id, user_id, title, timestamp, duration, audio_path, transcript, summary, action_items, segments_json
		FROM meetings
		WHERE id = ?
	`, meetingID).Scan(&m.ID, &m.UserID, &m.Title, &m.Timestamp, &m.Duration, &m.AudioPath, &m.Transcript, &m.Summary, &m.ActionItems, &m.SegmentsJSON)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (d *Database) DeleteMeeting(meetingID int64) error {
	_, err := d.db.Exec("DELETE FROM meetings WHERE id = ?", meetingID)
	return err
}

type AdminUserRow struct {
	ID           int64   `json:"id"`
	Email        string  `json:"email"`
	Name         string  `json:"name"`
	PlanTier     string  `json:"plan_tier"`
	PlanActive   int     `json:"plan_active"`
	RateLimitRPM *int    `json:"rate_limit_rpm"`
	TotalUsage   float64 `json:"total_usage_seconds"`
}

func (d *Database) GetAdminUsersList() ([]AdminUserRow, error) {
	rows, err := d.db.Query(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, u.rate_limit_rpm,
		       COALESCE((SELECT SUM(audio_duration) FROM usage_logs WHERE user_id = u.id AND status = 'success'), 0)
		FROM users u
		ORDER BY u.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AdminUserRow, 0)
	for rows.Next() {
		var r AdminUserRow
		var rpm sql.NullInt64
		err := rows.Scan(&r.ID, &r.Email, &r.Name, &r.PlanTier, &r.PlanActive, &rpm, &r.TotalUsage)
		if err != nil {
			return nil, err
		}
		if rpm.Valid {
			val := int(rpm.Int64)
			r.RateLimitRPM = &val
		}
		list = append(list, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (d *Database) AdminUpdateUserPlan(userID int64, planTier string, planActive int) error {
	_, err := d.db.Exec(`
		UPDATE users 
		SET plan_tier = ?, plan_active = ?, plan_activated_at = CURRENT_TIMESTAMP, monthly_usage_seconds = 0.0 
		WHERE id = ?
	`, planTier, planActive, userID)
	return err
}

func (d *Database) AdminUpdateUserRateLimit(userID int64, rateLimit *int) error {
	var val interface{}
	if rateLimit != nil {
		val = *rateLimit
	} else {
		val = nil
	}
	_, err := d.db.Exec("UPDATE users SET rate_limit_rpm = ? WHERE id = ?", val, userID)
	return err
}

func (d *Database) GetUserBySubscriptionID(subID string) (int64, error) {
	var userID int64
	err := d.db.QueryRow("SELECT id FROM users WHERE razorpay_subscription_id = ?", subID).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

type WebhookLogEntry struct {
	ID             int64   `json:"id"`
	ReceivedAt     string  `json:"received_at"`
	EventType      string  `json:"event_type"`
	SubscriptionID string  `json:"subscription_id"`
	UserID         *int64  `json:"user_id"`
	EventID        string  `json:"event_id"`
	Status         string  `json:"status"`
	Result         string  `json:"result"`
	RawPayload     string  `json:"raw_payload"`
}

func (d *Database) IsWebhookEventProcessed(eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM webhook_logs WHERE event_id = ? AND result = 'success'", eventID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *Database) InsertWebhookLog(eventID, eventType, subscriptionID string, userID *int64, status, result, rawPayload string) error {
	_, err := d.db.Exec(`
		INSERT INTO webhook_logs (event_id, event_type, subscription_id, user_id, status, result, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, eventID, eventType, subscriptionID, userID, status, result, rawPayload)
	return err
}

func (d *Database) GetWebhookLogs(limit int) ([]WebhookLogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, received_at, event_type, subscription_id, user_id, status, result, raw_payload
		FROM webhook_logs
		ORDER BY received_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []WebhookLogEntry
	for rows.Next() {
		var entry WebhookLogEntry
		if err := rows.Scan(&entry.ID, &entry.ReceivedAt, &entry.EventType, &entry.SubscriptionID, &entry.UserID, &entry.Status, &entry.Result, &entry.RawPayload); err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (d *Database) ResetAllFreeUserQuotas() error {
	_, err := d.db.Exec(`
		UPDATE users 
		SET monthly_usage_seconds = 0.0 
		WHERE plan_tier = 'free' OR plan_active = 0
	`)
	return err
}

func (d *Database) IsUserLocked(userID int64) (bool, error) {
	var isLocked int
	err := d.db.QueryRow("SELECT is_locked FROM users WHERE id = ?", userID).Scan(&isLocked)
	if err != nil {
		return false, err
	}
	return isLocked == 1, nil
}

func getEncryptionKey() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		keyStr = "speektome_default_fallback_encryption_key_32bytes"
	}
	hash := sha256.Sum256([]byte(keyStr))
	return hash[:]
}

func encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aesgcm.Seal(nil, nonce, []byte(plaintext), nil)
	combined := append(nonce, ciphertext...)
	return hex.EncodeToString(combined), nil
}

// decrypt decrypts an AES-256-GCM encrypted hex string produced by encrypt().
// If the value is not a valid encrypted blob (e.g. a legacy plaintext TOTP secret),
// it returns the raw value unchanged so that old plaintext secrets still work.
func decrypt(ciphertextHex string) (string, error) {
	if ciphertextHex == "" {
		return "", nil
	}
	combined, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		// Not a hex string at all — treat as raw plaintext (e.g. legacy TOTP secret)
		return ciphertextHex, nil
	}
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := aesgcm.NonceSize()
	if len(combined) < nonceSize {
		// Too short to be a valid AES-GCM ciphertext — treat as raw plaintext
		return ciphertextHex, nil
	}
	nonce := combined[:nonceSize]
	ciphertext := combined[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Decryption failed — this hex was not encrypted with our key.
		// Fall back to the raw value so legacy plaintext secrets remain functional.
		return ciphertextHex, nil
	}
	return string(plaintext), nil
}

// EncryptTOTPSecret encrypts a plaintext TOTP secret using the current ENCRYPTION_KEY.
// Use this to migrate a manually-pasted plaintext secret to encrypted form.
func EncryptTOTPSecret(plaintext string) (string, error) {
	return encrypt(plaintext)
}

