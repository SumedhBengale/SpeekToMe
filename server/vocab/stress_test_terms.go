package vocab

import (
	"fmt"
	"speektome-server/db"
)

// Generate1000TestTerms returns 1000 distinct single-word and multi-word vocabulary terms
// for stress testing the compiled trie / map / fuzzy matching engine.
func Generate1000TestTerms() []db.VocabularyTerm {
	terms := make([]db.VocabularyTerm, 0, 1000)

	// Real-world anchor terms
	anchors := []string{
		"PostgreSQL", "Kubernetes", "SpeekTo.Me", "TensorRT-LLM", "Cerebras CS-3",
		"FastAPI", "tRPC", "GraphQL", "WebAssembly", "Bioavailability",
		"Electrocardiography", "Microservices Architecture", "OAuth2 Provider",
		"Zero-Knowledge Proof", "NeMo-Speech", "DeepSeek", "PyTorch", "HuggingFace",
		"Scikit-Learn", "Transformer Engine", "FlashAttention", "RoPE Embeddings",
		"Quantization-Aware Training", "Low-Rank Adaptation", "Retrieval-Augmented Generation",
	}
	for _, a := range anchors {
		terms = append(terms, db.VocabularyTerm{
			Canonical:  a,
			Normalized: NormalizeString(a),
		})
	}

	// Synthetic diverse domain entities to reach 1000
	prefixes := []string{"Quantum", "Neuro", "Bio", "Cyber", "Aero", "Crypto", "Nano", "Hyper", "Meta", "Omni"}
	roots := []string{"Compute", "Synth", "Flux", "Core", "Node", "Matrix", "Pulse", "Wave", "Link", "Grid"}
	suffixes := []string{"Engine", "Protocol", "Framework", "Cluster", "Pipeline", "Optimizer", "Processor", "Module", "Vector", "Index"}

	count := len(terms)
	for _, p := range prefixes {
		for _, r := range roots {
			for _, s := range suffixes {
				if count >= 1000 {
					break
				}
				term := fmt.Sprintf("%s%s %s", p, r, s)
				terms = append(terms, db.VocabularyTerm{
					Canonical:  term,
					Normalized: NormalizeString(term),
				})
				count++
			}
		}
	}

	for i := count; i < 1000; i++ {
		term := fmt.Sprintf("DomainEntity%04d", i)
		terms = append(terms, db.VocabularyTerm{
			Canonical:  term,
			Normalized: NormalizeString(term),
		})
	}

	return terms
}
