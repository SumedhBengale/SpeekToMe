// Speekey Settings Frontend Controller
// Utilizes window.__TAURI__ global APIs for zero-bundler simplicity.

const { invoke } = window.__TAURI__.core;
const { listen } = window.__TAURI__.event;

// State variables
let currentConfig = null;
let unlistenLoginSuccess = null;
let unlistenLoginFailed = null;
let currentStep = 1;

// DOM Elements - Navigation & Tab Panes
const tabItems = document.querySelectorAll('.nav-item');
const tabPanes = document.querySelectorAll('.tab-pane');

// Dashboard Input Elements
const hotkeySelect = document.getElementById('hotkey-select');
const autostartCheck = document.getElementById('autostart-check');
const typingModeCheck = document.getElementById('typing-mode-check');
const soundFeedbackCheck = document.getElementById('sound-feedback-check');
const smartSpacingCheck = document.getElementById('smart-spacing-check');
const modelSelect = document.getElementById('model-select');
const enhanceCheck = document.getElementById('enhance-check');
const customPromptText = document.getElementById('custom-prompt-text');

// Dashboard Account Elements
const authLoggedOut = document.getElementById('auth-logged-out');
const authLoggingIn = document.getElementById('auth-logging-in');
const authLoggedIn = document.getElementById('auth-logged-in');
const userNameEl = document.getElementById('user-name');
const userEmailEl = document.getElementById('user-email');
const loginBtn = document.getElementById('login-btn');
const cancelLoginBtn = document.getElementById('cancel-login-btn');
const logoutBtn = document.getElementById('logout-btn');
const saveBtn = document.getElementById('save-btn');
const hideBtn = document.getElementById('hide-btn');

// Onboarding Elements
const onboardingWrapper = document.getElementById('onboarding');
const onboardSteps = document.querySelectorAll('.onboarding-step');
const stepDots = document.querySelectorAll('.step-dot');

const onboardLoginBtn = document.getElementById('onboard-login-btn');
const onboardCancelLoginBtn = document.getElementById('onboard-cancel-login-btn');
const onboardNext1Btn = document.getElementById('onboard-next-1-btn');
const onboardNext2Btn = document.getElementById('onboard-next-2-btn');
const onboardNext3Btn = document.getElementById('onboard-next-3-btn');
const onboardFinishBtn = document.getElementById('onboard-finish-btn');

const onboardAuthLoggedOut = document.getElementById('onboard-auth-logged-out');
const onboardAuthLoggingIn = document.getElementById('onboard-auth-logging-in');
const onboardAuthLoggedIn = document.getElementById('onboard-auth-logged-in');
const onboardUserNameEl = document.getElementById('onboard-user-name');
const onboardUserEmailEl = document.getElementById('onboard-user-email');

// Onboarding Inputs
const onboardHotkeySelect = document.getElementById('onboard-hotkey-select');
const onboardAutostartCheck = document.getElementById('onboard-autostart-check');
const onboardTypingModeCheck = document.getElementById('onboard-typing-mode-check');
const onboardSoundFeedbackCheck = document.getElementById('onboard-sound-feedback-check');
const onboardModelSelect = document.getElementById('onboard-model-select');
const onboardEnhanceCheck = document.getElementById('onboard-enhance-check');
const onboardCustomPromptText = document.getElementById('onboard-custom-prompt-text');

// ==========================================
// Initialization & Config Binding
// ==========================================

async function init() {
  try {
    // 1. Tab selection setup (Dashboard)
    tabItems.forEach(item => {
      item.addEventListener('click', (e) => {
        e.preventDefault();
        const tabId = item.getAttribute('data-tab');
        
        tabItems.forEach(nav => nav.classList.remove('active'));
        tabPanes.forEach(pane => pane.classList.remove('active'));
        
        item.classList.add('active');
        document.getElementById(`tab-${tabId}`).classList.add('active');
      });
    });

    // 2. Fetch and apply configuration
    currentConfig = await invoke('get_config');
    applyConfigToUI(currentConfig);

    // 3. Fetch user information status & update dashboard UI
    const userInfo = await invoke('get_user_info');
    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);

    // 4. Setup Onboarding flow state
    if (!currentConfig.session_token) {
      // User is logged out / first run: Show Onboarding Flow
      onboardingWrapper.classList.remove('hidden');
      goToStep(1);
    } else {
      // User is logged in: Go straight to settings dashboard
      onboardingWrapper.classList.add('hidden');
    }

    // 5. Button listeners (Dashboard)
    saveBtn.addEventListener('click', saveAndApply);
    hideBtn.addEventListener('click', hideWindow);
    loginBtn.addEventListener('click', () => triggerLogin(false));
    cancelLoginBtn.addEventListener('click', () => triggerCancelLogin(false));
    logoutBtn.addEventListener('click', triggerLogout);

    // 6. Button listeners (Onboarding)
    onboardLoginBtn.addEventListener('click', () => triggerLogin(true));
    onboardCancelLoginBtn.addEventListener('click', () => triggerCancelLogin(true));
    onboardNext1Btn.addEventListener('click', () => goToStep(2));
    onboardNext2Btn.addEventListener('click', () => goToStep(3));
    onboardNext3Btn.addEventListener('click', () => goToStep(4));
    onboardFinishBtn.addEventListener('click', finishOnboarding);

    // 7. Setup OAuth event listeners
    setupOAuthListeners();
  } catch (err) {
    console.error("Failed to initialize Speekey UI:", err);
  }
}

function applyConfigToUI(config) {
  // Dashboard inputs
  hotkeySelect.value = config.hotkey;
  autostartCheck.checked = config.autostart;
  typingModeCheck.checked = config.typing_mode;
  soundFeedbackCheck.checked = config.sound_feedback;
  smartSpacingCheck.checked = config.smart_spacing;
  modelSelect.value = config.model;
  enhanceCheck.checked = config.enhance;
  customPromptText.value = config.custom_prompt;

  // Onboarding inputs
  onboardHotkeySelect.value = config.hotkey;
  onboardAutostartCheck.checked = config.autostart;
  onboardTypingModeCheck.checked = config.typing_mode;
  onboardSoundFeedbackCheck.checked = config.sound_feedback;
  onboardModelSelect.value = config.model;
  onboardEnhanceCheck.checked = config.enhance;
  onboardCustomPromptText.value = config.custom_prompt;
}

function gatherConfigFromUI(isOnboarding) {
  if (isOnboarding) {
    return {
      hotkey: onboardHotkeySelect.value,
      autostart: onboardAutostartCheck.checked,
      typing_mode: onboardTypingModeCheck.checked,
      sound_feedback: onboardSoundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked, // Kept same as current
      model: onboardModelSelect.value,
      enhance: onboardEnhanceCheck.checked,
      custom_prompt: onboardCustomPromptText.value,
      session_token: currentConfig ? currentConfig.session_token : null
    };
  } else {
    return {
      hotkey: hotkeySelect.value,
      autostart: autostartCheck.checked,
      typing_mode: typingModeCheck.checked,
      sound_feedback: soundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked,
      model: modelSelect.value,
      enhance: enhanceCheck.checked,
      custom_prompt: customPromptText.value,
      session_token: currentConfig ? currentConfig.session_token : null
    };
  }
}

async function saveAndApply() {
  try {
    const updated = gatherConfigFromUI(false);
    await invoke('save_config', { config: updated });
    currentConfig = updated;
    hideWindow();
  } catch (err) {
    console.error("Failed to save config:", err);
  }
}

async function hideWindow() {
  try {
    await invoke('hide_window');
  } catch (err) {
    console.error("Failed to hide window:", err);
  }
}

// ==========================================
// Onboarding Wizard Progression
// ==========================================

function goToStep(step) {
  currentStep = step;
  onboardSteps.forEach(el => el.classList.remove('active'));
  stepDots.forEach(el => el.classList.remove('active'));

  document.getElementById(`onboard-step-${step}`).classList.add('active');
  document.querySelector(`.step-dot[data-step="${step}"]`).classList.add('active');
}

async function finishOnboarding() {
  try {
    // 1. Gather all configuration inputs from Onboarding
    const updated = gatherConfigFromUI(true);
    
    // 2. Save configurations to backend
    await invoke('save_config', { config: updated });
    currentConfig = updated;
    
    // 3. Synchronize dashboard UI inputs
    applyConfigToUI(updated);
    
    // 4. Hide onboarding overlay to reveal regular settings dashboard
    onboardingWrapper.classList.add('hidden');
  } catch (err) {
    console.error("Failed to complete onboarding:", err);
  }
}

// ==========================================
// Authentication Controller
// ==========================================

function updateAuthUI(userInfo) {
  if (userInfo) {
    const [name, email] = userInfo;
    userNameEl.textContent = name;
    userEmailEl.textContent = email;
    authLoggedOut.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedIn.classList.remove('hidden');
  } else {
    authLoggedIn.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedOut.classList.remove('hidden');
  }
}

function updateOnboardAuthUI(userInfo) {
  if (userInfo) {
    const [name, email] = userInfo;
    onboardUserNameEl.textContent = name;
    onboardUserEmailEl.textContent = email;
    onboardAuthLoggedOut.classList.add('hidden');
    onboardAuthLoggingIn.classList.add('hidden');
    onboardAuthLoggedIn.classList.remove('hidden');
  } else {
    onboardAuthLoggedIn.classList.add('hidden');
    onboardAuthLoggingIn.classList.add('hidden');
    onboardAuthLoggedOut.classList.remove('hidden');
  }
}

async function triggerLogin(isOnboarding) {
  if (isOnboarding) {
    onboardAuthLoggedOut.classList.add('hidden');
    onboardAuthLoggingIn.classList.remove('hidden');
  } else {
    authLoggedOut.classList.add('hidden');
    authLoggingIn.classList.remove('hidden');
  }
  await invoke('start_login');
}

function triggerCancelLogin(isOnboarding) {
  if (isOnboarding) {
    onboardAuthLoggingIn.classList.add('hidden');
    onboardAuthLoggedOut.classList.remove('hidden');
  } else {
    authLoggingIn.classList.add('hidden');
    authLoggedOut.classList.remove('hidden');
  }
}

async function triggerLogout() {
  try {
    await invoke('logout');
    if (currentConfig) {
      currentConfig.session_token = null;
    }
    updateAuthUI(null);
    updateOnboardAuthUI(null);
  } catch (err) {
    console.error("Failed to logout:", err);
  }
}

async function setupOAuthListeners() {
  // Listen for login-success emitted by Rust thread
  unlistenLoginSuccess = await listen('login-success', (event) => {
    const [name, email] = event.payload;
    
    // Automatically retrieve session token locally
    invoke('get_config').then(cfg => {
      currentConfig = cfg;
    });

    updateAuthUI([name, email]);
    updateOnboardAuthUI([name, email]);
  });

  // Listen for login-failed emitted by Rust thread
  unlistenLoginFailed = await listen('login-failed', (event) => {
    console.error("Login failed:", event.payload);
    triggerCancelLogin(true);
    triggerCancelLogin(false);
    alert("Authentication failed: " + event.payload);
  });
}

// Cleanup event listeners if window is destroyed
window.addEventListener('beforeunload', () => {
  if (unlistenLoginSuccess) unlistenLoginSuccess();
  if (unlistenLoginFailed) unlistenLoginFailed();
});

// Run initialization
document.addEventListener('DOMContentLoaded', init);

