package utils

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type Language struct {
	Code   string `json:"code"`           // 2-letter ISO 639-1 code (e.g. "en", "hi")
	Name   string `json:"name"`           // Display name (e.g. "English", "Hindi")
	Locale string `json:"locale"`         // Inference locale (e.g. "en-US", "hi-IN")
	Tier   string `json:"tier,omitempty"` // Tier from model card ("transcription-ready", "broad-coverage", "adaptation-ready")
}

// SupportedLanguages defines the 35 languages supported by Nemotron 3.5 ASR Streaming 0.6b and Whisper.
var SupportedLanguages = []Language{
	// Transcription-ready (Highest-accuracy out of the box)
	{Code: "en", Name: "English", Locale: "en-US", Tier: "transcription-ready"},
	{Code: "es", Name: "Spanish", Locale: "es-ES", Tier: "transcription-ready"},
	{Code: "de", Name: "German", Locale: "de-DE", Tier: "transcription-ready"},
	{Code: "fr", Name: "French", Locale: "fr-FR", Tier: "transcription-ready"},
	{Code: "it", Name: "Italian", Locale: "it-IT", Tier: "transcription-ready"},
	{Code: "pt", Name: "Portuguese", Locale: "pt-BR", Tier: "transcription-ready"},
	{Code: "nl", Name: "Dutch", Locale: "nl-NL", Tier: "transcription-ready"},
	{Code: "ru", Name: "Russian", Locale: "ru-RU", Tier: "transcription-ready"},
	{Code: "tr", Name: "Turkish", Locale: "tr-TR", Tier: "transcription-ready"},
	{Code: "ar", Name: "Arabic", Locale: "ar-AR", Tier: "transcription-ready"},
	{Code: "hi", Name: "Hindi", Locale: "hi-IN", Tier: "transcription-ready"},
	{Code: "ja", Name: "Japanese", Locale: "ja-JP", Tier: "transcription-ready"},
	{Code: "ko", Name: "Korean", Locale: "ko-KR", Tier: "transcription-ready"},
	{Code: "vi", Name: "Vietnamese", Locale: "vi-VN", Tier: "transcription-ready"},
	{Code: "uk", Name: "Ukrainian", Locale: "uk-UA", Tier: "transcription-ready"},

	// Broad-coverage (Production ASR across 13 locales)
	{Code: "zh", Name: "Chinese", Locale: "zh-CN", Tier: "broad-coverage"},
	{Code: "pl", Name: "Polish", Locale: "pl-PL", Tier: "broad-coverage"},
	{Code: "sv", Name: "Swedish", Locale: "sv-SE", Tier: "broad-coverage"},
	{Code: "cs", Name: "Czech", Locale: "cs-CZ", Tier: "broad-coverage"},
	{Code: "no", Name: "Norwegian", Locale: "nb-NO", Tier: "broad-coverage"},
	{Code: "da", Name: "Danish", Locale: "da-DK", Tier: "broad-coverage"},
	{Code: "bg", Name: "Bulgarian", Locale: "bg-BG", Tier: "broad-coverage"},
	{Code: "fi", Name: "Finnish", Locale: "fi-FI", Tier: "broad-coverage"},
	{Code: "hr", Name: "Croatian", Locale: "hr-HR", Tier: "broad-coverage"},
	{Code: "sk", Name: "Slovak", Locale: "sk-SK", Tier: "broad-coverage"},
	{Code: "hu", Name: "Hungarian", Locale: "hu-HU", Tier: "broad-coverage"},
	{Code: "ro", Name: "Romanian", Locale: "ro-RO", Tier: "broad-coverage"},
	{Code: "et", Name: "Estonian", Locale: "et-EE", Tier: "broad-coverage"},

	// Adaptation-ready (Tokenizer recognized)
	{Code: "el", Name: "Greek", Locale: "el-GR", Tier: "adaptation-ready"},
	{Code: "lt", Name: "Lithuanian", Locale: "lt-LT", Tier: "adaptation-ready"},
	{Code: "lv", Name: "Latvian", Locale: "lv-LV", Tier: "adaptation-ready"},
	{Code: "mt", Name: "Maltese", Locale: "mt-MT", Tier: "adaptation-ready"},
	{Code: "sl", Name: "Slovenian", Locale: "sl-SI", Tier: "adaptation-ready"},
	{Code: "he", Name: "Hebrew", Locale: "he-IL", Tier: "adaptation-ready"},
	{Code: "th", Name: "Thai", Locale: "th-TH", Tier: "adaptation-ready"},
}

// Secondary / Regional Locales recognized by Nemotron 3.5 that map to primary 2-letter codes
var secondaryLocaleMap = map[string]string{
	"en-gb": "en",
	"es-us": "es",
	"fr-ca": "fr",
	"pt-pt": "pt",
	"nn-no": "no",
}

var (
	langMu          sync.RWMutex
	currentLangs    = SupportedLanguages
	languageCodeMap = make(map[string]string) // "en" -> "English"
	codeToLocaleMap = make(map[string]string) // "en" -> "en-US", "hi" -> "hi-IN"
	localeToCodeMap = make(map[string]string) // "en-us" -> "en", "en-gb" -> "en", "hi-in" -> "hi"
)

func init() {
	rebuildLanguageMap(SupportedLanguages)
}

func rebuildLanguageMap(langs []Language) {
	langMu.Lock()
	defer langMu.Unlock()

	currentLangs = make([]Language, len(langs))
	copy(currentLangs, langs)

	languageCodeMap = make(map[string]string, len(langs))
	codeToLocaleMap = make(map[string]string, len(langs)*2)
	localeToCodeMap = make(map[string]string, len(langs)*3)

	// Populate primary entries
	for _, l := range langs {
		codeLower := strings.ToLower(strings.TrimSpace(l.Code))
		languageCodeMap[codeLower] = l.Name

		locale := strings.TrimSpace(l.Locale)
		if locale == "" {
			locale = codeLower
		}

		codeToLocaleMap[codeLower] = locale
		codeToLocaleMap[strings.ToLower(locale)] = locale

		localeToCodeMap[codeLower] = codeLower
		localeToCodeMap[strings.ToLower(locale)] = codeLower
	}

	// Populate secondary / dialect locales
	for secLocale, code := range secondaryLocaleMap {
		if _, ok := languageCodeMap[code]; ok {
			localeToCodeMap[secLocale] = code
			codeToLocaleMap[secLocale] = secLocale // preserve exact dialect if specified
		}
	}
}

// SetDynamicLanguages updates the in-memory master language registry dynamically from saas_config
func SetDynamicLanguages(langs []Language) {
	if len(langs) == 0 {
		return
	}
	rebuildLanguageMap(langs)
}

// GetSupportedLanguages returns the currently active list of languages
func GetSupportedLanguages() []Language {
	langMu.RLock()
	defer langMu.RUnlock()
	res := make([]Language, len(currentLangs))
	copy(res, currentLangs)
	return res
}

// GetSupportedLanguageCodes returns the 2-letter codes of the currently active languages
func GetSupportedLanguageCodes() []string {
	langMu.RLock()
	defer langMu.RUnlock()
	codes := make([]string, len(currentLangs))
	for i, l := range currentLangs {
		codes[i] = l.Code
	}
	return codes
}

// GetLanguageName returns display name for a code or locale
func GetLanguageName(codeOrLocale string) string {
	langMu.RLock()
	defer langMu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(codeOrLocale))
	if code, ok := localeToCodeMap[key]; ok {
		if name, ok := languageCodeMap[code]; ok {
			return name
		}
	}
	if name, ok := languageCodeMap[key]; ok {
		return name
	}
	return codeOrLocale
}

// GetLocaleForCode maps a 2-letter code to its primary BCP-47 locale (e.g. "hi" -> "hi-IN", "en" -> "en-US")
func GetLocaleForCode(codeOrLocale string) string {
	langMu.RLock()
	defer langMu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(codeOrLocale))
	if loc, ok := codeToLocaleMap[key]; ok {
		return loc
	}
	return codeOrLocale
}

// GetCodeForLocale maps a BCP-47 locale or dialect to its 2-letter ISO 639-1 code (e.g. "hi-IN" -> "hi", "en-US" -> "en")
func GetCodeForLocale(localeOrCode string) string {
	langMu.RLock()
	defer langMu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(localeOrCode))
	if code, ok := localeToCodeMap[key]; ok {
		return code
	}
	return key
}

// ResolveSpeakingLanguage resolves a single per-recording choice. Older clients
// default to their first saved spoken language; only an explicit auto enables detection.
func ResolveSpeakingLanguage(requested string, spoken []string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(requested))
	if clean == "" {
		clean = "en"
		if len(spoken) > 0 {
			clean = strings.ToLower(strings.TrimSpace(spoken[0]))
		}
	}
	if clean == "auto" {
		return "auto", nil
	}
	valid, err := ValidateLanguages([]string{clean})
	if err != nil {
		return "", err
	}
	return valid[0], nil
}

// ResolveNemotronLanguage maps the one active choice to a model locale.
func ResolveNemotronLanguage(languages []string) string {
	language, err := ResolveSpeakingLanguage("", languages)
	if err != nil {
		return "en-US"
	}
	if language == "auto" {
		return "auto"
	}
	return GetLocaleForCode(language)
}

// ValidateLanguages validates that given language codes/locales belong to the supported set,
// normalizes them to their canonical 2-letter ISO code, and caps selection at 5.
func ValidateLanguages(input []string) ([]string, error) {
	if len(input) == 0 {
		return []string{"en"}, nil
	}
	if len(input) > 5 {
		return nil, fmt.Errorf("a maximum of 5 languages can be selected")
	}
	langMu.RLock()
	defer langMu.RUnlock()

	var validated []string
	seen := make(map[string]bool)
	for _, raw := range input {
		clean := strings.ToLower(strings.TrimSpace(raw))
		code, ok := localeToCodeMap[clean]
		if !ok {
			// Also check if raw was directly in languageCodeMap
			if _, exists := languageCodeMap[clean]; exists {
				code = clean
			} else {
				return nil, fmt.Errorf("invalid language code: %s", raw)
			}
		}
		if !seen[code] {
			seen[code] = true
			validated = append(validated, code)
		}
	}
	if len(validated) == 0 {
		return []string{"en"}, nil
	}
	return validated, nil
}

func ParseLanguagesJSON(jsonStr string) []string {
	if jsonStr == "" {
		return []string{"en"}
	}
	var langs []string
	if err := json.Unmarshal([]byte(jsonStr), &langs); err != nil {
		return []string{"en"}
	}
	valid, err := ValidateLanguages(langs)
	if err != nil || len(valid) == 0 {
		return []string{"en"}
	}
	return valid
}

func FormatLanguagesJSON(langs []string) string {
	valid, err := ValidateLanguages(langs)
	if err != nil || len(valid) == 0 {
		valid = []string{"en"}
	}
	b, _ := json.Marshal(valid)
	return string(b)
}
