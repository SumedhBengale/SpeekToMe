// Settings and onboarding DOM references and language state.

// DOM Elements - Navigation & Tab Panes
const tabItems = document.querySelectorAll('.nav-item[data-tab]');
const tabPanes = document.querySelectorAll('.tab-pane');

// Dashboard Input Elements
const hotkeySelect = document.getElementById('hotkey-select');
const hotkeyModeSelect = document.getElementById('hotkey-mode-select');
const autostartCheck = document.getElementById('autostart-check');
const typingModeCheck = document.getElementById('typing-mode-check');
const soundFeedbackCheck = document.getElementById('sound-feedback-check');
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

// Onboarding Inputs
const onboardHotkeySelect = document.getElementById('onboard-hotkey-select');
const onboardHotkeyModeSelect = document.getElementById('onboard-hotkey-mode-select');
const onboardAutostartCheck = document.getElementById('onboard-autostart-check');
const onboardStreamingModeCheck = document.getElementById('onboard-streaming-mode-check');
const onboardTypingModeCheck = document.getElementById('onboard-typing-mode-check');
const onboardSoundFeedbackCheck = document.getElementById('onboard-sound-feedback-check');
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

let languageUpdatePending = false;
