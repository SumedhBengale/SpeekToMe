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

const IS_LINUX_CLIENT = /linux/i.test(`${navigator.platform || ''} ${navigator.userAgent || ''}`);
const DEFAULT_HOTKEY = 'Ctrl + Win';

function configureLinuxSettings() {
  if (!IS_LINUX_CLIENT) {
    return;
  }

  document.documentElement.dataset.platform = 'linux';
  [document.getElementById('hotkey-select'), document.getElementById('onboard-hotkey-select')].forEach(select => {
    if (!select) return;
    // Keep the same predefined shortcuts and stored values across platforms.
    // Linux desktops call the Windows key Meta (or Super), so only change its label.
    [...select.options].forEach(option => {
      option.hidden = false;
      option.textContent = option.textContent.replace(/Windows/g, 'Meta');
    });
  });

  document.querySelectorAll('.autostart-hint').forEach(hint => {
    hint.textContent = 'Automatically launch SpeekTo.Me when you sign in.';
  });

  const typingTitle = document.querySelector('#typing-mode-check + .checkmark + .checkbox-label-block .checkbox-title');
  const typingHint = document.querySelector('#typing-mode-check + .checkmark + .checkbox-label-block .setting-hint');
  if (typingTitle) typingTitle.textContent = 'Type text with simulated keystrokes';
  if (typingHint) typingHint.textContent = 'Off: paste the completed text. Characters unavailable on your keyboard layout also use paste to preserve the text.';

  const onboardTypingTitle = document.querySelector('#onboard-typing-mode-check + .checkmark + .checkbox-label-block .checkbox-title');
  const onboardTypingHint = document.querySelector('#onboard-typing-mode-check + .checkmark + .checkbox-label-block .setting-hint');
  if (onboardTypingTitle) onboardTypingTitle.textContent = 'Type text with simulated keystrokes';
  if (onboardTypingHint) onboardTypingHint.textContent = 'Off: paste the completed text. Characters unavailable on your keyboard layout also use paste to preserve the text.';
}

async function loadLinuxCapabilityStatus() {
  if (!IS_LINUX_CLIENT) return;
  const status = document.getElementById('linux-capability-status');
  if (!status) return;
  status.hidden = false;
  status.textContent = 'Checking desktop shortcut and text insertion permissions…';
  try {
    const capabilities = await invoke('get_linux_capabilities');
    const shortcut = capabilities?.shortcut ? 'Shortcut ready' : 'Shortcut permission needed';
    const insertion = capabilities?.insertion ? 'Text insertion ready' : 'Text insertion permission needed';
    status.textContent = [shortcut, insertion, capabilities?.detail].filter(Boolean).join(' · ');
  } catch (error) {
    console.warn('[LINUX] Capability status is unavailable:', error);
    status.textContent = 'Shortcut and text insertion permission status is unavailable.';
  }
}

configureLinuxSettings();

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

// ==========================================
// Custom Styled Dialog / Popup System
// ==========================================

function showConfirmDialog({
  title = "Confirm Action",
  message = "Are you sure you want to proceed?",
  confirmText = "Confirm",
  cancelText = "Cancel",
  isDanger = false,
  type = isDanger ? "danger" : "accent"
} = {}) {
  return new Promise((resolve) => {
    const overlay = document.getElementById('app-dialog-overlay');
    const titleEl = document.getElementById('app-dialog-title');
    const msgEl = document.getElementById('app-dialog-msg');
    const iconEl = document.getElementById('app-dialog-icon');
    const confirmBtn = document.getElementById('app-dialog-confirm-btn');
    const cancelBtn = document.getElementById('app-dialog-cancel-btn');
    const closeBtn = document.getElementById('app-dialog-close-btn');

    if (!overlay || !confirmBtn) {
      resolve(true);
      return;
    }

    titleEl.textContent = title;
    msgEl.textContent = message;
    confirmBtn.textContent = confirmText;
    cancelBtn.textContent = cancelText;
    cancelBtn.style.display = 'inline-flex';

    confirmBtn.className = isDanger ? 'fluent-button danger' : 'fluent-button primary';

    iconEl.className = `app-dialog-icon-wrapper ${isDanger ? 'danger' : type}`;
    if (isDanger || type === 'danger') {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>`;
    } else if (type === 'warning') {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>`;
    } else {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>`;
    }

    const cleanup = (result) => {
      overlay.classList.add('hidden');
      overlay.setAttribute('aria-hidden', 'true');
      document.removeEventListener('keydown', handleKey);
      confirmBtn.onclick = null;
      cancelBtn.onclick = null;
      if (closeBtn) closeBtn.onclick = null;
      const backdrop = overlay.querySelector('.app-dialog-backdrop');
      if (backdrop) backdrop.onclick = null;
      resolve(result);
    };

    const handleKey = (e) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        cleanup(false);
      } else if (e.key === 'Enter') {
        if (document.activeElement !== cancelBtn && document.activeElement !== closeBtn) {
          e.preventDefault();
          cleanup(true);
        }
      }
    };

    confirmBtn.onclick = () => cleanup(true);
    cancelBtn.onclick = () => cleanup(false);
    if (closeBtn) closeBtn.onclick = () => cleanup(false);

    const backdrop = overlay.querySelector('.app-dialog-backdrop');
    if (backdrop) backdrop.onclick = () => cleanup(false);

    document.addEventListener('keydown', handleKey);

    overlay.classList.remove('hidden');
    overlay.setAttribute('aria-hidden', 'false');

    if (isDanger) {
      cancelBtn.focus();
    } else {
      confirmBtn.focus();
    }
  });
}

function showAlertDialog({
  title = "Notice",
  message = "",
  confirmText = "OK",
  type = "info"
} = {}) {
  return new Promise((resolve) => {
    const overlay = document.getElementById('app-dialog-overlay');
    const titleEl = document.getElementById('app-dialog-title');
    const msgEl = document.getElementById('app-dialog-msg');
    const iconEl = document.getElementById('app-dialog-icon');
    const confirmBtn = document.getElementById('app-dialog-confirm-btn');
    const cancelBtn = document.getElementById('app-dialog-cancel-btn');
    const closeBtn = document.getElementById('app-dialog-close-btn');

    if (!overlay || !confirmBtn) {
      resolve();
      return;
    }

    titleEl.textContent = title;
    msgEl.textContent = message;
    confirmBtn.textContent = confirmText;
    cancelBtn.style.display = 'none';

    confirmBtn.className = type === 'error' ? 'fluent-button danger' : 'fluent-button primary';

    iconEl.className = `app-dialog-icon-wrapper ${type === 'error' ? 'danger' : type}`;
    if (type === 'error') {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="15" y1="9" x2="9" y2="15"></line><line x1="9" y1="9" x2="15" y2="15"></line></svg>`;
    } else if (type === 'warning') {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>`;
    } else {
      iconEl.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>`;
    }

    const cleanup = () => {
      overlay.classList.add('hidden');
      overlay.setAttribute('aria-hidden', 'true');
      document.removeEventListener('keydown', handleKey);
      confirmBtn.onclick = null;
      if (closeBtn) closeBtn.onclick = null;
      const backdrop = overlay.querySelector('.app-dialog-backdrop');
      if (backdrop) backdrop.onclick = null;
      resolve();
    };

    const handleKey = (e) => {
      if (e.key === 'Escape' || e.key === 'Enter') {
        e.preventDefault();
        cleanup();
      }
    };

    confirmBtn.onclick = cleanup;
    if (closeBtn) closeBtn.onclick = cleanup;
    const backdrop = overlay.querySelector('.app-dialog-backdrop');
    if (backdrop) backdrop.onclick = cleanup;

    document.addEventListener('keydown', handleKey);

    overlay.classList.remove('hidden');
    overlay.setAttribute('aria-hidden', 'false');
    confirmBtn.focus();
  });
}

// Intercept window.alert so that any unhandled alert call displays as a styled popup modal
window.alert = function(msg) {
  showAlertDialog({
    title: "Notice",
    message: String(msg)
  });
};

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
const commandModeCheck = document.getElementById('command-mode-check');
const commandModeProHint = document.getElementById('command-mode-pro-hint');
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
  { code: "en", name: "English", locale: "en-US", tier: "transcription-ready" },
  { code: "es", name: "Spanish", locale: "es-ES", tier: "transcription-ready" },
  { code: "de", name: "German", locale: "de-DE", tier: "transcription-ready" },
  { code: "fr", name: "French", locale: "fr-FR", tier: "transcription-ready" },
  { code: "it", name: "Italian", locale: "it-IT", tier: "transcription-ready" },
  { code: "pt", name: "Portuguese", locale: "pt-BR", tier: "transcription-ready" },
  { code: "nl", name: "Dutch", locale: "nl-NL", tier: "transcription-ready" },
  { code: "ru", name: "Russian", locale: "ru-RU", tier: "transcription-ready" },
  { code: "tr", name: "Turkish", locale: "tr-TR", tier: "transcription-ready" },
  { code: "ar", name: "Arabic", locale: "ar-AR", tier: "transcription-ready" },
  { code: "hi", name: "Hindi", locale: "hi-IN", tier: "transcription-ready" },
  { code: "ja", name: "Japanese", locale: "ja-JP", tier: "transcription-ready" },
  { code: "ko", name: "Korean", locale: "ko-KR", tier: "transcription-ready" },
  { code: "vi", name: "Vietnamese", locale: "vi-VN", tier: "transcription-ready" },
  { code: "uk", name: "Ukrainian", locale: "uk-UA", tier: "transcription-ready" },
  { code: "zh", name: "Chinese", locale: "zh-CN", tier: "broad-coverage" },
  { code: "pl", name: "Polish", locale: "pl-PL", tier: "broad-coverage" },
  { code: "sv", name: "Swedish", locale: "sv-SE", tier: "broad-coverage" },
  { code: "cs", name: "Czech", locale: "cs-CZ", tier: "broad-coverage" },
  { code: "no", name: "Norwegian", locale: "nb-NO", tier: "broad-coverage" },
  { code: "da", name: "Danish", locale: "da-DK", tier: "broad-coverage" },
  { code: "bg", name: "Bulgarian", locale: "bg-BG", tier: "broad-coverage" },
  { code: "fi", name: "Finnish", locale: "fi-FI", tier: "broad-coverage" },
  { code: "hr", name: "Croatian", locale: "hr-HR", tier: "broad-coverage" },
  { code: "sk", name: "Slovak", locale: "sk-SK", tier: "broad-coverage" },
  { code: "hu", name: "Hungarian", locale: "hu-HU", tier: "broad-coverage" },
  { code: "ro", name: "Romanian", locale: "ro-RO", tier: "broad-coverage" },
  { code: "et", name: "Estonian", locale: "et-EE", tier: "broad-coverage" },
  { code: "el", name: "Greek", locale: "el-GR", tier: "adaptation-ready" },
  { code: "lt", name: "Lithuanian", locale: "lt-LT", tier: "adaptation-ready" },
  { code: "lv", name: "Latvian", locale: "lv-LV", tier: "adaptation-ready" },
  { code: "mt", name: "Maltese", locale: "mt-MT", tier: "adaptation-ready" },
  { code: "sl", name: "Slovenian", locale: "sl-SI", tier: "adaptation-ready" },
  { code: "he", name: "Hebrew", locale: "he-IL", tier: "adaptation-ready" },
  { code: "th", name: "Thai", locale: "th-TH", tier: "adaptation-ready" }
];

let selectedLanguages = ["en"];
let speakingLanguage = "en";

function renderSpeakingLanguages() {
  if (speakingLanguage !== 'auto' && !selectedLanguages.includes(speakingLanguage)) {
    speakingLanguage = selectedLanguages[0] || 'en';
  }
  for (const id of ['client-speaking-language', 'onboard-speaking-language']) {
    const select = document.getElementById(id);
    if (!select) continue;
    select.replaceChildren();
    for (const code of ['auto', ...selectedLanguages]) {
      const option = document.createElement('option');
      option.value = code;
      option.textContent = code === 'auto' ? 'Automatic' : getLanguageName(code);
      select.appendChild(option);
    }
    select.value = speakingLanguage;
  }
  syncCustomSelects();
}

async function chooseSpeakingLanguage(event) {
  const choice = event.target.value;
  try {
    speakingLanguage = await invoke('set_active_language', { language: choice });
    if (currentConfig) currentConfig.active_language = speakingLanguage;
    showToast('Speaking language: ' + (speakingLanguage === 'auto' ? 'Automatic' : getLanguageName(speakingLanguage)), 'Applies to the next recording', 'info');
  } catch (err) {
    showToast(String(err), 'Languages', 'error');
  }
  renderSpeakingLanguages();
}


function getLanguageName(code) {
  const item = SUPPORTED_LANGUAGES.find(l => l.code === code);
  return item ? item.name : code;
}

function renderClientLanguageUI() {
  renderSpeakingLanguages();
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
    const saved = await invoke('get_config');
    selectedLanguages = saved.languages || ['en'];
    speakingLanguage = saved.active_language || selectedLanguages[0];
    renderClientLanguageUI();
  }
}

async function init() {
  // Initialize Custom Themed Dropdowns immediately before any async backend calls
  initCustomSelects();
  for (const id of ['client-speaking-language', 'onboard-speaking-language']) {
    document.getElementById(id)?.addEventListener('change', chooseSpeakingLanguage);
  }
  await listen('speaking-language-changed', ({ payload }) => {
    speakingLanguage = payload.language;
    if (Array.isArray(payload.languages) && payload.languages.length) selectedLanguages = payload.languages;
    if (currentConfig) {
      currentConfig.active_language = speakingLanguage;
      currentConfig.languages = [...selectedLanguages];
    }
    renderClientLanguageUI();
  });

  // 0. Synchronously bind all core UI listeners first so buttons are ALWAYS responsive
  const saveBtnEl = document.getElementById('save-btn');
  if (saveBtnEl) saveBtnEl.addEventListener('click', saveAndApply);

  const hideBtnEl = document.getElementById('hide-btn');
  if (hideBtnEl) hideBtnEl.addEventListener('click', hideWindow);

  const refreshBtnEl = document.getElementById('refresh-data-btn');
  if (refreshBtnEl) refreshBtnEl.addEventListener('click', refreshUserData);

  const winMinBtn = document.getElementById('win-min-btn');
  if (winMinBtn) winMinBtn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    invoke('minimize_window');
  });

  const winMaxBtn = document.getElementById('win-max-btn');
  async function updateMaximizeIcon() {
    if (!winMaxBtn) return;
    try {
      const isMax = await invoke('is_window_maximized');
      if (isMax) {
        winMaxBtn.title = 'Restore';
        winMaxBtn.setAttribute('aria-label', 'Restore');
        winMaxBtn.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 4h10a2 2 0 0 1 2 2v10"></path><rect x="4" y="8" width="12" height="12" rx="2"></rect></svg>`;
      } else {
        winMaxBtn.title = 'Maximize';
        winMaxBtn.setAttribute('aria-label', 'Maximize');
        winMaxBtn.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"></rect></svg>`;
      }
    } catch (e) {
      console.error('Failed to update maximize icon:', e);
    }
  }

  if (winMaxBtn) {
    winMaxBtn.addEventListener('click', async (e) => {
      e.preventDefault();
      e.stopPropagation();
      try {
        await invoke('toggle_maximize_window');
        setTimeout(updateMaximizeIcon, 60);
      } catch (err) {
        console.error('Toggle maximize failed:', err);
      }
    });
  }

  const titlebarDrag = document.querySelector('#app .window-titlebar-drag');
  if (titlebarDrag) {
    titlebarDrag.addEventListener('dblclick', async (e) => {
      e.preventDefault();
      e.stopPropagation();
      try {
        await invoke('toggle_maximize_window');
        setTimeout(updateMaximizeIcon, 60);
      } catch (err) {
        console.error('Toggle maximize failed:', err);
      }
    });
  }

  window.addEventListener('resize', () => {
    updateMaximizeIcon();
  });
  updateMaximizeIcon();

  const winCloseBtn = document.getElementById('win-close-btn');
  if (winCloseBtn) winCloseBtn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    invoke('close_window');
  });

  const onboardWinMinBtn = document.getElementById('onboard-win-min-btn');
  if (onboardWinMinBtn) onboardWinMinBtn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    invoke('minimize_window');
  });

  const onboardWinCloseBtn = document.getElementById('onboard-win-close-btn');
  if (onboardWinCloseBtn) onboardWinCloseBtn.addEventListener('click', (e) => {
    e.preventDefault();
    e.stopPropagation();
    invoke('close_window');
  });

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
        if (currentMeetingFile) {
          showRecordingView();
        } else {
          loadMeetings();
        }
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
    streamingModeCheck,
    commandModeCheck
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
    loadLinuxCapabilityStatus();
    if (IS_LINUX_CLIENT) setInterval(loadLinuxCapabilityStatus, 3000);

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
      markDirty();
    };

    if (streamingModeCheck) {
      streamingModeCheck.addEventListener('change', () => handleStreamingToggleChange(streamingModeCheck));
    }
    if (onboardStreamingModeCheck) {
      onboardStreamingModeCheck.addEventListener('change', () => handleStreamingToggleChange(onboardStreamingModeCheck));
    }

    const handleCommandModeToggleChange = (checkbox) => {
      const isPaid = isUserPro(currentUserInfo);
      if (!isPaid && checkbox.checked) {
        checkbox.checked = false;
        if (commandModeProHint) commandModeProHint.style.display = 'block';
        showToast("AI Command Mode is an exclusive Pro feature. Upgrade your subscription on your Web Dashboard to unlock it!", "Pro Subscription Required", "warning");
      } else if (isPaid) {
        if (commandModeProHint) commandModeProHint.style.display = 'none';
        if (commandModeCheck) commandModeCheck.checked = checkbox.checked;
      }
      markDirty();
    };

    if (commandModeCheck) {
      commandModeCheck.addEventListener('change', () => handleCommandModeToggleChange(commandModeCheck));
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

    // Check if an active meeting is recording in the background (Option 2 Session Rehydration)
    try {
      const activeMeeting = await invoke('get_active_meeting');
      if (activeMeeting && activeMeeting.is_recording) {
        console.log("[MEETING RESTORE] Active meeting found during init:", activeMeeting);
        rehydrateActiveMeeting(activeMeeting);
      }
    } catch (meetingErr) {
      console.warn("[MEETING RESTORE] Failed to check for active meeting during init:", meetingErr);
    }

    // Initialize Custom Themed Dropdowns
    initCustomSelects();
    console.log("[INIT] SpeekTo.Me Frontend initialization complete!");
  } catch (err) {
    console.error("Failed to initialize SpeekTo.Me UI:", err);
  }
}

function refreshCustomSelectMenu(select, wrapper, labelSpan, menu) {
  const trigger = wrapper.querySelector('.custom-select-trigger');
  const signature = JSON.stringify(Array.from(select.options, opt => [opt.value, opt.textContent, opt.hidden]));
  if (menu.dataset.optionsSignature !== signature) {
    menu.replaceChildren();
    Array.from(select.options).filter(opt => !opt.hidden).forEach(opt => {
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
    menu.dataset.optionsSignature = signature;
  }
  const current = select.options[select.selectedIndex];
  labelSpan.textContent = current ? current.textContent : '';
  menu.querySelectorAll('.custom-select-option').forEach(option => {
    option.classList.toggle('selected', option.getAttribute('data-value') === select.value);
  });
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

    refreshCustomSelectMenu(select, wrapper, labelSpan, menu);

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
      refreshCustomSelectMenu(select, wrapper, labelSpan, menu);
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
  const aCmd = a.command_mode !== undefined ? Boolean(a.command_mode) : true;
  const bCmd = b.command_mode !== undefined ? Boolean(b.command_mode) : true;
  if (aCmd !== bCmd) return true;
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
  if (hotkeySelect) hotkeySelect.value = normalizeConfiguredHotkey(config.hotkey);
  if (hotkeyModeSelect) hotkeyModeSelect.value = config.hotkey_mode || 'hold';
  if (autostartCheck) autostartCheck.checked = Boolean(config.autostart);
  if (typingModeCheck) typingModeCheck.checked = Boolean(config.typing_mode);
  if (soundFeedbackCheck) soundFeedbackCheck.checked = Boolean(config.sound_feedback);
  if (modelSelect) modelSelect.value = config.model || 'whisper-large-v3-turbo';
  if (streamingModeCheck) {
    streamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
  if (commandModeCheck) {
    commandModeCheck.checked = (config.command_mode !== false);
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
  if (onboardHotkeySelect) onboardHotkeySelect.value = normalizeConfiguredHotkey(config.hotkey);
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
    speakingLanguage = config.active_language || config.languages[0] || "en";
    selectedLanguages = config.languages;
    renderClientLanguageUI();
  }

  // Sync custom dropdown displays with applied config values
  syncCustomSelects();
}

function normalizeConfiguredHotkey(hotkey) {
  return hotkey || DEFAULT_HOTKEY;
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
      languages: [...selectedLanguages],
      active_language: speakingLanguage,
      api_url: api_url,
      hotkey: onboardHotkeySelect ? onboardHotkeySelect.value : DEFAULT_HOTKEY,
      hotkey_mode: onboardHotkeyModeSelect ? onboardHotkeyModeSelect.value : 'hold',
      autostart: onboardAutostartCheck ? onboardAutostartCheck.checked : false,
      typing_mode: onboardTypingModeCheck ? onboardTypingModeCheck.checked : false,
      sound_feedback: onboardSoundFeedbackCheck ? onboardSoundFeedbackCheck.checked : true,
      model: onboardModelSelect ? onboardModelSelect.value : 'whisper-large-v3-turbo',
      enhance: true,
      transcription_mode: (onboardStreamingModeCheck && onboardStreamingModeCheck.checked) ? "stream" : "batch",
      command_mode: currentConfig ? (currentConfig.command_mode !== false) : true,
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
      languages: [...selectedLanguages],
      active_language: speakingLanguage,
      api_url: api_url,
      hotkey: hotkeySelect ? hotkeySelect.value : DEFAULT_HOTKEY,
      hotkey_mode: hotkeyModeSelect ? hotkeyModeSelect.value : 'hold',
      autostart: autostartCheck ? autostartCheck.checked : false,
      typing_mode: typingModeCheck ? typingModeCheck.checked : false,
      sound_feedback: soundFeedbackCheck ? soundFeedbackCheck.checked : true,
      model: modelSelect ? modelSelect.value : 'whisper-large-v3-turbo',
      enhance: true,
      transcription_mode: (streamingModeCheck && streamingModeCheck.checked) ? "stream" : "batch",
      command_mode: (commandModeCheck ? commandModeCheck.checked : (currentConfig ? (currentConfig.command_mode !== false) : true)),
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
    if (commandModeCheck) {
      if (!isPaid) {
        commandModeCheck.checked = false;
        if (commandModeProHint) commandModeProHint.style.display = 'block';
      } else {
        if (commandModeProHint) commandModeProHint.style.display = 'none';
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

function showVocabLoading() {
  const loadingEl = document.getElementById('vocab-loading-state');
  const emptyEl = document.getElementById('vocab-empty-msg');
  const listEl = document.getElementById('vocab-list');
  const warningEl = document.getElementById('vocab-warning');

  if (loadingEl) {
    loadingEl.classList.remove('hidden');
    loadingEl.style.display = 'flex';
  }
  if (emptyEl) {
    emptyEl.classList.add('hidden');
    emptyEl.style.display = 'none';
  }
  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
  if (warningEl) {
    warningEl.classList.add('hidden');
  }
}

function hideVocabLoading() {
  const loadingEl = document.getElementById('vocab-loading-state');
  if (loadingEl) {
    loadingEl.classList.add('hidden');
    loadingEl.style.display = 'none';
  }
}

async function loadVocabulary() {
  const vocabList = document.getElementById('vocab-list');
  const vocabEmpty = document.getElementById('vocab-empty-msg');
  const vocabWarning = document.getElementById('vocab-warning');
  if (!vocabList) return;

  showVocabLoading();

  try {
    const terms = await invoke('get_vocabulary');
    hideVocabLoading();
    vocabList.innerHTML = '';

    if (!terms || terms.length === 0) {
      if (vocabEmpty) {
        vocabEmpty.classList.remove('hidden');
        vocabEmpty.style.display = 'flex';
      }
      vocabList.classList.add('hidden');
      vocabList.style.display = 'none';
      if (vocabWarning) vocabWarning.classList.add('hidden');
      return;
    }

    if (vocabEmpty) {
      vocabEmpty.classList.add('hidden');
      vocabEmpty.style.display = 'none';
    }
    vocabList.classList.remove('hidden');
    vocabList.style.display = 'flex';

    if (vocabWarning) {
      if (terms.length > 1000) {
        vocabWarning.classList.remove('hidden');
      } else {
        vocabWarning.classList.add('hidden');
      }
    }

    terms.forEach(term => {
      const li = document.createElement('li');
      li.className = 'vocab-item';

      const textSpan = document.createElement('div');
      textSpan.className = 'vocab-item-text';

      const termName = document.createElement('span');
      termName.className = 'vocab-term-text';
      termName.textContent = term.canonical_term;

      const normSpan = document.createElement('span');
      normSpan.className = 'vocab-term-normalized';
      normSpan.textContent = `(${term.normalized_term})`;

      textSpan.appendChild(termName);
      textSpan.appendChild(normSpan);

      const deleteBtn = document.createElement('button');
      deleteBtn.type = 'button';
      deleteBtn.className = 'vocab-delete-btn';
      deleteBtn.title = `Delete "${term.canonical_term}"`;
      deleteBtn.setAttribute('aria-label', `Delete ${term.canonical_term}`);
      deleteBtn.innerHTML = `
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="3 6 5 6 21 6"></polyline>
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          <line x1="10" y1="11" x2="10" y2="17"></line>
          <line x1="14" y1="11" x2="14" y2="17"></line>
        </svg>
      `;

      deleteBtn.addEventListener('click', async () => {
        const confirmed = await showConfirmDialog({
          title: "Delete Vocabulary Term?",
          message: `Are you sure you want to remove "${term.canonical_term}" from your custom vocabulary?`,
          confirmText: "Delete",
          cancelText: "Cancel",
          isDanger: true
        });
        if (!confirmed) return;

        deleteBtn.disabled = true;
        try {
          await invoke('delete_vocabulary_term', { term: term.canonical_term });
          showToast(`Removed "${term.canonical_term}"`, "Vocabulary", "info");
          await loadVocabulary();
        } catch (err) {
          console.error("Failed to delete term:", err);
          showToast(err, "Failed to delete term", "error");
          deleteBtn.disabled = false;
        }
      });

      li.appendChild(textSpan);
      li.appendChild(deleteBtn);
      vocabList.appendChild(li);
    });
  } catch (err) {
    hideVocabLoading();
    console.error("Failed to load vocabulary:", err);
    if (vocabEmpty) {
      const emptyText = vocabEmpty.querySelector('.vocab-empty-text');
      if (emptyText) {
        emptyText.textContent = `Error loading vocabulary: ${err}`;
      } else {
        vocabEmpty.textContent = `Error loading vocabulary: ${err}`;
      }
      vocabEmpty.classList.remove('hidden');
      vocabEmpty.style.display = 'flex';
    }
  }
}

// Initialization is registered once, after all controllers are defined.

// ==========================================
// Meetings & Notes Feature Controller
// ==========================================

let recordingInterval = null;
let timerInterval = null;
let meetingSeconds = 0;
let currentMeetingFile = null;
let activeMeetingId = null;
let liveMeetingTurnsMap = new Map();

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

  // Close meeting card dropdowns when clicking outside
  document.addEventListener('click', (e) => {
    if (!e.target.closest('.meeting-card-menu-wrapper') && !e.target.closest('.meeting-detail-menu-wrapper')) {
      document.querySelectorAll('.meeting-card-dropdown').forEach(d => d.classList.add('hidden'));
    }
  });

  // Setup real-time meeting events from Tauri Rust backend
  if (window.__TAURI__ && window.__TAURI__.event) {
    listen('meeting-turn-update', (event) => {
      handleLiveMeetingTurn(event.payload);
    });
    listen('meeting-session-ready', () => {
      const badge = document.getElementById('meetings-live-status-badge');
      if (badge) {
        badge.textContent = "Recording locally";
        badge.style.color = "#57bf7c";
      }
    });
    listen('meeting-completed', (event) => {
      console.log("[MEETING EVENT] meeting-completed received from backend:", event.payload);
      try {
        const m = typeof event.payload === 'string' ? JSON.parse(event.payload) : event.payload;
        if (m && m.id) {
          showDashboardView();
          loadMeetings();
          viewMeeting(m.id);
        }
      } catch (e) {
        console.error("[MEETING EVENT ERROR] Failed to parse meeting-completed payload:", e);
      }
    });
    listen('meeting-error', (event) => {
      const badge = document.getElementById('meetings-live-status-badge');
      if (badge) {
        badge.textContent = "Stream Disconnected";
        badge.style.color = "#ff453a";
      }
    });
    listen('open-meeting-view', async () => {
      console.log("[MEETING EVENT] open-meeting-view received from backend");
      try {
        const activeMeeting = await invoke('get_active_meeting');
        if (activeMeeting && activeMeeting.is_recording) {
          rehydrateActiveMeeting(activeMeeting);
        } else {
          tabItems.forEach(nav => nav.classList.remove('active'));
          tabPanes.forEach(pane => pane.classList.remove('active'));
          const meetingsTabNav = document.querySelector('.nav-item[data-tab="meetings"]');
          const meetingsTabPane = document.getElementById('tab-meetings');
          if (meetingsTabNav) meetingsTabNav.classList.add('active');
          if (meetingsTabPane) meetingsTabPane.classList.add('active');
          showDashboardView();
          loadMeetings();
        }
      } catch (err) {
        console.warn("[MEETING EVENT] open-meeting-view handling failed:", err);
      }
    });
  }
}

function showDashboardView() {
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.remove('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showRecordingView() {
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.remove('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showDetailView() {
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.add('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.remove('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showProcessingView() {
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.remove('hidden');
}

function showMeetingsLoading() {
  const loadingEl = document.getElementById('meetings-loading-state');
  const emptyEl = document.getElementById('meetings-empty-msg');
  const listEl = document.getElementById('meetings-list');

  if (loadingEl) {
    loadingEl.classList.remove('hidden');
    loadingEl.style.display = 'flex';
  }
  if (emptyEl) {
    emptyEl.classList.add('hidden');
    emptyEl.style.display = 'none';
  }
  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
}

function hideMeetingsLoading() {
  const loadingEl = document.getElementById('meetings-loading-state');
  if (loadingEl) {
    loadingEl.classList.add('hidden');
    loadingEl.style.display = 'none';
  }
}

function showMeetingsEmpty(message) {
  hideMeetingsLoading();
  const emptyEl = document.getElementById('meetings-empty-msg');
  const emptyText = document.getElementById('meetings-empty-text');
  const listEl = document.getElementById('meetings-list');

  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
  if (emptyEl) {
    if (emptyText && message) {
      emptyText.textContent = message;
    } else if (message) {
      emptyEl.textContent = message;
    }
    emptyEl.classList.remove('hidden');
    emptyEl.style.display = 'flex';
  }
}

let currentMeetingsReqId = 0;

async function loadMeetings(quiet = false) {
  const listGrid = document.getElementById('meetings-list');
  if (!listGrid) return;

  const reqId = ++currentMeetingsReqId;
  const token = currentConfig ? currentConfig.session_token : null;

  if (!token) {
    showMeetingsEmpty('Please sign in to view and record meeting notes.');
    return;
  }

  // Display the loading state while server fetch is in progress
  if (!quiet) showMeetingsLoading();

  try {
    const res = await fetch(`${API_URL}/meetings`, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });

    if (reqId !== currentMeetingsReqId) return; // Stale request, ignore

    if (!res.ok) throw new Error("Server error " + res.status);
    const meetings = await res.json();
    
    if (reqId !== currentMeetingsReqId) return; // Stale request, ignore

    window.allMeetings = Array.isArray(meetings) ? meetings : [];
    renderMeetingsList(window.allMeetings);
    await appendPendingLocalMeetings();
  } catch (err) {
    if (reqId !== currentMeetingsReqId) return;
    console.error("Failed to load meetings:", err);
    if (!quiet) showMeetingsEmpty('Error connecting to SpeekTo.Me server. Please check your connection.');
    await appendPendingLocalMeetings();
  }
}

function formatMeetingDate(timestamp) {
  if (!timestamp) return "N/A";
  try {
    let d;
    if (typeof timestamp === 'number') {
      d = new Date(timestamp < 10000000000 ? timestamp * 1000 : timestamp);
    } else {
      const num = Number(timestamp);
      if (!isNaN(num) && num > 0) {
        d = new Date(num < 10000000000 ? num * 1000 : num);
      } else {
        d = new Date(timestamp);
      }
    }
    if (isNaN(d.getTime())) return String(timestamp);
    const day = d.getDate();
    const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sept', 'Oct', 'Nov', 'Dec'];
    const month = months[d.getMonth()] || d.toLocaleDateString('en-US', { month: 'short' });
    const year = d.getFullYear();
    return `${day} ${month} ${year}`;
  } catch (_) {
    return String(timestamp);
  }
}

function getMeetingSpeakerCount(m) {
  if (!m) return 1;
  if (m.speaker_count && typeof m.speaker_count === 'number') {
    return Math.max(1, m.speaker_count);
  }
  let segments = [];
  if (Array.isArray(m.segments)) {
    segments = m.segments;
  } else if (m.segments_json) {
    try {
      segments = typeof m.segments_json === 'string' ? JSON.parse(m.segments_json) : m.segments_json;
    } catch (_) {}
  }
  if (Array.isArray(segments) && segments.length > 0) {
    const speakerSet = new Set();
    segments.forEach(s => {
      if (s && s.speaker && typeof s.speaker === 'string') {
        const clean = s.speaker.trim();
        if (clean) speakerSet.add(clean);
      }
    });
    if (speakerSet.size > 0) {
      return speakerSet.size;
    }
  }
  return 1;
}

async function deleteMeetingById(id) {
  if (!id) return;
  const confirmed = await showConfirmDialog({
    title: "Delete Meeting Note?",
    message: "Are you sure you want to delete this meeting note? This action cannot be undone.",
    confirmText: "Delete",
    cancelText: "Cancel",
    isDanger: true
  });
  if (!confirmed) return;

  const token = currentConfig ? currentConfig.session_token : null;
  try {
    const res = await fetch(`${API_URL}/meetings/${id}`, {
      method: 'DELETE',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });
    if (!res.ok) throw new Error("Failed to delete");
    if (activeMeetingId === id) {
      activeMeetingId = null;
    }
    showToast("Meeting note deleted successfully.", "SpeekTo.Me", "info");
    loadMeetings();
  } catch (err) {
    console.error(err);
    await showAlertDialog({
      title: "Failed to Delete",
      message: "Failed to delete meeting note: " + err,
      type: "error"
    });
  }
}

function renderMeetingsList(meetings) {
  hideMeetingsLoading();
  const emptyMsg = document.getElementById('meetings-empty-msg');
  const listGrid = document.getElementById('meetings-list');
  if (!listGrid) return;
  listGrid.innerHTML = '';

  if (!meetings || meetings.length === 0) {
    const searchInput = document.getElementById('meetings-search-input');
    const query = searchInput ? searchInput.value.trim() : '';
    if (query) {
      showMeetingsEmpty(`No meeting notes found matching "${query}".`);
    } else {
      showMeetingsEmpty('No meeting notes recorded yet. Click "New Meeting" to start!');
    }
    return;
  }

  if (emptyMsg) {
    emptyMsg.classList.add('hidden');
    emptyMsg.style.display = 'none';
  }
  listGrid.classList.remove('hidden');
  listGrid.style.display = 'grid';

  meetings.forEach(m => {
    const card = document.createElement('div');
    card.className = 'meeting-card';
    card.setAttribute('data-meeting-id', m.id);

    // Clicking card opens meeting details
    card.addEventListener('click', (e) => {
      if (e.target.closest('.meeting-card-menu-wrapper')) return;
      viewMeeting(m.id);
    });

    // 1. Top row: Badge + Title + Action Button
    const topRow = document.createElement('div');
    topRow.className = 'meeting-card-top';

    const leftGroup = document.createElement('div');
    leftGroup.className = 'meeting-card-left';

    const iconBadge = document.createElement('div');
    iconBadge.className = 'meeting-card-icon-badge';
    iconBadge.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
      <polyline points="14 2 14 8 20 8"></polyline>
      <line x1="16" y1="13" x2="8" y2="13"></line>
      <line x1="16" y1="17" x2="8" y2="17"></line>
      <line x1="10" y1="9" x2="8" y2="9"></line>
    </svg>`;

    const title = document.createElement('div');
    title.className = 'meeting-card-title';
    title.textContent = m.title || 'Recording...';
    title.title = m.title || 'Recording...';

    leftGroup.appendChild(iconBadge);
    leftGroup.appendChild(title);
    if (m.processing_status && m.processing_status !== 'completed') {
      const status = document.createElement('span');
      status.textContent = m.processing_status === 'failed' ? 'Processing failed' : m.processing_status === 'processing' ? 'Processing' : 'Queued';
      status.className = 'meeting-processing-status';
      leftGroup.appendChild(status);
    }

    // Three dots menu wrapper
    const menuWrapper = document.createElement('div');
    menuWrapper.className = 'meeting-card-menu-wrapper';

    const menuBtn = document.createElement('button');
    menuBtn.type = 'button';
    menuBtn.className = 'meeting-card-menu-btn';
    menuBtn.title = 'More options';
    menuBtn.setAttribute('aria-label', 'More options');
    menuBtn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <circle cx="5" cy="12" r="1.75"></circle>
      <circle cx="12" cy="12" r="1.75"></circle>
      <circle cx="19" cy="12" r="1.75"></circle>
    </svg>`;

    // Dropdown
    const dropdown = document.createElement('div');
    dropdown.className = 'meeting-card-dropdown hidden';

    const viewItem = document.createElement('button');
    viewItem.type = 'button';
    viewItem.className = 'meeting-dropdown-item';
    viewItem.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7z"></path>
      <circle cx="12" cy="12" r="3"></circle>
    </svg><span>View Note</span>`;
    viewItem.addEventListener('click', (e) => {
      e.stopPropagation();
      dropdown.classList.add('hidden');
      viewMeeting(m.id);
    });

    const deleteItem = document.createElement('button');
    deleteItem.type = 'button';
    deleteItem.className = 'meeting-dropdown-item danger';
    deleteItem.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <polyline points="3 6 5 6 21 6"></polyline>
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
    </svg><span>Delete Note</span>`;
    deleteItem.addEventListener('click', (e) => {
      e.stopPropagation();
      dropdown.classList.add('hidden');
      deleteMeetingById(m.id);
    });

    dropdown.appendChild(viewItem);
    if (m.processing_status === 'failed' && m.client_id) {
      const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'meeting-dropdown-item'; retry.textContent = 'Retry processing';
      retry.addEventListener('click', async e => {
        e.stopPropagation(); retry.disabled = true;
        try {
          const response = await fetch(`${API_URL}/v1/meetings/jobs/${encodeURIComponent(m.client_id)}/retry`, {method:'POST', headers:{Authorization:`Bearer ${currentConfig.session_token}`}});
          if (!response.ok) throw new Error(await response.text());
          await loadMeetings(true);
        } catch (error) { showToast(String(error), 'Retry failed', 'error'); retry.disabled = false; }
      });
      dropdown.appendChild(retry);
    }
    dropdown.appendChild(deleteItem);

    menuBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      const isCurrentlyHidden = dropdown.classList.contains('hidden');
      document.querySelectorAll('.meeting-card-dropdown').forEach(d => d.classList.add('hidden'));
      if (isCurrentlyHidden) {
        dropdown.classList.remove('hidden');
      }
    });

    menuWrapper.appendChild(menuBtn);
    menuWrapper.appendChild(dropdown);

    topRow.appendChild(leftGroup);
    topRow.appendChild(menuWrapper);

    // 2. Bottom Row: Calendar Date, Clock Duration, Users Speaker Count
    const bottomRow = document.createElement('div');
    bottomRow.className = 'meeting-card-bottom';

    // Date item
    const dateItem = document.createElement('div');
    dateItem.className = 'meeting-meta-item';
    dateItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
      <line x1="16" y1="2" x2="16" y2="6"></line>
      <line x1="8" y1="2" x2="8" y2="6"></line>
      <line x1="3" y1="10" x2="21" y2="10"></line>
    </svg><span class="meeting-meta-text">${formatMeetingDate(m.timestamp)}</span>`;

    // Duration item
    const durItem = document.createElement('div');
    durItem.className = 'meeting-meta-item';
    durItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="12" cy="12" r="10"></circle>
      <polyline points="12 6 12 12 16 14"></polyline>
    </svg><span class="meeting-meta-text">${formatDuration(m.duration)}</span>`;

    // Speakers item
    const speakerCount = getMeetingSpeakerCount(m);
    const speakerItem = document.createElement('div');
    speakerItem.className = 'meeting-meta-item';
    speakerItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
      <circle cx="9" cy="7" r="4"></circle>
      <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
      <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
    </svg><span class="meeting-meta-text">${speakerCount} ${speakerCount === 1 ? 'speaker' : 'speakers'}</span>`;

    bottomRow.appendChild(dateItem);
    bottomRow.appendChild(durItem);
    bottomRow.appendChild(speakerItem);

    card.appendChild(topRow);
    card.appendChild(bottomRow);

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

    // Date & Duration formatters matching template ("7 Sept 2026", "1h 3m 24s")
    const formatMeetingDate = (dateStr) => {
      if (!dateStr) return "N/A";
      try {
        const d = new Date(dateStr);
        if (isNaN(d.getTime())) return dateStr;
        const day = d.getDate();
        const monthNames = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sept", "Oct", "Nov", "Dec"];
        return `${day} ${monthNames[d.getMonth()]} ${d.getFullYear()}`;
      } catch (_) {
        return dateStr;
      }
    };

    const formatMeetingDurationDetailed = (seconds) => {
      if (!seconds || seconds <= 0) return "0s";
      const h = Math.floor(seconds / 3600);
      const m = Math.floor((seconds % 3600) / 60);
      const s = Math.round(seconds % 60);
      if (h > 0) return `${h}h ${m}m ${s}s`;
      if (m > 0) return `${m}m ${s}s`;
      return `${s}s`;
    };

    const formatCurrentTimeDisplay = (sec) => {
      if (isNaN(sec) || sec < 0) sec = 0;
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      const s = Math.floor(sec % 60);
      if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
      return `${m}:${String(s).padStart(2, '0')}`;
    };

    const formatTotalTimeDisplay = (sec) => {
      if (isNaN(sec) || sec <= 0) return '0:00';
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      const s = Math.floor(sec % 60);
      if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
      return `${m}:${String(s).padStart(2, '0')}`;
    };

    // Populate Overview Metadata Row
    const titleEl = document.getElementById('meeting-detail-title');
    if (titleEl) titleEl.textContent = m.title || 'Untitled Meeting';

    const dateEl = document.getElementById('meeting-detail-date');
    if (dateEl) dateEl.textContent = formatMeetingDate(m.timestamp);

    // Parse segments
    let segments = [];
    try {
      segments = JSON.parse(m.segments_json);
    } catch (_) {
      segments = [];
    }

    // Determine local user name if available (from DOM or window state)
    const domUserName = userNameEl && userNameEl.textContent ? userNameEl.textContent.trim() : '';

    let maxSegEnd = 0;
    if (segments && segments.length > 0) {
      segments.forEach(s => {
        if (s.end && s.end > maxSegEnd) maxSegEnd = s.end;
      });
    }
    let totalDuration = Math.max(m.duration || 0, maxSegEnd, 1);

    const durEl = document.getElementById('meeting-detail-duration');
    if (durEl) durEl.textContent = formatMeetingDurationDetailed(totalDuration);

    const uniqueSpeakers = new Set((segments || []).map(s => getMeetingSpeakerIdentity(s, domUserName).id));
    const spkCount = Math.max(1, uniqueSpeakers.size);
    const speakersEl = document.getElementById('meeting-detail-speakers');
    if (speakersEl) speakersEl.textContent = `${spkCount} speaker${spkCount === 1 ? '' : 's'}`;

    // Set Audio source using local disk audio (fast seeking, accurate duration, 100% private)
    const player = document.getElementById('meeting-audio-player');
    let audioLoaded = false;
    try {
      const ts = m.timestamp ? Math.floor(new Date(m.timestamp).getTime() / 1000) : null;
      console.log(`[MEETING PLAYER] Requesting local audio for meeting ${id} (ts: ${ts})...`);
      const audioDataUri = await invoke('get_local_meeting_audio', { 
        meetingId: id,
        timestamp: ts
      });
      if (audioDataUri) {
        player.src = audioDataUri;
        audioLoaded = true;
        console.log("[MEETING PLAYER] Successfully loaded local meeting audio from local disk!");
      }
    } catch (localErr) {
      console.warn("[MEETING PLAYER] Local audio loading failed, falling back to server audio:", localErr);
    }

    if (!audioLoaded) {
      try {
        const audioRes = await fetch(`${API_URL}/meetings/${id}/audio`, {
          headers: { 'Authorization': `Bearer ${token}` }
        });
        if (audioRes.ok) {
          const blob = await audioRes.blob();
          player.src = URL.createObjectURL(blob);
          console.log("[MEETING PLAYER] Loaded audio from server fallback.");
        }
      } catch (srvErr) {
        console.error("[MEETING PLAYER ERROR] Failed to fetch server audio:", srvErr);
      }
    }

    // Custom Audio Player Controls Elements
    const audioPlayBtn = document.getElementById('audio-play-btn');
    const audioPlayIcon = document.getElementById('audio-play-icon');
    const audioPauseIcon = document.getElementById('audio-pause-icon');
    const audioTimeCurrent = document.getElementById('audio-time-current');
    const audioTimeTotal = document.getElementById('audio-time-total');
    const audioScrubberTrack = document.getElementById('audio-scrubber-track');
    const audioScrubberFill = document.getElementById('audio-scrubber-fill');
    const audioScrubberThumb = document.getElementById('audio-scrubber-thumb');
    const audioVolBtn = document.getElementById('audio-vol-btn');
    const audioSpeedSelect = document.getElementById('audio-speed-select');

    if (audioTimeCurrent) audioTimeCurrent.textContent = '0:00';
    if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(totalDuration);
    if (audioScrubberFill) audioScrubberFill.style.width = '0%';
    if (audioScrubberThumb) audioScrubberThumb.style.left = '0%';

    const updateAudioPlayState = () => {
      if (!audioPlayIcon || !audioPauseIcon) return;
      if (player.paused) {
        audioPlayIcon.classList.remove('hidden');
        audioPauseIcon.classList.add('hidden');
      } else {
        audioPlayIcon.classList.add('hidden');
        audioPauseIcon.classList.remove('hidden');
      }
    };

    if (audioPlayBtn) {
      audioPlayBtn.onclick = () => {
        if (player.paused) {
          player.play();
        } else {
          player.pause();
        }
      };
    }

    player.onplay = updateAudioPlayState;
    player.onpause = updateAudioPlayState;
    player.onended = () => {
      updateAudioPlayState();
      if (audioScrubberFill) audioScrubberFill.style.width = '0%';
      if (audioScrubberThumb) audioScrubberThumb.style.left = '0%';
    };
    updateAudioPlayState();

    if (audioVolBtn) {
      audioVolBtn.onclick = () => {
        player.muted = !player.muted;
        audioVolBtn.style.opacity = player.muted ? '0.4' : '1.0';
      };
    }

    if (audioSpeedSelect) {
      audioSpeedSelect.value = "1.0";
      player.playbackRate = 1.0;
      audioSpeedSelect.onchange = (e) => {
        player.playbackRate = parseFloat(e.target.value);
      };
    }

    let isDraggingScrubber = false;
    const seekFromScrubber = (e) => {
      const effDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0) ? player.duration : totalDuration;
      if (!audioScrubberTrack || effDuration <= 0) return;
      const rect = audioScrubberTrack.getBoundingClientRect();
      const clickX = e.clientX - rect.left;
      const pct = Math.max(0, Math.min(1, clickX / rect.width));
      player.currentTime = pct * effDuration;
      if (audioScrubberFill) audioScrubberFill.style.width = `${pct * 100}%`;
      if (audioScrubberThumb) audioScrubberThumb.style.left = `${pct * 100}%`;
      if (audioTimeCurrent) audioTimeCurrent.textContent = formatCurrentTimeDisplay(player.currentTime);
    };

    if (audioScrubberTrack) {
      audioScrubberTrack.onmousedown = (e) => {
        isDraggingScrubber = true;
        seekFromScrubber(e);
      };
    }

    window.onmousemove = (e) => {
      if (isDraggingScrubber) seekFromScrubber(e);
    };

    window.onmouseup = () => {
      if (isDraggingScrubber) isDraggingScrubber = false;
    };

    // Detail 3-dots Dropdown Menu
    const detailMenuBtn = document.getElementById('meeting-detail-menu-btn');
    const detailDropdown = document.getElementById('meeting-detail-dropdown');
    const detailDeleteOpt = document.getElementById('meeting-detail-delete-opt');
    if (detailMenuBtn && detailDropdown) {
      detailMenuBtn.onclick = (e) => {
        e.stopPropagation();
        detailDropdown.classList.toggle('hidden');
      };
    }
    if (detailDeleteOpt) {
      detailDeleteOpt.onclick = (e) => {
        e.stopPropagation();
        if (detailDropdown) detailDropdown.classList.add('hidden');
        confirmDeleteMeeting();
      };
    }

    // Setup active tab (Timeline by default)
    const tabBtns = document.querySelectorAll('.detail-tab-btn');
    const panes = document.querySelectorAll('.detail-tab-pane');
    tabBtns.forEach(b => b.classList.remove('active'));
    panes.forEach(p => p.classList.remove('active'));
    const defaultTimelineBtn = document.querySelector('.detail-tab-btn[data-detail-tab="timeline"]') || document.querySelector('.detail-tab-btn');
    const defaultTimelinePane = document.getElementById('detail-pane-timeline') || document.getElementById('detail-pane-transcript');
    if (defaultTimelineBtn) defaultTimelineBtn.classList.add('active');
    if (defaultTimelinePane) defaultTimelinePane.classList.add('active');

    // Helper function to render or update timeline when audio duration is resolved
    const doRenderTimeline = () => {
      const audioDuration = player && player.duration && !isNaN(player.duration) && player.duration > 0 ? player.duration : 0;
      totalDuration = audioDuration > 0 ? audioDuration : (m.duration > 0 ? m.duration : Math.max(maxSegEnd, 1));
      
      const durLabel = document.getElementById('meeting-detail-duration');
      if (durLabel) durLabel.textContent = formatMeetingDurationDetailed(totalDuration);

      if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(totalDuration);
      
      renderMeetingTimeline(segments, totalDuration, player, domUserName);
    };

    if (player) {
      player.onloadedmetadata = () => {
        console.log(`[MEETING PLAYER] Audio loadedmetadata: duration = ${player.duration}s`);
        doRenderTimeline();
      };
      player.ondurationchange = () => {
        console.log(`[MEETING PLAYER] Audio durationchange: duration = ${player.duration}s`);
        doRenderTimeline();
      };
    }

    // 1. RENDER AUDIO TIMELINE VIEW
    doRenderTimeline();

    // 2. RENDER TRANSCRIPT VIEW
    const transcriptTextEl = document.getElementById('meeting-transcript-text');
    transcriptTextEl.innerHTML = '';

    renderMeetingTranscript(segments, player, domUserName, m.transcript);

    let isUserHoveringTranscript = false;
    transcriptTextEl.onmouseenter = () => { isUserHoveringTranscript = true; };
    transcriptTextEl.onmouseleave = () => { isUserHoveringTranscript = false; };
    transcriptTextEl.onwheel = () => { isUserHoveringTranscript = true; };

    let lastActiveSpan = null;
    let isUserHoveringTable = false;
    let lastActiveTableRow = null;

    player.ontimeupdate = () => {
      const time = player.currentTime;
      const effectiveDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0)
        ? player.duration
        : totalDuration;

      // 1. Digital Time Counter in Audio Bar
      if (audioTimeCurrent) audioTimeCurrent.textContent = formatCurrentTimeDisplay(time);
      if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(effectiveDuration);

      // 2. Scrubber Track Fill & Thumb
      if (!isDraggingScrubber && audioScrubberFill && audioScrubberThumb && effectiveDuration > 0) {
        const pct = Math.max(0, Math.min(100, (time / effectiveDuration) * 100));
        audioScrubberFill.style.width = `${pct}%`;
        audioScrubberThumb.style.left = `${pct}%`;
      }

      // 3. Timeline Playhead Needle & Auto-Scroll
      const playhead = document.getElementById('timeline-playhead');
      const scrollArea = document.getElementById('timeline-scroll-area');
      const scrollCanvas = document.getElementById('timeline-scroll-canvas');

      if (playhead && effectiveDuration > 0) {
        const pct = Math.max(0, Math.min(100, (time / effectiveDuration) * 100));
        playhead.style.left = `${pct}%`;

        // Auto-scroll timeline when zoomed
        if (scrollArea && scrollCanvas && scrollCanvas.offsetWidth > scrollArea.offsetWidth && !player.paused) {
          const playheadPx = (pct / 100) * scrollCanvas.offsetWidth;
          const targetScroll = playheadPx - (scrollArea.offsetWidth / 2);
          scrollArea.scrollLeft = targetScroll;
        }
      }

      // 4. Update Timeline Active Segment Boxes
      const segmentBoxes = document.querySelectorAll('.segment-box');
      segmentBoxes.forEach(box => {
        const start = parseFloat(box.dataset.start);
        const end = parseFloat(box.dataset.end);
        if (time >= start && time <= end) {
          box.classList.add('active');
        } else {
          box.classList.remove('active');
        }
      });

      // 5. Update Turn-by-Turn Inspector Table Active Row
      const tableRows = document.querySelectorAll('.turn-table-row');
      let currentActiveRow = null;
      tableRows.forEach(row => {
        const start = parseFloat(row.dataset.start);
        const end = parseFloat(row.dataset.end);
        if (time >= start && time <= end) {
          row.classList.add('active-turn-row');
          currentActiveRow = row;
        } else {
          row.classList.remove('active-turn-row');
        }
      });

      if (currentActiveRow && currentActiveRow !== lastActiveTableRow) {
        lastActiveTableRow = currentActiveRow;
        if (!isUserHoveringTable) {
          currentActiveRow.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }

      // 6. Update Transcript Active Spans
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

    // Inspector table hover lock
    const inspectorScroll = document.querySelector('.inspector-table-scroll');
    if (inspectorScroll) {
      inspectorScroll.onmouseenter = () => { isUserHoveringTable = true; };
      inspectorScroll.onmouseleave = () => { isUserHoveringTable = false; };
      inspectorScroll.onwheel = () => { isUserHoveringTable = true; };
    }

    // Render summary and actions
    document.getElementById('meeting-summary-text').innerHTML = parseMarkdownToHTML(m.summary);
    document.getElementById('meeting-actions-text').innerHTML = parseMarkdownToHTML(m.action_items);

    // Detail Tabs switching
    const currentTabBtns = document.querySelectorAll('.detail-tab-btn');
    currentTabBtns.forEach(btn => {
      const newBtn = btn.cloneNode(true);
      btn.parentNode.replaceChild(newBtn, btn);
      newBtn.addEventListener('click', () => {
        const activeBtns = document.querySelectorAll('.detail-tab-btn');
        const activePanes = document.querySelectorAll('.detail-tab-pane');
        activeBtns.forEach(b => b.classList.remove('active'));
        activePanes.forEach(p => p.classList.remove('active'));
        
        newBtn.classList.add('active');
        const targetPane = newBtn.getAttribute('data-detail-tab');
        const paneEl = document.getElementById(`detail-pane-${targetPane}`);
        if (paneEl) paneEl.classList.add('active');
      });
    });

  } catch (err) {
    console.error(err);
    showToast(err.message, "Failed to load meeting", "error");
    showDashboardView();
  }
}

// Dedicated High-Precision Visualizer & Timeline Renderer
function renderMeetingTimeline(segments, totalDuration, player, currentUserName) {
  const legendContainer = document.getElementById('timeline-speaker-legend');
  const headersList = document.getElementById('timeline-headers-list');
  const rulerTrack = document.getElementById('timeline-ruler-track');
  const lanesBody = document.getElementById('timeline-lanes-body');
  const scrollCanvas = document.getElementById('timeline-scroll-canvas');
  const scrollArea = document.getElementById('timeline-scroll-area');
  const inspectorTbody = document.getElementById('timeline-inspector-tbody');
  const turnsLabel = document.getElementById('inspector-total-turns-label');
  const searchInput = document.getElementById('timeline-search-speaker');

  if (!headersList || !lanesBody || !rulerTrack) return;

  // Clear previous content
  headersList.innerHTML = '';
  rulerTrack.innerHTML = '';
  if (legendContainer) legendContainer.innerHTML = '';
  if (inspectorTbody) inspectorTbody.innerHTML = '';
  lanesBody.innerHTML = `
    <div class="timeline-playhead" id="timeline-playhead">
      <div class="timeline-playhead-line"></div>
    </div>
  `;

  const formatOffset = (seconds) => {
    if (isNaN(seconds) || seconds < 0) return '0:00';
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

  // Sync with actual audio player duration when metadata loads
  if (player && player.duration && player.duration > 0 && !isNaN(player.duration)) {
    totalDuration = player.duration;
  }

  if (turnsLabel) turnsLabel.textContent = `${(segments || []).length}`;

  if (!segments || segments.length === 0) {
    lanesBody.innerHTML += '<div style="padding: 30px; text-align: center; color: var(--text-muted); font-size: 13px; font-style: italic;">No audio segment data available for this recording.</div>';
    return;
  }

  // Channel and stable speaker ID are shared with the transcript and inspector.
  const speakerGroups = new Map();
  segments.forEach((seg, idx) => {
    const identity = getMeetingSpeakerIdentity(seg, currentUserName);
    if (!speakerGroups.has(identity.id)) {
      speakerGroups.set(identity.id, { ...identity, name: identity.label, segments: [], totalTime: 0 });
    }
    const group = speakerGroups.get(identity.id);
    group.segments.push({ ...seg, turnIndex: idx + 1 });
    group.totalTime += Math.max(0, (seg.end || 0) - (seg.start || 0));
  });

  // Sort speakers: Local user (isMe) ALWAYS at Row 0 (Top Track)
  const sortedSpeakers = Array.from(speakerGroups.values()).sort((a, b) => {
    if (a.isMe && !b.isMe) return -1;
    if (!a.isMe && b.isMe) return 1;
    return a.name.localeCompare(b.name, undefined, { numeric: true });
  });

  window.__currentSpeakerThemes = {};
  sortedSpeakers.forEach(spk => {
    const theme = getMeetingSpeakerTheme(spk);
    window.__currentSpeakerThemes[spk.id] = theme;

    // Add to Legend
    if (legendContainer) {
      const pill = document.createElement('span');
      pill.className = 'legend-pill';
      pill.style.backgroundColor = theme.bg;
      pill.style.borderColor = theme.border;
      pill.style.color = theme.textColor;
      pill.innerHTML = `<span style="width: 6px; height: 6px; border-radius: 50%; background-color: ${theme.color}; display: inline-block;"></span><span>${escapeHtml(theme.label)}</span>`;
      legendContainer.appendChild(pill);
    }
  });

  // Render Time Ruler Ticks at clean human intervals
  const steps = [5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];
  let step = 600;
  for (const s of steps) {
    if (totalDuration / s <= 8) {
      step = s;
      break;
    }
  }

  for (let t = 0; t <= totalDuration; t += step) {
    const percent = (t / totalDuration) * 100;
    const tickEl = document.createElement('div');
    tickEl.className = 'timeline-ruler-tick';
    tickEl.style.left = `${percent}%`;
    tickEl.innerHTML = `
      <span class="timeline-ruler-tick-label">${formatOffset(t)}</span>
      <div class="timeline-ruler-tick-line"></div>
    `;
    rulerTrack.appendChild(tickEl);
  }

  // Interactive Drag-to-Scrub on Ruler & Lanes
  let isScrubbing = false;
  const handleScrub = (e) => {
    const effectiveTotalDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0) ? player.duration : totalDuration;
    if (!scrollCanvas || effectiveTotalDuration <= 0) return;
    const rect = scrollCanvas.getBoundingClientRect();
    const clickX = e.clientX - rect.left;
    const pct = Math.max(0, Math.min(1, clickX / rect.width));
    const seekTime = pct * effectiveTotalDuration;
    player.currentTime = seekTime;
    
    // Immediate visual update
    const playhead = document.getElementById('timeline-playhead');
    if (playhead) playhead.style.left = `${pct * 100}%`;
    const scrubberFill = document.getElementById('audio-scrubber-fill');
    const scrubberThumb = document.getElementById('audio-scrubber-thumb');
    if (scrubberFill) scrubberFill.style.width = `${pct * 100}%`;
    if (scrubberThumb) scrubberThumb.style.left = `${pct * 100}%`;
    const audioTimeCurrent = document.getElementById('audio-time-current');
    if (audioTimeCurrent) {
      const m = Math.floor(seekTime / 60);
      const s = Math.floor(seekTime % 60);
      audioTimeCurrent.textContent = `${m}:${String(s).padStart(2, '0')}`;
    }
  };

  const startScrub = (e) => {
    isScrubbing = true;
    handleScrub(e);
  };

  rulerTrack.onmousedown = startScrub;
  lanesBody.onmousedown = startScrub;

  window.addEventListener('mousemove', (e) => {
    if (isScrubbing) handleScrub(e);
  });

  window.addEventListener('mouseup', () => {
    if (isScrubbing) isScrubbing = false;
  });

  // Render Tracks & Segment Boxes
  sortedSpeakers.forEach((spk) => {
    const theme = window.__currentSpeakerThemes[spk.id];
    const talkPercent = Math.round((spk.totalTime / totalDuration) * 100);

    // 1. Left Sticky Header Item
    const headerItem = document.createElement('div');
    headerItem.className = 'timeline-header-item';
    headerItem.innerHTML = `
      <div class="timeline-header-swatch" style="background-color: ${theme.color};"></div>
      <div class="timeline-header-info">
        <div class="timeline-header-name" title="${escapeHtml(theme.label)}">${escapeHtml(theme.label)}</div>
        <div class="timeline-header-meta">${spk.segments.length} turns &bull; ${talkPercent}% talk time</div>
      </div>
    `;
    headersList.appendChild(headerItem);

    // 2. Right Track Lane Row
    const laneRow = document.createElement('div');
    laneRow.className = 'timeline-lane-row';
    laneRow.dataset.speaker = spk.id;

    spk.segments.forEach(seg => {
      const leftPercent = (seg.start / totalDuration) * 100;
      const widthPercent = ((seg.end - seg.start) / totalDuration) * 100;

      const box = document.createElement('div');
      box.className = 'segment-box';
      box.style.left = `${leftPercent}%`;
      box.style.width = `max(2px, ${widthPercent}%)`;
      box.style.minWidth = '2px';
      box.style.backgroundColor = theme.bg;
      box.style.border = `1px solid ${theme.color}`;
      box.dataset.start = seg.start;
      box.dataset.end = seg.end;
      box.dataset.speaker = spk.id;
      box.title = `${escapeHtml(theme.label)}: ${seg.start.toFixed(2)}s - ${seg.end.toFixed(2)}s (${(seg.end - seg.start).toFixed(2)}s)\n"${seg.text || ''}"`;

      // Click to seek & play
      box.onclick = (e) => {
        e.stopPropagation();
        player.currentTime = seg.start;
        player.play();
      };

      laneRow.appendChild(box);
    });

    lanesBody.appendChild(laneRow);
  });

  // Render Turn-by-Turn Inspector Table
  if (inspectorTbody) {
    segments.forEach((seg, idx) => {
      const identity = getMeetingSpeakerIdentity(seg, currentUserName);
      const spkName = identity.label;
      const theme = window.__currentSpeakerThemes[identity.id] || {
        color: 'var(--text-muted)',
        border: 'var(--border-subtle)',
        bg: 'var(--bg-card-subtle)',
        textColor: 'var(--text-primary)',
        label: spkName
      };

      const duration = Math.max(0, seg.end - seg.start);
      const leftPct = (seg.start / totalDuration) * 100;
      const widthPct = Math.max((duration / totalDuration) * 100, 1.5);

      const tr = document.createElement('tr');
      tr.className = 'turn-table-row';
      tr.dataset.turnIdx = idx;
      tr.dataset.start = seg.start;
      tr.dataset.end = seg.end;
      tr.dataset.speaker = spkName;
      tr.dataset.speakerId = identity.id;

      tr.innerHTML = `
        <td style="font-family: monospace; color: var(--text-muted);">#${String(idx + 1).padStart(2, '0')}</td>
        <td>
          <span class="inspector-speaker-pill" style="background-color: ${theme.bg}; border: 1px solid ${theme.border}; color: ${theme.textColor};">
            <span style="width: 6px; height: 6px; border-radius: 50%; background-color: ${theme.color}; display: inline-block;"></span>
            ${escapeHtml(theme.label)}
          </span>
        </td>
        <td>
          <span class="inspector-start-badge">${seg.start.toFixed(2)}s</span>
        </td>
        <td style="font-family: monospace; color: var(--text-muted);">${seg.end.toFixed(2)}s</td>
        <td style="font-family: monospace; color: var(--text-secondary); font-weight: 600;">${duration.toFixed(2)}s</td>
        <td>
          <div class="mini-timeline-bar">
            <div class="mini-timeline-fill" style="left: ${leftPct}%; width: ${widthPct}%; background-color: ${theme.color};"></div>
          </div>
        </td>
        <td style="color: var(--text-primary); max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(seg.text || '')}">
          ${seg.text ? escapeHtml(seg.text) : '<span style="color: var(--text-muted); font-style: italic;">(No speech text)</span>'}
        </td>
      `;

      tr.onclick = () => {
        player.currentTime = seg.start;
        player.play();
      };

      inspectorTbody.appendChild(tr);
    });
  }

  // Filter input logic
  if (searchInput) {
    searchInput.oninput = (e) => {
      const q = e.target.value.toLowerCase().trim();
      document.querySelectorAll('.turn-table-row').forEach(row => {
        const spk = (row.dataset.speaker || '').toLowerCase();
        const text = (row.querySelector('td:last-child')?.textContent || '').toLowerCase();
        if (!q || spk.includes(q) || text.includes(q)) {
          row.style.display = '';
        } else {
          row.style.display = 'none';
        }
      });
    };
  }
}

// Speaker numbers (including zero) are remote diarizer IDs, never a local-user shortcut.
function getMeetingSpeakerIdentity(segment, currentUserName = '') {
  const channel = String(segment.channel || '').toLowerCase();
  const sourceId = String(segment.speaker_id || '').trim();
  const rawName = String(segment.speaker || segment.speaker_name || '').replace(/[\[\]]/g, '').trim();
  const clean = rawName.toLowerCase();
  const numbered = clean.match(/^(?:speaker|spk)[ _-]*(\d+)$/) || sourceId.match(/^spk[_-]?(\d+)$/i);
  const remoteChannel = channel === 'system' || channel === 'loopback';
  const localAlias = ['me', 'you', 'local speaker'].includes(clean);
  const exactOwnerName = currentUserName && clean === currentUserName.toLowerCase().trim();
  const isMe = channel === 'mic' || (!remoteChannel && !numbered &&
    (sourceId === 'speaker_local' || (!sourceId && (localAlias || exactOwnerName))));
  if (isMe) return { id: 'mic:speaker_local', label: 'Me', isMe: true };
  const id = sourceId || (numbered ? `spk_${numbered[1]}` : (clean || 'unknown'));
  let label = rawName || (numbered ? `Speaker ${numbered[1]}` : 'Unknown speaker');
  if (localAlias) label += ' (System)';
  return { id: `system:${id}`, label, isMe: false };
}

function getMeetingSpeakerTheme(identity) {
  if (identity.isMe) return {
    color: 'var(--speaker-mic-color, var(--accent-color))',
    border: 'var(--speaker-mic-border, var(--border-focus))',
    bg: 'var(--speaker-mic-bg, var(--accent-tint))',
    textColor: 'var(--text-primary)', label: identity.label
  };
  const color = getMeetingSpeakerColor(identity.id);
  return { color, border: `${color}55`, bg: `${color}22`, textColor: 'var(--text-primary)', label: identity.label };
}

function renderMeetingTranscript(segments, player, currentUserName, fallbackText) {
  const container = document.getElementById('meeting-transcript-text');
  if (!container) return;
  container.replaceChildren();
  if (!segments || !segments.length) {
    container.textContent = fallbackText || 'No transcript text available.';
    return;
  }
  let previousId = null;
  let blockText = null;
  for (const segment of segments) {
    const identity = getMeetingSpeakerIdentity(segment, currentUserName);
    const theme = getMeetingSpeakerTheme(identity);
    if (identity.id !== previousId) {
      previousId = identity.id;
      const block = document.createElement('div');
      block.className = `meeting-speaker-block ${identity.isMe ? 'is-me' : 'is-remote'}`;
      block.dataset.speakerId = identity.id;
      block.style.borderLeftColor = theme.color;
      const header = document.createElement('div');
      header.className = 'meeting-speaker-header';
      const avatar = document.createElement('div');
      avatar.className = 'meeting-speaker-avatar';
      avatar.textContent = identity.isMe ? 'ME' : getMeetingSpeakerInitials(identity.label);
      avatar.style.backgroundColor = theme.color;
      const label = document.createElement('span');
      label.className = 'meeting-speaker-name';
      label.textContent = identity.label;
      const time = document.createElement('span');
      time.className = 'meeting-speaker-time';
      time.textContent = formatMeetingTimeOffset(segment.start);
      header.append(avatar, label, time);
      blockText = document.createElement('div');
      blockText.className = 'meeting-speaker-text';
      block.append(header, blockText);
      container.appendChild(block);
    }
    const text = document.createElement('span');
    text.className = 'transcript-segment';
    text.dataset.start = segment.start;
    text.dataset.end = segment.end;
    text.textContent = (segment.text || '') + ' ';
    text.onclick = () => { player.currentTime = segment.start; player.play(); };
    blockText.appendChild(text);
  }
}

function formatMeetingTimeOffset(seconds) {
  seconds = Math.max(0, Number(seconds) || 0);
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const sec = String(Math.floor(seconds % 60)).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
}

// Global helper functions for speaker avatars, initials & theme colors
function getMeetingSpeakerInitials(name) {
  if (!name) return 'SP';
  const cleanName = name.replace(/[\[\]]/g, '').trim();
  const lower = cleanName.toLowerCase();
  const numbered = lower.match(/^(?:speaker|spk)[ _-]*(\d+)$/);
  if (numbered) return `S${numbered[1]}`;
  if (lower === 'you' || lower === 'me') return 'ME';
  const parts = cleanName.split(' ');
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  return cleanName.slice(0, 2).toUpperCase();
}

function getMeetingSpeakerColor(name) {
  const colors = ['#2563eb', '#d97706', '#7c3aed', '#059669', '#e11d48', '#0284c7', '#4f46e5', '#0d9488'];
  const clean = String(name || '').toLowerCase().replace(/^system:/, '').trim();
  const numbered = clean.match(/^(?:speaker|spk)[ _-]*(\d+)$/);
  if (numbered) return colors[Number(numbered[1]) % colors.length];
  let hash = 0;
  for (let i = 0; i < clean.length; i++) hash = clean.charCodeAt(i) + ((hash << 5) - hash);
  return colors[Math.abs(hash) % colors.length];
}

function handleLiveMeetingTurn(turn) {
  if (!turn || !turn.id) return;
  const turnsList = document.getElementById('meetings-live-turns-list');
  const waitingText = document.getElementById('meetings-live-waiting-text');
  if (waitingText) waitingText.style.display = 'none';
  if (!turnsList) return;

  const identity = getMeetingSpeakerIdentity(turn, currentUserInfo?.name || '');
  const isMe = identity.isMe;
  const formatOffset = (seconds) => {
    if (isNaN(seconds) || seconds < 0) return '0:00';
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return `${m}:${s < 10 ? '0' + s : s}`;
  };

  const displayName = identity.label;
  const avatarText = isMe ? 'ME' : getMeetingSpeakerInitials(displayName);
  const avatarBg = getMeetingSpeakerTheme(identity).color;

  let turnEl = document.getElementById(`live-${turn.id}`);
  if (!turnEl) {
    turnEl = document.createElement('div');
    turnEl.id = `live-${turn.id}`;
    turnEl.dataset.startTime = turn.start_time != null ? String(turn.start_time) : "0";
    turnEl.className = `live-turn-item ${isMe ? 'is-me' : 'is-remote'}`;
    turnEl.style.borderLeftColor = avatarBg;

    const header = document.createElement('div');
    header.className = 'live-turn-header';

    const avatar = document.createElement('div');
    avatar.className = 'live-turn-avatar';
    avatar.textContent = avatarText;
    avatar.style.backgroundColor = avatarBg;

    const spkSpan = document.createElement('span');
    spkSpan.className = 'live-turn-speaker';
    spkSpan.textContent = displayName;

    const timeSpan = document.createElement('span');
    timeSpan.className = 'live-turn-time';
    timeSpan.textContent = formatOffset(turn.start_time);

    header.appendChild(avatar);
    header.appendChild(spkSpan);
    header.appendChild(timeSpan);
    turnEl.appendChild(header);

    const textEl = document.createElement('div');
    textEl.className = 'live-turn-text';
    turnEl.appendChild(textEl);

    // Insert in chronological order by start_time
    const existingItems = Array.from(turnsList.querySelectorAll('.live-turn-item'));
    const turnStart = parseFloat(turn.start_time || 0);
    const nextItem = existingItems.find(item => {
      const itemStart = parseFloat(item.dataset.startTime || 0);
      return itemStart > turnStart;
    });
    if (nextItem) {
      turnsList.insertBefore(turnEl, nextItem);
    } else {
      turnsList.appendChild(turnEl);
    }
  } else {
    // If start_time was updated
    if (turn.start_time != null) {
      turnEl.dataset.startTime = String(turn.start_time);
    }
    // Dynamically update speaker name, avatar, and border if refined by Sortformer diarization
    turnEl.className = `live-turn-item ${isMe ? 'is-me' : 'is-remote'}`;
    turnEl.style.borderLeftColor = avatarBg;
    const avatar = turnEl.querySelector('.live-turn-avatar');
    if (avatar) {
      avatar.textContent = avatarText;
      avatar.style.backgroundColor = avatarBg;
    }
    const spkSpan = turnEl.querySelector('.live-turn-speaker');
    if (spkSpan) {
      spkSpan.textContent = displayName;
    }
    const timeSpan = turnEl.querySelector('.live-turn-time');
    if (timeSpan && turn.start_time != null) {
      timeSpan.textContent = formatOffset(turn.start_time);
    }
  }

  const textEl = turnEl.querySelector('.live-turn-text');
  if (textEl) {
    textEl.textContent = turn.text;
    if (!turn.is_final) {
      textEl.classList.add('interim');
    } else {
      textEl.classList.remove('interim');
    }
  }

  // Smooth autoscroll down
  turnsList.scrollTop = turnsList.scrollHeight;
}

function rehydrateActiveMeeting(activeMeeting) {
  if (!activeMeeting || !activeMeeting.is_recording) return;
  console.log("[MEETING RESTORE] Rehydrating active meeting session:", activeMeeting);
  currentMeetingFile = activeMeeting.filepath;

  // 1. Switch to Meetings tab
  tabItems.forEach(nav => nav.classList.remove('active'));
  tabPanes.forEach(pane => pane.classList.remove('active'));
  const meetingsTabNav = document.querySelector('.nav-item[data-tab="meetings"]');
  const meetingsTabPane = document.getElementById('tab-meetings');
  if (meetingsTabNav) meetingsTabNav.classList.add('active');
  if (meetingsTabPane) meetingsTabPane.classList.add('active');

  // 2. Switch to Recording view
  showRecordingView();

  // 3. Restore title if set
  const titleInput = document.getElementById('meeting-title-input');
  if (titleInput) {
    if (activeMeeting.title && activeMeeting.title !== 'Recording...') {
      titleInput.value = activeMeeting.title;
    } else {
      titleInput.value = '';
    }
  }

  // 4. Restore timer to elapsed seconds
  meetingSeconds = Math.floor(activeMeeting.elapsed_seconds || 0);
  const timerEl = document.getElementById('meetings-timer');
  if (timerEl) {
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }

  // 5. Update status badge
  const badge = document.getElementById('meetings-live-status-badge');
  if (badge) {
    badge.textContent = "Recording locally";
    badge.style.color = "#57bf7c";
  }

  // 6. Restore turns list
  const turnsList = document.getElementById('meetings-live-turns-list');
  if (turnsList) {
    turnsList.innerHTML = '';
    if (activeMeeting.turns && activeMeeting.turns.length > 0) {
      activeMeeting.turns.forEach(turn => {
        handleLiveMeetingTurn(turn);
      });
    } else {
      turnsList.innerHTML = `
        <div id="meetings-live-waiting-text" class="live-waiting-text">
          Recording microphone and system audio. Transcription and speaker identification start after you stop.
        </div>
      `;
    }
  }

  // 7. Restart intervals (timer clock + audio level polling)
  clearInterval(timerInterval);
  clearInterval(recordingInterval);

  timerInterval = setInterval(() => {
    meetingSeconds++;
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    if (timerEl) timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }, 1000);

  recordingInterval = setInterval(async () => {
    try {
      const micFill = document.getElementById('mic-level-fill');
      const loopbackFill = document.getElementById('loopback-level-fill');
      const [mic, loopback] = await invoke('get_meeting_levels');
      // Perceptual dynamic scaling (0-1000 RMS -> 0-100% meter bar)
      const micPercent = mic > 0 ? Math.min(100, Math.max(3, Math.round(Math.pow(mic / 250, 0.5) * 100))) : 0;
      const loopbackPercent = loopback > 0 ? Math.min(100, Math.max(3, Math.round(Math.pow(loopback / 250, 0.5) * 100))) : 0;
      if (micFill) micFill.style.width = `${micPercent}%`;
      if (loopbackFill) loopbackFill.style.width = `${loopbackPercent}%`;
    } catch (e) {
      console.error("[MEETING FRONTEND ERROR] get_meeting_levels failed:", e);
    }
  }, 80);
}

async function startMeetingRecording() {
  console.log("[MEETING FRONTEND] startMeetingRecording clicked");
  const token = currentConfig ? currentConfig.session_token : null;
  if (!token) {
    console.warn("[MEETING FRONTEND] No session token found. Aborting.");
    showToast("Please sign in to record meeting notes.", "Sign-in Required", "info");
    return;
  }

  showRecordingView();
  const titleInput = document.getElementById('meeting-title-input');
  titleInput.value = '';

  const turnsList = document.getElementById('meetings-live-turns-list');
  if (turnsList) {
    turnsList.innerHTML = `
      <div id="meetings-live-waiting-text" class="live-waiting-text">
        Recording microphone and system audio. Transcription and speaker identification start after you stop.
      </div>
    `;
  }
  const badge = document.getElementById('meetings-live-status-badge');
  if (badge) {
    badge.textContent = "Starting recording...";
    badge.style.color = "#888";
  }

  const timerEl = document.getElementById('meetings-timer');
  timerEl.textContent = '00:00:00';
  meetingSeconds = 0;

  try {
    console.log("[MEETING FRONTEND] Invoking start_meeting command on Tauri backend...");
    currentMeetingFile = await invoke('start_meeting', { title: "Recording..." });
    if (badge) { badge.textContent="Recording locally"; badge.style.color="#4ec9b0"; }
    console.log("[MEETING FRONTEND] start_meeting returned successfully. Filepath:", currentMeetingFile);
  } catch (err) {
    console.error("[MEETING FRONTEND ERROR] Failed to start meeting recording:", err);
    showToast(String(err), "Meeting Start Error", "error");
    cancelMeetingRecording();
    return;
  }

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
      // Perceptual dynamic scaling (0-1000 RMS -> 0-100% meter bar)
      const micPercent = mic > 0 ? Math.min(100, Math.max(3, Math.round(Math.pow(mic / 250, 0.5) * 100))) : 0;
      const loopbackPercent = loopback > 0 ? Math.min(100, Math.max(3, Math.round(Math.pow(loopback / 250, 0.5) * 100))) : 0;
      if (micFill) micFill.style.width = `${micPercent}%`;
      if (loopbackFill) loopbackFill.style.width = `${loopbackPercent}%`;
    } catch (e) {
      console.error("[MEETING FRONTEND ERROR] get_meeting_levels failed:", e);
    }
  }, 80);
}

async function stopMeetingRecording() {
  console.log("[MEETING FRONTEND] stopMeetingRecording clicked");
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  
  showProcessingView();
  
  const titleInput = document.getElementById('meeting-title-input');
  let title = titleInput.value.trim();
  if (!title) {
    title = `Meeting on ${new Date().toLocaleDateString()} ${new Date().toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}`;
  }
  console.log("[MEETING FRONTEND] Stopping meeting with title:", title);

  try {
    console.log("[MEETING FRONTEND] Invoking stop_meeting command on Tauri backend...");
    const res = await invoke('stop_meeting', {title});
    currentMeetingFile = null;
    console.log("[MEETING FRONTEND] stop_meeting returned:", res);
    let m = null;
    try {
      m = JSON.parse(res);
      console.log("[MEETING FRONTEND] Parsed meeting object from stop_meeting:", m);
    } catch (_) {
      console.log("[MEETING FRONTEND] stop_meeting returned audio filepath. Uploading audio to server via upload_meeting...", res);
      const resultJson = await invoke('upload_meeting', { title, filepath: res });
      console.log("[MEETING FRONTEND] upload_meeting returned:", resultJson);
      m = JSON.parse(resultJson);
    }

    if (m && ['queued','processing'].includes(m.status)) {
      showDashboardView();
      loadMeetings();
      await showAlertDialog({title:"Meeting uploaded",message:"Your meeting is queued for transcription and speaker identification. You can close the app; results will be saved to your meeting history.",type:"info"});
    } else if (m && m.id) {
      console.log("[MEETING FRONTEND] Successfully finalized meeting note! ID:", m.id);
      showDashboardView();
      loadMeetings();
      viewMeeting(m.id);
    } else {
      throw new Error("Invalid meeting response from server");
    }
  } catch (err) {
    console.error("[MEETING FRONTEND ERROR] Failed to finalize meeting note:", err);
    currentMeetingFile = null;
    await showAlertDialog({
      title: "Finalization Failed",
      message: "Failed to finalize meeting note: " + err,
      type: "error"
    });
    showDashboardView();
    loadMeetings();
  }
}

async function cancelMeetingRecording() {
  console.log("[MEETING FRONTEND] cancelMeetingRecording called");
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  try {
    await invoke('cancel_meeting');
  } catch (e) {}
  currentMeetingFile = null;
  showDashboardView();
  loadMeetings();
}

async function confirmDeleteMeeting() {
  if (!activeMeetingId) return;
  const confirmed = await showConfirmDialog({
    title: "Delete Meeting Note?",
    message: "Are you sure you want to delete this meeting note? This action cannot be undone.",
    confirmText: "Delete",
    cancelText: "Cancel",
    isDanger: true
  });
  if (!confirmed) return;
  
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
    await showAlertDialog({
      title: "Failed to Delete",
      message: "Failed to delete meeting note: " + err,
      type: "error"
    });
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

// Only poll while pending server jobs are visible. Processing survives this UI.
setInterval(() => {
  const pane = document.getElementById('tab-meetings');
  if (document.hidden || !pane?.classList.contains('active')) return;
  if ((window.allMeetings || []).some(m => ['queued', 'processing'].includes(m.processing_status))) loadMeetings(true);
}, 10000);

async function appendPendingLocalMeetings() {
  try {
    const local = await invoke('list_local_meetings');
    const grid = document.getElementById('meetings-list'); if (!grid) return;
    grid.querySelectorAll('.local-pending-meeting').forEach(card => card.remove());
    const remote = new Set((window.allMeetings || []).map(meeting => meeting.client_id));
    for (const meeting of local) {
      if (remote.has(meeting.id) || !['recorded','uploading','failed','processing_failed'].includes(meeting.status)) continue;
      document.getElementById('meetings-empty-msg')?.classList.add('hidden'); grid.classList.remove('hidden'); grid.style.display='grid';
      const card=document.createElement('div'); card.className='meeting-card local-pending-meeting';
      const title=document.createElement('div'); title.className='meeting-card-title'; title.textContent=meeting.title || 'Local recording'; card.appendChild(title);
      const status=document.createElement('div'); status.textContent=meeting.status === 'uploading' ? 'Uploading audio' : 'Audio saved locally. Upload pending.'; card.appendChild(status);
      if (meeting.error) {const error=document.createElement('div'); error.textContent=meeting.error; card.appendChild(error);}
      const retry=document.createElement('button'); retry.type='button'; retry.className='fluent-button'; retry.textContent='Retry upload'; retry.disabled=meeting.status==='uploading';
      retry.addEventListener('click',async()=>{retry.disabled=true;try{await invoke('retry_upload_meeting',{filepath:meeting.filepath});await loadMeetings(true);}catch(error){showToast(String(error),'Upload pending','error');retry.disabled=false;}});
      card.appendChild(retry); grid.appendChild(card);
    }
  } catch(error) {console.warn('Cannot list pending local meeting audio',error);}
}
