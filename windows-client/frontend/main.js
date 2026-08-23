// SpeekTo.Me Settings Frontend Controller
// Utilizes window.__TAURI__ global APIs for zero-bundler simplicity.

const { invoke } = window.__TAURI__.core;
const { listen } = window.__TAURI__.event;

// State variables
let currentConfig = null;
let currentUserInfo = null;
let unlistenLoginSuccess = null;
let unlistenLoginFailed = null;
let currentStep = 1;
let API_URL = "https://api.speekto.me";

function isUserPro(info) {
  if (!info) return false;
  const active = (info.plan_active === true || info.plan_active === 1 || info.plan_active === 'true' || info.plan_active === '1');
  const tier = (info.plan_tier || '').toLowerCase();
  return Boolean(active && tier === 'pro');
}

function showToast(message, title = "Notification", type = "error") {
  let container = document.querySelector('.custom-toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'custom-toast-container';
    document.body.appendChild(container);
  }

  let iconSvg = '';
  if (type === 'warning') {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#FFD700" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>`;
  } else if (type === 'info') {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="var(--accent-color, #00e676)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>`;
  } else {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#ff5252" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`;
  }
  
  const toast = document.createElement('div');
  toast.className = `custom-toast ${type}`;
  toast.innerHTML = `
    <div class="custom-toast-icon">${iconSvg}</div>
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
const tabItems = document.querySelectorAll('.nav-item[data-tab]');
const tabPanes = document.querySelectorAll('.tab-pane');

// Dashboard Input Elements
const hotkeySelect = document.getElementById('hotkey-select');
const hotkeyModeSelect = document.getElementById('hotkey-mode-select');
const autostartCheck = document.getElementById('autostart-check');
const typingModeCheck = document.getElementById('typing-mode-check');
const soundFeedbackCheck = document.getElementById('sound-feedback-check');
const smartSpacingCheck = document.getElementById('smart-spacing-check');
const modelSelect = document.getElementById('model-select');
const enhanceCheck = document.getElementById('enhance-check');
const streamingModeCheck = document.getElementById('streaming-mode-check');
const streamingProHint = document.getElementById('streaming-pro-hint');
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
const onboardHotkeyModeSelect = document.getElementById('onboard-hotkey-mode-select');
const onboardAutostartCheck = document.getElementById('onboard-autostart-check');
const onboardStreamingModeCheck = document.getElementById('onboard-streaming-mode-check');
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

const SUPPORTED_LANGUAGES = [
  { code: "af", name: "Afrikaans" }, { code: "am", name: "Amharic" }, { code: "ar", name: "Arabic" },
  { code: "as", name: "Assamese" }, { code: "az", name: "Azerbaijani" }, { code: "ba", name: "Bashkir" },
  { code: "be", name: "Belarusian" }, { code: "bg", name: "Bulgarian" }, { code: "bn", name: "Bengali" },
  { code: "bo", name: "Tibetan" }, { code: "br", name: "Breton" }, { code: "bs", name: "Bosnian" },
  { code: "ca", name: "Catalan" }, { code: "cs", name: "Czech" }, { code: "cy", name: "Welsh" },
  { code: "da", name: "Danish" }, { code: "de", name: "German" }, { code: "el", name: "Greek" },
  { code: "en", name: "English" }, { code: "es", name: "Spanish" }, { code: "et", name: "Estonian" },
  { code: "eu", name: "Basque" }, { code: "fa", name: "Persian" }, { code: "fi", name: "Finnish" },
  { code: "fo", name: "Faroese" }, { code: "fr", name: "French" }, { code: "gl", name: "Galician" },
  { code: "gu", name: "Gujarati" }, { code: "ha", name: "Hausa" }, { code: "haw", name: "Hawaiian" },
  { code: "he", name: "Hebrew" }, { code: "hi", name: "Hindi" }, { code: "hr", name: "Croatian" },
  { code: "ht", name: "Haitian Creole" }, { code: "hu", name: "Hungarian" }, { code: "hy", name: "Armenian" },
  { code: "id", name: "Indonesian" }, { code: "is", name: "Icelandic" }, { code: "it", name: "Italian" },
  { code: "ja", name: "Japanese" }, { code: "jw", name: "Javanese" }, { code: "ka", name: "Georgian" },
  { code: "kk", name: "Kazakh" }, { code: "km", name: "Khmer" }, { code: "kn", name: "Kannada" },
  { code: "ko", name: "Korean" }, { code: "la", name: "Latin" }, { code: "lb", name: "Luxembourgish" },
  { code: "ln", name: "Lingala" }, { code: "lo", name: "Lao" }, { code: "lt", name: "Lithuanian" },
  { code: "lv", name: "Latvian" }, { code: "mg", name: "Malagasy" }, { code: "mi", name: "Maori" },
  { code: "mk", name: "Macedonian" }, { code: "ml", name: "Malayalam" }, { code: "mn", name: "Mongolian" },
  { code: "mr", name: "Marathi" }, { code: "ms", name: "Malay" }, { code: "mt", name: "Maltese" },
  { code: "my", name: "Burmese" }, { code: "ne", name: "Nepali" }, { code: "nl", name: "Dutch" },
  { code: "nn", name: "Nynorsk" }, { code: "no", name: "Norwegian" }, { code: "oc", name: "Occitan" },
  { code: "pa", name: "Punjabi" }, { code: "pl", name: "Polish" }, { code: "ps", name: "Pashto" },
  { code: "pt", name: "Portuguese" }, { code: "ro", name: "Romanian" }, { code: "ru", name: "Russian" },
  { code: "sa", name: "Sanskrit" }, { code: "sd", name: "Sindhi" }, { code: "si", name: "Sinhala" },
  { code: "sk", name: "Slovak" }, { code: "sl", name: "Slovenian" }, { code: "sn", name: "Shona" },
  { code: "so", name: "Somali" }, { code: "sq", name: "Albanian" }, { code: "sr", name: "Serbian" },
  { code: "su", name: "Sundanese" }, { code: "sv", name: "Swedish" }, { code: "sw", name: "Swahili" },
  { code: "ta", name: "Tamil" }, { code: "te", name: "Telugu" }, { code: "tg", name: "Tajik" },
  { code: "th", name: "Thai" }, { code: "tk", name: "Turkmen" }, { code: "tl", name: "Tagalog" },
  { code: "tr", name: "Turkish" }, { code: "tt", name: "Tatar" }, { code: "uk", name: "Ukrainian" },
  { code: "ur", name: "Urdu" }, { code: "uz", name: "Uzbek" }, { code: "vi", name: "Vietnamese" },
  { code: "yi", name: "Yiddish" }, { code: "yo", name: "Yoruba" }, { code: "yue", name: "Cantonese" },
  { code: "zh", name: "Chinese" }
];

let selectedLanguages = ["en"];

function getLanguageName(code) {
  const item = SUPPORTED_LANGUAGES.find(l => l.code === code);
  return item ? item.name : code;
}

function renderClientLanguageUI() {
  const containers = [
    { chips: document.getElementById('client-lang-chips'), select: document.getElementById('client-lang-select') },
    { chips: document.getElementById('onboard-lang-chips'), select: document.getElementById('onboard-lang-select') }
  ];

  containers.forEach(({ chips: chipsContainer, select: selectEl }) => {
    if (!chipsContainer || !selectEl) return;

    chipsContainer.innerHTML = '';
    selectedLanguages.forEach(code => {
      const chip = document.createElement('span');
      chip.className = 'lang-chip';
      chip.style.cssText = 'display:inline-flex;align-items:center;gap:6px;background:var(--accent-color, #00e676);color:#000000;padding:4px 10px;border-radius:12px;font-size:12px;font-weight:700;box-shadow:0 1px 3px rgba(0,0,0,0.3);';
      chip.innerHTML = `${escapeHtml(getLanguageName(code))} <button type="button" style="background:none;border:none;color:#000000;cursor:pointer;font-weight:bold;margin-left:2px;font-size:14px;line-height:1;" title="Remove">&times;</button>`;
      
      chip.querySelector('button').addEventListener('click', () => {
        if (selectedLanguages.length <= 1) {
          showToast("At least 1 language must be selected.", "Languages", "warning");
          return;
        }
        selectedLanguages = selectedLanguages.filter(c => c !== code);
        saveLanguagesToBackend();
      });
      chipsContainer.appendChild(chip);
    });

    selectEl.innerHTML = '';
    
    // Add placeholder default option
    const placeholderOpt = document.createElement('option');
    placeholderOpt.value = '';
    placeholderOpt.textContent = '-- Click to select language --';
    placeholderOpt.disabled = true;
    placeholderOpt.selected = true;
    selectEl.appendChild(placeholderOpt);

    SUPPORTED_LANGUAGES.forEach(lang => {
      if (!selectedLanguages.includes(lang.code)) {
        const opt = document.createElement('option');
        opt.value = lang.code;
        opt.textContent = lang.name;
        selectEl.appendChild(opt);
      }
    });
  });
}

async function saveLanguagesToBackend() {
  try {
    const updated = await invoke('update_languages', { languages: selectedLanguages });
    selectedLanguages = updated || selectedLanguages;
    renderClientLanguageUI();
    showToast("Spoken languages saved!", "Languages", "info");
  } catch (err) {
    showToast(err || "Failed to update language preferences.", "Languages", "error");
    renderClientLanguageUI();
  }
}

async function init() {
  try {
    try {
      API_URL = await invoke('get_api_url');
    } catch (err) {
      console.error("Failed to load API URL from backend:", err);
    }

    const addLangBtn = document.getElementById('client-add-lang-btn');
    if (addLangBtn) {
      addLangBtn.addEventListener('click', () => {
        const selectEl = document.getElementById('client-lang-select');
        if (!selectEl || !selectEl.value) {
          showToast("Please select a language from the dropdown.", "Languages", "warning");
          return;
        }
        if (selectedLanguages.length >= 5) {
          showToast("You can select at most 5 spoken languages.", "Languages", "warning");
          return;
        }
        selectedLanguages.push(selectEl.value);
        saveLanguagesToBackend();
      });
    }

    const onboardAddLangBtn = document.getElementById('onboard-add-lang-btn');
    if (onboardAddLangBtn) {
      onboardAddLangBtn.addEventListener('click', () => {
        const selectEl = document.getElementById('onboard-lang-select');
        if (!selectEl || !selectEl.value) {
          showToast("Please select a language from the dropdown.", "Languages", "warning");
          return;
        }
        if (selectedLanguages.length >= 5) {
          showToast("You can select at most 5 spoken languages.", "Languages", "warning");
          return;
        }
        selectedLanguages.push(selectEl.value);
        saveLanguagesToBackend();
      });
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
    currentUserInfo = userInfo;
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
          currentUserInfo = freshInfo;
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

    // 3.6. Bind Open Web Dashboard Sidebar Button
    const openWebDashboardNavBtn = document.getElementById('open-web-dashboard-nav-btn');
    if (openWebDashboardNavBtn) {
      openWebDashboardNavBtn.addEventListener('click', async (e) => {
        e.preventDefault();
        try {
          await invoke('open_external_url', { url: "https://speekto.me/dashboard" });
        } catch (err) {
          console.error("Failed to open web dashboard:", err);
          // Fallback if running outside full Tauri context
          window.open("https://speekto.me/dashboard", "_blank");
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

    const handleStreamingToggleChange = (checkbox) => {
      const isPaid = isUserPro(currentUserInfo);
      if (!isPaid && checkbox.checked) {
        checkbox.checked = false;
        if (streamingProHint) streamingProHint.style.display = 'block';
        showToast("Real-Time Streaming Dictation is an exclusive Pro feature. Upgrade your subscription on your Web Dashboard to unlock it!", "Pro Subscription Required", "warning");
      } else if (isPaid) {
        if (streamingProHint) streamingProHint.style.display = 'none';
        if (streamingModeCheck) streamingModeCheck.checked = checkbox.checked;
        if (onboardStreamingModeCheck) onboardStreamingModeCheck.checked = checkbox.checked;
      }
    };

    if (streamingModeCheck) {
      streamingModeCheck.addEventListener('change', () => handleStreamingToggleChange(streamingModeCheck));
    }
    if (onboardStreamingModeCheck) {
      onboardStreamingModeCheck.addEventListener('change', () => handleStreamingToggleChange(onboardStreamingModeCheck));
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
  if (hotkeySelect) hotkeySelect.value = config.hotkey || 'Ctrl + Shift';
  if (hotkeyModeSelect) hotkeyModeSelect.value = config.hotkey_mode || 'hold';
  autostartCheck.checked = config.autostart;
  typingModeCheck.checked = config.typing_mode;
  soundFeedbackCheck.checked = config.sound_feedback;
  smartSpacingCheck.checked = config.smart_spacing;
  modelSelect.value = config.model;
  enhanceCheck.checked = config.enhance;
  if (streamingModeCheck) {
    streamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
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
  if (onboardHotkeySelect) onboardHotkeySelect.value = config.hotkey || 'Ctrl + Shift';
  if (onboardHotkeyModeSelect) onboardHotkeyModeSelect.value = config.hotkey_mode || 'hold';
  onboardAutostartCheck.checked = config.autostart;
  if (onboardStreamingModeCheck) {
    onboardStreamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
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
      hotkey: onboardHotkeySelect ? onboardHotkeySelect.value : 'Ctrl + Shift',
      hotkey_mode: onboardHotkeyModeSelect ? onboardHotkeyModeSelect.value : 'hold',
      autostart: onboardAutostartCheck.checked,
      typing_mode: onboardTypingModeCheck.checked,
      sound_feedback: onboardSoundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked, // Kept same as current
      model: onboardModelSelect.value,
      enhance: onboardEnhanceCheck.checked,
      transcription_mode: (onboardStreamingModeCheck && onboardStreamingModeCheck.checked) ? "stream" : "batch",
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
      hotkey: hotkeySelect ? hotkeySelect.value : 'Ctrl + Shift',
      hotkey_mode: hotkeyModeSelect ? hotkeyModeSelect.value : 'hold',
      autostart: autostartCheck.checked,
      typing_mode: typingModeCheck.checked,
      sound_feedback: soundFeedbackCheck.checked,
      smart_spacing: smartSpacingCheck.checked,
      model: modelSelect.value,
      enhance: enhanceCheck.checked,
      transcription_mode: (streamingModeCheck && streamingModeCheck.checked) ? "stream" : "batch",
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

function formatUsageParts(usedSec, totalSec, isPaid = false) {
  const safeUsed = Math.max(0, Number(usedSec) || 0.0);
  const rawTotalSec = Math.max(0, Number(totalSec) || 3600.0);
  
  const totalHours = rawTotalSec / 3600.0;

  let usedStr = '0s';
  if (safeUsed < 60) {
    usedStr = `${Math.round(safeUsed)}s`;
  } else if (safeUsed < 3600) {
    usedStr = `${Math.round(safeUsed / 60)}m`;
  } else {
    usedStr = `${(safeUsed / 3600.0).toFixed(1)}h`;
  }

  // Strictly check if user is on a paid/pro plan and quota >= 730 hours before showing unlimited
  const isUnlimited = isPaid && (totalHours >= 730.0);
  
  let totalStr = '1h';
  if (isUnlimited) {
    totalStr = 'Unlimited';
  } else if (totalHours < 1.0) {
    const totalMins = Math.round(rawTotalSec / 60.0);
    totalStr = totalMins > 0 ? `${totalMins}m` : `${Math.round(rawTotalSec)}s`;
  } else if (Number.isInteger(totalHours)) {
    totalStr = `${Math.round(totalHours)}h`;
  } else {
    totalStr = `${totalHours.toFixed(1)}h`;
  }

  const fupHours = Math.round(totalHours);
  const fupTooltip = `Fair Usage Limit: ${fupHours.toLocaleString()} hours / month.`;

  return { usedStr, totalStr, isUnlimited, fupTooltip, display: `${usedStr} / ${totalStr}` };
}

function formatUsageFraction(usedSec, totalSec) {
  return formatUsageParts(usedSec, totalSec).display;
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
    if (userInfo.languages && Array.isArray(userInfo.languages)) {
      selectedLanguages = userInfo.languages;
      renderClientLanguageUI();
    }
    userNameEl.textContent = userInfo.name;
    userEmailEl.textContent = userInfo.email;
    
    // Bind subscription fields
    const subPlan = document.getElementById('sub-plan');
    const subStatus = document.getElementById('sub-status');
    const subActivated = document.getElementById('sub-activated');
    const subUsage = document.getElementById('sub-usage');
    
    if (subPlan) subPlan.textContent = (userInfo.plan_tier || 'Free').toUpperCase();
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

    currentUserInfo = userInfo;

    // Calculate quota limits and display used/total fraction
    const isPaid = isUserPro(userInfo);
    if (streamingModeCheck) {
      if (!isPaid) {
        streamingModeCheck.checked = false;
        if (streamingProHint) streamingProHint.style.display = 'block';
      } else {
        if (streamingProHint) streamingProHint.style.display = 'none';
      }
    }
    if (onboardStreamingModeCheck && !isPaid) {
      onboardStreamingModeCheck.checked = false;
    }
    const maxSeconds = isPaid ? (userInfo.monthly_limit_seconds || 180000.0) : (userInfo.monthly_limit_seconds || 3600.0);
    const usageSeconds = userInfo.duration_seconds || 0.0;
    const percent = Math.min(100.0, (usageSeconds / maxSeconds) * 100.0);
    
    const subUsagePercent = document.getElementById('sub-usage-percent');
    const subUsageBar = document.getElementById('sub-usage-bar');
    
    if (subUsagePercent) {
      const usage = formatUsageParts(usageSeconds, maxSeconds, isPaid);
      if (usage.isUnlimited) {
        subUsagePercent.innerHTML = `${usage.usedStr} / ${usage.totalStr} <span class="fup-tooltip-badge" title="${usage.fupTooltip}" aria-label="${usage.fupTooltip}"><svg viewBox="0 0 24 24" width="8" height="8" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg></span>`;
      } else {
        subUsagePercent.textContent = usage.display;
      }
    }
    if (subUsageBar) subUsageBar.style.width = `${percent.toFixed(1)}%`;

    // Hide descriptive text when user is successfully logged in
    if (accountDesc) accountDesc.classList.add('hidden');

    authLoggedOut.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedIn.classList.remove('hidden');

    // Render sidebar announcement / notification if provided
    const sidebarAnnouncement = document.getElementById('sidebar-announcement');
    const sidebarAnnouncementText = document.getElementById('sidebar-announcement-text');
    if (sidebarAnnouncement && sidebarAnnouncementText) {
      const notif = (userInfo.notification || '').trim();
      if (notif) {
        sidebarAnnouncementText.textContent = notif;
        sidebarAnnouncement.classList.remove('hidden');
      } else {
        sidebarAnnouncement.classList.add('hidden');
      }
    }

    const sidebarFooter = document.getElementById('sidebar-footer');
    if (sidebarFooter) sidebarFooter.style.display = 'block';
  } else {
    // Show descriptive text when logged out
    if (accountDesc) accountDesc.classList.remove('hidden');

    const sidebarAnnouncement = document.getElementById('sidebar-announcement');
    if (sidebarAnnouncement) sidebarAnnouncement.classList.add('hidden');

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
    currentUserInfo = null;
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
    currentUserInfo = userInfo;
    
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
        currentUserInfo = userInfo;
        
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
    emptyMsg.textContent = 'Error connecting to SpeekTo.Me server.';
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

      // Determine local user name if available (from DOM or window state)
      const domUserName = userNameEl && userNameEl.textContent ? userNameEl.textContent.trim() : '';
      const userName = domUserName ? domUserName.toLowerCase() : '';

      segments.forEach(seg => {
        const speakerName = seg.speaker || 'Speaker';
        
        if (speakerName !== currentSpeaker) {
          currentSpeaker = speakerName;
          
          currentBlock = document.createElement('div');
          
          const cleanLowerSpeaker = speakerName.toLowerCase().replace(/[\[\]]/g, '').trim();
          const isRemote = cleanLowerSpeaker.includes('remote') || cleanLowerSpeaker.includes('system') || cleanLowerSpeaker.includes('participant') || cleanLowerSpeaker.includes('caller');
          const isMe = !isRemote && (cleanLowerSpeaker === 'you' || cleanLowerSpeaker === 'me' || (userName && cleanLowerSpeaker.includes(userName)) || !cleanLowerSpeaker.includes('speaker 2'));

          currentBlock.className = `meeting-speaker-block ${isRemote ? 'is-remote' : 'is-me'}`;
          
          const header = document.createElement('div');
          header.className = 'meeting-speaker-header';
          
          const avatar = document.createElement('div');
          avatar.className = 'meeting-speaker-avatar';
          avatar.textContent = getInitials(speakerName);
          avatar.style.backgroundColor = isRemote ? '#2f80ed' : getAvatarColor(speakerName);
          
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

    let isUserHoveringTranscript = false;
    transcriptTextEl.onmouseenter = () => { isUserHoveringTranscript = true; };
    transcriptTextEl.onmouseleave = () => { isUserHoveringTranscript = false; };
    transcriptTextEl.onwheel = () => { isUserHoveringTranscript = true; };

    let lastActiveSpan = null;

    player.ontimeupdate = () => {
      const time = player.currentTime;
      const spans = transcriptTextEl.querySelectorAll('.transcript-segment');
      let currentActiveSpan = null;

      spans.forEach(span => {
        const start = parseFloat(span.dataset.start);
        const end = parseFloat(span.dataset.end);
        if (time >= start && time <= end) {
          span.classList.add('active');
          currentActiveSpan = span;
        } else {
          span.classList.remove('active');
        }
      });

      if (currentActiveSpan && currentActiveSpan !== lastActiveSpan) {
        lastActiveSpan = currentActiveSpan;
        if (!isUserHoveringTranscript) {
          currentActiveSpan.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }
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

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
