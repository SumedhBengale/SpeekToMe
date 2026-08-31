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

function showToast(message, title = "Notification", type = "info") {
  let container = document.querySelector('.custom-toast-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'custom-toast-container';
    document.body.appendChild(container);
  }

  let iconSvg = '';
  if (type === 'success') {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#22c55e" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="9 12 11.5 14.5 15.5 9.5"/></svg>`;
  } else if (type === 'warning') {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#f59e0b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>`;
  } else if (type === 'info') {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="var(--accent-color, #00e676)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>`;
  } else {
    iconSvg = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#ef4444" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`;
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
const modelSelect = document.getElementById('model-select');
const streamingModeCheck = document.getElementById('streaming-mode-check');
const streamingProHint = document.getElementById('streaming-pro-hint');
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
const onboardReopenBrowserBtn = document.getElementById('onboard-reopen-browser-btn');
const onboardNext1Btn = document.getElementById('onboard-next-1-btn');
const onboardNext2Btn = document.getElementById('onboard-next-2-btn');
const onboardNext3Btn = document.getElementById('onboard-next-3-btn');
const onboardFinishBtn = document.getElementById('onboard-finish-btn');
const onboardUpgradeProBtn = document.getElementById('onboard-upgrade-pro-btn');
const onboardSkipProBtn = document.getElementById('onboard-skip-pro-btn');
const onboardPrevStepBtn = document.getElementById('onboard-prev-step-btn');
const onboardNextStepBtn = document.getElementById('onboard-next-step-btn');

const onboardAuthLoggedOut = document.getElementById('onboard-auth-logged-out');
const onboardAuthLoggingIn = document.getElementById('onboard-auth-logging-in');
const onboardAuthLoggedIn = document.getElementById('onboard-auth-logged-in');
const onboardUserNameEl = document.getElementById('onboard-user-name');
const onboardUserEmailEl = document.getElementById('onboard-user-email');

// Promo UI Elements
const onboardPromoBadge = document.getElementById('onboard-promo-badge');
const onboardPromoTitle = document.getElementById('onboard-promo-title');
const onboardPromoSubtitle = document.getElementById('onboard-promo-subtitle');
const onboardPromoPrice = document.getElementById('onboard-promo-price');
const onboardPromoPeriod = document.getElementById('onboard-promo-period');
const onboardPromoCtaText = document.getElementById('onboard-promo-cta-text');
const onboardPromoSkipText = document.getElementById('onboard-promo-skip-text');
let promoUpgradeUrl = "https://speekto.me/dashboard";

// Step 5 Demo Video UI Elements
const onboardDemoVideo = document.getElementById('onboard-demo-video');
const videoPlayerContainer = document.getElementById('video-player-container');
const videoCenterPlayBtn = document.getElementById('video-center-play-btn');
const videoPlayPauseBtn = document.getElementById('video-play-pause-btn');
const videoPlayIcon = document.getElementById('video-play-icon');
const videoPauseIcon = document.getElementById('video-pause-icon');
const videoCurrentTime = document.getElementById('video-current-time');
const videoDuration = document.getElementById('video-duration');
const videoScrubber = document.getElementById('video-scrubber');
const videoProgressFill = document.getElementById('video-progress-fill');
const videoMuteBtn = document.getElementById('video-mute-btn');
const videoVolHighIcon = document.getElementById('video-vol-high-icon');
const videoVolMutedIcon = document.getElementById('video-vol-muted-icon');
const videoVolumeSlider = document.getElementById('video-volume-slider');
const videoFullscreenBtn = document.getElementById('video-fullscreen-btn');
let cachedDemoVideoUrl = "https://vjs.zencdn.net/v/oceans.mp4";

// Onboarding Inputs
const onboardHotkeySelect = document.getElementById('onboard-hotkey-select');
const onboardHotkeyModeSelect = document.getElementById('onboard-hotkey-mode-select');
const onboardAutostartCheck = document.getElementById('onboard-autostart-check');
const onboardStreamingModeCheck = document.getElementById('onboard-streaming-mode-check');
const onboardTypingModeCheck = document.getElementById('onboard-typing-mode-check');
const onboardSoundFeedbackCheck = document.getElementById('onboard-sound-feedback-check');
const onboardModelSelect = document.getElementById('onboard-model-select');
const onboardThemeSelect = document.getElementById('onboard-theme-select');

// ==========================================
// Initialization & Config Binding
// ==========================================

let SUPPORTED_LANGUAGES = [
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
    { chips: document.getElementById('client-lang-chips'), count: document.getElementById('client-lang-count') },
    { chips: document.getElementById('onboard-lang-chips'), count: document.getElementById('onboard-lang-count') }
  ];

  containers.forEach(({ chips: chipsContainer, count: countEl }) => {
    if (countEl) {
      countEl.textContent = `${selectedLanguages.length} / 5`;
    }
    if (!chipsContainer) return;

    chipsContainer.innerHTML = '';
    selectedLanguages.forEach(code => {
      const chip = document.createElement('span');
      chip.className = 'lang-chip';
      chip.innerHTML = `<span>${escapeHtml(getLanguageName(code))}</span> <button type="button" class="lang-chip-remove" title="Remove">&times;</button>`;
      
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
  });
}

function setupLanguageCombobox(inputId, dropdownId, addBtnId) {
  const inputEl = document.getElementById(inputId);
  const dropdownEl = document.getElementById(dropdownId);
  const addBtn = document.getElementById(addBtnId);

  if (!inputEl || !dropdownEl) return;

  let highlightedIndex = -1;
  let currentMatches = [];

  function filterLanguages(query) {
    const q = (query || '').trim().toLowerCase();
    const available = SUPPORTED_LANGUAGES.filter(lang => !selectedLanguages.includes(lang.code));
    if (!q) {
      return available.slice(0, 15);
    }
    return available.filter(lang => 
      lang.name.toLowerCase().includes(q) || lang.code.toLowerCase().includes(q)
    ).slice(0, 20);
  }

  function renderDropdown(matches) {
    currentMatches = matches;
    highlightedIndex = -1;
    dropdownEl.innerHTML = '';

    if (matches.length === 0) {
      dropdownEl.innerHTML = '<div class="lang-dropdown-empty">No matching languages found</div>';
      dropdownEl.classList.remove('hidden');
      return;
    }

    matches.forEach((lang, idx) => {
      const item = document.createElement('div');
      item.className = 'lang-dropdown-item';
      item.dataset.index = idx;
      item.innerHTML = `<span>${escapeHtml(lang.name)}</span><span class="lang-code">${escapeHtml(lang.code)}</span>`;
      
      item.addEventListener('mousedown', (e) => {
        e.preventDefault(); // Prevent blur
        addSelectedLanguage(lang.code);
      });
      dropdownEl.appendChild(item);
    });

    dropdownEl.classList.remove('hidden');
  }

  function addSelectedLanguage(code) {
    if (!code) return;
    if (selectedLanguages.length >= 5) {
      showToast("You can select at most 5 spoken languages.", "Languages", "warning");
      dropdownEl.classList.add('hidden');
      inputEl.value = '';
      return;
    }
    if (!selectedLanguages.includes(code)) {
      selectedLanguages.push(code);
      saveLanguagesToBackend();
    }
    inputEl.value = '';
    dropdownEl.classList.add('hidden');
  }

  inputEl.addEventListener('focus', () => {
    renderDropdown(filterLanguages(inputEl.value));
  });

  inputEl.addEventListener('input', () => {
    renderDropdown(filterLanguages(inputEl.value));
  });

  inputEl.addEventListener('keydown', (e) => {
    if (dropdownEl.classList.contains('hidden')) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        renderDropdown(filterLanguages(inputEl.value));
      }
      return;
    }

    const items = dropdownEl.querySelectorAll('.lang-dropdown-item');
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      highlightedIndex = Math.min(highlightedIndex + 1, items.length - 1);
      updateHighlight(items);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      highlightedIndex = Math.max(highlightedIndex - 1, 0);
      updateHighlight(items);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (highlightedIndex >= 0 && currentMatches[highlightedIndex]) {
        addSelectedLanguage(currentMatches[highlightedIndex].code);
      } else if (currentMatches.length > 0) {
        addSelectedLanguage(currentMatches[0].code);
      }
    } else if (e.key === 'Escape') {
      dropdownEl.classList.add('hidden');
    }
  });

  function updateHighlight(items) {
    items.forEach((item, idx) => {
      if (idx === highlightedIndex) {
        item.classList.add('active');
        item.scrollIntoView({ block: 'nearest' });
      } else {
        item.classList.remove('active');
      }
    });
  }

  inputEl.addEventListener('blur', () => {
    setTimeout(() => {
      dropdownEl.classList.add('hidden');
    }, 200);
  });

  if (addBtn) {
    addBtn.addEventListener('click', () => {
      const q = inputEl.value.trim().toLowerCase();
      const match = SUPPORTED_LANGUAGES.find(l => 
        !selectedLanguages.includes(l.code) && 
        (l.name.toLowerCase() === q || l.code.toLowerCase() === q)
      );
      if (match) {
        addSelectedLanguage(match.code);
      } else if (currentMatches.length > 0) {
        addSelectedLanguage(currentMatches[0].code);
      } else {
        showToast("Please search and select a language from the list.", "Languages", "warning");
      }
    });
  }
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
  // Initialize Custom Themed Dropdowns immediately before any async backend calls
  initCustomSelects();

  // 0. Synchronously bind all core UI listeners first so buttons are ALWAYS responsive
  const saveBtnEl = document.getElementById('save-btn');
  if (saveBtnEl) saveBtnEl.addEventListener('click', saveAndApply);

  const hideBtnEl = document.getElementById('hide-btn');
  if (hideBtnEl) hideBtnEl.addEventListener('click', hideWindow);

  const refreshBtnEl = document.getElementById('refresh-data-btn');
  if (refreshBtnEl) refreshBtnEl.addEventListener('click', refreshUserData);

  const winMinBtn = document.getElementById('win-min-btn');
  if (winMinBtn) winMinBtn.addEventListener('click', () => invoke('minimize_window'));

  const winMaxBtn = document.getElementById('win-max-btn');
  if (winMaxBtn) winMaxBtn.addEventListener('click', () => invoke('toggle_maximize_window'));

  const winCloseBtn = document.getElementById('win-close-btn');
  if (winCloseBtn) winCloseBtn.addEventListener('click', () => invoke('close_window'));

  const onboardWinMinBtn = document.getElementById('onboard-win-min-btn');
  if (onboardWinMinBtn) onboardWinMinBtn.addEventListener('click', () => invoke('minimize_window'));

  const onboardWinCloseBtn = document.getElementById('onboard-win-close-btn');
  if (onboardWinCloseBtn) onboardWinCloseBtn.addEventListener('click', () => invoke('close_window'));

  // 1. Tab selection setup (Dashboard)
  tabItems.forEach(item => {
    item.addEventListener('click', (e) => {
      const tabId = item.getAttribute('data-tab');
      if (!tabId) return;
      e.preventDefault();

      const targetPane = document.getElementById(`tab-${tabId}`);
      if (!targetPane) return;
      
      tabItems.forEach(nav => nav.classList.remove('active'));
      tabPanes.forEach(pane => pane.classList.remove('active'));
      
      item.classList.add('active');
      targetPane.classList.add('active');

      // Refresh user subscription status when visiting the account page
      if (tabId === 'account') {
        invoke('get_user_info').then(info => {
          updateAuthUI(info);
        }).catch(err => console.warn("Failed to refresh user info on account tab:", err));
      } else if (tabId === 'vocabulary') {
        loadVocabulary();
      } else if (tabId === 'meetings') {
        loadMeetings();
      }
    });
  });

  // 1.5. Attach live dirty detection listeners across all dashboard inputs
  const dashboardInputs = [
    hotkeySelect,
    hotkeyModeSelect,
    autostartCheck,
    typingModeCheck,
    soundFeedbackCheck,
    themeSelect,
    pillPosModeSelect,
    pillPredefinedSelect,
    pillCustomX,
    pillCustomY,
    modelSelect,
    streamingModeCheck
  ];

  dashboardInputs.forEach(input => {
    if (input) {
      input.addEventListener('change', markDirty);
      input.addEventListener('input', markDirty);
    }
  });

  try {
    try {
      API_URL = await invoke('get_api_url');
      console.log("[INIT] API_URL loaded:", API_URL);
    } catch (err) {
      console.error("[INIT] Failed to load API URL from backend:", err);
    }

    try {
      const dynamicLangs = await invoke('get_supported_languages');
      if (dynamicLangs && dynamicLangs.length > 0) {
        SUPPORTED_LANGUAGES = dynamicLangs;
        console.log(`[LANGUAGES] Loaded ${dynamicLangs.length} dynamic languages from backend.`);
      }
    } catch (err) {
      console.warn("[INIT] Could not load dynamic languages list from backend, using defaults:", err);
    }

    setupLanguageCombobox('client-lang-search-input', 'client-lang-dropdown-list', 'client-add-lang-btn');
    setupLanguageCombobox('onboard-lang-search-input', 'onboard-lang-dropdown-list', 'onboard-add-lang-btn');

    // 2. Fetch and apply configuration
    currentConfig = await invoke('get_config');
    console.log("[INIT] Config loaded:", currentConfig);
    applyConfigToUI(currentConfig);

    // 3. Fetch user information status & update dashboard UI
    const userInfo = await invoke('get_user_info');
    console.log("[INIT] UserInfo loaded:", userInfo);
    currentUserInfo = userInfo;
    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);

    // 3.55. Listen for Session Expiration Event from Backend
    try {
      listen('auth-session-expired', () => {
        console.warn("[AUTH] Session expired signal received from backend.");
        currentUserInfo = null;
        if (currentConfig) {
          currentConfig.session_token = null;
        }
        updateAuthUI(null);
        updateOnboardAuthUI(null);

        // Switch to account tab
        tabItems.forEach(nav => nav.classList.remove('active'));
        tabPanes.forEach(pane => pane.classList.remove('active'));

        const accountTabNav = document.querySelector('.nav-item[data-tab="account"]');
        const accountTabPane = document.getElementById('tab-account');
        if (accountTabNav) accountTabNav.classList.add('active');
        if (accountTabPane) accountTabPane.classList.add('active');

        showToast("Your session has expired. Please sign in again.", "Session Expired", "warning");
      });
    } catch (err) {
      console.error("Failed to bind auth-session-expired listener:", err);
    }

    // 3.6. Bind Open Web Dashboard Sidebar Button & Manage Account Button
    const openWebDashboardNavBtn = document.getElementById('open-web-dashboard-nav-btn');
    if (openWebDashboardNavBtn) {
      openWebDashboardNavBtn.addEventListener('click', async (e) => {
        e.preventDefault();
        try {
          await invoke('open_external_url', { url: "https://speekto.me/dashboard" });
        } catch (err) {
          console.error("Failed to open web dashboard:", err);
          window.open("https://speekto.me/dashboard", "_blank");
        }
      });
    }

    const manageAccountBtn = document.getElementById('manage-account-btn');
    if (manageAccountBtn) {
      manageAccountBtn.addEventListener('click', async (e) => {
        e.preventDefault();
        try {
          await invoke('open_external_url', { url: "https://speekto.me/dashboard" });
        } catch (err) {
          console.error("Failed to open web dashboard:", err);
          window.open("https://speekto.me/dashboard", "_blank");
        }
      });
    }

    // 3.8. Theme preview listeners
    if (themeSelect) {
      themeSelect.addEventListener('change', (e) => {
        console.log("[THEME] themeSelect changed to:", e.target.value);
        applyAppTheme(e.target.value);
      });
    }

    if (pillPosModeSelect) {
      pillPosModeSelect.addEventListener('change', (e) => {
        if (e.target.value === 'custom') {
          if (customPosGroup) customPosGroup.style.display = 'flex';
          if (predefinedPosGroup) predefinedPosGroup.style.display = 'none';
        } else {
          if (customPosGroup) customPosGroup.style.display = 'none';
          if (predefinedPosGroup) predefinedPosGroup.style.display = 'flex';
        }
      });
    }
    if (onboardThemeSelect) {
      onboardThemeSelect.addEventListener('change', (e) => {
        console.log("[THEME] onboardThemeSelect changed to:", e.target.value);
        applyAppTheme(e.target.value);
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
    if (currentConfig && !currentConfig.session_token) {
      // User is logged out / first run: Show Onboarding Flow
      onboardingWrapper.classList.remove('hidden');
      goToStep(1);
    } else {
      // User is logged in: Go straight to settings dashboard
      onboardingWrapper.classList.add('hidden');
    }

    // 5. Button listeners (Auth & Onboarding)
    if (loginBtn) loginBtn.addEventListener('click', () => triggerLogin(false));
    if (cancelLoginBtn) cancelLoginBtn.addEventListener('click', () => triggerCancelLogin(false));
    if (logoutBtn) logoutBtn.addEventListener('click', triggerLogout);

    // 6. Button listeners (Onboarding)
    if (onboardLoginBtn) onboardLoginBtn.addEventListener('click', () => triggerLogin(true));
    if (onboardCancelLoginBtn) onboardCancelLoginBtn.addEventListener('click', () => triggerCancelLogin(true));
    if (onboardReopenBrowserBtn) onboardReopenBrowserBtn.addEventListener('click', () => triggerLogin(true));
    if (onboardNext1Btn) onboardNext1Btn.addEventListener('click', () => goToStep(2));
    if (onboardUpgradeProBtn) onboardUpgradeProBtn.addEventListener('click', handleUpgradeProFromOnboarding);
    if (onboardSkipProBtn) onboardSkipProBtn.addEventListener('click', () => goToStep(3));
    if (onboardNext2Btn) onboardNext2Btn.addEventListener('click', () => goToStep(4));
    if (onboardNext3Btn) onboardNext3Btn.addEventListener('click', () => goToStep(5));
    if (onboardFinishBtn) onboardFinishBtn.addEventListener('click', finishOnboarding);

    // Step dots navigation
    stepDots.forEach(dot => {
      dot.addEventListener('click', () => {
        const stepNum = parseInt(dot.getAttribute('data-step'), 10);
        if (stepNum && stepNum >= 1 && stepNum <= 5) {
          goToStep(stepNum);
        }
      });
    });

    // Step arrow navigation buttons
    if (onboardPrevStepBtn) {
      onboardPrevStepBtn.addEventListener('click', () => {
        if (currentStep > 1) {
          goToStep(currentStep - 1);
        }
      });
    }
    if (onboardNextStepBtn) {
      onboardNextStepBtn.addEventListener('click', () => {
        if (currentStep < 5) {
          goToStep(currentStep + 1);
        }
      });
    }

    // Auto-sync onboarding preferences to Rust immediately on change
    const syncOnboardingConfig = async () => {
      try {
        const updated = gatherConfigFromUI(true);
        await invoke('save_config', { config: updated });
        currentConfig = updated;
      } catch (e) {
        console.warn("Auto-syncing onboarding config:", e);
      }
    };

    if (onboardHotkeySelect) onboardHotkeySelect.addEventListener('change', syncOnboardingConfig);
    if (onboardHotkeyModeSelect) onboardHotkeyModeSelect.addEventListener('change', syncOnboardingConfig);
    if (onboardThemeSelect) onboardThemeSelect.addEventListener('change', (e) => {
      syncOnboardingConfig();
      document.body.className = `theme-${e.target.value}`;
    });
    if (onboardAutostartCheck) onboardAutostartCheck.addEventListener('change', syncOnboardingConfig);
    if (onboardTypingModeCheck) onboardTypingModeCheck.addEventListener('change', syncOnboardingConfig);
    if (onboardSoundFeedbackCheck) onboardSoundFeedbackCheck.addEventListener('change', syncOnboardingConfig);

    // Demo Video Player setup
    setupDemoVideoPlayer();

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

    setupPasswordToggle('app-password-input', 'app-password-toggle');
    setupPasswordToggle('onboard-password-input', 'onboard-password-toggle');
    
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

    // Initialize Custom Themed Dropdowns
    initCustomSelects();
    console.log("[INIT] SpeekTo.Me Frontend initialization complete!");
  } catch (err) {
    console.error("Failed to initialize SpeekTo.Me UI:", err);
  }
}

function initCustomSelects() {
  const selects = document.querySelectorAll('select.fluent-select');
  selects.forEach(select => {
    if (select.parentElement && select.parentElement.classList.contains('custom-select-wrapper')) {
      return;
    }

    const wrapper = document.createElement('div');
    wrapper.className = 'custom-select-wrapper';
    
    select.parentNode.insertBefore(wrapper, select);
    wrapper.appendChild(select);

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'custom-select-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');

    const labelSpan = document.createElement('span');
    labelSpan.className = 'custom-select-label';
    const selectedOption = select.options[select.selectedIndex];
    labelSpan.textContent = selectedOption ? selectedOption.textContent : (select.options[0]?.textContent || '');

    const arrowSpan = document.createElement('span');
    arrowSpan.className = 'custom-select-arrow';
    arrowSpan.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>`;

    trigger.appendChild(labelSpan);
    trigger.appendChild(arrowSpan);
    wrapper.appendChild(trigger);

    const menu = document.createElement('div');
    menu.className = 'custom-select-menu';
    menu.setAttribute('role', 'listbox');

    Array.from(select.options).forEach(opt => {
      const optEl = document.createElement('div');
      optEl.className = 'custom-select-option' + (opt.value === select.value ? ' selected' : '');
      optEl.setAttribute('role', 'option');
      optEl.setAttribute('data-value', opt.value);
      optEl.textContent = opt.textContent;

      optEl.addEventListener('click', (e) => {
        e.stopPropagation();
        select.value = opt.value;
        labelSpan.textContent = opt.textContent;

        menu.querySelectorAll('.custom-select-option').forEach(o => o.classList.remove('selected'));
        optEl.classList.add('selected');

        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');

        if (select.id === 'theme-select' || select.id === 'onboard-theme-select') {
          applyAppTheme(opt.value);
        }

        select.dispatchEvent(new Event('change', { bubbles: true }));
        if (typeof markDirty === 'function') {
          setTimeout(markDirty, 10);
        }
      });

      menu.appendChild(optEl);
    });

    wrapper.appendChild(menu);

    trigger.addEventListener('click', (e) => {
      e.stopPropagation();
      const isOpen = wrapper.classList.contains('open');
      document.querySelectorAll('.custom-select-wrapper.open').forEach(w => {
        if (w !== wrapper) {
          w.classList.remove('open');
          const t = w.querySelector('.custom-select-trigger');
          if (t) t.setAttribute('aria-expanded', 'false');
        }
      });

      if (isOpen) {
        wrapper.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
      } else {
        wrapper.classList.add('open');
        trigger.setAttribute('aria-expanded', 'true');
      }
    });

    select.addEventListener('change', () => {
      const curOpt = select.options[select.selectedIndex];
      if (curOpt) {
        labelSpan.textContent = curOpt.textContent;
        menu.querySelectorAll('.custom-select-option').forEach(o => {
          o.classList.toggle('selected', o.getAttribute('data-value') === select.value);
        });
      }
    });
  });

  if (!window._customSelectsGlobalBound) {
    window._customSelectsGlobalBound = true;
    document.addEventListener('click', () => {
      document.querySelectorAll('.custom-select-wrapper.open').forEach(w => {
        w.classList.remove('open');
        const t = w.querySelector('.custom-select-trigger');
        if (t) t.setAttribute('aria-expanded', 'false');
      });
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        document.querySelectorAll('.custom-select-wrapper.open').forEach(w => {
          w.classList.remove('open');
          const t = w.querySelector('.custom-select-trigger');
          if (t) t.setAttribute('aria-expanded', 'false');
        });
      }
    });
  }
}

function syncCustomSelects() {
  document.querySelectorAll('.custom-select-wrapper').forEach(wrapper => {
    const select = wrapper.querySelector('select');
    const labelSpan = wrapper.querySelector('.custom-select-label');
    const menu = wrapper.querySelector('.custom-select-menu');
    if (select && labelSpan && menu) {
      const curOpt = select.options[select.selectedIndex];
      if (curOpt) {
        labelSpan.textContent = curOpt.textContent;
        menu.querySelectorAll('.custom-select-option').forEach(o => {
          o.classList.toggle('selected', o.getAttribute('data-value') === select.value);
        });
      }
    }
  });
}

function checkIfConfigDirty(a, b) {
  if (!a || !b) return false;
  if (a.hotkey !== b.hotkey) return true;
  if (a.hotkey_mode !== b.hotkey_mode) return true;
  if (Boolean(a.autostart) !== Boolean(b.autostart)) return true;
  if (Boolean(a.typing_mode) !== Boolean(b.typing_mode)) return true;
  if (Boolean(a.sound_feedback) !== Boolean(b.sound_feedback)) return true;
  if (a.theme !== b.theme) return true;
  if (a.model !== b.model) return true;
  if (a.transcription_mode !== b.transcription_mode) return true;
  if (a.pill_position_mode !== b.pill_position_mode) return true;
  if (a.pill_predefined_position !== b.pill_predefined_position) return true;
  if (Number(a.pill_custom_x) !== Number(b.pill_custom_x)) return true;
  if (Number(a.pill_custom_y) !== Number(b.pill_custom_y)) return true;
  return false;
}

function markDirty() {
  const saveBtn = document.getElementById('save-btn');
  if (!saveBtn) return;
  if (!currentConfig) {
    saveBtn.style.display = 'none';
    return;
  }
  const currentUI = gatherConfigFromUI(false);
  const isDirty = checkIfConfigDirty(currentUI, currentConfig);
  if (isDirty) {
    saveBtn.style.display = 'flex';
  } else {
    saveBtn.style.display = 'none';
  }
}

function applyAppTheme(theme) {
  const t = theme || 'forest';
  document.documentElement.setAttribute('data-theme', t);
  document.body.setAttribute('data-theme', t);
  if (themeSelect && themeSelect.value !== t) {
    themeSelect.value = t;
  }
  if (onboardThemeSelect && onboardThemeSelect.value !== t) {
    onboardThemeSelect.value = t;
  }
  syncCustomSelects();
  try {
    if (window.__TAURI__ && window.__TAURI__.core) {
      invoke('update_tray_theme', { theme: t }).catch(() => {});
    }
  } catch (_) {}
}

function applyConfigToUI(config) {
  if (!config) return;

  const saveBtn = document.getElementById('save-btn');
  if (saveBtn) {
    saveBtn.style.display = 'none';
  }

  // General settings inputs
  if (hotkeySelect) hotkeySelect.value = config.hotkey || 'Ctrl + Win';
  if (hotkeyModeSelect) hotkeyModeSelect.value = config.hotkey_mode || 'hold';
  if (autostartCheck) autostartCheck.checked = Boolean(config.autostart);
  if (typingModeCheck) typingModeCheck.checked = Boolean(config.typing_mode);
  if (soundFeedbackCheck) soundFeedbackCheck.checked = Boolean(config.sound_feedback);
  if (modelSelect) modelSelect.value = config.model || 'whisper-large-v3-turbo';
  if (streamingModeCheck) {
    streamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
  if (themeSelect) themeSelect.value = config.theme || 'forest';

  // Apply new pill positioning settings
  if (pillPosModeSelect) {
    pillPosModeSelect.value = config.pill_position_mode || 'predefined';
    if (config.pill_position_mode === 'custom') {
      if (customPosGroup) customPosGroup.style.display = 'flex';
      if (predefinedPosGroup) predefinedPosGroup.style.display = 'none';
    } else {
      if (customPosGroup) customPosGroup.style.display = 'none';
      if (predefinedPosGroup) predefinedPosGroup.style.display = 'flex';
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
  if (onboardHotkeySelect) onboardHotkeySelect.value = config.hotkey || 'Ctrl + Win';
  if (onboardHotkeyModeSelect) onboardHotkeyModeSelect.value = config.hotkey_mode || 'hold';
  if (onboardAutostartCheck) onboardAutostartCheck.checked = Boolean(config.autostart);
  if (onboardStreamingModeCheck) {
    onboardStreamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
  if (onboardTypingModeCheck) onboardTypingModeCheck.checked = Boolean(config.typing_mode);
  if (onboardSoundFeedbackCheck) onboardSoundFeedbackCheck.checked = Boolean(config.sound_feedback);
  if (onboardModelSelect) onboardModelSelect.value = config.model || 'whisper-large-v3-turbo';
  if (onboardThemeSelect) onboardThemeSelect.value = config.theme || 'forest';

  // Apply theme class to document
  applyAppTheme(config.theme || 'forest');

  if (config.languages && Array.isArray(config.languages) && config.languages.length > 0) {
    selectedLanguages = config.languages;
    renderClientLanguageUI();
  }

  // Sync custom dropdown displays with applied config values
  syncCustomSelects();
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
      hotkey: onboardHotkeySelect ? onboardHotkeySelect.value : 'Ctrl + Win',
      hotkey_mode: onboardHotkeyModeSelect ? onboardHotkeyModeSelect.value : 'hold',
      autostart: onboardAutostartCheck ? onboardAutostartCheck.checked : false,
      typing_mode: onboardTypingModeCheck ? onboardTypingModeCheck.checked : false,
      sound_feedback: onboardSoundFeedbackCheck ? onboardSoundFeedbackCheck.checked : true,
      model: onboardModelSelect ? onboardModelSelect.value : 'whisper-large-v3-turbo',
      enhance: true,
      transcription_mode: (onboardStreamingModeCheck && onboardStreamingModeCheck.checked) ? "stream" : "batch",
      custom_prompt: "",
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
      hotkey: hotkeySelect ? hotkeySelect.value : 'Ctrl + Win',
      hotkey_mode: hotkeyModeSelect ? hotkeyModeSelect.value : 'hold',
      autostart: autostartCheck ? autostartCheck.checked : false,
      typing_mode: typingModeCheck ? typingModeCheck.checked : false,
      sound_feedback: soundFeedbackCheck ? soundFeedbackCheck.checked : true,
      model: modelSelect ? modelSelect.value : 'whisper-large-v3-turbo',
      enhance: true,
      transcription_mode: (streamingModeCheck && streamingModeCheck.checked) ? "stream" : "batch",
      custom_prompt: "",
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

let isSavingInProgress = false;
let isRefreshingInProgress = false;

async function saveAndApply(e) {
  if (e && e.preventDefault) e.preventDefault();
  if (isSavingInProgress) return;
  isSavingInProgress = true;

  const activeSaveBtn = document.getElementById('save-btn') || saveBtn;
  const defaultContent = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg><span>Save & Apply</span>';

  if (activeSaveBtn) {
    activeSaveBtn.disabled = true;
    activeSaveBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" style="animation: spin 1s linear infinite;"><circle cx="12" cy="12" r="10"/><path d="M12 2a10 10 0 0 1 10 10"/></svg><span>Saving...</span>';
  }

  try {
    const updated = gatherConfigFromUI(false);
    await invoke('save_config', { config: updated });
    currentConfig = updated;
    
    if (activeSaveBtn) {
      activeSaveBtn.disabled = false;
      activeSaveBtn.innerHTML = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg><span>Saved</span>';
    }
    showToast("Settings saved and applied successfully.", "Settings Saved", "success");
    
    // Auto-hide the save button after a brief confirmation delay
    setTimeout(() => {
      if (activeSaveBtn) {
        activeSaveBtn.style.display = 'none';
        activeSaveBtn.innerHTML = defaultContent;
      }
    }, 1200);
  } catch (err) {
    console.error("Failed to save config:", err);
    if (activeSaveBtn) {
      activeSaveBtn.disabled = false;
      activeSaveBtn.innerHTML = defaultContent;
    }
    showToast(formatAuthError(err), "Save Failed", "error");
  } finally {
    isSavingInProgress = false;
  }
}

async function refreshUserData(e) {
  if (e && e.preventDefault) e.preventDefault();
  if (isRefreshingInProgress) return;
  isRefreshingInProgress = true;

  const refreshBtn = document.getElementById('refresh-data-btn');
  const defaultContent = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg><span>Refresh</span>';
  if (refreshBtn) {
    refreshBtn.disabled = true;
    refreshBtn.innerHTML = '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" style="animation: spin 1s linear infinite;"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg><span>Refreshing...</span>';
  }
  
  try {
    const freshInfo = await invoke('get_user_info');
    currentUserInfo = freshInfo;
    if (freshInfo) {
      updateAuthUI(freshInfo);
      updateOnboardAuthUI(freshInfo);
    }
    currentConfig = await invoke('get_config');
    applyConfigToUI(currentConfig);
    showToast("Account status and settings synchronized.", "Refreshed Successfully", "success");
  } catch (err) {
    console.error("Failed to refresh user data:", err);
    showToast(formatAuthError(err), "Refresh Error", "error");
  } finally {
    if (refreshBtn) {
      refreshBtn.disabled = false;
      refreshBtn.innerHTML = defaultContent;
    }
    isRefreshingInProgress = false;
  }
}

async function hideWindow(e) {
  if (e && e.preventDefault) e.preventDefault();
  try {
    await invoke('hide_window');
  } catch (err) {
    console.error("Failed to hide window:", err);
  }
}

window.saveAndApply = saveAndApply;
window.hideWindow = hideWindow;
window.refreshUserData = refreshUserData;
window.markDirty = markDirty;

// ====================================================
// Onboarding Wizard Progression
// ==========================================

async function goToStep(step) {
  currentStep = step;
  onboardSteps.forEach(el => el.classList.remove('active'));
  stepDots.forEach(el => el.classList.remove('active'));

  const stepEl = document.getElementById(`onboard-step-${step}`);
  if (stepEl) stepEl.classList.add('active');
  const dotEl = document.querySelector(`.step-dot[data-step="${step}"]`);
  if (dotEl) dotEl.classList.add('active');

  // Update navigation arrow states
  if (onboardPrevStepBtn) onboardPrevStepBtn.disabled = (step <= 1);
  if (onboardNextStepBtn) onboardNextStepBtn.disabled = (step >= 5);

  // Pause demo video if user navigates away from Step 5
  if (step !== 5 && onboardDemoVideo) {
    onboardDemoVideo.pause();
  }

  if (step === 2) {
    await renderOnboardingPromoUI();
  } else if (step === 5) {
    await initDemoVideoStep();
  }
}

async function renderOnboardingPromoUI() {
  if (isUserPro(currentUserInfo)) {
    if (onboardPromoBadge) onboardPromoBadge.textContent = "✨ PRO PLAN ACTIVE";
    if (onboardPromoTitle) onboardPromoTitle.textContent = "You're on SpeekTo.Me Pro";
    if (onboardPromoSubtitle) onboardPromoSubtitle.textContent = "All Pro capabilities and unlimited AI voice dictation are active on your account.";
    if (onboardPromoCtaText) onboardPromoCtaText.textContent = "Continue Setup ➔";
    if (onboardSkipProBtn) onboardSkipProBtn.style.display = "none";
    return;
  }

  if (onboardSkipProBtn) onboardSkipProBtn.style.display = "block";

  try {
    const apiUrl = getApiUrl();
    const res = await fetch(`${apiUrl}/config/promo`);
    if (res.ok) {
      const promo = await res.json();
      if (promo.title && onboardPromoTitle) onboardPromoTitle.textContent = promo.title;
      if (promo.subtitle && onboardPromoSubtitle) onboardPromoSubtitle.textContent = promo.subtitle;
      if (promo.badge_text && onboardPromoBadge) onboardPromoBadge.textContent = promo.badge_text;
      if (promo.price && onboardPromoPrice) onboardPromoPrice.textContent = promo.price;
      if (promo.period && onboardPromoPeriod) onboardPromoPeriod.textContent = promo.period;
      if (promo.primary_cta && onboardPromoCtaText) onboardPromoCtaText.textContent = promo.primary_cta;
      if (promo.secondary_cta && onboardPromoSkipText) onboardPromoSkipText.textContent = promo.secondary_cta;
      if (promo.upgrade_url) promoUpgradeUrl = promo.upgrade_url;
      if (promo.video_url) cachedDemoVideoUrl = promo.video_url;
      if (promo.demo_video_url) cachedDemoVideoUrl = promo.demo_video_url;
      if (promo.features && Array.isArray(promo.features) && promo.features.length > 0) {
        const featGrid = document.getElementById('onboard-promo-features');
        if (featGrid) {
          featGrid.innerHTML = promo.features.map(f => `
            <div class="promo-feat-item">
              <span class="promo-check">✓</span>
              <span>${escapeHtml(f)}</span>
            </div>
          `).join('');
        }
      }
    }
  } catch (err) {
    console.warn("[PROMO] Could not fetch remote promo config, using default:", err);
  }
}

async function handleUpgradeProFromOnboarding() {
  if (isUserPro(currentUserInfo)) {
    goToStep(3);
    return;
  }
  const targetUrl = promoUpgradeUrl || `${getApiUrl()}/dashboard`;
  await invoke('open_external_url', { url: targetUrl });
  showToast("Opening billing dashboard in default browser...", "SpeekTo.Me Pro", "info");
  goToStep(3);
}

// ==========================================
// Demo Video Player (Step 5)
// ==========================================

function formatVideoTime(seconds) {
  if (isNaN(seconds) || seconds < 0) return "0:00";
  const mins = Math.floor(seconds / 60);
  const secs = Math.floor(seconds % 60);
  return `${mins}:${secs < 10 ? '0' : ''}${secs}`;
}

async function initDemoVideoStep() {
  // Sync the latest UI configuration to Rust backend immediately
  try {
    const updated = gatherConfigFromUI(true);
    await invoke('save_config', { config: updated });
    currentConfig = updated;
  } catch (err) {
    console.error("Failed to sync config before demo video step:", err);
  }

  // Fetch demo video URL from backend /config/video or cached
  try {
    const apiUrl = getApiUrl();
    const res = await fetch(`${apiUrl}/config/video`);
    if (res.ok) {
      const data = await res.json();
      if (data.video_url && data.video_url.trim() !== '') {
        cachedDemoVideoUrl = data.video_url.trim();
      }
    }
  } catch (e) {
    console.warn("Could not fetch demo video URL from server, using cached:", e);
  }

  if (onboardDemoVideo && onboardDemoVideo.src !== cachedDemoVideoUrl) {
    onboardDemoVideo.src = cachedDemoVideoUrl;
    onboardDemoVideo.load();
  }
}

function setupDemoVideoPlayer() {
  if (!onboardDemoVideo) return;

  const togglePlay = () => {
    if (onboardDemoVideo.paused || onboardDemoVideo.ended) {
      onboardDemoVideo.play().catch(e => console.warn("Auto-play error:", e));
    } else {
      onboardDemoVideo.pause();
    }
  };

  if (videoCenterPlayBtn) videoCenterPlayBtn.addEventListener('click', togglePlay);
  if (videoPlayPauseBtn) videoPlayPauseBtn.addEventListener('click', togglePlay);
  onboardDemoVideo.addEventListener('click', togglePlay);

  onboardDemoVideo.addEventListener('play', () => {
    if (videoPlayIcon) videoPlayIcon.classList.add('hidden');
    if (videoPauseIcon) videoPauseIcon.classList.remove('hidden');
    if (videoCenterPlayBtn) videoCenterPlayBtn.classList.add('hidden');
  });

  onboardDemoVideo.addEventListener('pause', () => {
    if (videoPlayIcon) videoPlayIcon.classList.remove('hidden');
    if (videoPauseIcon) videoPauseIcon.classList.add('hidden');
    if (videoCenterPlayBtn) videoCenterPlayBtn.classList.remove('hidden');
  });

  onboardDemoVideo.addEventListener('timeupdate', () => {
    const curr = onboardDemoVideo.currentTime;
    const dur = onboardDemoVideo.duration || 0;
    if (videoCurrentTime) videoCurrentTime.textContent = formatVideoTime(curr);
    if (videoDuration && dur > 0) videoDuration.textContent = formatVideoTime(dur);
    if (videoScrubber && dur > 0) {
      videoScrubber.value = (curr / dur) * 100;
    }
  });

  onboardDemoVideo.addEventListener('loadedmetadata', () => {
    const dur = onboardDemoVideo.duration || 0;
    if (videoDuration && dur > 0) videoDuration.textContent = formatVideoTime(dur);
  });

  if (videoScrubber) {
    videoScrubber.addEventListener('input', () => {
      const dur = onboardDemoVideo.duration || 0;
      if (dur > 0) {
        onboardDemoVideo.currentTime = (videoScrubber.value / 100) * dur;
      }
    });
  }

  // Volume & Mute Controls
  if (videoMuteBtn) {
    videoMuteBtn.addEventListener('click', () => {
      onboardDemoVideo.muted = !onboardDemoVideo.muted;
      updateVolumeUI();
    });
  }

  if (videoVolumeSlider) {
    videoVolumeSlider.addEventListener('input', () => {
      onboardDemoVideo.volume = parseFloat(videoVolumeSlider.value);
      onboardDemoVideo.muted = onboardDemoVideo.volume === 0;
      updateVolumeUI();
    });
  }

  function updateVolumeUI() {
    const isMuted = onboardDemoVideo.muted || onboardDemoVideo.volume === 0;
    if (videoVolHighIcon) videoVolHighIcon.classList.toggle('hidden', isMuted);
    if (videoVolMutedIcon) videoVolMutedIcon.classList.toggle('hidden', !isMuted);
    if (videoVolumeSlider) {
      videoVolumeSlider.value = isMuted ? 0 : onboardDemoVideo.volume;
    }
  }

  // Fullscreen Control
  if (videoFullscreenBtn && videoPlayerContainer) {
    videoFullscreenBtn.addEventListener('click', () => {
      if (!document.fullscreenElement) {
        videoPlayerContainer.requestFullscreen().catch(err => {
          console.warn("Fullscreen request failed:", err);
        });
      } else {
        document.exitFullscreen().catch(err => console.warn(err));
      }
    });
  }
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
    if (userNameEl) userNameEl.textContent = userInfo.name || '';
    if (userEmailEl) userEmailEl.textContent = userInfo.email || '';

    const userProviderEl = document.getElementById('user-provider');
    if (userProviderEl) {
      if (userInfo.auth_provider === 'google' || (userInfo.email && userInfo.email.endsWith('@gmail.com'))) {
        userProviderEl.textContent = 'Signed in with Google';
      } else {
        userProviderEl.textContent = 'Signed in with Email';
      }
    }
    
    // Bind subscription fields
    const subPlan = document.getElementById('sub-plan');
    const subStatus = document.getElementById('sub-status');
    const subActivated = document.getElementById('sub-activated');
    const subUsage = document.getElementById('sub-usage');
    
    if (subPlan) subPlan.textContent = (userInfo.plan_tier || 'Free').toUpperCase();
    if (subStatus) {
      if (userInfo.plan_active) {
        subStatus.innerHTML = '<span class="status-pill-dot"></span>ACTIVE';
        subStatus.className = "status-pill-tag active";
      } else {
        subStatus.innerHTML = '<span class="status-pill-dot inactive"></span>INACTIVE';
        subStatus.className = "status-pill-tag inactive";
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
        subUsagePercent.innerHTML = `${usage.usedStr} / ${usage.totalStr} <span class="info-bubble" title="${usage.fupTooltip}" aria-label="${usage.fupTooltip}">ⓘ</span>`;
      } else {
        subUsagePercent.textContent = usage.display;
      }
    }
    if (subUsageBar) subUsageBar.style.width = `${percent.toFixed(1)}%`;

    if (accountDesc) {
      accountDesc.textContent = "Manage your account and subscription.";
      accountDesc.classList.remove('hidden');
    }

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
  } else {
    // Show descriptive text when logged out
    if (accountDesc) accountDesc.classList.remove('hidden');

    const sidebarAnnouncement = document.getElementById('sidebar-announcement');
    if (sidebarAnnouncement) sidebarAnnouncement.classList.add('hidden');

    authLoggedIn.classList.add('hidden');
    authLoggingIn.classList.add('hidden');
    authLoggedOut.classList.remove('hidden');
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

function setupPasswordToggle(inputId, toggleBtnId) {
  const input = document.getElementById(inputId);
  const toggleBtn = document.getElementById(toggleBtnId);
  if (!input || !toggleBtn) return;

  toggleBtn.addEventListener('click', (e) => {
    e.preventDefault();
    const isPassword = input.type === 'password';
    input.type = isPassword ? 'text' : 'password';
    toggleBtn.innerHTML = isPassword
      ? `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path><line x1="1" y1="1" x2="23" y2="23"></line></svg>`
      : `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path><circle cx="12" cy="12" r="3"></circle></svg>`;
  });
}

function setupCredentialForm(formId, nameInputId, emailInputId, passwordInputId, submitBtnId, toggleLinkId, errorId, isOnboarding) {
  const nameInput = document.getElementById(nameInputId);
  const emailInput = document.getElementById(emailInputId);
  const passwordInput = document.getElementById(passwordInputId);
  const submitBtn = document.getElementById(submitBtnId);
  const toggleLink = document.getElementById(toggleLinkId);
  const errorEl = document.getElementById(errorId);
  const nameGroup = nameInput ? (nameInput.closest('.auth-input-group') || nameInput) : null;

  let isSignUp = false;

  toggleLink.addEventListener('click', (e) => {
    e.preventDefault();
    isSignUp = !isSignUp;
    if (isSignUp) {
      if (nameGroup) nameGroup.style.display = 'flex';
      submitBtn.textContent = 'Create Account';
      toggleLink.textContent = 'Already have an account? Sign in';
    } else {
      if (nameGroup) nameGroup.style.display = 'none';
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
    const name = nameInput ? nameInput.value.trim() : '';

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
        // Show success message
        errorEl.textContent = msg;
        errorEl.style.color = 'var(--validation-success-text)';
        errorEl.style.display = 'block';
        
        // Auto-switch to login pane after 4 seconds
        setTimeout(() => {
          isSignUp = false;
          if (nameGroup) nameGroup.style.display = 'none';
          submitBtn.textContent = 'Sign In';
          toggleLink.textContent = 'Need an account? Sign up';
          errorEl.style.display = 'none';
          errorEl.style.color = ''; // reset color to default
        }, 4000);

        // Clear input fields (leave email for quick login)
        passwordInput.value = '';
        if (nameInput) nameInput.value = '';
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
    vocabList.innerHTML = `<li class="vocab-empty" style="color: var(--btn-danger-text);">Error loading vocabulary: ${escapeHtml(String(err))}</li>`;
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
