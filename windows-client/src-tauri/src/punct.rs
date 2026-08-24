use std::path::{Path, PathBuf};
use sherpa_onnx::{OnlinePunctuation, OnlinePunctuationConfig, OnlinePunctuationModelConfig};

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

fn create_punct_instance() -> Option<OnlinePunctuation> {
    let (model_path, vocab_path) = get_model_paths();
    if !model_path.exists() || !vocab_path.exists() {
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

    OnlinePunctuation::create(&config)
}

/// Pre-warms the neural punctuation model asynchronously in memory
pub fn prewarm_punct_engine() {
    tauri::async_runtime::spawn_blocking(|| {
        let _ = create_punct_instance();
    });
}

/// Cleans duplicate punctuation marks (e.g. `,,`, `..`, `,.`)
fn sanitize_punctuation(s: &str) -> String {
    let mut out = s.to_string();
    while out.contains(",,") {
        out = out.replace(",,", ",");
    }
    while out.contains("..") {
        out = out.replace("..", ".");
    }
    while out.contains(",.") {
        out = out.replace(",.", ".");
    }
    while out.contains(".,") {
        out = out.replace(".,", ".");
    }
    while out.contains("  ") {
        out = out.replace("  ", " ");
    }
    out
}

/// Restores punctuation and capitalization on raw or semi-punctuated transcript text.
pub fn restore_punctuation(text: &str) -> String {
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return String::new();
    }

    let t_start = std::time::Instant::now();
    if let Some(punct) = create_punct_instance() {
        // Strip trailing premature periods from in-flight phrases so the neural model can link clauses
        let clean_input = trimmed.trim_end_matches('.');
        if let Some(punctuated) = punct.add_punctuation(clean_input) {
            let res = sanitize_punctuation(punctuated.trim());
            if !res.is_empty() {
                let dur = t_start.elapsed();
                println!("[CLIENT BENCHMARK] Neural Punctuation inference: {:.2?} (in: {} chars -> out: {} chars)",
                    dur, clean_input.len(), res.len());
                return res;
            }
        }
    }

    sanitize_punctuation(trimmed)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_stateless_punctuation() {
        let (model_path, _) = get_model_paths();
        if !model_path.exists() {
            return;
        }

        let input = "welcome to the live test of speak to me voice dictation should feel natural as i speak the streaming engine captures each phrase in real time and automatically organizes my thoughts into clean polished sentences";
        let result = restore_punctuation(input);
        println!("Input:  {}", input);
        println!("Output: {}", result);
        assert!(!result.is_empty());
        assert!(result.contains('.'));
        assert!(!result.contains(",,"));
        assert!(!result.contains(".."));
    }
}
