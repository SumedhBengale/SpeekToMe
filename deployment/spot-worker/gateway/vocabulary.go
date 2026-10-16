package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// speechContext is the native NeMo per-request decoder bias, not text replacement.
type speechContext struct {
	Phrases []string `json:"phrases"`
	Boost   float64  `json:"boost"`
}

func validateContexts(contexts []speechContext) error {
	count, size := 0, 0
	for _, c := range contexts {
		if math.IsNaN(c.Boost) || math.IsInf(c.Boost, 0) || c.Boost < 0 || c.Boost > 5 {
			return fmt.Errorf("vocabulary boost must be between 0 and 5")
		}
		for _, phrase := range c.Phrases {
			count++
			size += len(phrase)
			if strings.TrimSpace(phrase) == "" || len(phrase) > 1024 {
				return fmt.Errorf("vocabulary phrases must contain 1 to 1024 bytes")
			}
		}
	}
	if count > 512 || size > 32768 {
		return fmt.Errorf("vocabulary exceeds 512 phrases or 32768 bytes")
	}
	return nil
}

// Prefer native JSON contexts; support the boosted_words query sent by the API.
func queryContexts(q url.Values) ([]speechContext, error) {
	var contexts []speechContext
	if raw, ok := q["speech_contexts"]; ok {
		if len(raw) != 1 || json.Unmarshal([]byte(raw[0]), &contexts) != nil {
			return nil, fmt.Errorf("speech_contexts must be a JSON array")
		}
	} else if raw := q.Get("boosted_words"); raw != "" {
		boost := 1.5
		if value := q.Get("vocabulary_boost"); value != "" {
			var err error
			boost, err = strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid vocabulary_boost")
			}
		}
		var phrases []string
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				phrases = append(phrases, p)
			}
		}
		contexts = []speechContext{{Phrases: phrases, Boost: boost}}
	}
	return contexts, validateContexts(contexts)
}

// Keep vocabulary on subsequent config frames, which otherwise clear it in
// NeMo. An explicit speech_contexts array replaces it, including [] to disable.
func vocabularyControl(data []byte, current *[]speechContext) ([]byte, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(data, &event) != nil {
		return data, nil
	}
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	if kind != "session.update" && kind != "config" && kind != "start" {
		return data, nil
	}
	session := event
	if raw, ok := event["session"]; ok {
		if json.Unmarshal(raw, &session) != nil || session == nil {
			return nil, fmt.Errorf("invalid session")
		}
	}
	if raw, ok := session["speech_contexts"]; ok {
		var contexts []speechContext
		if json.Unmarshal(raw, &contexts) != nil {
			return nil, fmt.Errorf("invalid speech_contexts")
		}
		if err := validateContexts(contexts); err != nil {
			return nil, err
		}
		*current = contexts
	}
	if len(*current) > 0 {
		session["speech_contexts"], _ = json.Marshal(*current)
	}
	// Legacy start is a session configuration, not a native NeMo event.
	if kind == "start" {
		kind = "session.update"
	}
	if _, nested := event["session"]; nested {
		event["session"], _ = json.Marshal(session)
	} else {
		delete(session, "type")
		event = map[string]json.RawMessage{}
		event["session"], _ = json.Marshal(session)
	}
	event["type"], _ = json.Marshal(kind)
	return json.Marshal(event)
}
