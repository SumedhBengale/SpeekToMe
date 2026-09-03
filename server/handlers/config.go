package handlers

import (
	"encoding/json"
	"net/http"

	"speektome-server/config"
)

type ClientConfigResponse struct {
	TransformInputTokens int    `json:"transform_input_tokens"`
	Notification         string `json:"notification,omitempty"`
}

func HandleGetClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	resp := ClientConfigResponse{
		TransformInputTokens: config.ConfigManager.GetTransformInputWordLimit(),
		Notification:         config.ConfigManager.Get().Notification,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
