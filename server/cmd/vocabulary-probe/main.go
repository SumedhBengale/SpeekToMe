// A bounded manual probe. No credential is included in its output.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"speektome-server/vocabularydecision"
	"speektome/vocabulary"
	"time"
)

func main() {
	live := flag.Bool("live", false, "evaluate with Jev; at most 12 requests")
	input := flag.String("input", "", "JSON array of {raw_transcript,terms}")
	rounds := flag.Int("rounds", 1, "1 or 2 rounds")
	flag.Parse()
	if *rounds < 1 || *rounds > 2 {
		panic("rounds must be 1 or 2")
	}
	_ = godotenv.Load(".env")
	client := vocabularydecision.FromEnv()
	cases := []vocabulary.Request{{Raw: "Why don't you speak to me about the problem?", Terms: []string{"SpeekToMe"}}, {Raw: "We installed speak to me on the laptop to dictate emails.", Terms: []string{"SpeekToMe"}}, {Raw: "We are using postgrsq as our database.", Terms: []string{"PostgreSQL"}}, {Raw: "Please read this page before the meeting.", Terms: []string{"Redis"}}, {Raw: "We use rediss to cache database queries.", Terms: []string{"Redis"}}, {Raw: "Why don't you speak to me? We installed speak to me on our laptop. We use postgrsq as our database.", Terms: []string{"SpeekToMe", "PostgreSQL"}}}
	if *input != "" {
		b, e := os.ReadFile(*input)
		if e != nil {
			panic(e)
		}
		if e = json.Unmarshal(b, &cases); e != nil {
			panic(e)
		}
	}
	if len(cases)**rounds > 12 {
		panic("maximum 12 probe requests")
	}
	tokens := 0
	for round := 0; round < *rounds; round++ {
		for i, c := range cases {
			v, e := vocabulary.Compile(c.Terms)
			if e != nil {
				panic(e)
			}
			start := time.Now()
			r := vocabulary.Propose(context.Background(), c.Raw, v, vocabulary.TermsHash(c.Terms))
			r.MatchMillis = float64(time.Since(start).Microseconds()) / 1000
			if !*live {
				json.NewEncoder(os.Stdout).Encode(r)
				continue
			}
			a := client.Evaluate(context.Background(), r, "probe")
			tokens += a.InputTokens
			json.NewEncoder(os.Stdout).Encode(map[string]any{"case": i, "round": round, "audit": a})
			if tokens > 50000 {
				fmt.Fprintln(os.Stderr, "probe input token cap reached")
				return
			}
		}
	}
}
