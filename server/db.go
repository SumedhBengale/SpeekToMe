package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

type Database struct {
	db *sql.DB
}

func NewDatabase(connStr string) (*Database, error) {
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}
	
	// Configure SQLite connection pooling (Group A Optimization)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// Enable WAL journal mode and busy timeout (Group A Optimization)
	_, _ = db.Exec("PRAGMA journal_mode=WAL;")
	_, _ = db.Exec("PRAGMA synchronous=NORMAL;")
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")
	
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
			verification_token TEXT
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
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
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
		);
		CREATE TABLE IF NOT EXISTS saas_config (
			key TEXT PRIMARY KEY,
			value TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_usage_logs_user_status_timestamp ON usage_logs(user_id, status, timestamp);
		CREATE INDEX IF NOT EXISTS idx_sessions_token_expires ON sessions(token, expires_at);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	
	// Dynamic migrations for existing databases to avoid breaking changes
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_tier TEXT DEFAULT 'free'")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_active INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE usage_logs ADD COLUMN audio_size_bytes INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN plan_activated_at TIMESTAMP")
	_, _ = db.Exec("UPDATE users SET plan_activated_at = created_at WHERE plan_activated_at IS NULL")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN password_hash TEXT")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN is_verified INTEGER DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN verification_token TEXT")

	// Set default configurations
	configs := map[string]string{
		"free_rate_limit_rpm":          "3",
		"paid_rate_limit_rpm":          "15",
		"free_fup_limit_seconds_daily": "300",
		"paid_fup_limit_seconds_daily": "3600",
		"free_monthly_limit_seconds":   "3600",
		"paid_monthly_limit_seconds":   "180000",
	}
	for k, v := range configs {
		_, _ = db.Exec("INSERT OR IGNORE INTO saas_config (key, value) VALUES (?, ?)", k, v)
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
func (d *Database) CreateSession(token string, userID int64) error {
	expiresAt := time.Now().AddDate(0, 0, 30).Format(time.RFC3339)
	_, err := d.db.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expiresAt)
	return err
}

type UserProfile struct {
	ID              int64
	Email           string
	Name            string
	PlanTier        string
	PlanActive      int
	PlanActivatedAt string
}

func (d *Database) GetUserBySession(token string) (*UserProfile, error) {
	now := time.Now().Format(time.RFC3339)
	var u UserProfile
	err := d.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.plan_tier, u.plan_active, COALESCE(u.plan_activated_at, u.created_at) 
		FROM sessions s 
		JOIN users u ON s.user_id = u.id 
		WHERE s.token = ? AND s.expires_at > ?
	`, token, now).Scan(&u.ID, &u.Email, &u.Name, &u.PlanTier, &u.PlanActive, &u.PlanActivatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *Database) DeleteSession(token string) error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// Usage logging
func (d *Database) LogUsage(userID int64, duration, whisperTime, llmTime, totalTime float64, enhanceEnabled bool, model, input, output, status string, audioSizeBytes int) error {
	enhanceVal := 0
	if enhanceEnabled {
		enhanceVal = 1
	}
	_, err := d.db.Exec(`
		INSERT INTO usage_logs (
			user_id, audio_duration, whisper_time, llm_time, total_time, 
			enhance_enabled, enhance_model, input_transcript, final_output, status, audio_size_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, duration, whisperTime, llmTime, totalTime, enhanceVal, model, input, output, status, audioSizeBytes)
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
func (d *Database) UpdateUserPlan(userID int64, planTier string, planActive int) error {
	_, err := d.db.Exec("UPDATE users SET plan_tier = ?, plan_active = ?, plan_activated_at = CURRENT_TIMESTAMP WHERE id = ?", planTier, planActive, userID)
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
	rows, err := d.db.Query("SELECT key, value FROM saas_config")
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
	return config, nil
}

func (d *Database) UpdateSaasConfig(key, value string) error {
	_, err := d.db.Exec("INSERT INTO saas_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
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

	err := d.db.QueryRow("SELECT id, password_hash, is_verified FROM users WHERE email = ? AND password_hash IS NOT NULL", email).Scan(&id, &hashedPassword, &isVerified)
	if err != nil {
		return 0, fmt.Errorf("user not found or invalid login method")
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return 0, fmt.Errorf("invalid password")
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
	return logs, nil
}
