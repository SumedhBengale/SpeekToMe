// First-run onboarding wizard and Pro promotion.

let wizardSaveQueue = Promise.resolve();
let wizardCapabilities = null;
let wizardMicrophoneReady = false;
let wizardPracticeSkipped = false;
let wizardCapabilityTimer = null;
const WIZARD_STEPS = 11;
function wizardStatus(message) {
  const el = document.getElementById('onboard-save-status');
  el.textContent = message;
  el.closest('.setup-footer').hidden = currentStep <= 2 && !message;
}
function wizardIsSignedIn() { return Boolean(currentUserInfo && currentConfig?.signed_in); }
function wizardAccessReady() { return Boolean(wizardCapabilities?.shortcut && wizardCapabilities?.insertion && wizardCapabilities?.microphone !== false && wizardMicrophoneReady); }
function saveWizardConfig(extra = {}) {
  const save = wizardSaveQueue.then(async () => {
    const latest = await invoke('get_config');
    currentConfig = { ...currentConfig, ...latest };
    const updated = { ...gatherConfigFromUI(true), ...extra };
    await invoke('save_config', { config: updated });
    currentConfig = updated;
    wizardStatus('');
    return updated;
  });
  wizardSaveQueue = save.catch(() => {});
  return save;
}
function wizardCanAdvance() {
  if (!wizardIsSignedIn()) return false;
  if (currentStep === 3) return wizardAccessReady();
  if (currentStep === 10) return wizardPracticeSkipped || Boolean(document.getElementById('onboard-practice-text')?.value.trim());
  return true;
}
function updateWizardNavigation() {
  const next = document.getElementById('onboard-continue-btn');
  next.hidden = currentStep <= 2 || currentStep === 11;
  next.closest('.setup-footer').hidden = currentStep <= 2 && !document.getElementById('onboard-save-status').textContent;
  next.disabled = !wizardCanAdvance();
  document.getElementById('onboard-back-btn').hidden = currentStep <= 2;
  document.getElementById('onboard-finish-btn').hidden = currentStep !== 11;
  if (onboardNextStepBtn) onboardNextStepBtn.hidden = true;
  if (onboardPrevStepBtn) {
    onboardPrevStepBtn.hidden = currentStep >= 3;
    onboardPrevStepBtn.disabled = currentStep <= 1;
  }
  document.getElementById('onboard-step-label').textContent = `Step ${currentStep} of ${WIZARD_STEPS}`;
  document.getElementById('onboard-progress').value = currentStep;
}
function wizardPermissionIcon(kind) {
  const paths = {
    shortcut: '<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M7 9h.01M11 9h.01M15 9h.01M7 12h.01M11 12h.01M15 12h.01M7 15h10"/>',
    insertion: '<path d="M5 5h14M12 5v14M8 19h8"/>',
    microphone: '<rect x="9" y="2" width="6" height="13" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M9 22h6"/>'
  };
  const icon = document.createElement('span');
  icon.className = 'setup-icon-badge';
  icon.setAttribute('aria-hidden', 'true');
  icon.innerHTML = `<svg class="setup-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${paths[kind]}</svg>`;
  return icon;
}
async function refreshWizardAccess() {
  try {
    wizardCapabilities=await invoke('get_setup_capabilities');
    const list=document.getElementById('onboard-access-status'); list.replaceChildren();
    const items = [
      ['shortcut', 'Global shortcut', 'Start dictation from any app.'],
      ['insertion', 'Text insertion', 'Put your words into the active text field.'],
      ['microphone', 'Microphone', 'Capture your voice for transcription.']
    ];
    for (const [key, label, description] of items) {
      const ready = key === 'microphone'
        ? wizardMicrophoneReady && wizardCapabilities?.microphone !== false
        : Boolean(wizardCapabilities?.[key]);
      const row = document.createElement('div');
      row.className = 'setup-check' + (ready ? ' is-ready' : '');
      const copy = document.createElement('div');
      copy.className = 'setup-check-copy';
      const name = document.createElement('strong');
      name.textContent = label;
      const detail = document.createElement('small');
      detail.textContent = description;
      copy.append(name, detail);
      const value = document.createElement('span');
      value.className = 'setup-check-status';
      value.textContent = ready ? '✓ Ready' : key === 'microphone' && wizardCapabilities?.microphone_device && wizardCapabilities?.microphone !== false ? 'Test needed' : 'Needs access';
      row.append(wizardPermissionIcon(key), copy, value);
      list.append(row);
    }
    const request=document.getElementById('onboard-access-request'); request.hidden=!IS_MAC_CLIENT&&!IS_LINUX_CLIENT;
    document.getElementById('onboard-access-repair').hidden=!IS_MAC_CLIENT;
    request.textContent=IS_MAC_CLIENT?'Open permission settings':'Reconnect desktop access';
    document.getElementById('onboard-access-help').textContent=IS_MAC_CLIENT
      ? 'Allow Microphone and Accessibility in System Settings. Input Monitoring is only needed if shortcut listening still requires it. Return here after approval. If a saved grant is stale, use Repair saved permissions below.'
      : IS_LINUX_CLIENT ? 'Approve the desktop shortcut and keyboard-control dialogs. Portal support depends on your desktop. Reconnect to retry; the selected shortcut may also need the packaged keyboard helper.'
      : 'We use your default microphone and native keyboard hooks. If microphone access is blocked, enable desktop-app microphone access in Windows Settings.';
    if(!wizardCapabilities?.microphone_device) { wizardMicrophoneReady=false; document.getElementById('onboard-mic-result').textContent='No default microphone is available. Connect one and check again.'; }
    updateWizardNavigation();
  } catch { wizardCapabilities=null;wizardStatus('Could not check desktop access. Please try again.');updateWizardNavigation(); }
}
function renderWizardEntitlements() {
  const pro=isUserPro(currentUserInfo);
  for(const id of ['onboard-command-mode-check','onboard-streaming-mode-check']) {
    const el=document.getElementById(id);el.disabled=!pro;if(!pro) el.checked=false;
  }
  document.getElementById('onboard-command-help').textContent=pro
    ? 'Optional. In Push to Talk, tap once, then hold to speak a command. In Hands-Free mode, double-tap to start a command, then tap to finish.'
    : 'Command Mode is available with Pro. Your Free account can still dictate text. Continue to keep it off.';
  document.getElementById('onboard-stream-help').textContent=pro
    ? 'Selected by default for Pro during setup. Turn it off if you prefer to receive the completed transcription after recording.'
    : 'Your Free account receives the completed transcription after you stop. This still uses the transcription service and an internet connection.';
}
function updateWizardGesture() {
  const hold = document.getElementById('onboard-hotkey-mode-select').value === 'hold';
  const gesture = hold
    ? 'Hold your shortcut while speaking. Release it to submit.' : 'Tap your shortcut to start. Tap again to submit.';
  const shortcut = displayHotkey(document.getElementById('onboard-hotkey-select').value);
  document.getElementById('onboard-gesture-help').textContent = gesture;
  document.getElementById('onboard-practice-gesture').textContent = gesture;
  document.getElementById('onboard-ready-shortcut').textContent = `Use ${shortcut} in a text field. ${hold ? 'Hold to talk.' : 'Tap to start and stop.'}`;
  for (const radio of document.querySelectorAll('input[name="onboard-gesture"]')) {
    radio.checked = radio.value === (hold ? 'hold' : 'toggle');
    radio.closest('.setup-gesture-card').classList.toggle('is-selected', radio.checked);
  }
  for (const id of ['onboard-shortcut-keys', 'onboard-practice-keys']) {
    const keys = document.getElementById(id);
    keys.replaceChildren();
    for (const [index, key] of shortcut.split(' + ').entries()) {
      if (index) {
        const separator = document.createElement('span');
        separator.textContent = '+';
        separator.setAttribute('aria-hidden', 'true');
        keys.append(separator);
      }
      const cap = document.createElement('kbd');
      cap.textContent = key;
      keys.append(cap);
    }
  }
}
async function initializeWizard() {
  document.getElementById('onboard-back-btn').addEventListener('click', () => goToStep(currentStep - 1));
  for (const radio of document.querySelectorAll('input[name="onboard-gesture"]')) {
    radio.addEventListener('change', () => {
      if (!radio.checked) return;
      const select = document.getElementById('onboard-hotkey-mode-select');
      select.value = radio.value;
      select.dispatchEvent(new Event('change', { bubbles: true }));
      saveWizardConfig().catch(() => wizardStatus('Could not save this choice. Please retry.'));
    });
  }
  document.getElementById('onboard-hotkey-mode-select').addEventListener('change', updateWizardGesture);
  document.getElementById('onboard-hotkey-select').addEventListener('change', updateWizardGesture);
  document.getElementById('onboard-continue-btn').addEventListener('click',()=>goToStep(currentStep+1));
  document.getElementById('onboard-access-recheck').addEventListener('click',refreshWizardAccess);
  document.getElementById('onboard-access-request').addEventListener('click',async()=>{
    try { await invoke('request_desktop_permissions'); await refreshWizardAccess(); } catch { wizardStatus('Desktop access could not be requested. Check your system settings, then retry.'); }
  });
  document.getElementById('onboard-access-repair').addEventListener('click', async event => {
    event.target.disabled = true;
    try {
      await invoke('repair_desktop_permissions');
      wizardStatus('Old keyboard grants cleared. Enable SpeekTo.Me in System Settings, then return here. Microphone approval is preserved.');
      await refreshWizardAccess();
    } catch { wizardStatus('Permission repair failed. Check System Settings and retry.'); }
    finally { event.target.disabled = false; }
  });
  document.getElementById('onboard-mic-test').addEventListener('click',async event=>{
    event.target.disabled=true;wizardMicrophoneReady=false;updateWizardNavigation();
    const result=document.getElementById('onboard-mic-result');result.textContent='Listening locally for three seconds. Say a few words…';
    try { const probe=await invoke('check_setup_microphone');wizardMicrophoneReady=Boolean(probe.ready);result.textContent=probe.ready?`${probe.device}: ${probe.level>2?'sound detected.':'microphone opened, but little sound was detected. Check the input volume.'}`:probe.error||'Microphone check failed.'; }
    catch { result.textContent='Microphone could not be opened. Check device and privacy settings, then retry.'; }
    finally { event.target.disabled=false;await refreshWizardAccess(); }
  });
  document.getElementById('onboard-practice-text').addEventListener('input',()=>{
    const hasText=Boolean(document.getElementById('onboard-practice-text').value.trim());
    document.getElementById('onboard-practice-status').textContent=hasText?'Text received. You can keep practicing or continue.':'Click in the box and use your shortcut to dictate.';
    updateWizardNavigation();
  });
  document.getElementById('onboard-practice-skip').addEventListener('click',()=>{wizardPracticeSkipped=true;goToStep(11);});
  for(const id of ['onboard-command-mode-check','onboard-streaming-mode-check']) document.getElementById(id).addEventListener('change',()=>saveWizardConfig().catch(()=>wizardStatus('Could not save this choice. Please retry.')));
  renderWizardEntitlements();
  const incomplete=(currentConfig?.onboarding_version??0)<1;
  if(wizardIsSignedIn()&&!incomplete) { onboardingWrapper.classList.add('hidden');document.getElementById('app').inert=false;return; }
  onboardingWrapper.classList.remove('hidden');document.getElementById('app').inert=true;
  const saved=Math.max(1,Math.min(WIZARD_STEPS,currentConfig?.onboarding_step||1));
  wizardMicrophoneReady=saved>3;
  await goToStep(wizardIsSignedIn()?Math.max(2,saved):1,true);
}
async function applyWizardProDefaults() {
  if(isUserPro(currentUserInfo) && !currentConfig.onboarding_pro_defaults_applied && (currentConfig.onboarding_version??0)<1) {
    document.getElementById('onboard-streaming-mode-check').checked=true;
    await saveWizardConfig({onboarding_pro_defaults_applied:true});
  }
  renderWizardEntitlements();
}
async function goToStep(step, restoring=false) {
  step=Math.max(1,Math.min(WIZARD_STEPS,step));
  if(step>1&&!wizardIsSignedIn()) {wizardStatus('Sign in before continuing setup.');return;}
  if(!restoring&&step>currentStep&&!wizardCanAdvance()){wizardStatus(currentStep===3?'Complete the access and microphone checks first.':'Complete this step before continuing.');return;}
  try {
    if(step>1) await applyWizardProDefaults();
    if (!restoring) await saveWizardConfig({onboarding_step:step});
  } catch {wizardStatus('Could not save setup. Please try again.');return;}
  currentStep=step;
  onboardSteps.forEach(el=>el.classList.toggle('active',el.id===`onboard-step-${step}`));
  if(wizardCapabilityTimer) {clearInterval(wizardCapabilityTimer);wizardCapabilityTimer=null;}
  if(step===2) await renderOnboardingPromoUI();
  if(step===3) {await refreshWizardAccess();wizardCapabilityTimer=setInterval(refreshWizardAccess,3000);}
  updateWizardGesture();
  document.getElementById('onboard-tray-help').textContent=IS_MAC_CLIENT?'Open SpeekTo.Me in the menu bar at the top of your screen.':'Open SpeekTo.Me in the system tray, including the hidden-icons menu.';
  document.getElementById('onboard-ready-summary').textContent=wizardPracticeSkipped?'Your preferences are saved. Try dictation when you’re ready.':'Your preferences are saved. Dictate wherever you type.';
  updateWizardNavigation();
  const heading=document.querySelector(`#onboard-step-${step} h2`);heading?.setAttribute('tabindex','-1');heading?.focus({preventScroll:true});
  document.querySelector('#onboarding .onboarding-content').scrollTop=0;
}
async function finishOnboarding() {
  if(!wizardIsSignedIn()){await goToStep(1,true);return;}
  await refreshWizardAccess();
  if(!wizardAccessReady()){await goToStep(3,true);wizardStatus('Desktop access has changed. Check it before finishing.');return;}
  try {await saveWizardConfig({onboarding_version:1,onboarding_step:11});applyConfigToUI(currentConfig);onboardingWrapper.classList.add('hidden');document.getElementById('app').inert=false;if(wizardCapabilityTimer)clearInterval(wizardCapabilityTimer);}
  catch {wizardStatus('Setup could not be saved. Your choices are still here; please retry.');}
}

async function renderOnboardingPromoUI() {
  if (isUserPro(currentUserInfo)) {
    if (onboardPromoBadge) onboardPromoBadge.textContent = "✨ PRO PLAN ACTIVE";
    if (onboardPromoTitle) onboardPromoTitle.textContent = "You're on SpeekTo.Me Pro";
    if (onboardPromoSubtitle) onboardPromoSubtitle.textContent = "Your Pro transcription, Command Mode, and meeting features are active.";
    if (onboardPromoCtaText) onboardPromoCtaText.textContent = "Continue Setup ➔";
    if (onboardSkipProBtn) onboardSkipProBtn.style.display = "none";
    return;
  }

  if (onboardSkipProBtn) onboardSkipProBtn.style.display = "block";

  try {
    const res = await apiRequest('GET', '/config/promo');
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
    console.warn("[PROMO] Could not fetch remote promo config, using default:");
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
// Authentication Controller
// ==========================================
