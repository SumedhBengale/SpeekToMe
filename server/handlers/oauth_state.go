package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"speektome-server/auth"
	"speektome-server/config"
	"strings"
	"time"
)

func oauthDigest(s string) string     { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func oauthCookie(state string) string { return "speektome_oauth_" + oauthDigest(state)[:16] }
func (a *App) issueOAuthState(w http.ResponseWriter, r *http.Request, state string) error {
	_, _ = a.DB.GetDB().Exec("DELETE FROM oauth_states WHERE expires<?", time.Now().Unix())
	verifier := auth.GenerateNonce()
	_, e := a.DB.GetDB().Exec("INSERT INTO oauth_states VALUES(?,?,?)", oauthDigest(state), oauthDigest(verifier), time.Now().Add(10*time.Minute).Unix())
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie(state), Value: verifier, Path: "/auth", HttpOnly: true, Secure: strings.HasPrefix(config.BackendURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	return nil
}
func (a *App) consumeOAuthState(w http.ResponseWriter, r *http.Request, state string) bool {
	if len(state) == 0 || len(state) > 4096 {
		return false
	}
	c, e := r.Cookie(oauthCookie(state))
	if e != nil {
		return false
	}
	result, e := a.DB.GetDB().Exec("DELETE FROM oauth_states WHERE state_hash=? AND cookie_hash=? AND expires>?", oauthDigest(state), oauthDigest(c.Value), time.Now().Unix())
	if e != nil {
		return false
	}
	n, e := result.RowsAffected()
	http.SetCookie(w, &http.Cookie{Name: oauthCookie(state), Path: "/auth", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(config.BackendURL, "https://"), SameSite: http.SameSiteLaxMode})
	return e == nil && n == 1
}
