use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};
use sherpa_onnx::{OnlinePunctuation, OnlinePunctuationConfig, OnlinePunctuationModelConfig};

static PUNCT_ENGINE: OnceLock<Arc<Mutex<Option<OnlinePunctuation>>>> = OnceLock::new();

fn get_model_paths() -> (PathBuf, PathBuf) {
    let mut model_path = PathBuf::from("models/punct-en/model.int8.onnx");
    let mut vocab_path = PathBuf::from("models/punct-en/bpe.vocab");

    if !model_path.exists() {
        if let Ok(exe_dir) = std::env::current_exe().map(|p| p.parent().unwrap_or(Path::new("")).to_path_buf()) {
            let alt_model = exe_dir.join("models/punct-en/model.int8.onnx");
            let alt_vocab = exe_dir.join("models/punct-en/bpe.vocab");
            if alt_model.exists() {
                model_path = alt_model;
                vocab_path = alt_vocab;
            }
        }
    }

    (model_path, vocab_path)
}

pub fn get_punct_engine() -> Arc<Mutex<Option<OnlinePunctuation>>> {
    PUNCT_ENGINE.get_or_init(|| {
        Arc::new(Mutex::new(init_punct_engine()))
    }).clone()
}

fn init_punct_engine() -> Option<OnlinePunctuation> {
    let (model_path, vocab_path) = get_model_paths();
    if !model_path.exists() || !vocab_path.exists() {
        eprintln!("[PUNCT WARN] Neural punctuation model files not found: {:?}, {:?}", model_path, vocab_path);
        return None;
    }

    let config = OnlinePunctuationConfig {
        model: OnlinePunctuationModelConfig {
            cnn_bilstm: Some(model_path.to_string_lossy().to_string()),
            bpe_vocab: Some(vocab_path.to_string_lossy().to_string()),
            num_threads: 1,
            provider: Some("cpu".to_string()),
            debug: false,
        },
    };

    let t_load = std::time::Instant::now();
    match OnlinePunctuation::create(&config) {
        Some(punct) => {
            let load_dur = t_load.elapsed();
            println!("[CLIENT BENCHMARK] Neural Punctuation model loaded into RAM in {:.2?} (~7.2 MB)", load_dur);
            Some(punct)
        }
        None => {
            eprintln!("[PUNCT ERROR] Failed to initialize neural punctuation model with config");
            None
        }
    }
}

/// Pre-warms the neural punctuation model asynchronously in memory
pub fn prewarm_punct_engine() {
    tauri::async_runtime::spawn_blocking(|| {
        let _ = get_punct_engine();
    });
}

/// Restores punctuation and capitalization on raw or semi-punctuated transcript text.
pub fn restore_punctuation(text: &str) -> String {
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return String::new();
    }

    let engine_lock = get_punct_engine();
    let guard = engine_lock.lock().unwrap_or_else(|e| e.into_inner());

    if let Some(ref punct) = *guard {
        // Strip trailing premature periods from in-flight text
        let clean_input = trimmed.trim_end_matches('.');
        let t_infer = std::time::Instant::now();
        if let Some(punctuated) = punct.add_punctuation(clean_input) {
            let infer_dur = t_infer.elapsed();
            let res = punctuated.trim();
            if !res.is_empty() {
                println!("[CLIENT BENCHMARK] Neural Punctuation inference: {:.2?} (in: {} chars -> out: {} chars)",
                    infer_dur, clean_input.len(), res.len());
                return res.to_string();
            }
        }
    }

    // Fallback: return original trimmed text
    trimmed.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_neural_punctuation_restoration() {
        let (model_path, _) = get_model_paths();
        if !model_path.exists() {
            println!("Model not found at {:?}, skipping test", model_path);
            return;
        }

        let input = "welcome to the live test of speak to me voice dictation should feel natural as i speak the streaming engine captures each phrase in real time and automatically organizes my thoughts into clean polished sentences";
        let result = restore_punctuation(input);
        println!("Input:  {}", input);
        println!("Output: {}", result);
        assert!(!result.is_empty());
        assert!(result.contains('.'));
    }
}
