pub mod api;
pub mod audio_processing;
pub mod config;

#[cfg(feature = "wasm")]
use wasm_bindgen::prelude::*;

#[cfg(feature = "wasm")]
#[wasm_bindgen]
pub fn init_panic_hook() {
    console_error_panic_hook::set_once();
}

#[cfg(feature = "wasm")]
#[wasm_bindgen]
pub fn is_speech(samples: &[i16], sample_rate: u32, channels: u16) -> bool {
    audio_processing::is_speech(samples, sample_rate, channels)
}

#[cfg(feature = "wasm")]
#[wasm_bindgen]
pub fn resample_to_16k_mono(samples: &[i16], source_rate: u32, source_channels: u16) -> Vec<i16> {
    audio_processing::resample_to_16k_mono(samples, source_rate, source_channels)
}

// Expose async API client endpoints to JavaScript/WebExtension directly
#[cfg(feature = "wasm")]
#[wasm_bindgen]
pub async fn transcribe_audio_wasm(
    api_url: String,
    token: String,
    ogg_opus_bytes: Vec<u8>,
    model: String,
    enhance: bool,
    custom_prompt: String,
) -> Result<String, JsValue> {
    api::transcribe_audio(&api_url, &token, ogg_opus_bytes, &model, enhance, &custom_prompt)
        .await
        .map_err(|err| JsValue::from_str(&err))
}

#[cfg(feature = "wasm")]
#[wasm_bindgen]
pub async fn fetch_user_info_wasm(api_url: String, token: String) -> Result<JsValue, JsValue> {
    let info = api::fetch_user_info(&api_url, &token)
        .await
        .map_err(|err| JsValue::from_str(&err))?;
    serde_wasm_bindgen::to_value(&info).map_err(|err| JsValue::from_str(&err.to_string()))
}
