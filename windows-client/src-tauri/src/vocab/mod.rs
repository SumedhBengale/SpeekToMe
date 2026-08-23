pub mod matcher;
pub mod engine;
pub mod manager;

use std::sync::OnceLock;
pub use matcher::{CompiledVocabulary, CompiledTerm, Token};
pub use engine::{VocabularyEngine, StandardVocabularyEngine, StreamVocabState};
pub use manager::VocabManager;

static GLOBAL_VOCAB_MANAGER: OnceLock<VocabManager> = OnceLock::new();

/// Returns the global static VocabManager instance.
pub fn get_vocab_manager() -> &'static VocabManager {
    GLOBAL_VOCAB_MANAGER.get_or_init(VocabManager::new)
}
