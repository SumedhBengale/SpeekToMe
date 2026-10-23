package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseOAuthStateTargets(t *testing.T) {
	web := fmt.Sprintf("exchange_web_%x_nonce_client", []byte("https://speekto.me/account"))
	target, err := parseOAuthState(web)
	if err != nil || !target.exchange || !target.web || target.redirectTo != "https://speekto.me/account" || target.clientState != "client" {
		t.Fatalf("web state = %+v, %v", target, err)
	}
	target, err = parseOAuthState("exchange_43123_nonce_desktop")
	if err != nil || !target.exchange || target.web || target.port != 43123 || target.clientState != "desktop" {
		t.Fatalf("desktop state = %+v, %v", target, err)
	}
	for _, bad := range []string{"x", "70000_nonce", fmt.Sprintf("web_%x_nonce", []byte("https://evil.test/")), "web_zz_nonce"} {
		if _, err = parseOAuthState(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func totpCode(secret []byte, step int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0xf
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
}

func TestTwoFactorCodeCannotBeReplayed(t *testing.T) {
	a := limitApp(t)
	// Base32 of "12345678901234567890".
	if _, err := a.DB.GetDB().Exec(`UPDATE users SET is_admin=1,totp_secret='GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"temp-a", "temp-b"} {
		if err := a.DB.CreateSession(token, 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	code := totpCode([]byte("12345678901234567890"), time.Now().Unix()/30)
	verify := func(token string) int {
		r := httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"temp_token":%q,"code":%q}`, token, code)))
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		a.HandleAuth2FAVerify(w, r)
		return w.Code
	}
	if code := verify("temp-a"); code != 200 {
		t.Fatalf("first use rejected: %d", code)
	}
	if code := verify("temp-b"); code != 401 {
		t.Fatalf("replayed code accepted: %d", code)
	}
}

func TestDictationLimitsPreferCurrentKeys(t *testing.T) {
	rpm, monthly := dictationLimits(false)
	if rpm <= 0 || monthly <= 0 {
		t.Fatalf("free limits %d %v", rpm, monthly)
	}
	paidRPM, paidMonthly := dictationLimits(true)
	if paidRPM < rpm || paidMonthly < monthly {
		t.Fatalf("paid limits %d %v below free %d %v", paidRPM, paidMonthly, rpm, monthly)
	}
}
