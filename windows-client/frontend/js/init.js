// Application start-up: loads settings, wires every control.

async function init() {
  // Resolve native labels before custom dropdowns copy the option text.
  await configureDesktopSettings();
  initCustomSelects();
  await listen('account-limit-error', ({ payload }) => {
    showToast(payload.message, payload.feature + ' limit', 'warning');
  });
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
      console.error('Failed to update maximize icon:');
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
        console.error('Toggle maximize failed:');
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
        console.error('Toggle maximize failed:');
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

  document.getElementById('account-storage-refresh').addEventListener('click', () => loadAccountStorage());

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
        }).catch(err => console.warn("Failed to refresh user info on account tab:"));
      } else if (tabId === 'history') {
        loadTranscriptionHistory();
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

  setupHotkeyRecorder('');
  setupHotkeyRecorder('onboard-');

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
      console.log("[INIT] API_URL loaded:");
    } catch (err) {
      console.error("[INIT] Failed to load API URL from backend:");
    }

    try {
      const dynamicLangs = await invoke('get_supported_languages');
      if (dynamicLangs && dynamicLangs.length > 0) {
        SUPPORTED_LANGUAGES = dynamicLangs;
        console.log("[diagnostic event: private values omitted]");
      }
    } catch (err) {
      console.warn("[INIT] Could not load dynamic languages list from backend, using defaults:");
    }

    setupLanguageCombobox('client-lang-search-input', 'client-lang-dropdown-list', 'client-add-lang-btn');
    setupLanguageCombobox('onboard-lang-search-input', 'onboard-lang-dropdown-list', 'onboard-add-lang-btn');

    // 2. Fetch and apply configuration
    currentConfig = await invoke('get_config');
    console.log("[INIT] Config loaded:");
    applyConfigToUI(currentConfig);
    loadLinuxCapabilityStatus();
    if (IS_LINUX_CLIENT || IS_MAC_CLIENT) setInterval(loadLinuxCapabilityStatus, 3000);

    // 3. Fetch user information status & update dashboard UI
    const userInfo = await invoke('get_user_info');
    console.log("[INIT] UserInfo loaded:");
    currentUserInfo = userInfo;
    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);

    // 3.55. Listen for Session Expiration Event from Backend
    try {
      listen('auth-session-expired', () => {
        console.warn("[AUTH] Session expired signal received from backend.");
        currentUserInfo = null;
        if (currentConfig) {
          currentConfig.signed_in = false;
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
      console.error("Failed to bind auth-session-expired listener:");
    }

    // 3.6. Bind Open Web Dashboard Sidebar Button & Manage Account Button
    const openWebDashboardNavBtn = document.getElementById('open-web-dashboard-nav-btn');
    if (openWebDashboardNavBtn) {
      openWebDashboardNavBtn.addEventListener('click', async (e) => {
        e.preventDefault();
        try {
          await invoke('open_external_url', { url: "https://speekto.me/dashboard" });
        } catch (err) {
          console.error("Failed to open web dashboard:");
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
          console.error("Failed to open web dashboard:");
          window.open("https://speekto.me/dashboard", "_blank");
        }
      });
    }

    // 3.8. Theme preview listeners
    if (themeSelect) {
      themeSelect.addEventListener('change', (e) => {
        console.log("[THEME] themeSelect changed to:");
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
        console.log("[THEME] onboardThemeSelect changed to:");
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

    await initializeWizard();
    window.addEventListener('focus', async () => {
      if(onboardingWrapper.classList.contains('hidden')) return;
      try {const user=await invoke('get_user_info');if(user){currentUserInfo=user;currentConfig=await invoke('get_config');updateAuthUI(user);updateOnboardAuthUI(user);await applyWizardProDefaults();}if(currentStep===3)await refreshWizardAccess();updateWizardNavigation();}catch{wizardStatus('Account status could not refresh. Your choices are saved.');}
    });

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
        if (currentStep < WIZARD_STEPS) {
          goToStep(currentStep + 1);
        }
      });
    }

    // Auto-sync onboarding preferences to Rust immediately on change
    const syncOnboardingConfig = () => saveWizardConfig().catch(() => wizardStatus('Could not save this choice. Please retry.'));

    if (onboardHotkeySelect) onboardHotkeySelect.addEventListener('change', syncOnboardingConfig);
    if (onboardHotkeyModeSelect) onboardHotkeyModeSelect.addEventListener('change', syncOnboardingConfig);
    if (onboardThemeSelect) onboardThemeSelect.addEventListener('change', (e) => {
      syncOnboardingConfig();
      document.body.className = `theme-${e.target.value}`;
    });
    if (onboardAutostartCheck) onboardAutostartCheck.addEventListener('change', syncOnboardingConfig);
    if (onboardTypingModeCheck) onboardTypingModeCheck.addEventListener('change', syncOnboardingConfig);
    if (onboardSoundFeedbackCheck) onboardSoundFeedbackCheck.addEventListener('change', syncOnboardingConfig);



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
          console.error("Failed to add vocabulary term:");
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
        console.log("[MEETING RESTORE] Active meeting found during init:");
        rehydrateActiveMeeting(activeMeeting);
      }
    } catch (meetingErr) {
      console.warn("[MEETING RESTORE] Failed to check for active meeting during init:");
    }

    // Initialize Custom Themed Dropdowns
    initCustomSelects();
    console.log("[INIT] SpeekTo.Me Frontend initialization complete!");
  } catch (err) {
    console.error("Failed to initialize SpeekTo.Me UI:");
  }
}
