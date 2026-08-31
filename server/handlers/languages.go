package handlers

import (
	"encoding/json"
	"net/http"

	"speektome-server/config"
	"speektome-server/utils"
)

type LanguagesResponse struct {
	Languages    []utils.Language    `json:"languages"`
	ModelSupport map[string][]string `json:"model_support"`
}

func HandleGetLanguages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.ConfigManager.Get()
	langs := cfg.Languages
	if len(langs) == 0 {
		langs = utils.GetSupportedLanguages()
	}

	modelSupport := make(map[string][]string)
	for name, prov := range cfg.Models.BatchProviders {
		if prov.Enabled {
			modelSupport[name] = prov.SupportedLanguages
		}
	}
	for name, prov := range cfg.Models.StreamingProviders {
		if prov.Enabled {
			modelSupport[name] = prov.SupportedLanguages
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=30")
	_ = json.NewEncoder(w).Encode(LanguagesResponse{
		Languages:    langs,
		ModelSupport: modelSupport,
	})
}

func HandleGetOnboardingPromo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	promo := config.ConfigManager.Get().OnboardingPromo
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(promo)
}
