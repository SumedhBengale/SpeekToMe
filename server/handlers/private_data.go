package handlers

import (
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"speektome-server/auth"
	"strconv"
	"strings"
)

func (a *App) HandleFinalTranscription(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, a.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if a.DB.Privacy == nil {
		http.Error(w, "encrypted history unavailable", 503)
		return
	}
	if !a.accountRate(w, r, user.ID, "history_write", 60) {
		return
	}
	var req struct {
		ID   string `json:"request_id"`
		Text string `json:"text"`
		Mode string `json:"mode"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, (1<<20)+4096)).Decode(&req) != nil {
		http.Error(w, "invalid final transcription", 400)
		return
	}
	_, idErr := uuid.Parse(req.ID)
	if idErr != nil || len(req.ID) != 36 || strings.TrimSpace(req.Text) == "" || len(req.Text) > 1<<20 || (req.Mode != "dictation" && req.Mode != "command") {
		http.Error(w, "invalid final transcription", 400)
		return
	}
	if e = a.DB.Privacy.SaveFinal(r.Context(), user.ID, req.ID, req.Mode, req.Text); e != nil {
		http.Error(w, "encrypted history could not be saved", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"saved": true})
}
func (a *App) HandlePrivateHistory(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, a.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if a.DB.Privacy == nil {
		http.Error(w, "encrypted history unavailable", 503)
		return
	}
	if !a.accountRate(w, r, user.ID, "history_read", 60) {
		return
	}
	if r.Method == "DELETE" {
		e = a.DB.Privacy.DeleteHistory(r.Context(), user.ID, r.PathValue("id"))
		if e != nil {
			http.Error(w, "history deletion unavailable", 503)
			return
		}
		w.WriteHeader(204)
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	data, e := a.DB.Privacy.History(r.Context(), user.ID, before, r.URL.Query().Get("before_id"))
	if e != nil {
		http.Error(w, "encrypted history unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
func (a *App) HandleCloudStorage(w http.ResponseWriter, r *http.Request) {
	user, e := auth.Authenticate(r, a.DB)
	if e != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if a.DB.Privacy == nil {
		http.Error(w, "cloud storage unavailable", 503)
		return
	}
	if !a.accountRate(w, r, user.ID, "storage_read", 60) {
		return
	}
	u, e := a.DB.Privacy.Usage(r.Context(), user.ID)
	if e != nil {
		http.Error(w, "cloud storage unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(u)
}
