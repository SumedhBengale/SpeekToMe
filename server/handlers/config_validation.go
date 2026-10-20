package handlers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func validateConfigUpdate(current, updates map[string]string) error {
	for key, value := range updates {
		if _, ok := current[key]; !ok {
			return fmt.Errorf("unknown configuration key %s", key)
		}
		if key == "last_free_tier_quota_reset_month" {
			if value != current[key] {
				return fmt.Errorf("quota reset state is read-only")
			}
			continue
		}
		switch key {
		case "models_config_json", "languages_config_json", "onboarding_promo_config_json":
			if !json.Valid([]byte(value)) {
				return fmt.Errorf("invalid JSON for %s", key)
			}
			if key == "languages_config_json" {
				var v []json.RawMessage
				if json.Unmarshal([]byte(value), &v) != nil || len(v) == 0 {
					return fmt.Errorf("languages must be a nonempty array")
				}
			} else {
				var v map[string]json.RawMessage
				if json.Unmarshal([]byte(value), &v) != nil || len(v) == 0 {
					return fmt.Errorf("%s must be a nonempty object", key)
				}
			}
		case "meeting_stream_url", "onboarding_demo_video_url":
			if value == "" {
				continue
			}
			u, e := url.Parse(value)
			if e != nil || u.Host == "" || u.User != nil {
				return fmt.Errorf("invalid URL for %s", key)
			}
			if key == "meeting_stream_url" {
				if u.Scheme != "ws" && u.Scheme != "wss" && u.Scheme != "https" && u.Scheme != "http" {
					return fmt.Errorf("invalid stream URL")
				}
			} else if u.Scheme != "https" && u.Scheme != "http" {
				return fmt.Errorf("invalid video URL")
			}
		case "notification", "whisper_base_prompt":
			if len(value) > 16384 {
				return fmt.Errorf("%s is too long", key)
			}
		default:
			if strings.HasPrefix(key, "quota_") || strings.HasPrefix(key, "ratelimit_") || key == "transform_input_tokens" || key == "command_max_output_tokens" {
				n, e := strconv.ParseInt(value, 10, 64)
				if e != nil || n <= 0 || n > 1_000_000_000 {
					return fmt.Errorf("invalid positive limit for %s", key)
				}
			} else {
				return fmt.Errorf("unsupported configuration key %s", key)
			}
		}
	}
	return nil
}
