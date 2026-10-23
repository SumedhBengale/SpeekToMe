package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// RFC 6238 SHA-1 seed "12345678901234567890" in base32.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPMatchesRFCVectorAndReportsStep(t *testing.T) {
	// RFC 6238: T=59 gives 94287082; six digits are the low-order 287082.
	step, ok := VerifyTOTPAt(rfcSecret, "287082", time.Unix(59, 0))
	if !ok || step != 1 {
		t.Fatalf("step=%d ok=%v", step, ok)
	}
	// One step of clock drift either way is accepted, two are not.
	if _, ok = VerifyTOTPAt(rfcSecret, "287082", time.Unix(59+30, 0)); !ok {
		t.Fatal("previous step rejected")
	}
	if _, ok = VerifyTOTPAt(rfcSecret, "287082", time.Unix(59+90, 0)); ok {
		t.Fatal("stale code accepted")
	}
	for _, code := range []string{"", "28708", "2870822", "000000"} {
		if _, ok = VerifyTOTPAt(rfcSecret, code, time.Unix(59, 0)); ok {
			t.Fatalf("accepted %q", code)
		}
	}
	if _, ok = VerifyTOTPAt("", "287082", time.Unix(59, 0)); ok {
		t.Fatal("empty secret accepted")
	}
}

func TestRedirectValidation(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://app.example.test")
	t.Setenv("BACKEND_URL", "")
	cases := map[string]bool{
		"https://speekto.me/dashboard":      true,
		"https://dashboard.speekto.me/x":    true,
		"https://app.example.test/callback": true,
		"http://localhost:5173/auth":        true,
		"/relative/path":                    true,
		"//evil.example/path":               false,
		"/\\evil.example":                   false,
		"http://speekto.me/insecure":        false,
		"https://speekto.me.evil.test/":     false,
		"https://user@speekto.me/":          false,
		"https://speekto.me/#fragment":      false,
		"javascript:alert(1)":               false,
		"data:text/html,x":                  false,
		"":                                  false,
	}
	for target, want := range cases {
		if got := IsValidRedirect(target); got != want {
			t.Errorf("IsValidRedirect(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestPasswordStrength(t *testing.T) {
	for password, ok := range map[string]bool{
		"short1":                 false,
		"onlyletters":            false,
		"1234567890":             false,
		"letters123":             true,
		strings.Repeat("a1", 37): false,
	} {
		if (ValidatePasswordStrength(password) == nil) != ok {
			t.Errorf("password %q: want ok=%v", password, ok)
		}
	}
}

func TestAckTokensBindUserAndLog(t *testing.T) {
	SetAckSecret([]byte("test-ack-secret"))
	token := GenerateLogAckToken(1, 2)
	if !VerifyLogAckToken(1, 2, token) {
		t.Fatal("valid token rejected")
	}
	if VerifyLogAckToken(2, 2, token) || VerifyLogAckToken(1, 3, token) || VerifyLogAckToken(1, 2, "") {
		t.Fatal("token accepted for a different user, log or empty value")
	}
}

func TestSessionTokensAreUniqueAndPrefixed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		token := GenerateSessionToken()
		if !strings.HasPrefix(token, "speektome_session_") || len(token) != len("speektome_session_")+48 || seen[token] {
			t.Fatalf("bad or repeated token %q", token)
		}
		seen[token] = true
	}
}

func TestClientIPTrustsForwardingOnlyFromLoopback(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:4000"
	r.Header.Set("X-Real-IP", "198.51.100.1")
	if got := GetClientIP(r); got != "203.0.113.5" {
		t.Fatalf("spoofed header trusted from remote peer: %s", got)
	}
	r.RemoteAddr = "127.0.0.1:4000"
	if got := GetClientIP(r); got != "198.51.100.1" {
		t.Fatalf("proxy header ignored: %s", got)
	}
}
