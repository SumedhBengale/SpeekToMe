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
const themeSelect = document.getElementById('theme-select');

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
const onboardThemeSelect = document.getElementById('onboard-theme-select');

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

        // Refresh user subscription status when visiting the account page
        if (tabId === 'account') {
          invoke('get_user_info').then(info => {
            updateAuthUI(info);
          });
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
    onboardNext3Btn.addEventListener('click', () => goToStep(4));
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
  if (themeSelect) themeSelect.value = config.theme || 'forest';

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
      session_token: currentConfig ? currentConfig.session_token : null,
      theme: onboardThemeSelect ? onboardThemeSelect.value : 'forest'
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
      session_token: currentConfig ? currentConfig.session_token : null,
      theme: themeSelect ? themeSelect.value : 'forest'
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
    alert("Authentication failed: " + event.payload);
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

// Run initialization
document.addEventListener('DOMContentLoaded', init);

