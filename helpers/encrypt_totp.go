// Usage:
//   go run helpers/encrypt_totp.go -secret YOUR_RAW_TOTP_SECRET -email admin@example.com
//   or simply:
//   go run helpers/encrypt_totp.go
//
// This script encrypts a raw Base32 TOTP secret using the ENCRYPTION_KEY from server/.env
// and generates a copy-paste SQL query to set totp_secret and is_admin=1 in the DB.

package main

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadDotEnv attempts to read .env file from common relative paths
func loadDotEnv() {
	paths := []string{
		".env",
		"../server/.env",
		"server/.env",
		filepath.Join("..", "server", ".env"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					val = strings.Trim(val, `"'`)
					if os.Getenv(key) == "" {
						os.Setenv(key, val)
					}
				}
			}
			fmt.Printf("[+] Loaded environment variables from %s\n", p)
			break
		}
	}
}

func getEncryptionKey() ([]byte, string) {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	source := "ENCRYPTION_KEY environment variable"
	if keyStr == "" {
		keyStr = "speektome_default_fallback_encryption_key_32bytes"
		source = "Default fallback key (ENCRYPTION_KEY not set)"
	}
	hash := sha256.Sum256([]byte(keyStr))
	return hash[:], source
}

func encryptTOTPSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", fmt.Errorf("plaintext secret cannot be empty")
	}
	key, _ := getEncryptionKey()
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

func main() {
	loadDotEnv()

	secretFlag := flag.String("secret", "", "Raw TOTP secret key (e.g. JBSWY3DPEHPK3PXP)")
	emailFlag := flag.String("email", "your_admin_email@example.com", "Admin user email for SQL template")
	flag.Parse()

	secret := *secretFlag

	// If no positional argument or flag passed, try first non-flag arg
	if secret == "" && len(flag.Args()) > 0 {
		secret = flag.Arg(0)
	}

	// Prompt interactively if still empty
	if secret == "" {
		fmt.Print("Enter raw TOTP secret (e.g., JBSWY3DPEHPK3PXP): ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			os.Exit(1)
		}
		secret = strings.TrimSpace(input)
	}

	if secret == "" {
		fmt.Println("Error: TOTP secret cannot be empty.")
		os.Exit(1)
	}

	encrypted, err := encryptTOTPSecret(secret)
	if err != nil {
		fmt.Printf("Error encrypting TOTP secret: %v\n", err)
		os.Exit(1)
	}

	_, keySource := getEncryptionKey()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println(" SpeekTo.Me Admin TOTP Encryption Helper")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("[+] Raw TOTP Secret:  %s\n", secret)
	fmt.Printf("[+] Key Source:       %s\n", keySource)
	fmt.Printf("[+] Encrypted Secret: %s\n\n", encrypted)
	fmt.Println("[+] Copy & Paste SQL Query to update DB:")
	fmt.Printf("UPDATE users SET totp_secret = '%s', is_admin = 1 WHERE email = '%s';\n", encrypted, *emailFlag)
	fmt.Println(strings.Repeat("=", 60) + "\n")
}
