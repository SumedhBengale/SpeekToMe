use std::sync::RwLock;
use serde::{Serialize, Deserialize};
use super::matcher::{self, CompiledVocabulary};
use super::engine::{VocabularyEngine, StandardVocabularyEngine, StreamVocabState};

#[derive(Serialize, Deserialize, Debug, Clone)]
struct CachedVocabFile {
    terms: Vec<String>,
}

pub struct VocabManager {
    raw_terms: RwLock<Vec<String>>,
    compiled_vocab: RwLock<CompiledVocabulary>,
    engine: Box<dyn VocabularyEngine>,
}

impl Default for VocabManager {
    fn default() -> Self {
        Self::new()
    }
}

impl VocabManager {
    pub fn new() -> Self {
        let manager = Self {
            raw_terms: RwLock::new(Vec::new()),
            compiled_vocab: RwLock::new(CompiledVocabulary::default()),
            engine: Box::new(StandardVocabularyEngine::default()),
        };
        manager.load_from_local_cache();
        manager
    }

    /// Sets a custom vocabulary engine (for future advanced ML/neural engines).
    pub fn set_engine(&mut self, engine: Box<dyn VocabularyEngine>) {
        self.engine = engine;
    }

    /// Returns the currently configured terms.
    pub fn get_terms(&self) -> Vec<String> {
        self.raw_terms.read().unwrap_or_else(|e| e.into_inner()).clone()
    }

    /// Replaces the active vocabulary terms and re-compiles the lookup indexes.
    pub fn set_terms(&self, terms: Vec<String>) {
        let compiled = matcher::compile_vocabulary(&terms);
        {
            let mut raw_lock = self.raw_terms.write().unwrap_or_else(|e| e.into_inner());
            *raw_lock = terms;
        }
        {
            let mut compiled_lock = self.compiled_vocab.write().unwrap_or_else(|e| e.into_inner());
            *compiled_lock = compiled;
        }
        self.save_to_local_cache();
    }

    /// Adds a single term and immediately recompiles the vocabulary.
    pub fn add_term(&self, term: &str) {
        let trimmed = term.trim().to_string();
        if trimmed.is_empty() {
            return;
        }

        let mut terms = self.get_terms();
        if !terms.iter().any(|t| t.eq_ignore_ascii_case(&trimmed)) {
            terms.push(trimmed);
            self.set_terms(terms);
        }
    }

    /// Deletes a single term and immediately recompiles the vocabulary.
    pub fn delete_term(&self, term: &str) {
        let trimmed = term.trim();
        let mut terms = self.get_terms();
        terms.retain(|t| !t.eq_ignore_ascii_case(trimmed));
        self.set_terms(terms);
    }

    /// Normalizes complete text through the active vocabulary engine.
    pub fn process_text(&self, text: &str) -> String {
        let vocab_guard = self.compiled_vocab.read().unwrap_or_else(|e| e.into_inner());
        self.engine.process_text(text, &vocab_guard)
    }

    /// Processes an incoming streaming transcript chunk through the active vocabulary engine.
    pub fn process_stream_chunk(&self, chunk: &str, state: &mut StreamVocabState) -> String {
        let vocab_guard = self.compiled_vocab.read().unwrap_or_else(|e| e.into_inner());
        self.engine.process_stream_chunk(chunk, &vocab_guard, state)
    }

    /// Synchronizes vocabulary terms from the backend server asynchronously.
    pub async fn sync_from_server(&self, api_url: &str, token: &str) -> Result<usize, String> {
        let terms = crate::api::fetch_vocabulary(api_url, token).await?;
        let canonical_list: Vec<String> = terms.into_iter().map(|t| t.canonical).collect();
        let count = canonical_list.len();
        self.set_terms(canonical_list);
        println!("[VOCAB] Successfully synchronized {} vocabulary terms from server.", count);
        Ok(count)
    }

    fn get_cache_path() -> std::path::PathBuf {
        let mut path = crate::config::get_config_path();
        path.set_file_name("vocabulary_cache.json");
        path
    }

    fn save_to_local_cache(&self) {
        let path = Self::get_cache_path();
        let terms = self.get_terms();
        let cache_obj = CachedVocabFile { terms };
        if let Ok(data) = serde_json::to_string_pretty(&cache_obj) {
            let _ = std::fs::write(path, data);
        }
    }

    fn load_from_local_cache(&self) {
        let path = Self::get_cache_path();
        if let Ok(data) = std::fs::read_to_string(&path) {
            if let Ok(cache_obj) = serde_json::from_str::<CachedVocabFile>(&data) {
                if !cache_obj.terms.is_empty() {
                    let compiled = matcher::compile_vocabulary(&cache_obj.terms);
                    let mut raw_lock = self.raw_terms.write().unwrap_or_else(|e| e.into_inner());
                    *raw_lock = cache_obj.terms;
                    let mut compiled_lock = self.compiled_vocab.write().unwrap_or_else(|e| e.into_inner());
                    *compiled_lock = compiled;
                }
            }
        }
    }
}
