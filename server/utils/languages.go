package utils

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Tier string `json:"tier,omitempty"`
}

var SupportedLanguages = []Language{
	{Code: "af", Name: "Afrikaans"},
	{Code: "am", Name: "Amharic"},
	{Code: "ar", Name: "Arabic"},
	{Code: "as", Name: "Assamese"},
	{Code: "az", Name: "Azerbaijani"},
	{Code: "ba", Name: "Bashkir"},
	{Code: "be", Name: "Belarusian"},
	{Code: "bg", Name: "Bulgarian"},
	{Code: "bn", Name: "Bengali"},
	{Code: "bo", Name: "Tibetan"},
	{Code: "br", Name: "Breton"},
	{Code: "bs", Name: "Bosnian"},
	{Code: "ca", Name: "Catalan"},
	{Code: "cs", Name: "Czech"},
	{Code: "cy", Name: "Welsh"},
	{Code: "da", Name: "Danish"},
	{Code: "de", Name: "German"},
	{Code: "el", Name: "Greek"},
	{Code: "en", Name: "English"},
	{Code: "es", Name: "Spanish"},
	{Code: "et", Name: "Estonian"},
	{Code: "eu", Name: "Basque"},
	{Code: "fa", Name: "Persian"},
	{Code: "fi", Name: "Finnish"},
	{Code: "fo", Name: "Faroese"},
	{Code: "fr", Name: "French"},
	{Code: "gl", Name: "Galician"},
	{Code: "gu", Name: "Gujarati"},
	{Code: "ha", Name: "Hausa"},
	{Code: "haw", Name: "Hawaiian"},
	{Code: "he", Name: "Hebrew"},
	{Code: "hi", Name: "Hindi"},
	{Code: "hr", Name: "Croatian"},
	{Code: "ht", Name: "Haitian Creole"},
	{Code: "hu", Name: "Hungarian"},
	{Code: "hy", Name: "Armenian"},
	{Code: "id", Name: "Indonesian"},
	{Code: "is", Name: "Icelandic"},
	{Code: "it", Name: "Italian"},
	{Code: "ja", Name: "Japanese"},
	{Code: "jw", Name: "Javanese"},
	{Code: "ka", Name: "Georgian"},
	{Code: "kk", Name: "Kazakh"},
	{Code: "km", Name: "Khmer"},
	{Code: "kn", Name: "Kannada"},
	{Code: "ko", Name: "Korean"},
	{Code: "la", Name: "Latin"},
	{Code: "lb", Name: "Luxembourgish"},
	{Code: "ln", Name: "Lingala"},
	{Code: "lo", Name: "Lao"},
	{Code: "lt", Name: "Lithuanian"},
	{Code: "lv", Name: "Latvian"},
	{Code: "mg", Name: "Malagasy"},
	{Code: "mi", Name: "Maori"},
	{Code: "mk", Name: "Macedonian"},
	{Code: "ml", Name: "Malayalam"},
	{Code: "mn", Name: "Mongolian"},
	{Code: "mr", Name: "Marathi"},
	{Code: "ms", Name: "Malay"},
	{Code: "mt", Name: "Maltese"},
	{Code: "my", Name: "Burmese"},
	{Code: "ne", Name: "Nepali"},
	{Code: "nl", Name: "Dutch"},
	{Code: "nn", Name: "Nynorsk"},
	{Code: "no", Name: "Norwegian"},
	{Code: "oc", Name: "Occitan"},
	{Code: "pa", Name: "Punjabi"},
	{Code: "pl", Name: "Polish"},
	{Code: "ps", Name: "Pashto"},
	{Code: "pt", Name: "Portuguese"},
	{Code: "ro", Name: "Romanian"},
	{Code: "ru", Name: "Russian"},
	{Code: "sa", Name: "Sanskrit"},
	{Code: "sd", Name: "Sindhi"},
	{Code: "si", Name: "Sinhala"},
	{Code: "sk", Name: "Slovak"},
	{Code: "sl", Name: "Slovenian"},
	{Code: "sn", Name: "Shona"},
	{Code: "so", Name: "Somali"},
	{Code: "sq", Name: "Albanian"},
	{Code: "sr", Name: "Serbian"},
	{Code: "su", Name: "Sundanese"},
	{Code: "sv", Name: "Swedish"},
	{Code: "sw", Name: "Swahili"},
	{Code: "ta", Name: "Tamil"},
	{Code: "te", Name: "Telugu"},
	{Code: "tg", Name: "Tajik"},
	{Code: "th", Name: "Thai"},
	{Code: "tk", Name: "Turkmen"},
	{Code: "tl", Name: "Tagalog"},
	{Code: "tr", Name: "Turkish"},
	{Code: "tt", Name: "Tatar"},
	{Code: "uk", Name: "Ukrainian"},
	{Code: "ur", Name: "Urdu"},
	{Code: "uz", Name: "Uzbek"},
	{Code: "vi", Name: "Vietnamese"},
	{Code: "yi", Name: "Yiddish"},
	{Code: "yo", Name: "Yoruba"},
	{Code: "yue", Name: "Cantonese"},
	{Code: "zh", Name: "Chinese"},
}

var (
	langMu          sync.RWMutex
	currentLangs    = SupportedLanguages
	languageCodeMap = make(map[string]string)
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
	for _, l := range langs {
		languageCodeMap[l.Code] = l.Name
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

func GetLanguageName(code string) string {
	langMu.RLock()
	defer langMu.RUnlock()
	if name, ok := languageCodeMap[code]; ok {
		return name
	}
	return code
}

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
	for _, code := range input {
		if _, ok := languageCodeMap[code]; !ok {
			return nil, fmt.Errorf("invalid language code: %s", code)
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
