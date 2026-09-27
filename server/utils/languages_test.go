package utils

import (
	"testing"
)

func TestSupportedLanguagesCatalog(t *testing.T) {
	langs := GetSupportedLanguages()
	if len(langs) != 35 {
		t.Fatalf("Expected 35 supported languages, got %d", len(langs))
	}

	seenCodes := make(map[string]bool)
	for _, l := range langs {
		if l.Code == "" {
			t.Errorf("Language %s has empty code", l.Name)
		}
		if l.Name == "" {
			t.Errorf("Language code %s has empty name", l.Code)
		}
		if l.Locale == "" {
			t.Errorf("Language %s has empty locale", l.Name)
		}
		if l.Tier == "" {
			t.Errorf("Language %s has empty tier", l.Name)
		}
		if seenCodes[l.Code] {
			t.Errorf("Duplicate language code: %s", l.Code)
		}
		seenCodes[l.Code] = true
	}
}

func TestDualCodeMapping(t *testing.T) {
	tests := []struct {
		code   string
		locale string
	}{
		{"en", "en-US"},
		{"es", "es-ES"},
		{"de", "de-DE"},
		{"fr", "fr-FR"},
		{"it", "it-IT"},
		{"hi", "hi-IN"},
		{"zh", "zh-CN"},
		{"ja", "ja-JP"},
		{"ko", "ko-KR"},
		{"ru", "ru-RU"},
		{"ar", "ar-AR"},
		{"no", "nb-NO"},
	}

	for _, tc := range tests {
		loc := GetLocaleForCode(tc.code)
		if loc != tc.locale {
			t.Errorf("GetLocaleForCode(%s): expected %s, got %s", tc.code, tc.locale, loc)
		}

		code := GetCodeForLocale(tc.locale)
		if code != tc.code {
			t.Errorf("GetCodeForLocale(%s): expected %s, got %s", tc.locale, tc.code, code)
		}
	}

	// Test regional dialects mapping back to base code
	dialects := map[string]string{
		"en-gb": "en",
		"es-us": "es",
		"fr-ca": "fr",
		"pt-pt": "pt",
		"nn-no": "no",
	}
	for d, expected := range dialects {
		c := GetCodeForLocale(d)
		if c != expected {
			t.Errorf("GetCodeForLocale(%s): expected %s, got %s", d, expected, c)
		}
	}
}

func TestResolveNemotronLanguage(t *testing.T) {
	if got := ResolveNemotronLanguage([]string{"hi"}); got != "hi-IN" {
		t.Errorf("Expected 'hi-IN', got %s", got)
	}
	if got := ResolveNemotronLanguage([]string{"en"}); got != "en-US" {
		t.Errorf("Expected 'en-US', got %s", got)
	}
	if got := ResolveNemotronLanguage([]string{"en", "hi"}); got != "auto" {
		t.Errorf("Expected 'auto' for multiple languages, got %s", got)
	}
	if got := ResolveNemotronLanguage([]string{}); got != "auto" {
		t.Errorf("Expected 'auto' for empty languages, got %s", got)
	}
}

func TestValidateLanguages(t *testing.T) {
	// Normalization from locales to 2-letter codes
	valid, err := ValidateLanguages([]string{"en-US", "hi-IN", "FR"})
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}
	if len(valid) != 3 || valid[0] != "en" || valid[1] != "hi" || valid[2] != "fr" {
		t.Errorf("Unexpected validated list: %v", valid)
	}

	// Unsupported language should fail
	_, err = ValidateLanguages([]string{"klingon"})
	if err == nil {
		t.Errorf("Expected error for unsupported language, got nil")
	}
}
