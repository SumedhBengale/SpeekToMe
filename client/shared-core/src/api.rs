use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct UserInfo {
    pub name: String,
    pub email: String,
    pub plan_tier: String,
    pub plan_active: bool,
    pub plan_activated_at: String,
    pub duration_seconds: f64,
    pub monthly_limit_seconds: f64,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct ClientVocabularyTerm {
    #[serde(rename = "canonical_term")]
    pub canonical: String,
    #[serde(rename = "normalized_term")]
    pub normalized: String,
}

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct PromptValidationResult {
    pub safe: bool,
    pub explanation: Option<String>,
}

#[derive(Deserialize)]
struct TranscriptResponse {
    text: String,
}

#[derive(Deserialize)]
struct AuthResponse {
    token: String,
}

fn create_client() -> reqwest::Client {
    let mut builder = reqwest::Client::builder();
    #[cfg(not(target_arch = "wasm32"))]
    {
        builder = builder.timeout(std::time::Duration::from_secs(120));
    }
    builder.build().unwrap_or_else(|_| reqwest::Client::new())
}

pub async fn fetch_user_info(api_url: &str, token: &str) -> Result<UserInfo, String> {
    let client = create_client();
    let res = client.get(format!("{}/auth/me", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    if res.status().is_success() {
        res.json::<UserInfo>().await.map_err(|e| format!("Failed to parse user info: {}", e))
    } else {
        let err_text = res.text().await.unwrap_or_default();
        Err(format!("Server error: {}", err_text))
    }
}

pub async fn login_with_credentials(api_url: &str, email: &str, password: &str) -> Result<(String, UserInfo), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);

    let res = client.post(format!("{}/auth/login", api_url))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&body_text) {
            if v.get("require_2fa").and_then(|b| b.as_bool()).unwrap_or(false) {
                return Err("This account is not allowed to login.".to_string());
            }
        }
        let auth_res: AuthResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse login response: {}", e))?;
        let user_info = fetch_user_info(api_url, &auth_res.token).await?;
        Ok((auth_res.token, user_info))
    } else {
        let body_trimmed = body_text.trim().to_string();
        if !body_trimmed.is_empty() {
            Err(body_trimmed)
        } else {
            Err(format!("Login failed with status: {}", status))
        }
    }
}

pub async fn signup_with_credentials(api_url: &str, email: &str, password: &str, name: &str) -> Result<String, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("email", email);
    map.insert("password", password);
    map.insert("name", name);

    let res = client.post(format!("{}/auth/signup", api_url))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        if let Ok(json_res) = serde_json::from_str::<serde_json::Value>(&body_text) {
            if let Some(msg) = json_res.get("message").and_then(|v| v.as_str()) {
                return Ok(msg.to_string());
            }
        }
        Ok("Account created successfully.".to_string())
    } else {
        let body_trimmed = body_text.trim().to_string();
        if !body_trimmed.is_empty() {
            Err(body_trimmed)
        } else {
            Err(format!("Signup failed with status: {}", status))
        }
    }
}

pub async fn fetch_vocabulary(api_url: &str, token: &str) -> Result<Vec<ClientVocabularyTerm>, String> {
    let client = create_client();
    let res = client.get(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        serde_json::from_str::<Vec<ClientVocabularyTerm>>(&body_text)
            .map_err(|e| format!("Failed to parse vocabulary list: {}", e))
    } else {
        Err(format!("Failed to retrieve vocabulary: {}", body_text))
    }
}

pub async fn add_vocab_term(api_url: &str, token: &str, term: &str) -> Result<(), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    let res = client.post(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(())
    } else {
        Err(format!("Failed to add vocabulary term: {}", body_text))
    }
}

pub async fn delete_vocab_term(api_url: &str, token: &str, term: &str) -> Result<(), String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("canonical_term", term);

    let res = client.delete(format!("{}/auth/vocabulary", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(())
    } else {
        Err(format!("Failed to delete vocabulary term: {}", body_text))
    }
}

pub async fn validate_custom_prompt(api_url: &str, token: &str, prompt: &str) -> Result<PromptValidationResult, String> {
    let client = create_client();
    let mut map = std::collections::HashMap::new();
    map.insert("prompt", prompt);

    let res = client.post(format!("{}/validate-prompt", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .json(&map)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        serde_json::from_str::<PromptValidationResult>(&body_text)
            .map_err(|e| format!("Failed to parse validation result: {}", e))
    } else {
        Err(format!("Prompt validation failed: {}", body_text))
    }
}

pub async fn transcribe_audio(
    api_url: &str,
    token: &str,
    ogg_opus_bytes: Vec<u8>,
    model: &str,
    enhance: bool,
    custom_prompt: &str,
) -> Result<String, String> {
    let client = create_client();
    let part = reqwest::multipart::Part::bytes(ogg_opus_bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let form = reqwest::multipart::Form::new()
        .part("file", part)
        .text("model", model.to_string())
        .text("enhance", enhance.to_string())
        .text("prompt", custom_prompt.to_string());

    let res = client.post(format!("{}/generate", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        let resp: TranscriptResponse = serde_json::from_str(&body_text)
            .map_err(|e| format!("Failed to parse response: {}", e))?;
        Ok(resp.text)
    } else {
        Err(body_text)
    }
}

pub async fn upload_meeting(
    api_url: &str,
    token: &str,
    title: &str,
    mixed_bytes: Vec<u8>,
    mic_bytes: Option<Vec<u8>>,
    loopback_bytes: Option<Vec<u8>>,
) -> Result<String, String> {
    let client = create_client();
    let mixed_part = reqwest::multipart::Part::bytes(mixed_bytes)
        .file_name("recording.ogg")
        .mime_str("audio/ogg")
        .unwrap();

    let mut form = reqwest::multipart::Form::new()
        .part("file", mixed_part)
        .text("title", title.to_string());

    if let Some(mic) = mic_bytes {
        let mic_part = reqwest::multipart::Part::bytes(mic)
            .file_name("recording_mic.ogg")
            .mime_str("audio/ogg")
            .unwrap();
        form = form.part("file_mic", mic_part);
    }

    if let Some(loopback) = loopback_bytes {
        let lb_part = reqwest::multipart::Part::bytes(loopback)
            .file_name("recording_loopback.ogg")
            .mime_str("audio/ogg")
            .unwrap();
        form = form.part("file_loopback", lb_part);
    }

    let res = client.post(format!("{}/meetings", api_url))
        .header("Authorization", format!("Bearer {}", token))
        .multipart(form)
        .send()
        .await
        .map_err(|e| format!("Network error: {}", e))?;

    let status = res.status();
    let body_text = res.text().await.unwrap_or_default();
    if status.is_success() {
        Ok(body_text)
    } else {
        Err(format!("Upload failed with status {}: {}", status, body_text))
    }
}
