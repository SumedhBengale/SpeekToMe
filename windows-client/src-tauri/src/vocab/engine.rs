use super::matcher::{self, CompiledVocabulary};

/// StreamVocabState tracks uncommitted word tokens across streaming chunks
/// to ensure multi-word vocabulary terms spanning chunk boundaries are not split prematurely.
#[derive(Debug, Clone, Default)]
pub struct StreamVocabState {
    pub tail_buffer: Vec<String>,
    pub lookahead_words: usize,
}

impl StreamVocabState {
    pub fn new(lookahead_words: usize) -> Self {
        Self {
            tail_buffer: Vec::new(),
            lookahead_words: lookahead_words.max(3),
        }
    }

    pub fn reset(&mut self) {
        self.tail_buffer.clear();
    }

    /// Flushes any remaining uncommitted words at the end of the audio stream.
    pub fn flush(&mut self, engine: &dyn VocabularyEngine, vocab: &CompiledVocabulary) -> String {
        if self.tail_buffer.is_empty() {
            return String::new();
        }
        let remaining_text = self.tail_buffer.join(" ");
        self.tail_buffer.clear();
        engine.process_text(&remaining_text, vocab)
    }
}

/// Pluggable trait for text normalization and vocabulary correction engines.
pub trait VocabularyEngine: Send + Sync {
    fn name(&self) -> &'static str;

    /// Normalizes a complete transcript (batch request-response mode).
    fn process_text(&self, text: &str, vocab: &CompiledVocabulary) -> String;

    /// Incrementally normalizes streaming transcript chunks.
    /// Emits safely committed words while buffering uncommitted lookahead tokens.
    fn process_stream_chunk(
        &self,
        chunk: &str,
        vocab: &CompiledVocabulary,
        state: &mut StreamVocabState,
    ) -> String;
}

/// Standard 4-stage vocabulary engine implementation.
pub struct StandardVocabularyEngine;

impl Default for StandardVocabularyEngine {
    fn default() -> Self {
        Self
    }
}

impl VocabularyEngine for StandardVocabularyEngine {
    fn name(&self) -> &'static str {
        "Standard 4-Stage Phonetic Vocabulary Engine"
    }

    fn process_text(&self, text: &str, vocab: &CompiledVocabulary) -> String {
        matcher::normalize_with_vocabulary(text, vocab)
    }

    fn process_stream_chunk(
        &self,
        chunk: &str,
        vocab: &CompiledVocabulary,
        state: &mut StreamVocabState,
    ) -> String {
        let trimmed = chunk.trim();
        if trimmed.is_empty() {
            return String::new();
        }

        let new_words: Vec<String> = trimmed.split_whitespace().map(|s| s.to_string()).collect();
        state.tail_buffer.extend(new_words);

        let lookahead = state.lookahead_words.max(vocab.max_token_count + 1);

        if state.tail_buffer.len() > lookahead {
            let commit_count = state.tail_buffer.len() - lookahead;
            let to_commit: Vec<String> = state.tail_buffer.drain(0..commit_count).collect();
            let commit_text = to_commit.join(" ");
            self.process_text(&commit_text, vocab)
        } else {
            String::new()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::vocab::matcher::compile_vocabulary;

    #[test]
    fn test_streaming_chunk_processing_and_flush() {
        let terms = vec!["Google DeepMind".to_string(), "PostgreSQL".to_string()];
        let vocab = compile_vocabulary(&terms);
        let engine = StandardVocabularyEngine::default();
        let mut state = StreamVocabState::new(3);

        // Chunk 1: "We are using google" -> buffered (not enough lookahead yet)
        let out1 = engine.process_stream_chunk("We are using google", &vocab, &mut state);
        assert_eq!(out1, "");

        // Chunk 2: "deep mind database for" -> emits committed words, keeps lookahead
        let out2 = engine.process_stream_chunk("deep mind database for", &vocab, &mut state);
        assert!(!out2.is_empty());

        // End of stream -> flush
        let flushed = state.flush(&engine, &vocab);
        let full_output = format!("{} {}", out2, flushed).trim().to_string();

        assert!(full_output.contains("Google DeepMind"));
    }
}
