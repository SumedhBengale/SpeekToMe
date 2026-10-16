// vocab-eval runs the production text matcher on isolated benchmark transcripts.
// It never opens the database or touches user accounts.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"speektome-server/db"
	"speektome-server/vocab"
	"time"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	cache := vocab.NewVocabCache()
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID      string            `json:"id"`
			Text    string            `json:"text"`
			Terms   []string          `json:"terms"`
			Mode    string            `json:"mode"`
			Aliases map[string]string `json:"aliases"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		terms := make([]db.VocabularyTerm, 0, len(req.Terms))
		for _, t := range req.Terms {
			terms = append(terms, db.VocabularyTerm{Canonical: t})
		}
		cache.Clear()
		var text string
		var replaced []string
		var elapsed time.Duration
		switch req.Mode {
		case "", "legacy":
			text, replaced, elapsed = cache.ProcessText(1, req.Text, terms)
		case "exact", "guarded":
			n, err := vocab.NewConservativeNormalizer(terms, req.Aliases, req.Mode == "guarded")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			start := time.Now()
			text, replaced = n.Normalize(req.Text)
			elapsed = time.Since(start)
		default:
			fmt.Fprintln(os.Stderr, "unknown matcher mode:", req.Mode)
			os.Exit(1)
		}
		enc.Encode(map[string]any{"id": req.ID, "mode": req.Mode, "text": text, "replaced": replaced, "duration_us": elapsed.Microseconds()})
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
