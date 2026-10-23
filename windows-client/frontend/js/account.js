// Sign-in, account usage, cloud storage and credentials forms.

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

function updateCommandUsage(userInfo) {
  const usage = userInfo?.command_usage;
  const label = document.getElementById('command-remaining');
  if (!label) return;
  const meter = document.getElementById('command-usage-meter');
  const reset = document.getElementById('command-reset');
  meter.hidden = true;
  reset.textContent = '';
  label.removeAttribute('aria-label');
  label.classList.remove('is-message');
  if (!usage) { label.classList.add('is-message'); label.textContent = 'Usage unavailable'; reset.textContent = 'Refresh your account to try again.'; return; }
  if (!usage.enabled) { label.classList.add('is-message'); label.textContent = 'Pro feature'; reset.textContent = 'Available with an active Pro subscription.'; return; }
  const used = Math.max(0, Math.min(100, 100 - usage.remaining_percent));
  const percentUsed = percent => percent > 0 && percent < 1 ? '<1% used' : `${Math.floor(percent)}% used`;
  const exhausted = usage.input_remaining <= 0 || usage.output_remaining <= 0;
  label.textContent = percentUsed(used);
  label.setAttribute('aria-label', `${used > 0 && used < 1 ? 'Less than one' : Math.floor(used)} percent of monthly Command Mode usage limit used`);
  meter.hidden = false;
  meter.value = used;
  reset.textContent = exhausted
    ? `${usage.input_remaining <= 0 ? 'Input' : 'Output'} allowance reached. ${usage.renewal_pending ? 'Awaiting renewal confirmation.' : `Resets ${formatDate(usage.resets_at)}.`}`
    : usage.renewal_pending ? 'Awaiting renewal confirmation' : `Resets ${formatDate(usage.resets_at)}`;
  reset.title = usage.renewal_pending ? 'Your current billing period has ended. Your allowance refreshes when subscription renewal is confirmed.' : '';

}

let accountStorageGeneration = 0;
function formatStorageBytes(bytes) {
  if (bytes === 0) return '0 B';
  const units = [[1e9, 'GB'], [1e6, 'MB'], [1e3, 'KB'], [1, 'B']];
  const [size, unit] = units.find(([size]) => bytes >= size) || units[units.length - 1];
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(bytes / size)} ${unit}`;
}
async function loadAccountStorage(userInfo = currentUserInfo) {
  const generation = ++accountStorageGeneration;
  const owner = userInfo?.id;
  const remaining = document.getElementById('account-storage-remaining');
  const detail = document.getElementById('account-storage-detail');
  const note = document.getElementById('account-storage-note');
  const meter = document.getElementById('account-storage-meter');
  const refresh = document.getElementById('account-storage-refresh');
  remaining.textContent = userInfo ? 'Loading storage…' : 'Sign in to see your storage';
  detail.textContent = '';
  note.textContent = 'Local recordings do not count toward your cloud quota.';
  meter.hidden = true;
  meter.value = 0;
  refresh.disabled = true;
  if (!userInfo) return;
  try {
    const usage = await invoke('cloud_storage_usage');
    if (generation !== accountStorageGeneration || owner !== currentUserInfo?.id) return;
    const { limit_bytes: limit, used_bytes: used, reserved_bytes: reserved } = usage || {};
    if (![limit, used, reserved].every(n => Number.isSafeInteger(n) && n >= 0) || limit <= 0) throw new Error('Invalid storage usage');
    const available = Math.max(0, limit - used - reserved);
    remaining.textContent = `${formatStorageBytes(available)} of ${formatStorageBytes(limit)} remaining`;
    detail.textContent = `${formatStorageBytes(used)} stored${reserved ? ` · ${formatStorageBytes(reserved)} reserved for uploads` : ''}`;
    meter.value = Math.min(100, (used + reserved) / limit * 100);
    meter.hidden = false;
    const notes = ['Encrypted meeting audio. Local recordings do not count toward this quota.'];
    if (!usage.enabled) notes.push('Cloud audio backup is currently unavailable.');
    else if (!isUserPro(userInfo)) notes.push('An active Pro plan is required for new cloud backups.');
    if (usage.purge_after) {
      const date = new Date(usage.purge_after * 1000);
      if (!Number.isNaN(date.getTime())) notes.push(`Cloud audio will be deleted on ${date.toLocaleDateString()} unless your paid plan is renewed.`);
    }
    note.textContent = notes.join(' ');
  } catch {
    if (generation !== accountStorageGeneration || owner !== currentUserInfo?.id) return;
    remaining.textContent = 'Storage usage unavailable';
    detail.textContent = 'Refresh to try again.';
  } finally {
    if (generation === accountStorageGeneration) refresh.disabled = false;
  }
}

function updateAuthUI(userInfo) {
  const owner=userInfo?.id||null;
  if (owner!==displayedHistoryOwner) {
    displayedHistoryOwner=owner;historyGeneration++;historyLoading=false;historyPreviewObserver?.disconnect();meetingDetailGeneration++;resetMeetingAudio();
    const refresh=document.getElementById('transcription-history-refresh');if(refresh)refresh.disabled=false;
    document.getElementById('transcription-history-list')?.replaceChildren();
    historyCursor=null;
  }

  updateCommandUsage(userInfo);
  loadAccountStorage(userInfo);
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
    
    const subUsagePercent = document.getElementById('sub-usage-percent');
    
    if (subUsagePercent) {
      const usage = formatUsageParts(usageSeconds, maxSeconds, isPaid);
      if (usage.isUnlimited) {
        subUsagePercent.innerHTML = `${usage.totalStr}
          <button type="button" class="info-bubble" title="${usage.fupTooltip}" aria-label="${usage.fupTooltip}">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
              <circle cx="12" cy="12" r="9"></circle>
              <path d="M12 11v6M12 7h.01"></path>
            </svg>
          </button>`;
      } else {
        subUsagePercent.textContent = usage.totalStr;
      }
    }

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
    updateWizardNavigation();
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
  try { await invoke('start_login'); } catch { triggerCancelLogin(isOnboarding);showToast('Could not open sign in. Please try again.', 'Sign in', 'error'); }
}

function triggerCancelLogin(isOnboarding) {
  invoke('cancel_login').catch(()=>{});
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
      currentConfig.signed_in = false;
    }
    currentUserInfo = null;
    updateAuthUI(null);
    updateOnboardAuthUI(null);
  } catch (err) {
    console.error("Failed to logout:");
  }
}

async function setupOAuthListeners() {
  // Listen for login-success emitted by Rust thread
  unlistenLoginSuccess = await listen('login-success', (event) => {
    const userInfo = event.payload;
    currentUserInfo = userInfo;
    
    // Reload the session before enabling progression.
    invoke('get_config').then(async cfg => { currentConfig=cfg;updateWizardNavigation(); if((cfg.onboarding_version??0)>=1) {onboardingWrapper.classList.add('hidden');document.getElementById('app').inert=false;} });

    updateAuthUI(userInfo);
    updateOnboardAuthUI(userInfo);
  });

  // Listen for login-failed emitted by Rust thread
  unlistenLoginFailed = await listen('login-failed', (event) => {
    console.error("Login failed:");
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
        
        // Refresh settings so the signed-in state is current
        currentConfig = await invoke('get_config');
        
        updateAuthUI(userInfo);
        updateOnboardAuthUI(userInfo);
        updateWizardNavigation();
        if((currentConfig.onboarding_version??0)>=1) {onboardingWrapper.classList.add('hidden');document.getElementById('app').inert=false;}

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
