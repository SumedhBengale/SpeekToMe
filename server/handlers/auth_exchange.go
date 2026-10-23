package handlers

import (
	"encoding/json"
	"net/http"
	"speektome-server/auth"
	"time"
)

func (a *App) issueAuthCode(code string, user int64, temp int) error {
	_, _ = a.DB.GetDB().Exec("DELETE FROM auth_codes WHERE expires<?", time.Now().Unix())
	_, e := a.DB.GetDB().Exec("INSERT INTO auth_codes VALUES(?,?,?,?)", auth.HashSecurityToken(code), user, temp, time.Now().Add(time.Minute).Unix())
	return e
}
func (a *App) HandleAuthExchange(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var input struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Code) < 32 || len(input.Code) > 128 {
		http.Error(w, "Invalid sign-in code", 400)
		return
	}
	var user int64
	var temp int
	e := a.DB.GetDB().QueryRow(`DELETE FROM auth_codes WHERE code_hash=? AND expires>? RETURNING user_id,is_temp`, auth.HashSecurityToken(input.Code), time.Now().Unix()).Scan(&user, &temp)
	if e != nil {
		http.Error(w, "Sign-in code expired or already used", 401)
		return
	}
	token := auth.GenerateSessionToken()
	if e = a.DB.CreateSession(token, user, temp); e != nil {
		http.Error(w, "Cannot create session; sign in again", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"token": token, "require_2fa": temp == 1})
}
