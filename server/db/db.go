package db

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

	"speektome-server/utils"
)

type Database struct {
	db *sql.DB
}

func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *Database) GetDB() *sql.DB {
	return d.db
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

	dbConn, err := sql.Open(driver, connStr)
	if err != nil {
		return nil, err
	}
	
	// Configure connection pooling
	dbConn.SetMaxOpenConns(10)
	dbConn.SetMaxIdleConns(5)
	dbConn.SetConnMaxLifetime(time.Hour)

	if driver == "sqlite" {
		// Enable WAL journal mode and busy timeout (Group A Optimization)
		_, _ = dbConn.Exec("PRAGMA journal_mode=WAL;")
		_, _ = dbConn.Exec("PRAGMA synchronous=NORMAL;")
		_, _ = dbConn.Exec("PRAGMA busy_timeout = 5000;")
	}
	
	// Create tables and indices (Group A Optimization)
	_, err = dbConn.Exec(`
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
			razorpay_subscription_status TEXT DEFAULT '',
			razorpay_current_start INTEGER DEFAULT 0,
			razorpay_current_end INTEGER DEFAULT 0,
			rate_limit_rpm INTEGER DEFAULT NULL,
			is_admin INTEGER DEFAULT 0,
			totp_secret TEXT DEFAULT NULL,
			monthly_usage_seconds REAL DEFAULT 0.0,
			is_locked INTEGER DEFAULT 0,
			languages TEXT DEFAULT '["en"]'
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

		CREATE TABLE IF NOT EXISTS newsletter_subscribers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

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
			event_type TEXT NOT NULL,
			subscription_id TEXT,
			payment_id TEXT,
			invoice_id TEXT,
			user_id INTEGER,
			status TEXT,
			result TEXT,
			raw_payload TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_webhook_logs_event ON webhook_logs(event_type);
		CREATE INDEX IF NOT EXISTS idx_webhook_logs_user ON webhook_logs(user_id);

		CREATE TABLE IF NOT EXISTS subscription_payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			subscription_id TEXT NOT NULL,
			payment_id TEXT,
			invoice_id TEXT,
			dedupe_key TEXT NOT NULL UNIQUE,
			event_id TEXT,
			event_type TEXT NOT NULL,
			amount INTEGER DEFAULT 0,
			currency TEXT DEFAULT '',
			status TEXT DEFAULT '',
			period_start INTEGER,
			period_end INTEGER,
			received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			raw_payload TEXT,
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_subscription_payments_user ON subscription_payments(user_id);
		CREATE INDEX IF NOT EXISTS idx_subscription_payments_subscription ON subscription_payments(subscription_id);
	`)
	if err != nil {
		dbConn.Close()
		return nil, err
	}
	
	// Dynamic migrations for existing databases
	_, _ = dbConn.Exec("ALTER TABLE webhook_logs ADD COLUMN event_id TEXT DEFAULT ''")
	_, _ = dbConn.Exec("ALTER TABLE webhook_logs ADD COLUMN payment_id TEXT DEFAULT ''")
	_, _ = dbConn.Exec("ALTER TABLE webhook_logs ADD COLUMN invoice_id TEXT DEFAULT ''")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_webhook_logs_event_id ON webhook_logs(event_id)")
	
	// Dynamic migrations for existing databases to avoid breaking changes
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN plan_tier TEXT DEFAULT 'free'")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN plan_active INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE usage_logs ADD COLUMN audio_size_bytes INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN plan_activated_at TIMESTAMP")
	_, _ = dbConn.Exec("UPDATE users SET plan_activated_at = created_at WHERE plan_activated_at IS NULL")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN password_hash TEXT")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN is_verified INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN verification_token TEXT")
	_, _ = dbConn.Exec("ALTER TABLE usage_logs ADD COLUMN transcription_provider TEXT DEFAULT ''")
	_, _ = dbConn.Exec("ALTER TABLE usage_logs ADD COLUMN llm_provider TEXT DEFAULT ''")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN razorpay_subscription_id TEXT")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN razorpay_subscription_status TEXT DEFAULT ''")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN razorpay_current_start INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN razorpay_current_end INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN rate_limit_rpm INTEGER DEFAULT NULL")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT NULL")
	_, _ = dbConn.Exec("ALTER TABLE sessions ADD COLUMN is_temp INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN monthly_usage_seconds REAL DEFAULT 0.0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN is_locked INTEGER DEFAULT 0")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN languages TEXT DEFAULT '[\"en\"]'")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN password_reset_token TEXT")
	_, _ = dbConn.Exec("ALTER TABLE users ADD COLUMN password_reset_expires TIMESTAMP")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_users_pwd_reset ON users(password_reset_token)")
	_, _ = dbConn.Exec("CREATE TABLE IF NOT EXISTS webhook_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, event_type TEXT NOT NULL, subscription_id TEXT, payment_id TEXT, invoice_id TEXT, user_id INTEGER, status TEXT, result TEXT, raw_payload TEXT)")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_webhook_logs_event ON webhook_logs(event_type)")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_webhook_logs_user ON webhook_logs(user_id)")
	_, _ = dbConn.Exec("CREATE TABLE IF NOT EXISTS subscription_payments (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, subscription_id TEXT NOT NULL, payment_id TEXT, invoice_id TEXT, dedupe_key TEXT NOT NULL UNIQUE, event_id TEXT, event_type TEXT NOT NULL, amount INTEGER DEFAULT 0, currency TEXT DEFAULT '', status TEXT DEFAULT '', period_start INTEGER, period_end INTEGER, received_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, raw_payload TEXT)")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_subscription_payments_user ON subscription_payments(user_id)")
	_, _ = dbConn.Exec("CREATE INDEX IF NOT EXISTS idx_subscription_payments_subscription ON subscription_payments(subscription_id)")

	defaultModelsJSON := `{
  "default_batch_provider": "groq_whisper",
  "default_streaming_provider": "deepinfra_nemotron",
  "default_llm_provider": "deepseek_v4",
  "batch_providers": {
    "groq_whisper": { "enabled": true, "model": "whisper-large-v3-turbo", "base_url": "https://api.groq.com/openai/v1/audio/transcriptions", "timeout_ms": 10000 },
    "deepinfra_whisper": { "enabled": true, "model": "openai/whisper-large-v3-turbo", "base_url": "https://api.deepinfra.com/v1/inference/openai/whisper-large-v3-turbo", "timeout_ms": 10000 },
    "cloudflare_whisper": { "enabled": true, "model": "@cf/openai/whisper-large-v3-turbo", "timeout_ms": 15000 },
    "custom_batch": { "enabled": false, "model": "", "base_url": "", "api_key": "", "timeout_ms": 10000 }
  },
  "streaming_providers": {
    "deepinfra_nemotron": { "enabled": true, "model": "nvidia/nemotron-3.5-asr-streaming-0.6b", "base_url": "https://api.deepinfra.com/v1/inference/nvidia/nemotron-3.5-asr-streaming-0.6b", "timeout_ms": 15000 },
    "custom_streaming": { "enabled": false, "model": "", "base_url": "", "api_key": "", "timeout_ms": 15000 }
  },
  "llm_providers": {
    "deepseek_v4": { "enabled": true, "model": "deepseek-v4-flash", "base_url": "https://api.deepseek.com/chat/completions", "timeout_ms": 30000 },
    "deepinfra_deepseek": { "enabled": true, "model": "deepseek-ai/DeepSeek-V4-Flash", "base_url": "https://api.deepinfra.com/v1/openai/chat/completions", "timeout_ms": 30000 },
    "custom_llm": { "enabled": false, "model": "", "base_url": "", "api_key": "", "timeout_ms": 30000 }
  }
}`

	// Set default configurations
	configs := map[string]string{
		"models_config_json":           defaultModelsJSON,
		"ratelimit_free_rpm":           "3",
		"ratelimit_paid_rpm":           "15",
		"quota_free_daily_fup_seconds": "3600",
		"quota_paid_daily_fup_seconds": "86400",
		"quota_free_monthly_seconds":   "3600",
		"quota_paid_monthly_seconds":   "2700000",
		"ratelimit_groq_rpm":           "20",
		"ratelimit_default_user_rpm":   "10",
		"notification":                 "",
		"whisper_base_prompt":          "Transcribe the spoken audio accurately with correct grammar, spelling, and punctuation.",
	}
	for k, v := range configs {
		_, _ = dbConn.Exec("INSERT OR IGNORE INTO saas_config (\"key\", value) VALUES (?, ?)", k, v)
	}

	return &Database{db: dbConn}, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// User operations
func (d *Database) GetOrCreateUser(googleID, email, name string) (int64, error) {
	var id int64
	// 1. Check if user with this google_id already exists
	err := d.db.QueryRow("SELECT id FROM users WHERE google_id = ?", googleID).Scan(&id)
	if err == nil {
		return id, nil
	}

	// 2. Check if a user with this verified email already exists (e.g. registered via email/password)
	if email != "" {
		var existingID int64
		err = d.db.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&existingID)
		if err == nil {
			// Link google_id and mark as verified
			_, _ = d.db.Exec("UPDATE users SET google_id = ?, is_verified = 1 WHERE id = ?", googleID, existingID)
			return existingID, nil
		}
	}
	
	// 3. Insert new user
	res, err := d.db.Exec("INSERT INTO users (google_id, email, name, is_verified, languages) VALUES (?, ?, ?, 1, '[\"en\"]')", googleID, email, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *Database) UserExists(userID int64) bool {
	var id int64
	err := d.db.QueryRow("SELECT id FROM users WHERE id = ?", userID).Scan(&id)
	return err == nil
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
	Languages              []string
}

func (d *Database) GetUserBySession(token string) (*UserProfile, error) {
	now := time.Now().Format(time.RFC3339)
	var u UserProfile
	var langsStr sql.NullString
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, COALESCE(u.plan_activated_at, u.created_at), COALESCE(u.razorpay_subscription_id, ''), u.rate_limit_rpm, u.is_admin, s.is_temp, COALESCE(u.monthly_usage_seconds, 0.0), u.is_locked, COALESCE(u.languages, '["en"]')
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		WHERE s.token = ? AND s.expires_at > ?
	`, token, now).Scan(&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, &u.PlanActivatedAt, &u.RazorpaySubscriptionID, &u.RateLimitRPM, &u.IsAdmin, &u.IsTempSession, &u.MonthlyUsageSeconds, &u.IsLocked, &langsStr)
	if err != nil {
		return nil, err
	}
	u.Languages = utils.ParseLanguagesJSON(langsStr.String)
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
		       COALESCE(u.languages, '["en"]'), v.canonical_term, v.normalized_term
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
	var langsStr sql.NullString
	userParsed := false

	for rows.Next() {
		var canonical, normalized sql.NullString
		err := rows.Scan(
			&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, 
			&u.PlanActivatedAt, &u.RazorpaySubscriptionID, &u.RateLimitRPM, 
			&u.IsAdmin, &u.IsTempSession, &u.MonthlyUsageSeconds, &u.IsLocked,
			&langsStr, &canonical, &normalized,
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
	if userParsed {
		u.Languages = utils.ParseLanguagesJSON(langsStr.String)
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

type UsageLogRecord struct {
	ID              int64
	UserID          int64
	Timestamp       string
	InputTranscript string
	FinalOutput     string
	Status          string
}

func (d *Database) LogUsageAndUpdateQuota(userID int64, duration, whisperTime, llmTime, totalTime float64, enhanceEnabled bool, model, input, output, status string, audioSizeBytes int, transcriptionProvider, llmProvider string) (int64, error) {
	enhanceVal := 0
	if enhanceEnabled {
		enhanceVal = 1
	}

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO usage_logs (
			user_id, audio_duration, whisper_time, llm_time, total_time, 
			enhance_enabled, enhance_model, input_transcript, final_output, status, audio_size_bytes,
			transcription_provider, llm_provider
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, duration, whisperTime, llmTime, totalTime, enhanceVal, model, input, output, status, audioSizeBytes, transcriptionProvider, llmProvider)
	if err != nil {
		return 0, err
	}

	var logID int64
	if res != nil {
		logID, _ = res.LastInsertId()
	}

	_, err = tx.Exec(`
		UPDATE users 
		SET monthly_usage_seconds = COALESCE(monthly_usage_seconds, 0.0) + ? 
		WHERE id = ?
	`, duration, userID)
	if err != nil {
		return logID, err
	}

	return logID, tx.Commit()
}

func (d *Database) UpdateUsageFinalOutput(logID int64, userID int64, finalOutput string) error {
	_, err := d.db.Exec(`
		UPDATE usage_logs 
		SET final_output = ? 
		WHERE id = ? AND user_id = ?
	`, finalOutput, logID, userID)
	return err
}

func (d *Database) GetUsageLogByID(logID int64, userID int64) (*UsageLogRecord, error) {
	var rec UsageLogRecord
	err := d.db.QueryRow(`
		SELECT id, user_id, timestamp, input_transcript, final_output, status
		FROM usage_logs
		WHERE id = ? AND user_id = ?
	`, logID, userID).Scan(&rec.ID, &rec.UserID, &rec.Timestamp, &rec.InputTranscript, &rec.FinalOutput, &rec.Status)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (d *Database) UpdateUserLanguages(userID int64, languages []string) error {
	jsonStr := utils.FormatLanguagesJSON(languages)
	_, err := d.db.Exec("UPDATE users SET languages = ? WHERE id = ?", jsonStr, userID)
	return err
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
	_, err := d.db.Exec("UPDATE users SET plan_tier = ?, plan_active = ?, plan_activated_at = CURRENT_TIMESTAMP, razorpay_subscription_id = ? WHERE id = ?", planTier, planActive, subID, userID)
	return err
}

func (d *Database) SetPendingSubscription(userID int64, subID string) error {
	_, err := d.db.Exec("UPDATE users SET razorpay_subscription_id = ?, razorpay_subscription_status = 'created', razorpay_current_start = 0, razorpay_current_end = 0 WHERE id = ?", subID, userID)
	return err
}

func (d *Database) UpdateSubscriptionState(userID int64, subID, status string, periodStart, periodEnd int64, planActive *int) error {
	if planActive == nil {
		_, err := d.db.Exec(`
			UPDATE users
			SET razorpay_subscription_id = ?, razorpay_subscription_status = ?,
				razorpay_current_start = ?, razorpay_current_end = ?
			WHERE id = ?
		`, subID, status, periodStart, periodEnd, userID)
		return err
	}
	planTier := "pro"
	if *planActive == 0 {
		planTier = "free"
	}
	_, err := d.db.Exec(`
		UPDATE users
		SET razorpay_subscription_id = ?, razorpay_subscription_status = ?,
			razorpay_current_start = ?, razorpay_current_end = ?, plan_active = ?,
			plan_tier = ?
		WHERE id = ?
	`, subID, status, periodStart, periodEnd, *planActive, planTier, userID)
	return err
}

func (d *Database) ResetMonthlyUsage(userID int64) error {
	_, err := d.db.Exec("UPDATE users SET monthly_usage_seconds = 0.0 WHERE id = ?", userID)
	return err
}

// ApplySuccessfulSubscriptionPayment records a successful billing event and activates
// the plan/reset usage exactly once for the payment or invoice represented by dedupeKey.
func (d *Database) ApplySuccessfulSubscriptionPayment(userID int64, subID, paymentID, invoiceID, dedupeKey, eventID, eventType string, amount int64, currency, status string, periodStart, periodEnd int64, rawPayload string) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT OR IGNORE INTO subscription_payments
			(user_id, subscription_id, payment_id, invoice_id, dedupe_key, event_id, event_type, amount, currency, status, period_start, period_end, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, subID, paymentID, invoiceID, dedupeKey, eventID, eventType, amount, currency, status, periodStart, periodEnd, rawPayload)
	if err != nil {
		return false, err
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		return false, nil
	}

	_, err = tx.Exec(`
		UPDATE users
		SET plan_tier = 'pro', plan_active = 1, plan_activated_at = CURRENT_TIMESTAMP,
			razorpay_subscription_id = ?, razorpay_subscription_status = 'active',
			razorpay_current_start = CASE WHEN ? > 0 THEN ? ELSE razorpay_current_start END,
			razorpay_current_end = CASE WHEN ? > 0 THEN ? ELSE razorpay_current_end END,
			monthly_usage_seconds = 0.0
		WHERE id = ?
	`, subID, periodStart, periodStart, periodEnd, periodEnd, userID)
	if err != nil {
		return false, err
	}

	return true, tx.Commit()
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
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}

	tokenHash := hashToken(verificationToken)
	res, err := d.db.Exec("INSERT INTO users (email, password_hash, name, is_verified, verification_token) VALUES (?, ?, ?, 0, ?)", email, string(hashedPassword), name, tokenHash)
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
	if token == "" {
		return "", fmt.Errorf("invalid or expired verification token")
	}
	var id int64
	var email string
	tokenHash := hashToken(token)
	// Support checking both hashed token and legacy raw token
	err := d.db.QueryRow("SELECT id, email FROM users WHERE verification_token = ? OR verification_token = ?", tokenHash, token).Scan(&id, &email)
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
	ID                    int64   `json:"id"`
	Timestamp             string  `json:"timestamp"`
	AudioDuration         float64 `json:"audio_duration"`
	WhisperTime           float64 `json:"whisper_time"`
	LLMTime               float64 `json:"llm_time"`
	TotalTime             float64 `json:"total_time"`
	EnhanceEnabled        bool    `json:"enhance_enabled"`
	EnhanceModel          string  `json:"enhance_model"`
	InputTranscript       string  `json:"input_transcript"`
	FinalOutput           string  `json:"final_output"`
	Status                string  `json:"status"`
	TranscriptionProvider string  `json:"transcription_provider"`
	LLMProvider           string  `json:"llm_provider"`
}

func (d *Database) GetUserUsageLogs(userID int64) ([]UsageLog, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, audio_duration, COALESCE(whisper_time, 0.0), COALESCE(llm_time, 0.0), total_time, 
		       COALESCE(enhance_enabled, 0), COALESCE(enhance_model, ''), COALESCE(input_transcript, ''), 
		       COALESCE(final_output, ''), status, COALESCE(transcription_provider, ''), COALESCE(llm_provider, '')
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
		var enhanceVal int
		err := rows.Scan(
			&l.ID, &l.Timestamp, &l.AudioDuration, &l.WhisperTime, &l.LLMTime, &l.TotalTime,
			&enhanceVal, &l.EnhanceModel, &l.InputTranscript, &l.FinalOutput,
			&l.Status, &l.TranscriptionProvider, &l.LLMProvider,
		)
		if err == nil {
			l.EnhanceEnabled = enhanceVal == 1
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
	PaymentID      string  `json:"payment_id"`
	InvoiceID      string  `json:"invoice_id"`
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
	return d.InsertWebhookLogDetailed(eventID, eventType, subscriptionID, "", "", userID, status, result, rawPayload)
}

func (d *Database) InsertWebhookLogDetailed(eventID, eventType, subscriptionID, paymentID, invoiceID string, userID *int64, status, result, rawPayload string) error {
	_, err := d.db.Exec(`
		INSERT INTO webhook_logs (event_id, event_type, subscription_id, payment_id, invoice_id, user_id, status, result, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, eventID, eventType, subscriptionID, paymentID, invoiceID, userID, status, result, rawPayload)
	return err
}

func (d *Database) GetWebhookLogs(limit int) ([]WebhookLogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, received_at, event_type, subscription_id, payment_id, invoice_id, user_id, status, result, raw_payload
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
		if err := rows.Scan(&entry.ID, &entry.ReceivedAt, &entry.EventType, &entry.SubscriptionID, &entry.PaymentID, &entry.InvoiceID, &entry.UserID, &entry.Status, &entry.Result, &entry.RawPayload); err != nil {
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

func decrypt(ciphertextHex string) (string, error) {
	if ciphertextHex == "" {
		return "", nil
	}
	combined, err := hex.DecodeString(ciphertextHex)
	if err != nil {
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
		return ciphertextHex, nil
	}
	nonce := combined[:nonceSize]
	ciphertext := combined[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ciphertextHex, nil
	}
	return string(plaintext), nil
}

func EncryptTOTPSecret(plaintext string) (string, error) {
	return encrypt(plaintext)
}

func (d *Database) AddNewsletterSubscriber(email string) error {
	_, err := d.db.Exec("INSERT OR IGNORE INTO newsletter_subscribers (email) VALUES (?)", email)
	return err
}

type UserProfileSummary struct {
	ID                     int64    `json:"id"`
	Email                  string   `json:"email"`
	Name                   string   `json:"name"`
	PlanTier               string   `json:"plan_tier"`
	PlanActive             bool     `json:"plan_active"`
	PlanActivatedAt        string   `json:"plan_activated_at"`
	CreatedAt              string   `json:"created_at"`
	Languages              []string `json:"languages"`
	HasPassword            bool     `json:"has_password"`
	HasGoogleAuth          bool     `json:"has_google_auth"`
	TotalTranscriptions    int      `json:"total_transcriptions"`
	TotalAudioDuration     float64  `json:"total_audio_duration_seconds"`
	TotalWordsTranscribed  int      `json:"total_words_transcribed"`
	TotalMeetings          int      `json:"total_meetings"`
	TotalMeetingDuration   float64  `json:"total_meeting_duration_seconds"`
	TotalVocabularyTerms   int      `json:"total_vocabulary_terms"`
	TotalEnhancements      int      `json:"total_enhancements"`
	MonthlyUsageSeconds    float64  `json:"monthly_usage_seconds"`
	LastActiveAt           string   `json:"last_active_at"`
}

func (d *Database) GetUserProfileSummary(userID int64) (*UserProfileSummary, error) {
	var summary UserProfileSummary
	var langsStr sql.NullString
	var pwdHash sql.NullString
	var googleID sql.NullString
	var planActivatedAt sql.NullString
	var createdAt sql.NullString
	var planActive int

	err := d.db.QueryRow(`
		SELECT id, email, name, plan_tier, plan_active, plan_activated_at, created_at,
		       COALESCE(languages, '["en"]'), password_hash, google_id, COALESCE(monthly_usage_seconds, 0.0)
		FROM users
		WHERE id = ?
	`, userID).Scan(
		&summary.ID, &summary.Email, &summary.Name, &summary.PlanTier, &planActive,
		&planActivatedAt, &createdAt, &langsStr, &pwdHash, &googleID, &summary.MonthlyUsageSeconds,
	)
	if err != nil {
		return nil, err
	}

	summary.PlanActive = planActive == 1
	summary.PlanActivatedAt = planActivatedAt.String
	summary.CreatedAt = createdAt.String
	summary.Languages = utils.ParseLanguagesJSON(langsStr.String)
	summary.HasPassword = pwdHash.Valid && strings.TrimSpace(pwdHash.String) != ""
	summary.HasGoogleAuth = googleID.Valid && strings.TrimSpace(googleID.String) != ""

	// Get usage logs stats
	var totalLogs int
	var totalDuration float64
	var totalEnhance int
	var lastLogTimestamp sql.NullString

	_ = d.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(audio_duration), 0.0), COALESCE(SUM(enhance_enabled), 0), MAX(timestamp)
		FROM usage_logs
		WHERE user_id = ? AND status = 'success'
	`, userID).Scan(&totalLogs, &totalDuration, &totalEnhance, &lastLogTimestamp)

	summary.TotalTranscriptions = totalLogs
	summary.TotalAudioDuration = totalDuration
	summary.TotalEnhancements = totalEnhance

	// Calculate words transcribed from usage logs (estimate by whitespace splitting on non-empty outputs)
	rows, err := d.db.Query(`
		SELECT COALESCE(final_output, input_transcript, '')
		FROM usage_logs
		WHERE user_id = ? AND status = 'success'
	`, userID)
	if err == nil {
		totalWords := 0
		for rows.Next() {
			var txt string
			if err := rows.Scan(&txt); err == nil && txt != "" {
				totalWords += len(strings.Fields(txt))
			}
		}
		rows.Close()
		summary.TotalWordsTranscribed = totalWords
	}

	// Get meetings stats
	var totalMeetings int
	var totalMeetingDuration float64
	var lastMeetingTimestamp sql.NullString

	_ = d.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(duration), 0.0), MAX(timestamp)
		FROM meetings
		WHERE user_id = ?
	`, userID).Scan(&totalMeetings, &totalMeetingDuration, &lastMeetingTimestamp)

	summary.TotalMeetings = totalMeetings
	summary.TotalMeetingDuration = totalMeetingDuration

	// Add meeting transcript word counts
	mRows, err := d.db.Query(`
		SELECT COALESCE(transcript, '')
		FROM meetings
		WHERE user_id = ?
	`, userID)
	if err == nil {
		meetingWords := 0
		for mRows.Next() {
			var mTxt string
			if err := mRows.Scan(&mTxt); err == nil && mTxt != "" {
				meetingWords += len(strings.Fields(mTxt))
			}
		}
		mRows.Close()
		summary.TotalWordsTranscribed += meetingWords
	}

	// Get vocabulary stats
	var totalVocab int
	_ = d.db.QueryRow(`SELECT COUNT(*) FROM vocabulary WHERE user_id = ?`, userID).Scan(&totalVocab)
	summary.TotalVocabularyTerms = totalVocab

	// Determine most recent activity
	summary.LastActiveAt = summary.CreatedAt
	if lastLogTimestamp.Valid && lastLogTimestamp.String != "" {
		summary.LastActiveAt = lastLogTimestamp.String
	}
	if lastMeetingTimestamp.Valid && lastMeetingTimestamp.String != "" {
		if lastMeetingTimestamp.String > summary.LastActiveAt {
			summary.LastActiveAt = lastMeetingTimestamp.String
		}
	}

	return &summary, nil
}

func (d *Database) SetPasswordResetToken(email, token string, expiresAt time.Time) (string, error) {
	var name string
	var id int64
	err := d.db.QueryRow("SELECT id, name FROM users WHERE email = ?", email).Scan(&id, &name)
	if err != nil {
		return "", fmt.Errorf("user not found")
	}

	tokenHash := hashToken(token)
	expiresStr := expiresAt.UTC().Format("2006-01-02 15:04:05")
	_, err = d.db.Exec(`
		UPDATE users
		SET password_reset_token = ?, password_reset_expires = ?
		WHERE id = ?
	`, tokenHash, expiresStr, id)
	if err != nil {
		return "", err
	}
	return name, nil
}

func (d *Database) ValidatePasswordResetToken(token string) (int64, string, error) {
	if token == "" {
		return 0, "", fmt.Errorf("invalid reset token")
	}
	var id int64
	var email string
	var expiresStr string
	tokenHash := hashToken(token)
	// Support checking both hashed token and raw token for backward compatibility
	err := d.db.QueryRow(`
		SELECT id, email, password_reset_expires
		FROM users
		WHERE password_reset_token = ? OR password_reset_token = ?
	`, tokenHash, token).Scan(&id, &email, &expiresStr)
	if err != nil {
		return 0, "", fmt.Errorf("invalid or expired reset token")
	}

	t, err := time.Parse("2006-01-02 15:04:05", expiresStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, expiresStr)
	}
	if err == nil && time.Now().UTC().After(t) {
		return 0, "", fmt.Errorf("password reset token has expired")
	}

	return id, email, nil
}

func (d *Database) ResetPasswordWithToken(token, newPasswordHash string) error {
	id, _, err := d.ValidatePasswordResetToken(token)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`
		UPDATE users
		SET password_hash = ?, password_reset_token = NULL, password_reset_expires = NULL, is_verified = 1
		WHERE id = ?
	`, newPasswordHash, id)
	if err != nil {
		return err
	}

	// Security: Invalidate all existing active sessions upon password reset
	_, _ = d.db.Exec("DELETE FROM sessions WHERE user_id = ?", id)

	return nil
}

func (d *Database) DeleteUserAccount(userID int64) error {
	// Clean up any local meeting audio files
	rows, err := d.db.Query("SELECT audio_path FROM meetings WHERE user_id = ?", userID)
	if err == nil {
		for rows.Next() {
			var audioPath string
			if err := rows.Scan(&audioPath); err == nil && audioPath != "" {
				_ = os.Remove(audioPath)
			}
		}
		rows.Close()
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	_, _ = tx.Exec("DELETE FROM meetings WHERE user_id = ?", userID)
	_, _ = tx.Exec("DELETE FROM usage_logs WHERE user_id = ?", userID)
	_, _ = tx.Exec("DELETE FROM vocabulary WHERE user_id = ?", userID)
	_, _ = tx.Exec("DELETE FROM subscription_payments WHERE user_id = ?", userID)
	_, _ = tx.Exec("DELETE FROM webhook_logs WHERE user_id = ?", userID)
	_, err = tx.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
