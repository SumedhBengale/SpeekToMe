// SpeekTo.Me Settings Frontend Controller
// Utilizes window.__TAURI__ global APIs for zero-bundler simplicity.

const { invoke } = window.__TAURI__.core;
const { listen } = window.__TAURI__.event;

// State variables
let currentConfig = null;
let unlistenLoginSuccess = null;
let unlistenLoginFailed = null;
let currentStep = 1;
let API_URL = "https://api.speekto.me";

function showToast(message, title = "Notification", type = "error") {
  let container = document.querySelector('.custom-toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'custom-toast-container';
    document.body.appendChild(container);
  }

  const icon = type === 'error' ? '🔒' : type === 'warning' ? '⚠️' : 'ℹ️';
  
  const toast = document.createElement('div');
  toast.className = `custom-toast ${type}`;
  toast.innerHTML = `
    <div class="custom-toast-icon">${icon}</div>
    <div class="custom-toast-body">
      <div class="custom-toast-title">${escapeHtml(title)}</div>
      <div class="custom-toast-msg">${escapeHtml(message)}</div>
    </div>
    <button class="custom-toast-close" title="Close">&times;</button>
  `;

  const closeBtn = toast.querySelector('.custom-toast-close');
  const removeToast = () => {
    toast.style.animation = 'toastFadeOut 0.3s cubic-bezier(0.16, 1, 0.3, 1) forwards';
    setTimeout(() => {
      if (toast.parentNode) {
        toast.parentNode.removeChild(toast);
      }
    }, 300);
  };

  closeBtn.addEventListener('click', removeToast);
  container.appendChild(toast);

  setTimeout(removeToast, 6000);
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

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
const promptValidationBanner = document.getElementById('prompt-validation-banner');
const themeSelect = document.getElementById('theme-select');

// Pill Position Elements
const pillPosModeSelect = document.getElementById('pill-pos-mode-select');
const predefinedPosGroup = document.getElementById('predefined-pos-group');
const pillPredefinedSelect = document.getElementById('pill-predefined-select');
const customPosGroup = document.getElementById('custom-pos-group');
const pillCustomX = document.getElementById('pill-custom-x');
const pillCustomY = document.getElementById('pill-custom-y');

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
const onboardPromptValidationBanner = document.getElementById('onboard-prompt-validation-banner');
const onboardThemeSelect = document.getElementById('onboard-theme-select');

// ==========================================
// Initialization & Config Binding
// ==========================================

async function init() {
  try {
    try {
      API_URL = await invoke('get_api_url');
    } catch (err) {
      console.error("Failed to load API URL from backend:", err);
    }

    // 1. Tab selection setup (Dashboard)
    tabItems.forEach(item => {
      item.addEventListener('click', (e) => {
        e.preventDefault();
        const tabId = item.getAttribute('data-tab');
        
        tabItems.forEach(nav => nav.classList.remove('active'));
        tabPanes.forEach(pane => pane.classList.remove('active'));
        
        item.classList.add('active');
        document.getElementById(`tab-${tabId}`).classList.add('active');

        // Refresh user subscription status when visiting the account page
        if (tabId === 'account') {
          invoke('get_user_info').then(info => {
            updateAuthUI(info);
          });
        } else if (tabId === 'vocabulary') {
          loadVocabulary();
        } else if (tabId === 'meetings') {
          loadMeetings();
        }
      });
    });

    // 2. Fetch and apply configuration
    currentConfig = await invoke('get_config');
    applyConfigToUI(currentConfig);

    // 3. Fetch user information status & update dashboard UI
    const userInfo = await invoke('get_user_info');
    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);

    // 3.5. Bind Refresh Button Click Event
    const refreshBtn = document.getElementById('refresh-data-btn');
    if (refreshBtn) {
      refreshBtn.addEventListener('click', async () => {
        const originalText = refreshBtn.innerHTML;
        refreshBtn.disabled = true;
        refreshBtn.innerHTML = '<span>🔄</span> Refreshing...';
        
        try {
          const freshInfo = await invoke('get_user_info');
          if (freshInfo) {
            updateAuthUI(freshInfo);
            updateOnboardAuthUI(freshInfo);
          }
        } catch (err) {
          console.error("Failed to refresh user data:", err);
        } finally {
          refreshBtn.disabled = false;
          refreshBtn.innerHTML = originalText;
        }
      });
    }

    // 3.8. Theme preview listeners
    if (themeSelect) {
      themeSelect.addEventListener('change', (e) => {
        document.body.setAttribute('data-theme', e.target.value);
      });
    }

    if (pillPosModeSelect) {
      pillPosModeSelect.addEventListener('change', (e) => {
        if (e.target.value === 'custom') {
          if (customPosGroup) customPosGroup.style.display = 'block';
          if (predefinedPosGroup) predefinedPosGroup.style.display = 'none';
        } else {
          if (customPosGroup) customPosGroup.style.display = 'none';
          if (predefinedPosGroup) predefinedPosGroup.style.display = 'block';
        }
      });
    }
    if (onboardThemeSelect) {
      onboardThemeSelect.addEventListener('change', (e) => {
        document.body.setAttribute('data-theme', e.target.value);
      });
    }

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
    onboardNext3Btn.addEventListener('click', validateOnboardingPrompt);
    onboardFinishBtn.addEventListener('click', finishOnboarding);

    // 7. Setup OAuth event listeners
    setupOAuthListeners();

    // 8. Setup Credential Forms (Onboarding & Dashboard)
    setupCredentialForm(
      'onboard-auth-form',
      'onboard-name-input',
      'onboard-email-input',
      'onboard-password-input',
      'onboard-email-submit-btn',
      'onboard-form-toggle-link',
      'onboard-auth-error',
      true
    );

    setupCredentialForm(
      'app-auth-form',
      'app-name-input',
      'app-email-input',
      'app-password-input',
      'app-email-submit-btn',
      'app-form-toggle-link',
      'app-auth-error',
      false
    );
    // 9. Vocabulary Engine UI binding
    const vocabInput = document.getElementById('vocab-input');
    const vocabAddBtn = document.getElementById('vocab-add-btn');
    if (vocabAddBtn && vocabInput) {
      vocabAddBtn.addEventListener('click', async () => {
        const term = vocabInput.value.trim();
        if (!term) return;
        
        vocabAddBtn.disabled = true;
        try {
          await invoke('add_vocabulary_term', { term });
          vocabInput.value = '';
          await loadVocabulary();
        } catch (err) {
          console.error("Failed to add vocabulary term:", err);
          showToast(err, "Failed to add term", "error");
        } finally {
          vocabAddBtn.disabled = false;
        }
      });
      
      vocabInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          vocabAddBtn.click();
        }
      });
    }

    // Initialize Meeting Notes UI event listeners
    setupMeetingsUI();
  } catch (err) {
    console.error("Failed to initialize SpeekTo.Me UI:", err);
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
  if (themeSelect) themeSelect.value = config.theme || 'forest';

  // Apply new pill positioning settings
  if (pillPosModeSelect) {
    pillPosModeSelect.value = config.pill_position_mode || 'predefined';
    if (config.pill_position_mode === 'custom') {
      if (customPosGroup) customPosGroup.style.display = 'block';
      if (predefinedPosGroup) predefinedPosGroup.style.display = 'none';
    } else {
      if (customPosGroup) customPosGroup.style.display = 'none';
      if (predefinedPosGroup) predefinedPosGroup.style.display = 'block';
    }
  }
  if (pillPredefinedSelect) {
    pillPredefinedSelect.value = config.pill_predefined_position || 'Top Center';
  }
  if (pillCustomX) {
    pillCustomX.value = config.pill_custom_x !== undefined ? config.pill_custom_x : 0;
  }
  if (pillCustomY) {
    pillCustomY.value = config.pill_custom_y !== undefined ? config.pill_custom_y : 60;
  }

  // Onboarding inputs
  onboardHotkeySelect.value = config.hotkey;
  onboardAutostartCheck.checked = config.autostart;
  onboardTypingModeCheck.checked = config.typing_mode;
  onboardSoundFeedbackCheck.checked = config.sound_feedback;
  onboardModelSelect.value = config.model;
  onboardEnhanceCheck.checked = config.enhance;
  onboardCustomPromptText.value = config.custom_prompt;
  if (onboardThemeSelect) onboardThemeSelect.value = config.theme || 'forest';

  // Apply theme class to document body
  document.body.setAttribute('data-theme', config.theme || 'forest');
}

function cssColorToBgr(colorStr) {
  if (!colorStr) return 0;
  colorStr = colorStr.trim();
  
  // Handle hex format (e.g., #fff9fa or #fff)
  if (colorStr.startsWith('#')) {
    let hex = colorStr.substring(1);
    if (hex.length === 3) {
      hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
    }
    if (hex.length === 6) {
      const r = parseInt(hex.substring(0, 2), 16);
      const g = parseInt(hex.substring(2, 4), 16);
      const b = parseInt(hex.substring(4, 6), 16);
      return (b << 16) | (g << 8) | r;
    }
  }
  
  // Handle rgb format (e.g., rgb(26, 16, 47) or rgba(26, 16, 47, 0.5))
  const rgbMatch = colorStr.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/);
  if (rgbMatch) {
    const r = parseInt(rgbMatch[1], 10);
    const g = parseInt(rgbMatch[2], 10);
    const b = parseInt(rgbMatch[3], 10);
    return (b << 16) | (g << 8) | r;
  }
  
  return 0;
}

function getPillColorsForTheme(themeName) {
  const bodyStyle = getComputedStyle(document.body);
  let bg, wave;
  
  if (themeName === 'forest') {
    // For light theme, we use light pink background (bg-content) and dark burgundy waves (border-color)
    bg = cssColorToBgr(bodyStyle.getPropertyValue('--bg-content')) || 0xFAF9FF;
    wave = cssColorToBgr(bodyStyle.getPropertyValue('--border-color')) || 0x120A24;
  } else {
    // For dark themes, we use the content background and accent color
    bg = cssColorToBgr(bodyStyle.getPropertyValue('--bg-content')) || 0x1A102F;
    wave = cssColorToBgr(bodyStyle.getPropertyValue('--accent-color')) || 0xF0D7FF;
  }
  
  return { bg, wave };
}

function gatherConfigFromUI(isOnboarding) {
  const api_url = currentConfig ? currentConfig.api_url : null;
  if (isOnboarding) {
    const themeName = onboardThemeSelect ? onboardThemeSelect.value : 'forest';
    const colors = getPillColorsForTheme(themeName);
    return {
      api_url: api_url,
      hotkey: onboardHotkeySelect.value,
      autostart: onboardAutostartCheck.checked,
      typing_mode: onboardTypingModeCheck.checked,
      sound_feedback: onboardSoundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked, // Kept same as current
      model: onboardModelSelect.value,
      enhance: onboardEnhanceCheck.checked,
      custom_prompt: onboardCustomPromptText.value,
      session_token: currentConfig ? currentConfig.session_token : null,
      theme: themeName,
      pill_position_mode: currentConfig ? currentConfig.pill_position_mode : 'predefined',
      pill_predefined_position: currentConfig ? currentConfig.pill_predefined_position : 'Top Center',
      pill_custom_x: currentConfig ? currentConfig.pill_custom_x : 0,
      pill_custom_y: currentConfig ? currentConfig.pill_custom_y : 60,
      pill_bg_color: colors.bg,
      pill_wave_color: colors.wave
    };
  } else {
    const themeName = themeSelect ? themeSelect.value : 'forest';
    const colors = getPillColorsForTheme(themeName);
    return {
      api_url: api_url,
      hotkey: hotkeySelect.value,
      autostart: autostartCheck.checked,
      typing_mode: typingModeCheck.checked,
      sound_feedback: soundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked,
      model: modelSelect.value,
      enhance: enhanceCheck.checked,
      custom_prompt: customPromptText.value,
      session_token: currentConfig ? currentConfig.session_token : null,
      theme: themeName,
      pill_position_mode: pillPosModeSelect ? pillPosModeSelect.value : 'predefined',
      pill_predefined_position: pillPredefinedSelect ? pillPredefinedSelect.value : 'Top Center',
      pill_custom_x: pillCustomX ? parseInt(pillCustomX.value, 10) || 0 : 0,
      pill_custom_y: pillCustomY ? parseInt(pillCustomY.value, 10) || 0 : 60,
      pill_bg_color: colors.bg,
      pill_wave_color: colors.wave
    };
  }
}

async function saveAndApply() {
  const banner = promptValidationBanner;
  const prompt = customPromptText.value.trim();
  const originalText = saveBtn.innerHTML;

  if (banner) {
    banner.classList.add('hidden');
    banner.className = 'validation-banner hidden';
    banner.innerHTML = '';
  }

  // If enhance is enabled and prompt is not empty, validate it first
  if (enhanceCheck.checked && prompt !== '') {
    saveBtn.disabled = true;
    saveBtn.innerHTML = '<span>⏳</span> Validating...';

    if (banner) {
      banner.classList.remove('hidden');
      banner.className = 'validation-banner info';
      banner.innerHTML = '<div class="spinner"></div> Checking instruction safety...';
    }

    try {
      const result = await invoke('validate_prompt', { prompt: prompt });
      if (!result.safe) {
        if (banner) {
          banner.className = 'validation-banner error';
          banner.innerHTML = `<span>⚠️</span> Safety Check Failed: ${escapeHtml(result.explanation || 'Not eligible to be a system prompt.')}`;
        }
        saveBtn.disabled = false;
        saveBtn.innerHTML = originalText;
        return; // Block saving
      }
      
      if (banner) {
        banner.className = 'validation-banner success';
        banner.innerHTML = '<span>✅</span> Instructions validated!';
      }
      // Brief delay to let the user see validation success state
      await new Promise(resolve => setTimeout(resolve, 800));
    } catch (err) {
      console.error("Failed to validate custom prompt:", err);
      if (banner) {
        banner.className = 'validation-banner error';
        banner.innerHTML = `<span>❌</span> Validation error: ${escapeHtml(formatAuthError(err))}`;
      }
      saveBtn.disabled = false;
      saveBtn.innerHTML = originalText;
      return; // Block saving on validation error
    }
  }

  try {
    saveBtn.disabled = true;
    saveBtn.innerHTML = '<span>⏳</span> Saving...';
    const updated = gatherConfigFromUI(false);
    await invoke('save_config', { config: updated });
    currentConfig = updated;
    
    saveBtn.innerHTML = '<span>✅</span> Saved Successfully!';
    if (banner) {
      banner.className = 'validation-banner success';
      banner.innerHTML = '<span>✅</span> Settings saved and applied!';
      banner.classList.remove('hidden');
    }
    
    // Reset save button after a brief delay
    setTimeout(() => {
      saveBtn.disabled = false;
      saveBtn.innerHTML = originalText;
      if (banner) {
        banner.classList.add('hidden');
      }
    }, 2000);
  } catch (err) {
    console.error("Failed to save config:", err);
    saveBtn.disabled = false;
    saveBtn.innerHTML = originalText;
    if (banner) {
      banner.className = 'validation-banner error';
      banner.innerHTML = `<span>❌</span> Save failed: ${escapeHtml(formatAuthError(err))}`;
      banner.classList.remove('hidden');
    }
  }
}

async function validateOnboardingPrompt() {
  const prompt = onboardCustomPromptText.value.trim();
  const banner = onboardPromptValidationBanner;
  
  if (!banner) {
    goToStep(4);
    return;
  }
  
  // Hide any previous validation state
  banner.classList.add('hidden');
  banner.className = 'validation-banner hidden';
  banner.innerHTML = '';

  // If prompt is empty or AI enhancement is disabled, proceed immediately
  if (!onboardEnhanceCheck.checked || prompt === '') {
    goToStep(4);
    return;
  }

  // Validate the prompt
  onboardNext3Btn.disabled = true;
  const originalText = onboardNext3Btn.innerHTML;
  onboardNext3Btn.innerHTML = '<span>⏳</span> Validating instructions...';
  
  banner.classList.remove('hidden');
  banner.className = 'validation-banner info';
  banner.innerHTML = '<div class="spinner"></div> Checking instruction safety...';

  try {
    const result = await invoke('validate_prompt', { prompt: prompt });
    if (result.safe) {
      banner.className = 'validation-banner success';
      banner.innerHTML = '<span>✅</span> Instructions are safe and ready!';
      
      // Delay slightly so the user sees the success state, then transition
      setTimeout(() => {
        banner.classList.add('hidden');
        onboardNext3Btn.disabled = false;
        onboardNext3Btn.innerHTML = originalText;
        goToStep(4);
      }, 1000);
    } else {
      banner.className = 'validation-banner error';
      banner.innerHTML = `<span>⚠️</span> Safety Check Failed: ${escapeHtml(result.explanation || 'Not eligible to be a system prompt.')}`;
      onboardNext3Btn.disabled = false;
      onboardNext3Btn.innerHTML = originalText;
    }
  } catch (err) {
    console.error("Failed to validate onboarding prompt:", err);
    banner.className = 'validation-banner error';
    banner.innerHTML = `<span>❌</span> Validation error: ${escapeHtml(formatAuthError(err))}`;
    onboardNext3Btn.disabled = false;
    onboardNext3Btn.innerHTML = originalText;
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

function formatDuration(seconds) {
  if (!seconds || seconds <= 0) return "0 seconds";
  if (seconds < 60) return `${Math.round(seconds)} seconds`;
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return `${m}m ${s}s`;
}

function formatDate(dateStr) {
  if (!dateStr) return "N/A";
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return dateStr;
    return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  } catch (_) {
    return dateStr;
  }
}

function updateAuthUI(userInfo) {
  const accountDesc = document.getElementById('account-desc');
  
  if (userInfo) {
    userNameEl.textContent = userInfo.name;
    userEmailEl.textContent = userInfo.email;
    
    // Bind subscription fields
    const subPlan = document.getElementById('sub-plan');
    const subStatus = document.getElementById('sub-status');
    const subActivated = document.getElementById('sub-activated');
    const subUsage = document.getElementById('sub-usage');
    
    if (subPlan) subPlan.textContent = userInfo.plan_tier.toUpperCase();
    if (subStatus) {
      if (userInfo.plan_active) {
        subStatus.textContent = "Active";
        subStatus.className = "sub-value badge-active";
      } else {
        subStatus.textContent = "Inactive";
        subStatus.className = "sub-value badge-inactive";
      }
    }
    if (subActivated) subActivated.textContent = formatDate(userInfo.plan_activated_at);
    if (subUsage) subUsage.textContent = formatDuration(userInfo.duration_seconds);

    // Enforce 50-hour monthly limit progress bar calculations (50 hours = 180,000 seconds)
    const maxSeconds = 180000.0;
    const usageSeconds = userInfo.duration_seconds || 0.0;
    const percent = Math.min(100.0, (usageSeconds / maxSeconds) * 100.0).toFixed(1);
    
    const subUsagePercent = document.getElementById('sub-usage-percent');
    const subUsageBar = document.getElementById('sub-usage-bar');
    
    if (subUsagePercent) subUsagePercent.textContent = `${percent}%`;
    if (subUsageBar) subUsageBar.style.width = `${percent}%`;

    // Hide descriptive text when user is successfully logged in
    if (accountDesc) accountDesc.classList.add('hidden');

    authLoggedOut.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedIn.classList.remove('hidden');

    const sidebarFooter = document.getElementById('sidebar-footer');
    if (sidebarFooter) sidebarFooter.style.display = 'block';
  } else {
    // Show descriptive text when logged out
    if (accountDesc) accountDesc.classList.remove('hidden');

    authLoggedIn.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedOut.classList.remove('hidden');

    const sidebarFooter = document.getElementById('sidebar-footer');
    if (sidebarFooter) sidebarFooter.style.display = 'none';
  }
}

function updateOnboardAuthUI(userInfo) {
  if (userInfo) {
    onboardUserNameEl.textContent = userInfo.name;
    onboardUserEmailEl.textContent = userInfo.email;
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
    const userInfo = event.payload;
    
    // Automatically retrieve session token locally
    invoke('get_config').then(cfg => {
      currentConfig = cfg;
    });

    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);
  });

  // Listen for login-failed emitted by Rust thread
  unlistenLoginFailed = await listen('login-failed', (event) => {
    console.error("Login failed:", event.payload);
    triggerCancelLogin(true);
    triggerCancelLogin(false);
    showToast(event.payload, "Authentication Notice", "error");
  });
}

// Cleanup event listeners if window is destroyed
window.addEventListener('beforeunload', () => {
  if (unlistenLoginSuccess) unlistenLoginSuccess();
  if (unlistenLoginFailed) unlistenLoginFailed();
});

function setupCredentialForm(formId, nameInputId, emailInputId, passwordInputId, submitBtnId, toggleLinkId, errorId, isOnboarding) {
  const nameInput = document.getElementById(nameInputId);
  const emailInput = document.getElementById(emailInputId);
  const passwordInput = document.getElementById(passwordInputId);
  const submitBtn = document.getElementById(submitBtnId);
  const toggleLink = document.getElementById(toggleLinkId);
  const errorEl = document.getElementById(errorId);

  let isSignUp = false;

  toggleLink.addEventListener('click', (e) => {
    e.preventDefault();
    isSignUp = !isSignUp;
    if (isSignUp) {
      nameInput.style.display = 'block';
      submitBtn.textContent = 'Create Account';
      toggleLink.textContent = 'Already have an account? Sign in';
    } else {
      nameInput.style.display = 'none';
      submitBtn.textContent = 'Sign In';
      toggleLink.textContent = 'Need an account? Sign up';
    }
    errorEl.style.display = 'none';
    errorEl.textContent = '';
  });

  submitBtn.addEventListener('click', async (e) => {
    e.preventDefault();
    const email = emailInput.value.trim();
    const password = passwordInput.value;
    const name = nameInput.value.trim();

    if (!email || !password || (isSignUp && !name)) {
      errorEl.textContent = 'All fields are required.';
      errorEl.style.display = 'block';
      return;
    }

    errorEl.style.display = 'none';
    submitBtn.disabled = true;
    const originalText = submitBtn.textContent;
    submitBtn.textContent = isSignUp ? 'Creating Account...' : 'Signing In...';

    try {
      if (isSignUp) {
        const msg = await invoke('signup_with_credentials', { email, password, name });
        // Show success message in green
        errorEl.textContent = msg;
        errorEl.style.color = '#00e676';
        errorEl.style.display = 'block';
        
        // Auto-switch to login pane after 4 seconds
        setTimeout(() => {
          isSignUp = false;
          nameInput.style.display = 'none';
          submitBtn.textContent = 'Sign In';
          toggleLink.textContent = 'Need an account? Sign up';
          errorEl.style.display = 'none';
          errorEl.style.color = ''; // reset color to default
        }, 4000);

        // Clear input fields (leave email for quick login)
        passwordInput.value = '';
        nameInput.value = '';
      } else {
        const userInfo = await invoke('login_with_credentials', { email, password });
        
        // Fetch updated config so we have the new session token
        currentConfig = await invoke('get_config');
        
        updateAuthUI(userInfo);
        updateOnboardAuthUI(userInfo);

        // Clear input fields
        emailInput.value = '';
        passwordInput.value = '';
        nameInput.value = '';
      }
    } catch (err) {
      errorEl.style.color = ''; // reset to default red
      errorEl.textContent = formatAuthError(err);
      errorEl.style.display = 'block';
    } finally {
      submitBtn.disabled = false;
      submitBtn.textContent = originalText;
    }
  });
}

function formatAuthError(err) {
  if (typeof err === 'string') {
    try {
      const parsed = JSON.parse(err);
      if (parsed && parsed.error) {
        return parsed.error;
      }
    } catch (e) {
      // Not a JSON string
    }
    return err;
  }
  return err.message || JSON.stringify(err);
}

// ==========================================
// Vocabulary Settings
// ==========================================

async function loadVocabulary() {
  const vocabList = document.getElementById('vocab-list');
  const vocabEmpty = document.getElementById('vocab-empty-msg');
  const vocabWarning = document.getElementById('vocab-warning');
  if (!vocabList) return;

  try {
    const terms = await invoke('get_vocabulary');
    vocabList.innerHTML = '';
    
    if (!terms || terms.length === 0) {
      vocabEmpty.style.display = 'block';
      vocabList.style.display = 'none';
      vocabWarning.classList.add('hidden');
      return;
    }

    vocabEmpty.style.display = 'none';
    vocabList.style.display = 'flex';

    if (terms.length > 1000) {
      vocabWarning.classList.remove('hidden');
    } else {
      vocabWarning.classList.add('hidden');
    }

    terms.forEach(term => {
      const li = document.createElement('li');
      li.className = 'vocab-item';
      
      const textSpan = document.createElement('span');
      textSpan.className = 'vocab-term-text';
      textSpan.textContent = term.canonical_term;
      
      const normSpan = document.createElement('span');
      normSpan.className = 'vocab-term-normalized';
      normSpan.textContent = ` (${term.normalized_term})`;
      textSpan.appendChild(normSpan);

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'vocab-delete-btn';
      deleteBtn.innerHTML = '🗑️';
      deleteBtn.title = 'Delete word';
      deleteBtn.addEventListener('click', async () => {
        deleteBtn.disabled = true;
        try {
          await invoke('delete_vocabulary_term', { term: term.canonical_term });
          await loadVocabulary();
        } catch (err) {
          console.error("Failed to delete term:", err);
          showToast(err, "Failed to delete term", "error");
        }
      });

      li.appendChild(textSpan);
      li.appendChild(deleteBtn);
      vocabList.appendChild(li);
    });
  } catch (err) {
    console.error("Failed to load vocabulary:", err);
    vocabList.innerHTML = `<li class="vocab-empty" style="color: #ff5252;">Error loading vocabulary: ${escapeHtml(String(err))}</li>`;
    vocabEmpty.style.display = 'none';
  }
}

// Run initialization
document.addEventListener('DOMContentLoaded', init);

// ==========================================
// Meetings & Notes Feature Controller
// ==========================================

let recordingInterval = null;
let timerInterval = null;
let meetingSeconds = 0;
let currentMeetingFile = null;
let activeMeetingId = null;

function setupMeetingsUI() {
  const startRecBtn = document.getElementById('meetings-start-rec-btn');
  const stopRecBtn = document.getElementById('meetings-stop-rec-btn');
  const cancelRecBtn = document.getElementById('meetings-cancel-rec-btn');
  const backBtn = document.getElementById('meetings-back-btn');
  const deleteBtn = document.getElementById('meeting-delete-btn');
  const searchInput = document.getElementById('meetings-search-input');
  
  if (startRecBtn) startRecBtn.addEventListener('click', startMeetingRecording);
  if (stopRecBtn) stopRecBtn.addEventListener('click', stopMeetingRecording);
  if (cancelRecBtn) cancelRecBtn.addEventListener('click', cancelMeetingRecording);
  if (backBtn) backBtn.addEventListener('click', () => {
    const player = document.getElementById('meeting-audio-player');
    player.pause();
    player.src = '';
    showDashboardView();
    loadMeetings();
  });
  if (deleteBtn) deleteBtn.addEventListener('click', confirmDeleteMeeting);
  
  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      const query = e.target.value.toLowerCase();
      if (!window.allMeetings) return;
      const filtered = window.allMeetings.filter(m => 
        m.title.toLowerCase().includes(query) || 
        (m.transcript && m.transcript.toLowerCase().includes(query)) ||
        (m.summary && m.summary.toLowerCase().includes(query))
      );
      renderMeetingsList(filtered);
    });
  }
}

function showDashboardView() {
  document.getElementById('meetings-dashboard-view').classList.remove('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showRecordingView() {
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.remove('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showDetailView() {
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.remove('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showProcessingView() {
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.remove('hidden');
}

async function loadMeetings() {
  const emptyMsg = document.getElementById('meetings-empty-msg');
  const listGrid = document.getElementById('meetings-list');
  if (!listGrid) return;

  const token = currentConfig ? currentConfig.session_token : null;
  if (!token) {
    listGrid.innerHTML = '';
    emptyMsg.textContent = 'Please sign in to view and record meeting notes.';
    emptyMsg.style.display = 'block';
    return;
  }

  try {
    const res = await fetch(`${API_URL}/meetings`, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });
    if (!res.ok) throw new Error("Server error");
    const meetings = await res.json();
    
    window.allMeetings = meetings;
    renderMeetingsList(meetings);
  } catch (err) {
    console.error("Failed to load meetings:", err);
    listGrid.innerHTML = '';
    emptyMsg.textContent = 'Error connecting to SpeekTo.Me server. Ensure the server is running.';
    emptyMsg.style.display = 'block';
  }
}

function renderMeetingsList(meetings) {
  const emptyMsg = document.getElementById('meetings-empty-msg');
  const listGrid = document.getElementById('meetings-list');
  listGrid.innerHTML = '';

  if (!meetings || meetings.length === 0) {
    emptyMsg.style.display = 'block';
    emptyMsg.textContent = 'No meeting notes found.';
    return;
  }
  emptyMsg.style.display = 'none';

  meetings.forEach(m => {
    const card = document.createElement('div');
    card.className = 'meeting-card';
    card.addEventListener('click', () => viewMeeting(m.id));

    const title = document.createElement('div');
    title.className = 'meeting-card-title';
    title.textContent = m.title;

    const meta = document.createElement('div');
    meta.className = 'meeting-card-meta';

    const dateSpan = document.createElement('span');
    dateSpan.textContent = formatDate(m.timestamp);

    const durSpan = document.createElement('span');
    durSpan.textContent = formatDuration(m.duration);

    meta.appendChild(dateSpan);
    meta.appendChild(durSpan);

    card.appendChild(title);
    card.appendChild(meta);
    listGrid.appendChild(card);
  });
}

async function viewMeeting(id) {
  showProcessingView();
  const token = currentConfig ? currentConfig.session_token : null;
  try {
    const res = await fetch(`${API_URL}/meetings/${id}`, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });
    if (!res.ok) throw new Error("Failed to load meeting details");
    const m = await res.json();
    activeMeetingId = id;

    showDetailView();

    document.getElementById('meeting-detail-title').textContent = m.title;
    document.getElementById('meeting-detail-date').textContent = `${formatDate(m.timestamp)} • ${formatDuration(m.duration)}`;

    // Set Audio source using Object URL to authorize playback request
    const player = document.getElementById('meeting-audio-player');
    const audioRes = await fetch(`${API_URL}/meetings/${id}/audio`, {
      headers: { 'Authorization': `Bearer ${token}` }
    });
    const blob = await audioRes.blob();
    player.src = URL.createObjectURL(blob);

    // Setup active tab
    const tabBtns = document.querySelectorAll('.detail-tab-btn');
    const panes = document.querySelectorAll('.detail-tab-pane');
    tabBtns.forEach(b => b.classList.remove('active'));
    panes.forEach(p => p.classList.remove('active'));
    document.querySelector('.detail-tab-btn[data-detail-tab="transcript"]').classList.add('active');
    document.getElementById('detail-pane-transcript').classList.add('active');

    // Parse and render transcripts with sync highlights
    const transcriptTextEl = document.getElementById('meeting-transcript-text');
    transcriptTextEl.innerHTML = '';
    
    let segments = [];
    try {
      segments = JSON.parse(m.segments_json);
    } catch (_) {
      segments = [];
    }

    // Helper functions for avatars
    const getInitials = (name) => {
      if (!name) return 'SP';
      const cleanName = name.replace(/[\[\]]/g, ''); // strip brackets
      const parts = cleanName.split(' ');
      if (parts.length >= 2) {
        return (parts[0][0] + parts[1][0]).toUpperCase();
      }
      return cleanName.slice(0, 2).toUpperCase();
    };

    const getAvatarColor = (name) => {
      if (!name) return '#757575';
      let hash = 0;
      for (let i = 0; i < name.length; i++) {
        hash = name.charCodeAt(i) + ((hash << 5) - hash);
      }
      const colors = [
        '#5c6bc0', '#26a69a', '#ec407a', '#7e57c2', 
        '#26c6da', '#ff7043', '#8d6e63', '#66bb6a'
      ];
      return colors[Math.abs(hash) % colors.length];
    };

    const formatOffset = (seconds) => {
      if (isNaN(seconds)) return '0:00';
      const h = Math.floor(seconds / 3600);
      const m = Math.floor((seconds % 3600) / 60);
      const s = Math.floor(seconds % 60);
      const sStr = s < 10 ? '0' + s : s;
      if (h > 0) {
        const mStr = m < 10 ? '0' + m : m;
        return `${h}:${mStr}:${sStr}`;
      }
      return `${m}:${sStr}`;
    };

    if (segments && segments.length > 0) {
      let currentSpeaker = null;
      let currentBlock = null;
      let currentBlockText = null;

      segments.forEach(seg => {
        const speakerName = seg.speaker || 'Speaker';
        
        if (speakerName !== currentSpeaker) {
          currentSpeaker = speakerName;
          
          currentBlock = document.createElement('div');
          currentBlock.className = 'meeting-speaker-block';
          
          const header = document.createElement('div');
          header.className = 'meeting-speaker-header';
          
          const avatar = document.createElement('div');
          avatar.className = 'meeting-speaker-avatar';
          avatar.textContent = getInitials(speakerName);
          avatar.style.backgroundColor = getAvatarColor(speakerName);
          
          const nameSpan = document.createElement('span');
          nameSpan.className = 'meeting-speaker-name';
          nameSpan.textContent = speakerName;
          
          const timeSpan = document.createElement('span');
          timeSpan.className = 'meeting-speaker-time';
          timeSpan.textContent = formatOffset(seg.start);
          
          header.appendChild(avatar);
          header.appendChild(nameSpan);
          header.appendChild(timeSpan);
          currentBlock.appendChild(header);
          
          currentBlockText = document.createElement('div');
          currentBlockText.className = 'meeting-speaker-text';
          currentBlock.appendChild(currentBlockText);
          
          transcriptTextEl.appendChild(currentBlock);
        }
        
        const span = document.createElement('span');
        span.className = 'transcript-segment';
        span.dataset.start = seg.start;
        span.dataset.end = seg.end;
        span.textContent = seg.text + ' ';
        
        span.addEventListener('click', () => {
          player.currentTime = seg.start;
          player.play();
        });
        
        if (currentBlockText) {
          currentBlockText.appendChild(span);
        }
      });
    } else {
      transcriptTextEl.textContent = m.transcript || 'No transcript text available.';
    }

    player.ontimeupdate = () => {
      const time = player.currentTime;
      const spans = transcriptTextEl.querySelectorAll('.transcript-segment');
      spans.forEach(span => {
        const start = parseFloat(span.dataset.start);
        const end = parseFloat(span.dataset.end);
        if (time >= start && time <= end) {
          span.classList.add('active');
          span.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        } else {
          span.classList.remove('active');
        }
      });
    };

    // Render summary and actions
    document.getElementById('meeting-summary-text').innerHTML = parseMarkdownToHTML(m.summary);
    document.getElementById('meeting-actions-text').innerHTML = parseMarkdownToHTML(m.action_items);

    // Detail Tabs switching
    tabBtns.forEach(btn => {
      // Remove existing event listener if any (by cloning)
      const newBtn = btn.cloneNode(true);
      btn.parentNode.replaceChild(newBtn, btn);
      newBtn.addEventListener('click', () => {
        const activeBtns = document.querySelectorAll('.detail-tab-btn');
        const activePanes = document.querySelectorAll('.detail-tab-pane');
        activeBtns.forEach(b => b.classList.remove('active'));
        activePanes.forEach(p => p.classList.remove('active'));
        
        newBtn.classList.add('active');
        const targetPane = newBtn.getAttribute('data-detail-tab');
        document.getElementById(`detail-pane-${targetPane}`).classList.add('active');
      });
    });

  } catch (err) {
    console.error(err);
    showToast(err.message, "Failed to load meeting", "error");
    showDashboardView();
  }
}

async function startMeetingRecording() {
  const token = currentConfig ? currentConfig.session_token : null;
  if (!token) {
    showToast("Please sign in to record meeting notes.", "Sign-in Required", "info");
    return;
  }

  showRecordingView();
  const titleInput = document.getElementById('meeting-title-input');
  titleInput.value = '';
  
  const timerEl = document.getElementById('meetings-timer');
  timerEl.textContent = '00:00:00';
  meetingSeconds = 0;
  
  timerInterval = setInterval(() => {
    meetingSeconds++;
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }, 1000);

  recordingInterval = setInterval(async () => {
    try {
      const micFill = document.getElementById('mic-level-fill');
      const loopbackFill = document.getElementById('loopback-level-fill');
      const [mic, loopback] = await invoke('get_meeting_levels');
      const micPercent = Math.min(100, mic / 10); // normalize 0-1000 to 0-100%
      const loopbackPercent = Math.min(100, loopback / 10);
      if (micFill) micFill.style.width = `${micPercent}%`;
      if (loopbackFill) loopbackFill.style.width = `${loopbackPercent}%`;
    } catch (e) {
      console.error(e);
    }
  }, 100);

  try {
    currentMeetingFile = await invoke('start_meeting', { title: "Recording..." });
  } catch (err) {
    console.error(err);
    alert("Failed to start meeting: " + err);
    cancelMeetingRecording();
  }
}

async function stopMeetingRecording() {
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  
  showProcessingView();
  
  const titleInput = document.getElementById('meeting-title-input');
  let title = titleInput.value.trim();
  if (!title) {
    title = `Meeting on ${new Date().toLocaleDateString()} ${new Date().toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}`;
  }

  try {
    const oggPath = await invoke('stop_meeting');
    const resultJson = await invoke('upload_meeting', { title, filepath: oggPath });
    const m = JSON.parse(resultJson);
    
    showDashboardView();
    loadMeetings();
    viewMeeting(m.id);
  } catch (err) {
    console.error(err);
    alert("Failed to finalize meeting note: " + err);
    showDashboardView();
    loadMeetings();
  }
}

async function cancelMeetingRecording() {
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  try {
    await invoke('cancel_meeting');
  } catch (e) {}
  showDashboardView();
  loadMeetings();
}

async function confirmDeleteMeeting() {
  if (!activeMeetingId) return;
  if (!confirm("Are you sure you want to delete this meeting note? This action cannot be undone.")) return;
  
  const token = currentConfig ? currentConfig.session_token : null;
  try {
    const res = await fetch(`${API_URL}/meetings/${activeMeetingId}`, {
      method: 'DELETE',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });
    if (!res.ok) throw new Error("Failed to delete");
    activeMeetingId = null;
    showDashboardView();
    loadMeetings();
  } catch (err) {
    console.error(err);
    alert("Failed to delete meeting note: " + err);
  }
}

function parseMarkdownToHTML(md) {
  if (!md) return '';
  // Escape untrusted server/model output before adding the small markdown subset
  // supported by this view. This prevents stored XSS in meeting summaries.
  let html = escapeHtml(String(md));
  // Convert headers
  html = html.replace(/^### (.*$)/gim, '<h3>$1</h3>');
  html = html.replace(/^## (.*$)/gim, '<h2>$1</h2>');
  html = html.replace(/^# (.*$)/gim, '<h1>$1</h1>');
  // Convert bold
  html = html.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
  
  const lines = html.split('\n');
  let inList = false;
  let parsedLines = [];
  for (let line of lines) {
    let clean = line.trim();
    if (clean.startsWith('- ') || clean.startsWith('* ')) {
      if (!inList) {
        parsedLines.push('<ul style="margin: 0 0 10px 0; padding-left: 20px;">');
        inList = true;
      }
      parsedLines.push(`<li>${clean.substring(2)}</li>`);
    } else {
      if (inList) {
        parsedLines.push('</ul>');
        inList = false;
      }
      if (clean) {
        parsedLines.push(`<p style="margin: 0 0 8px 0;">${line}</p>`);
      } else {
        parsedLines.push('<br>');
      }
    }
  }
  if (inList) {
    parsedLines.push('</ul>');
  }
  return parsedLines.join('\n');
}

function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}


