import init, { is_speech, resample_to_16k_mono } from './wasm/speektome_core.js';

let wasmReady = false;

// Initialize the Rust WASM Core
async function loadWasm() {
  try {
    await init(chrome.runtime.getURL('wasm/speektome_core_bg.wasm'));
    wasmReady = true;
    console.log("[BACKGROUND] WebAssembly Core loaded successfully.");
  } catch (err) {
    console.error("[BACKGROUND] Failed to load WebAssembly Core:", err);
  }
}
loadWasm();

// ==========================================
// Extension Storage Config Accessors
// ==========================================
const DEFAULT_CONFIG = {
  api_url: "https://api.speekto.me",
  hotkey: "Ctrl+Shift+Y",
  model: "whisper-large-v3-turbo",
  enhance: false,
  typing_mode: false,
  sound_feedback: true,
  smart_spacing: true,
  custom_prompt: "",
  session_token: null,
  autostart: false,
  theme: "forest"
};

async function getConfig() {
  return new Promise((resolve) => {
    chrome.storage.local.get("config", (data) => {
      resolve(data.config || { ...DEFAULT_CONFIG });
    });
  });
}

async function saveConfig(config) {
  return new Promise((resolve) => {
    chrome.storage.local.set({ config }, resolve);
  });
}

// ==========================================
// Offscreen Capture Document Management
// ==========================================
async function createOffscreenDocument() {
  if (await chrome.offscreen.hasDocument()) return;
  await chrome.offscreen.createDocument({
    url: 'offscreen.html',
    reasons: [chrome.offscreen.Reason.USER_MEDIA],
    justification: 'Capture microphone and tab audio streams for dictation and meetings.'
  });
}

async function closeOffscreenDocument() {
  if (await chrome.offscreen.hasDocument()) {
    await chrome.offscreen.closeDocument();
  }
}

// ==========================================
// OAuth Authentication Flow
// ==========================================
async function triggerOAuthLogin() {
  const config = await getConfig();
  const apiUrl = config.api_url || "https://api.speekto.me";
  const clientState = Math.random().toString(36).substring(7);
  
  const redirectUrl = chrome.identity.getRedirectURL();
  const authUrl = `${apiUrl}/auth/login?redirect_uri=${encodeURIComponent(redirectUrl)}&client_state=${clientState}`;
  
  chrome.identity.launchWebAuthFlow({
    url: authUrl,
    interactive: true
  }, async (responseUrl) => {
    if (chrome.runtime.lastError || !responseUrl) {
      console.error("[OAUTH] Authorization failed:", chrome.runtime.lastError);
      chrome.runtime.sendMessage({ event: "login-failed", payload: "Authorization failed" });
      return;
    }
    
    const url = new URL(responseUrl);
    const token = url.searchParams.get("token");
    if (token) {
      config.session_token = token;
      await saveConfig(config);
      
      const userInfo = await fetchUserInfo(token);
      chrome.runtime.sendMessage({ event: "login-success", payload: userInfo });
    } else {
      chrome.runtime.sendMessage({ event: "login-failed", payload: "No token returned" });
    }
  });
}

async function fetchUserInfo(token) {
  const config = await getConfig();
  const apiUrl = config.api_url || "https://api.speekto.me";
  
  const res = await fetch(`${apiUrl}/auth/me`, {
    headers: { "Authorization": `Bearer ${token}` }
  });
  if (res.ok) {
    return await res.json();
  }
  return null;
}

// ==========================================
// REST Web API Client Interceptors
// ==========================================
async function handleCredentialsLogin(email, password) {
  const config = await getConfig();
  const apiUrl = config.api_url || "https://api.speekto.me";
  
  const res = await fetch(`${apiUrl}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password })
  });
  
  if (res.ok) {
    const data = await res.json();
    if (data.require_2fa) {
      throw new Error("Admin accounts are not permitted to sign in via Browser Extension. Please use the Web Dashboard.");
    }
    config.session_token = data.token;
    await saveConfig(config);
    const userInfo = await fetchUserInfo(data.token);
    return { token: data.token, user_info: userInfo };
  } else {
    throw new Error(await res.text() || "Login failed");
  }
}

async function handleCredentialsSignup(email, password, name) {
  const config = await getConfig();
  const apiUrl = config.api_url || "https://api.speekto.me";
  
  const res = await fetch(`${apiUrl}/auth/signup`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password, name })
  });
  
  if (res.ok) {
    const data = await res.json();
    return data.message || "Account created successfully.";
  } else {
    throw new Error(await res.text() || "Signup failed");
  }
}

// ==========================================
// Main Messages Route Router
// ==========================================
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  // Process asynchronous requests inside helper wrapper
  const processMessage = async () => {
    switch (message.action) {
      case 'get_api_url':
        const cfg = await getConfig();
        return cfg.api_url || "https://api.speekto.me";

      case 'get_config':
        return await getConfig();

      case 'save_config':
        await saveConfig(message.config);
        return { success: true };

      case 'get_user_info':
        const activeCfg = await getConfig();
        if (activeCfg.session_token) {
          return await fetchUserInfo(activeCfg.session_token);
        }
        return null;

      case 'start_login':
        triggerOAuthLogin();
        return { success: true };

      case 'logout':
        const tokenCfg = await getConfig();
        if (tokenCfg.session_token) {
          const apiUrl = tokenCfg.api_url || "https://api.speekto.me";
          await fetch(`${apiUrl}/auth/logout`, {
            method: "POST",
            headers: { "Authorization": `Bearer ${tokenCfg.session_token}` }
          });
        }
        tokenCfg.session_token = null;
        await saveConfig(tokenCfg);
        return { success: true };

      case 'login_with_credentials':
        try {
          const result = await handleCredentialsLogin(message.email, message.password);
          return result;
        } catch (err) {
          return { error: err.message };
        }

      case 'signup_with_credentials':
        try {
          const msg = await handleCredentialsSignup(message.email, message.password, message.name);
          return msg;
        } catch (err) {
          return { error: err.message };
        }

      case 'validate_prompt':
        // Safe check using mock for testing, or standard REST validation endpoint
        const valConfig = await getConfig();
        const apiUrl = valConfig.api_url || "https://api.speekto.me";
        if (valConfig.session_token) {
          const res = await fetch(`${apiUrl}/validate-prompt`, {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "Authorization": `Bearer ${valConfig.session_token}`
            },
            body: JSON.stringify({ prompt: message.prompt })
          });
          if (res.ok) {
            return await res.json();
          }
        }
        return { safe: true };

      // Meeting and tab record mappings
      case 'start_meeting':
        await createOffscreenDocument();
        return await chrome.runtime.sendMessage({ target: 'offscreen', action: 'start_meeting' });

      case 'add_tab_stream':
        // Retrieve media stream ID for target tab to tunnel it into offscreen frame
        const streamId = await new Promise((resolve) => {
          chrome.tabCapture.getMediaStreamId({ targetTabId: message.tabId }, resolve);
        });
        return await chrome.runtime.sendMessage({
          target: 'offscreen',
          action: 'add_tab_stream',
          tabId: message.tabId,
          streamId: streamId
        });

      case 'remove_tab_stream':
        return await chrome.runtime.sendMessage({
          target: 'offscreen',
          action: 'remove_tab_stream',
          tabId: message.tabId
        });

      case 'stop_meeting':
        const stopRes = await chrome.runtime.sendMessage({ target: 'offscreen', action: 'stop_meeting' });
        await closeOffscreenDocument();
        return stopRes;
    }
  };

  processMessage().then(sendResponse);
  return true; // Keep message channel open for async response
});

// ==========================================
// Dictation & Speech Detection Event Hooks
// ==========================================
let isRecording = false;

// Trigger dictation toggle from keyboard shortcuts
chrome.commands.onCommand.addListener(async (command) => {
  if (command === "toggle_recording") {
    if (!isRecording) {
      await startDictationRecording();
    } else {
      await stopDictationRecording();
    }
  }
});

async function startDictationRecording() {
  const config = await getConfig();
  if (!config.session_token) {
    console.warn("[BACKGROUND] User is not logged in. Cannot record.");
    return;
  }

  isRecording = true;
  await createOffscreenDocument();
  await chrome.runtime.sendMessage({ target: 'offscreen', action: 'start_dictation' });
  console.log("[BACKGROUND] Dictation recording started.");
}

async function stopDictationRecording() {
  isRecording = false;
  const response = await chrome.runtime.sendMessage({ target: 'offscreen', action: 'stop_dictation' });
  await closeOffscreenDocument();

  if (response && response.success && response.samples) {
    // Run VAD directly inside compiled WASM core!
    const samplesArr = Int16Array.from(response.samples);
    const hasSpeech = is_speech(samplesArr, response.sampleRate, response.channels);
    
    console.log(`[BACKGROUND] Recording stopped. Speech detected: ${hasSpeech}`);
    if (hasSpeech) {
      // Resample PCM to 16kHz inside WASM core!
      const resampled = resample_to_16k_mono(samplesArr, response.sampleRate, response.channels);
      
      // Perform transcription API request
      const config = await getConfig();
      const apiUrl = config.api_url || "https://api.speekto.me";
      
      // Simple raw binary upload or base64 upload, depending on server configurations
      // Convert resampled PCM Int16 into standard WAV byte array for server processing
      const wavBytes = writeWavHeader(resampled, 16000, 1);
      
      const formData = new FormData();
      const blob = new Blob([wavBytes], { type: 'audio/wav' });
      formData.append('file', blob, 'recording.wav');
      formData.append('model', config.model);
      formData.append('enhance', config.enhance.toString());
      formData.append('prompt', config.custom_prompt);

      const res = await fetch(`${apiUrl}/generate`, {
        method: 'POST',
        headers: { "Authorization": `Bearer ${config.session_token}` },
        body: formData
      });

      if (res.ok) {
        const data = await res.json();
        if (data.text) {
          // Send transcription to content script to paste into current text box
          const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
          if (tab) {
            chrome.tabs.sendMessage(tab.id, {
              action: "insert_transcription",
              text: data.text,
              typingMode: config.typing_mode
            });
          }
        }
      }
    }
  }
}

// Lightweight standard WAV header writer helper
function writeWavHeader(samples, sampleRate, numChannels) {
  const buffer = new ArrayBuffer(44 + samples.length * 2);
  const view = new DataView(buffer);

  // RIFF identifier
  writeString(view, 0, 'RIFF');
  view.setUint32(4, 36 + samples.length * 2, true);
  writeString(view, 8, 'WAVE');

  // Format chunk
  writeString(view, 12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true); // PCM Format
  view.setUint16(22, numChannels, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * numChannels * 2, true);
  view.setUint16(32, numChannels * 2, true);
  view.setUint16(34, 16, true); // Bits per sample

  // Data chunk
  writeString(view, 36, 'data');
  view.setUint32(40, samples.length * 2, true);

  // Write samples
  let offset = 44;
  for (let i = 0; i < samples.length; i++, offset += 2) {
    view.setInt16(offset, samples[i], true);
  }

  return new Uint8Array(buffer);
}

function writeString(view, offset, string) {
  for (let i = 0; i < string.length; i++) {
    view.setUint8(offset + i, string.charCodeAt(i));
  }
}
