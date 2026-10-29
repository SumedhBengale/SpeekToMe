// Settings form: custom selects, themes, hotkeys, saving.

function renderSelectLabel(select, label) {
  const option = select.options[select.selectedIndex];
  if (select.id !== 'hotkey-select') { label.textContent = option?.textContent || ''; return; }
  label.replaceChildren();
  label.classList.add('shortcut-keycaps');
  displayHotkey(select.value).split(' + ').forEach((key, index) => {
    if (index) { const plus = document.createElement('span'); plus.className = 'shortcut-plus'; plus.textContent = '+'; label.append(plus); }
    const cap = document.createElement('kbd'); cap.textContent = key; label.append(cap);
  });
  const trigger = label.closest('button');
  if (trigger) trigger.setAttribute('aria-label', `Trigger hotkey: ${option?.textContent || displayHotkey(select.value)}`);
}

function refreshCustomSelectMenu(select, wrapper, labelSpan, menu) {
  const trigger = wrapper.querySelector('.custom-select-trigger');
  const signature = JSON.stringify(Array.from(select.options, opt => [opt.value, opt.textContent, opt.hidden, opt.disabled, opt.title]));
  if (menu.dataset.optionsSignature !== signature) {
    menu.replaceChildren();
    Array.from(select.options).filter(opt => !opt.hidden).forEach(opt => {
      const optEl = document.createElement('div');
      optEl.className = 'custom-select-option' + (opt.value === select.value ? ' selected' : '');
      optEl.setAttribute('role', 'option');
      optEl.setAttribute('data-value', opt.value);
      optEl.textContent = opt.textContent;
      optEl.setAttribute('aria-disabled', String(opt.disabled));
      optEl.title = opt.title;
      if (opt.disabled) optEl.classList.add('is-disabled');

      optEl.addEventListener('click', (e) => {
        e.stopPropagation();
        if (opt.disabled) return;
        select.value = opt.value;
        renderSelectLabel(select, labelSpan);

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
  renderSelectLabel(select, labelSpan);
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
      if (!isOpen && select.id.endsWith('hotkey-select')) checkHotkeyPresets(select, true);
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
        renderSelectLabel(select, labelSpan);
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
}

function applyConfigToUI(config) {
  if (!config) return;

  const saveBtn = document.getElementById('save-btn');
  if (saveBtn) {
    saveBtn.style.display = 'none';
  }

  // General settings inputs
  setHotkeyOption(hotkeySelect, normalizeConfiguredHotkey(config.hotkey));
  if (hotkeyModeSelect) hotkeyModeSelect.value = config.hotkey_mode || 'hold';
  if (autostartCheck) autostartCheck.checked = Boolean(config.autostart);
  if (typingModeCheck) typingModeCheck.checked = Boolean(config.typing_mode);
  if (soundFeedbackCheck) soundFeedbackCheck.checked = Boolean(config.sound_feedback);
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
  setHotkeyOption(onboardHotkeySelect, normalizeConfiguredHotkey(config.hotkey));
  if (onboardHotkeyModeSelect) onboardHotkeyModeSelect.value = config.hotkey_mode || 'hold';
  if (onboardAutostartCheck) onboardAutostartCheck.checked = Boolean(config.autostart);
  const commandSetup=document.getElementById('onboard-command-mode-check');if(commandSetup)commandSetup.checked=config.command_mode!==false;
  if (onboardStreamingModeCheck) {
    onboardStreamingModeCheck.checked = (config.transcription_mode === 'stream');
  }
  if (onboardTypingModeCheck) onboardTypingModeCheck.checked = Boolean(config.typing_mode);
  if (onboardSoundFeedbackCheck) onboardSoundFeedbackCheck.checked = Boolean(config.sound_feedback);
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


function setHotkeyOption(select, value) {
  if (!select) return;
  // Only retain the latest custom entry; presets remain available.
  [...select.options].filter(option => option.dataset.custom === 'true').forEach(option => option.remove());
  if (![...select.options].some(option => option.value === value)) {
    const option = new Option(displayHotkey(value) + ' (Custom)', value);
    option.dataset.custom = 'true';
    select.add(option);
  }
  select.value = value;
  syncCustomSelects();
  updateHotkeyWarning(select);
}

// Per-control generations prevent a slow conflict check for an earlier
// selection from replacing the warning for the current one.
const hotkeyWarningRequests = new WeakMap();
async function updateHotkeyWarning(select) {
  if (!select) return;
  const warning = document.getElementById(select.id.replace('hotkey-select', 'hotkey-conflict-warning'));
  if (!warning) return;
  const request = (hotkeyWarningRequests.get(select) || 0) + 1;
  hotkeyWarningRequests.set(select, request);
  const value = select.value;
  try {
    const report = await invoke('check_hotkey_conflicts', { value });
    if (hotkeyWarningRequests.get(select) !== request || select.value !== value) return;
    const conflicts = report.conflicts;
    const selected = select.options[select.selectedIndex];
    if (selected) {
      selected.hidden = conflicts.length > 0;
      selected.disabled = conflicts.length > 0;
      syncCustomSelects();
    }
    warning.classList.toggle('is-blocked', conflicts.length > 0);
    warning.hidden = !conflicts.length && report.system_check !== 'unknown';
    warning.textContent = conflicts.length
      ? `${displayHotkey(value)} cannot be used: reserved for ${conflicts.slice(0, 3).map(c => c.action).join('; ')}. Choose another shortcut.`
      : (report.system_check === 'unknown' ? `${report.detail} Standard shortcut reservations are still enforced.` : '');
  } catch (error) {
    if (hotkeyWarningRequests.get(select) !== request || select.value !== value) return;
    warning.hidden = false;
    warning.textContent = 'Could not check this shortcut. Save & Apply will verify it again before changing your settings.';
  }
}

// Check predefined choices once per settings window, shared by onboarding and
// General. Native save validation always performs a fresh check.
const presetHotkeyChecks = new Map();
async function checkHotkeyPresets(select, refresh = false) {
  await Promise.all([...select.options].filter(option => !option.dataset.custom).map(async option => {
    if (refresh || !presetHotkeyChecks.has(option.value)) {
      presetHotkeyChecks.set(option.value, invoke('check_hotkey_conflicts', { value: option.value }));
    }
    try {
      const report = await presetHotkeyChecks.get(option.value);
      // Keep the backing value for a previously saved shortcut so filtering
      // never silently changes preferences. Omit blocked choices from both menus.
      option.hidden = report.conflicts.length > 0;
      option.disabled = report.conflicts.length > 0;
      option.title = report.conflicts.length ? `Reserved for ${report.conflicts.map(c => c.action).join('; ')}` : '';
    } catch { /* Save validation remains authoritative if discovery failed. */ }
  }));
  syncCustomSelects();
}

let activeHotkeyRecorder = null;
function setupHotkeyRecorder(prefix) {
  const button = document.getElementById(`${prefix}hotkey-record`);
  const select = document.getElementById(`${prefix}hotkey-select`);
  const hint = document.getElementById(`${prefix}hotkey-record-hint`);
  if (!button || !select || !hint) return;
  const defaultHint = hint.textContent;
  select.addEventListener('change', () => {
    updateHotkeyWarning(select);
    hint.textContent = `Selected ${displayHotkey(select.value)}.${prefix ? '' : ' Save & Apply to activate it.'}${IS_LINUX_CLIENT ? ' Your desktop may ask you to approve this shortcut.' : ''}`;
  });
  updateHotkeyWarning(select);
  checkHotkeyPresets(select);
  button.addEventListener('click', async () => {
    if (activeHotkeyRecorder) {
      const same = activeHotkeyRecorder.button === button;
      activeHotkeyRecorder.stop();
      if (same) return;
    }
    const held = new Set();
    let candidate = null;
    let triggerCode = null;
    let stopped = false;
    const recorder = { button, stop };
    function stop(message = defaultHint) {
      if (stopped) return;
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener('keydown', keydown, true);
      document.removeEventListener('keyup', keyup, true);
      window.removeEventListener('blur', blur);
      document.removeEventListener('visibilitychange', visibility);
      button.textContent = prefix === 'onboard-' ? 'Change shortcut' : 'Record shortcut';
      button.classList.remove('is-recording');
      select.disabled = false;
      const trigger = select.parentElement.querySelector('.custom-select-trigger');
      if (trigger) trigger.disabled = false;
      hint.textContent = message;
      hint.classList.remove('has-error');
      if (activeHotkeyRecorder === recorder) activeHotkeyRecorder = null;
      invoke('set_shortcut_capture', { active: false }).catch(console.warn);
    }
    function blur() { stop(); }
    function visibility() { if (document.hidden) stop(); }
    function keydown(event) {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (event.key === 'Escape') { stop(); return; }
      if (event.repeat || event.isComposing) return;
      hint.classList.remove('has-error');
      held.add(event.code);
      const mods = [];
      if (event.ctrlKey) mods.push('Ctrl');
      if (event.shiftKey) mods.push('Shift');
      if (event.altKey) mods.push('Alt');
      if (event.metaKey) mods.push('Win');
      if (['Control', 'Shift', 'Alt', 'Meta', 'AltGraph'].includes(event.key)) {
        if (!candidate || !candidate.hasKey) {
          candidate = { value: event.code === 'AltRight' && (mods.length <= 1 || event.getModifierState('AltGraph')) ? 'Right Alt' : mods.join(' + '), hasKey: false };
        }
      } else {
        if (triggerCode && triggerCode !== event.code && held.has(triggerCode)) {
          stop('Use one key with modifiers. Click Record shortcut to try again.');
          return;
        }
        triggerCode = event.code;
        if (event.code.startsWith('Numpad')) {
          candidate = null;
          hint.textContent = 'Use the main keyboard rather than the number pad. Esc cancels.';
          return;
        }
        const digit = /^Digit([0-9])$/.exec(event.code);
        const key = digit ? digit[1] : (event.key === ' ' ? 'Space' : event.key);
        candidate = { value: [...mods, key].join(' + '), hasKey: true };
      }
      hint.textContent = candidate?.value ? `Release to use ${displayHotkey(candidate.value)}. Esc cancels.` : 'Press your shortcut. Esc cancels.';
    }
    async function keyup(event) {
      event.preventDefault();
      event.stopImmediatePropagation();
      held.delete(event.code);
      if (held.size || event.ctrlKey || event.shiftKey || event.altKey || event.metaKey || !candidate) return;
      const value = candidate.value;
      // Native validation is the authority for both capture and persisted settings.
      try {
        const canonical = await invoke('validate_hotkey', { value });
        if (stopped) return;
        stop(`Selected ${displayHotkey(canonical)}.${prefix ? '' : ' Save & Apply to activate it.'}${IS_LINUX_CLIENT ? ' Your desktop may ask you to approve this shortcut.' : ''}`);
        setHotkeyOption(select, canonical);
        select.dispatchEvent(new Event('change', { bubbles: true }));
      } catch (error) {
        if (!stopped) {
          candidate = null;
          hint.classList.add('has-error');
          hint.textContent = `${error} Try another shortcut, or press Esc to cancel.`;
        }
      }
    }
    activeHotkeyRecorder = recorder;
    button.textContent = 'Cancel';
    button.classList.add('is-recording');
    select.disabled = true;
    const trigger = select.parentElement.querySelector('.custom-select-trigger');
    if (trigger) trigger.disabled = true;
    select.parentElement.classList.remove('open');
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
    hint.textContent = 'Press your shortcut, then release all keys. Esc cancels.';
    const timer = setTimeout(() => stop('Shortcut recording timed out. Click Record shortcut to try again.'), 25000);
    try {
      await invoke('set_shortcut_capture', { active: true });
      if (stopped) {
        await invoke('set_shortcut_capture', { active: false });
        return;
      }
      document.addEventListener('keydown', keydown, true);
      document.addEventListener('keyup', keyup, true);
      window.addEventListener('blur', blur);
      document.addEventListener('visibilitychange', visibility);
    } catch (error) { stop(`Could not record a shortcut: ${error}`); }
  });
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
  const prefix = isOnboarding ? 'onboard-' : '';
  const value = (id, fallback) => document.getElementById(prefix + id)?.value ?? fallback;
  const checked = (id, fallback) => document.getElementById(prefix + id)?.checked ?? fallback;
  const saved = currentConfig || {};
  const themeName = value('theme-select', saved.theme ?? 'forest');
  const colors = isOnboarding ? {bg: saved.pill_bg_color, wave: saved.pill_wave_color} : getPillColorsForTheme(themeName);
  // Both screens edit the same schema. Retain fields without a control here,
  // including server/session state and existing overlay settings in onboarding.
  const config = {
    ...saved,
    languages: [...selectedLanguages],
    active_language: speakingLanguage,
    api_url: saved.api_url ?? null,
    session_token: null, // Held by the desktop process, never by the webview.
    hotkey: value('hotkey-select', DEFAULT_HOTKEY),
    hotkey_mode: value('hotkey-mode-select', 'hold'),
    autostart: checked('autostart-check', false),
    typing_mode: checked('typing-mode-check', false),
    sound_feedback: checked('sound-feedback-check', saved.sound_feedback ?? true),
    transcription_mode: checked('streaming-mode-check', false) ? 'stream' : 'batch',
    command_mode: checked('command-mode-check', saved.command_mode !== false),
    custom_prompt: saved.custom_prompt ?? '',
    theme: themeName,
    pill_position_mode: saved.pill_position_mode ?? 'predefined',
    pill_predefined_position: saved.pill_predefined_position ?? 'Top Center',
    pill_custom_x: saved.pill_custom_x ?? 0,
    pill_custom_y: saved.pill_custom_y ?? 60,
    pill_bg_color: colors.bg,
    pill_wave_color: colors.wave
  };
  if (!isOnboarding) {
    const coordinate = (id, fallback) => {
      const number = Number.parseInt(value(id, ''), 10);
      return Number.isFinite(number) ? number : fallback;
    };
    config.pill_position_mode = value('pill-pos-mode-select', config.pill_position_mode);
    config.pill_predefined_position = value('pill-predefined-select', config.pill_predefined_position);
    config.pill_custom_x = coordinate('pill-custom-x', config.pill_custom_x);
    config.pill_custom_y = coordinate('pill-custom-y', config.pill_custom_y);
  }
  return config;
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
    console.error("Failed to save config:");
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
    console.error("Failed to refresh user data:");
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
    console.error("Failed to hide window:");
  }
}

window.saveAndApply = saveAndApply;
window.hideWindow = hideWindow;
window.refreshUserData = refreshUserData;
window.markDirty = markDirty;

// ====================================================
// Onboarding Wizard Progression
// ==========================================
