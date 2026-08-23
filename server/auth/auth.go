package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"speektome-server/db"
)

// Generate secure session tokens
func GenerateSessionToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return fmt.Sprintf("speektome_session_%x", b)
}

// Generate random hex nonces for OAuth states
func GenerateNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// Generate high-entropy cryptographic token for password resets and verifications (default 32 bytes = 256 bits)
func GenerateCryptoToken(bytesCount int) string {
	if bytesCount <= 0 {
		bytesCount = 32
	}
	b := make([]byte, bytesCount)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// HashSecurityToken computes the SHA-256 hash of a security token for secure database storage
func HashSecurityToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

func getAckSecret() []byte {
	secret := os.Getenv("SPEEKTOME_ACK_SECRET")
	if secret == "" {
		secret = os.Getenv("SESSION_SECRET")
	}
	if secret == "" {
		secret = "speektome-secure-transcription-ack-secret-key"
	}
	return []byte(secret)
}

// GenerateLogAckToken generates a cryptographic HMAC-SHA256 signature for a transcription log
func GenerateLogAckToken(userID, logID int64) string {
	h := hmac.New(sha256.New, getAckSecret())
	msg := fmt.Sprintf("speektome:ack:%d:%d", userID, logID)
	h.Write([]byte(msg))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// VerifyLogAckToken verifies that an ack token matches the given userID and logID
func VerifyLogAckToken(userID, logID int64, token string) bool {
	if token == "" || userID <= 0 || logID <= 0 {
		return false
	}
	expected := GenerateLogAckToken(userID, logID)
	return hmac.Equal([]byte(expected), []byte(token))
}

// Extract bearer token and retrieve user
func Authenticate(r *http.Request, database *db.Database) (*db.UserProfile, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("missing bearer token")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, err := database.GetUserBySession(token)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired session token")
	}
	return user, nil
}

func AuthenticateWithVocab(r *http.Request, database *db.Database) (*db.UserProfile, []db.VocabularyTerm, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, nil, fmt.Errorf("missing bearer token")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	user, vocabTerms, err := database.GetUserAndVocabularyBySession(token)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid or expired session token")
	}
	return user, vocabTerms, nil
}

// AuthenticateAdmin checks if request contains a valid admin session
func AuthenticateAdmin(r *http.Request, database *db.Database) (*db.UserProfile, error) {
	user, err := Authenticate(r, database)
	if err != nil {
		return nil, err
	}
	if user.IsAdmin != 1 {
		return nil, fmt.Errorf("Forbidden: Admin access required")
	}
	return user, nil
}

func GetClientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func IsValidRedirect(redirectURL string) bool {
	if redirectURL == "" {
		return false
	}
	u, err := url.Parse(redirectURL)
	if err != nil {
		return false
	}

	// Strictly block dangerous pseudo-schemes
	schemeLower := strings.ToLower(u.Scheme)
	if schemeLower == "javascript" || schemeLower == "data" || schemeLower == "vbscript" || schemeLower == "file" || schemeLower == "blob" {
		return false
	}

	// Relative paths
	if u.Scheme == "" && u.Host == "" {
		return strings.HasPrefix(redirectURL, "/") && !strings.HasPrefix(redirectURL, "//") && !strings.Contains(redirectURL, "\\")
	}

	// Known allowed desktop client custom schemes
	if schemeLower == "tauri" || schemeLower == "speektome" {
		return true
	}

	if schemeLower != "http" && schemeLower != "https" {
		return false
	}

	frontend := os.Getenv("FRONTEND_URL")
	backend := os.Getenv("BACKEND_URL")

	var allowedHosts = []string{"localhost", "127.0.0.1", "speekto.me", "api.speekto.me", "dashboard.speekto.me"}
	if frontend != "" {
		if fu, err := url.Parse(frontend); err == nil && fu.Hostname() != "" {
			allowedHosts = append(allowedHosts, fu.Hostname())
		}
	}
	if backend != "" {
		if bu, err := url.Parse(backend); err == nil && bu.Hostname() != "" {
			allowedHosts = append(allowedHosts, bu.Hostname())
		}
	}

	redirectHost := strings.ToLower(u.Hostname())
	for _, allowed := range allowedHosts {
		if redirectHost == strings.ToLower(allowed) || strings.HasSuffix(redirectHost, ".pages.dev") || strings.HasSuffix(redirectHost, ".speekto.me") {
			return true
		}
	}

	return false
}

func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}
	if len(password) > 72 {
		return fmt.Errorf("password must not be longer than 72 characters")
	}
	var hasDigit, hasLetter bool
	for _, c := range password {
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		}
	}
	if !hasDigit || !hasLetter {
		return fmt.Errorf("password must contain both letters and digits")
	}
	return nil
}

// Simple self-contained base32 decoding logic to avoid external dependencies
func DecodeBase32(s string) ([]byte, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	var charMap [256]byte
	for i := 0; i < len(alphabet); i++ {
		charMap[alphabet[i]] = byte(i)
	}

	s = strings.ToUpper(strings.TrimRight(s, "="))
	if len(s) == 0 {
		return []byte{}, nil
	}

	length := len(s)
	numBytes := (length * 5) / 8
	dst := make([]byte, numBytes)

	var buffer uint64
	var bits uint
	var count int

	for i := 0; i < length; i++ {
		val := charMap[s[i]]
		buffer = (buffer << 5) | uint64(val)
		bits += 5
		if bits >= 8 {
			bits -= 8
			if count < len(dst) {
				dst[count] = byte(buffer >> bits)
				count++
			}
		}
	}

	return dst, nil
}

func VerifyTOTP(secret string, code string) bool {
	secretBytes, err := DecodeBase32(secret)
	if err != nil || len(secretBytes) == 0 {
		return false
	}

	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}

	currentTime := time.Now().Unix()
	step := currentTime / 30

	for d := -1; d <= 1; d++ {
		testStep := step + int64(d)
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(testStep))

		mac := hmac.New(sha1.New, secretBytes)
		mac.Write(buf)
		sum := mac.Sum(nil)

		offset := sum[len(sum)-1] & 0xf
		binaryVal := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
		otp := binaryVal % 1000000

		expectedCode := fmt.Sprintf("%06d", otp)
		if expectedCode == code {
			return true
		}
	}

	return false
}
