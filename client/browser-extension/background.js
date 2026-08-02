let wasmReady = false;
let is_speech = null;
let resample_to_16k_mono = null;

// The browser service worker uses the JavaScript fallback for VAD/resampling.
// Dynamic import() is not supported from ServiceWorkerGlobalScope in Chrome,
// so do not attempt to load the desktop WASM module here.
async function loadWasm() {
  console.log('[BACKGROUND] Using browser JavaScript audio processing fallback.');
}
// Kept as a named hook for future bundling, but intentionally not invoked.

// ==========================================
// Extension Storage Config Accessors
// ==========================================
const DEFAULT_CONFIG = {
  api_url: "https://api.speekto.me",
  // Chrome owns the command binding. It can be customized at
  // chrome://extensions/shortcuts, rather than being handled by a page.
  hotkey: "Ctrl+Shift+Y",
  model: "whisper-large-v3-turbo",
  enhance: false,
  sound_feedback: true,
  smart_spacing: true,
  custom_prompt: "",
  session_token: null,
  theme: "forest"
};

const BROWSER_CONFIG_KEYS = [
  'api_url',
  'hotkey',
  'model',
  'enhance',
  'sound_feedback',
  'smart_spacing',
  'custom_prompt',
  'session_token',
  'theme'
];

function normalizeBrowserConfig(config = {}) {
  const merged = { ...DEFAULT_CONFIG, ...config };
  return BROWSER_CONFIG_KEYS.reduce((browserConfig, key) => {
    browserConfig[key] = merged[key];
    return browserConfig;
  }, {});
}

async function getConfig() {
  return new Promise((resolve) => {
    chrome.storage.local.get("config", (data) => {
      resolve(normalizeBrowserConfig(data.config));
    });
  });
}

async function saveConfig(config) {
  return new Promise((resolve) => {
    chrome.storage.local.set({ config: normalizeBrowserConfig(config) }, resolve);
  });
}

async function getVocabulary() {
  return new Promise((resolve) => {
    chrome.storage.local.get('vocabulary', (data) => resolve(data.vocabulary || []));
  });
}

async function addVocabularyTerm(term) {
  const value = String(term || '').trim();
  if (!value) return { success: false, error: 'Vocabulary term is empty.' };

  const vocabulary = await getVocabulary();
  if (!vocabulary.some((item) => item.canonical_term.toLowerCase() === value.toLowerCase())) {
    vocabulary.push({ canonical_term: value, normalized_term: value.toLowerCase() });
    await new Promise((resolve) => chrome.storage.local.set({ vocabulary }, resolve));
  }
  return { success: true };
}

async function deleteVocabularyTerm(term) {
  const value = String(term || '').trim().toLowerCase();
  const vocabulary = (await getVocabulary()).filter(
    (item) => item.canonical_term.toLowerCase() !== value
  );
  await new Promise((resolve) => chrome.storage.local.set({ vocabulary }, resolve));
  return { success: true };
}

// ==========================================
// Offscreen Capture Document Management
// ==========================================
const OFFSCREEN_READY_TIMEOUT_MS = 5000;
const OFFSCREEN_READY_RETRY_MS = 25;
let offscreenCreationPromise = null;
let dictationRequestSequence = 0;
const pendingDictationRequests = new Map();

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function createDictationRequestId() {
  dictationRequestSequence += 1;
  return `dictation-${Date.now()}-${dictationRequestSequence}`;
}

function waitForDictationEvent(requestId, timeoutMs = 15000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pendingDictationRequests.delete(requestId);
      reject(new Error('Timed out waiting for the offscreen recording result.'));
    }, timeoutMs);

    pendingDictationRequests.set(requestId, { resolve, reject, timer });
  });
}

function settleDictationRequest(requestId, error, value) {
  const pending = pendingDictationRequests.get(requestId);
  if (!pending) return;

  clearTimeout(pending.timer);
  pendingDictationRequests.delete(requestId);
  if (error) pending.reject(error);
  else pending.resolve(value);
}

async function waitForOffscreenReady() {
  const deadline = Date.now() + OFFSCREEN_READY_TIMEOUT_MS;
  let lastError = null;

  while (Date.now() < deadline) {
    if (!(await chrome.offscreen.hasDocument())) {
      lastError = new Error('The offscreen document closed before it became ready.');
      break;
    }

    try {
      // createDocument() only guarantees that the document exists. The ping
      // confirms that offscreen.js has been parsed and registered its listener.
      const response = await chrome.runtime.sendMessage({
        target: 'offscreen',
        action: 'ping'
      });
      if (response?.ready) return;
      lastError = new Error('The offscreen document returned an invalid readiness response.');
    } catch (err) {
      // A message sent during script startup can have no listener yet. Retry
      // until the document confirms readiness instead of relying on a delay.
      lastError = err;
    }

    await delay(OFFSCREEN_READY_RETRY_MS);
  }

  throw new Error(
    `Offscreen document did not become ready within ${OFFSCREEN_READY_TIMEOUT_MS} ms${
      lastError?.message ? `: ${lastError.message}` : ''
    }`
  );
}

async function createOffscreenDocument() {
  if (offscreenCreationPromise) return offscreenCreationPromise;

  offscreenCreationPromise = (async () => {
    if (!(await chrome.offscreen.hasDocument())) {
      await chrome.offscreen.createDocument({
        url: 'offscreen.html',
        reasons: [chrome.offscreen.Reason.USER_MEDIA],
        justification: 'Capture microphone and tab audio streams for dictation and meetings.'
      });
    }

    await waitForOffscreenReady();
  })();

  try {
    await offscreenCreationPromise;
  } finally {
    offscreenCreationPromise = null;
  }
}

async function sendMessageToOffscreen(message) {
  await createOffscreenDocument();
  return await chrome.runtime.sendMessage({ target: 'offscreen', ...message });
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
      throw new Error("This account is not allowed to login.");
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

const HANDLED_BACKGROUND_ACTIONS = new Set([
  'get_api_url',
  'get_config',
  'save_config',
  'get_dictation_shortcut',
  'get_pending_transcription',
  'clear_pending_transcription',
  'get_user_info',
  'start_login',
  'logout',
  'login_with_credentials',
  'signup_with_credentials',
  'validate_prompt',
  'get_vocabulary',
  'add_vocabulary_term',
  'delete_vocabulary_term',
  'start_meeting',
  'add_tab_stream',
  'remove_tab_stream',
  'stop_meeting',
  'offscreen_dictation_status',
  'dictation_audio_ready'
]);

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  // Ignore messages targeted specifically for offscreen document or unhandled actions
  if (message.target === 'offscreen' || !HANDLED_BACKGROUND_ACTIONS.has(message.action)) {
    return false;
  }

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

      case 'get_dictation_shortcut':
        return await getDictationShortcut();

      case 'get_pending_transcription':
        return await new Promise((resolve) => {
          chrome.storage.local.get('pending_transcription', (data) => {
            resolve(data.pending_transcription || null);
          });
        });

      case 'clear_pending_transcription':
        await new Promise((resolve) => chrome.storage.local.remove('pending_transcription', resolve));
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

      case 'get_vocabulary':
        return await getVocabulary();

      case 'add_vocabulary_term':
        return await addVocabularyTerm(message.term);

      case 'delete_vocabulary_term':
        return await deleteVocabularyTerm(message.term);

      // Meeting and tab record mappings
      case 'start_meeting':
        return await sendMessageToOffscreen({ action: 'start_meeting' });

      case 'add_tab_stream':
        await createOffscreenDocument();
        // Retrieve media stream ID for target tab to tunnel it into offscreen frame
        const streamId = await new Promise((resolve) => {
          chrome.tabCapture.getMediaStreamId({ targetTabId: message.tabId }, resolve);
        });
        return await sendMessageToOffscreen({
          action: 'add_tab_stream',
          tabId: message.tabId,
          streamId: streamId
        });

      case 'remove_tab_stream':
        return await sendMessageToOffscreen({ action: 'remove_tab_stream', tabId: message.tabId });

      case 'stop_meeting':
        const stopRes = await sendMessageToOffscreen({ action: 'stop_meeting' });
        await closeOffscreenDocument();
        return stopRes;

      case 'offscreen_dictation_status':
        if (message.status === 'started') {
          await updateDictationPresentation('recording');
        }
        if (message.status === 'failed') {
          isRecording = false;
          isProcessing = false;
          const error = new Error(message.error || 'Dictation failed in the offscreen document.');
          settleDictationRequest(message.requestId, error);
          await updateDictationPresentation('error', error.message);
          if (error.message === 'Permission dismissed' || error.message === 'Permission denied') {
            console.error(
              '[BACKGROUND] Microphone permission was dismissed or denied. Open the extension popup, '
              + 'click "Allow microphone", then retry dictation.'
            );
          } else {
            console.error('[BACKGROUND] Dictation failed in offscreen document:', error.message);
          }
        }
        return { success: true };

      case 'dictation_audio_ready':
        settleDictationRequest(message.requestId, null, message);
        return { success: true };
    }
  };

  processMessage()
    .then((res) => sendResponse(res || { success: true }))
    .catch((err) => sendResponse({ error: err.message }));
  return true; // Keep message channel open for async response
});

// ==========================================
// Dictation & Speech Detection Event Hooks
// ==========================================
let isRecording = false;
let isProcessing = false;
let dictationStartPromise = null;
let dictationTargetTabId = null;

const DICTATION_COMMAND = 'toggle-dictation';
// chrome.commands does not expose keyup. Some Chrome/OS combinations repeat
// onCommand while a shortcut is held, so re-arm only after repeat events have
// been quiet longer than the normal initial key-repeat delay.
const COMMAND_RELEASE_IDLE_MS = 700;
let commandPressLatched = false;
let commandReleaseTimer = null;

function claimCommandPress() {
  if (commandReleaseTimer) {
    clearTimeout(commandReleaseTimer);
  }

  const isNewPress = !commandPressLatched;
  commandPressLatched = true;
  commandReleaseTimer = setTimeout(() => {
    commandPressLatched = false;
    commandReleaseTimer = null;
  }, COMMAND_RELEASE_IDLE_MS);

  return isNewPress;
}

async function getDictationShortcut() {
  const commands = await chrome.commands.getAll();
  const command = commands.find(({ name }) => name === DICTATION_COMMAND);
  return {
    shortcut: command?.shortcut || '',
    configured: Boolean(command?.shortcut)
  };
}

async function getCurrentTabId() {
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  return tab?.id ?? null;
}

async function updateDictationPresentation(state, detail = '') {
  const presentation = {
    recording: { badge: 'REC', color: '#b3261e', title: 'SpeekTo.Me: recording' },
    processing: { badge: '…', color: '#40101d', title: 'SpeekTo.Me: processing dictation' },
    error: { badge: '!', color: '#7a1f1f', title: `SpeekTo.Me: ${detail || 'dictation error'}` },
    complete: { badge: '', color: '#40101d', title: 'SpeekTo.Me: dictation complete' },
    idle: { badge: '', color: '#40101d', title: 'SpeekTo.Me Settings' }
  }[state] || { badge: '', color: '#40101d', title: 'SpeekTo.Me Settings' };

  const actionOptions = dictationTargetTabId ? { tabId: dictationTargetTabId } : {};
  await Promise.allSettled([
    chrome.action.setBadgeText({ ...actionOptions, text: presentation.badge }),
    chrome.action.setBadgeBackgroundColor({ ...actionOptions, color: presentation.color }),
    chrome.action.setTitle({ ...actionOptions, title: presentation.title })
  ]);

  // The content script is optional: browser-controlled pages do not allow it.
  // The action badge remains the universal recording cue in those contexts.
  if (dictationTargetTabId) {
    try {
      await chrome.tabs.sendMessage(dictationTargetTabId, {
        action: 'dictation_status',
        state,
        detail
      });
    } catch (_) {
      // Expected for chrome:// pages, the Web Store, and pages that have not
      // finished loading. Do not make recording depend on page injection.
    }
  }
}

async function showFallbackNotification(message) {
  await chrome.notifications.create('dictation-fallback', {
    type: 'basic',
    iconUrl: 'assets/icons/logo.png',
    title: 'SpeekTo.Me dictation',
    message
  });
}

async function toggleDictationFromCommand(tab) {
  if (isProcessing) {
    await updateDictationPresentation('processing');
    return;
  }

  if (isRecording) {
    await stopDictationRecording();
  } else {
    await startDictationRecording(tab?.id ?? await getCurrentTabId());
  }
}

chrome.commands.onCommand.addListener((command, tab) => {
  if (command !== DICTATION_COMMAND) return;

  if (!claimCommandPress()) {
    console.debug(`[BACKGROUND] Ignoring repeated held command: ${command}`);
    return;
  }

  console.log(`[BACKGROUND] Chrome command received: ${command}`);
  toggleDictationFromCommand(tab).catch(async (err) => {
    console.error('[BACKGROUND] Dictation command failed:', err);
    isRecording = false;
    isProcessing = false;
    await updateDictationPresentation('error', err.message || 'Dictation unavailable');
  });
});

async function startDictationRecording(targetTabId = null) {
  if (dictationStartPromise) return dictationStartPromise;

  dictationStartPromise = startDictationRecordingInternal(targetTabId);
  try {
    await dictationStartPromise;
  } finally {
    dictationStartPromise = null;
  }
}

async function startDictationRecordingInternal(targetTabId) {
  isRecording = true;
  dictationTargetTabId = targetTabId;
  await updateDictationPresentation('recording');
  const config = await getConfig();
  if (!config.session_token) {
    isRecording = false;
    throw new Error('Sign in to SpeekTo.Me before starting dictation.');
  }

  try {
    const requestId = createDictationRequestId();
    const response = await sendMessageToOffscreen({ action: 'start_dictation', requestId });
    if (!response?.accepted) {
      throw new Error('The offscreen document did not accept the recording command.');
    }
    console.log('[BACKGROUND] Dictation recording requested.');
  } catch (err) {
    console.error('[BACKGROUND] Failed to start dictation:', err);
    isRecording = false;
    await updateDictationPresentation('error', err.message || 'Could not start recording.');
    throw err;
  }
}

async function stopDictationRecording() {
  isRecording = false;
  isProcessing = true;
  await updateDictationPresentation('processing');
  const requestId = createDictationRequestId();
  const resultPromise = waitForDictationEvent(requestId);

  try {
    const response = await sendMessageToOffscreen({ action: 'stop_dictation', requestId });
    if (!response?.accepted) {
      throw new Error('The offscreen document did not accept the stop command.');
    }
  } catch (err) {
    settleDictationRequest(requestId, err);
  }

  let response;
  try {
    response = await resultPromise;
  } finally {
    await closeOffscreenDocument();
  }

  if (response && response.success !== false && response.audioBuffer) {
    // Run VAD directly inside compiled WASM core (or JS fallback if pending build)
    const samplesArr = new Int16Array(response.audioBuffer);
    const hasSpeech = (typeof is_speech === 'function')
      ? is_speech(samplesArr, response.sampleRate, response.channels)
      : (samplesArr.some(s => Math.abs(s) > 300)); // Simple JS RMS/amplitude fallback
    
    console.log(`[BACKGROUND] Recording stopped. Speech detected: ${hasSpeech}`);
    if (hasSpeech) {
      // Resample PCM to 16kHz inside WASM core (or JS fallback)
      const resampled = (typeof resample_to_16k_mono === 'function')
        ? resample_to_16k_mono(samplesArr, response.sampleRate, response.channels)
        : jsResampleTo16kMono(samplesArr, response.sampleRate, response.channels);
      
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

      if (!res.ok) {
        throw new Error(`Transcription failed (${res.status}).`);
      }

      const data = await res.json();
      if (!data.text) {
        throw new Error('The transcription service returned no text.');
      }

      let inserted = false;
      if (dictationTargetTabId) {
        try {
          const result = await chrome.tabs.sendMessage(dictationTargetTabId, {
            action: 'insert_transcription',
            text: data.text
          });
          inserted = result?.success === true;
        } catch (_) {
          // This is normal for browser-controlled pages where content scripts
          // cannot run. The notification below explains the fallback.
        }
      }

      if (!inserted) {
        await new Promise((resolve) => chrome.storage.local.set({
          pending_transcription: data.text
        }, resolve));
        await showFallbackNotification(
          'Transcription is ready, but Chrome cannot insert it into this page. Open SpeekTo.Me to copy it.'
        );
      }
      await updateDictationPresentation('complete');
    } else {
      await updateDictationPresentation('idle');
    }
  }

  // Keep the command disabled until transcription and insertion have fully
  // finished; otherwise a second shortcut press can start a recording while
  // this result still targets the same tab.
  isProcessing = false;
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

function jsResampleTo16kMono(samples, sourceRate, sourceChannels) {
  if (sourceRate === 16000 && sourceChannels === 1) return samples;
  const ratio = sourceRate / 16000;
  const targetLength = Math.floor(samples.length / (ratio * sourceChannels));
  const result = new Int16Array(targetLength);
  for (let i = 0; i < targetLength; i++) {
    const srcIndex = Math.floor(i * ratio * sourceChannels);
    result[i] = samples[srcIndex] || 0;
  }
  return result;
}
