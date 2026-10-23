// SpeekTo.Me settings window. Plain scripts share one global scope and
// load in the order listed in index.html; only start.js runs the app.
// Tauri bridge, shared state, platform settings, toasts and dialogs.

const { invoke } = window.__TAURI__.core;
const { listen } = window.__TAURI__.event;

// Authenticated API calls go through the desktop process, which adds the
// session token; the webview never sees it. The result mimics fetch's Response.
async function apiRequest(method, path, body = null) {
  const response = await invoke('api_request', { method, path, body });
  return {
    ok: response.status >= 200 && response.status < 300,
    status: response.status,
    text: async () => response.body,
    json: async () => JSON.parse(response.body),
  };
}

// State variables
let currentConfig = null;
let currentUserInfo = null;
let unlistenLoginSuccess = null;
let unlistenLoginFailed = null;
let currentStep = 1;
let API_URL = "https://api.speekto.me";

// The native build identifies its platform; WebView user agents are not authoritative.
let IS_LINUX_CLIENT = false;
let IS_MAC_CLIENT = false;
const DEFAULT_HOTKEY = 'Ctrl + Win';
function displayHotkey(value) {
  return value.replace(/\bWin\b/g, IS_MAC_CLIENT ? 'Command' : (IS_LINUX_CLIENT ? 'Meta' : 'Windows'));
}

async function configureDesktopSettings() {
  const platform = await invoke('get_desktop_platform');
  IS_LINUX_CLIENT = platform === 'linux';
  IS_MAC_CLIENT = platform === 'macos';
  document.documentElement.dataset.platform = platform;
  [document.getElementById('hotkey-select'), document.getElementById('onboard-hotkey-select')].forEach(select => {
    if (!select) return;
    // Stored shortcut names stay identical on every platform.
    [...select.options].forEach(option => {
      const recommended = option.value === DEFAULT_HOTKEY ? ' (Recommended)' : '';
      option.textContent = displayHotkey(option.value) + recommended;
    });
  });

  document.querySelectorAll('[data-typing-hint]').forEach(hint => {
    hint.textContent = IS_LINUX_CLIENT
      ? 'Off: paste the completed text. Characters unavailable on your keyboard layout also use paste to preserve the text.'
      : 'Off: paste the completed text. On: insert text using simulated keystrokes.';
  });
}

let linuxCapabilityStatusPending = false;

async function loadLinuxCapabilityStatus() {
  if (!IS_LINUX_CLIENT && !IS_MAC_CLIENT) return;
  const status = document.getElementById('linux-capability-status');
  if (!status || linuxCapabilityStatusPending) return;
  linuxCapabilityStatusPending = true;
  try {
    const capabilities = await invoke('get_linux_capabilities');
    const state = `${Boolean(capabilities?.shortcut)}:${Boolean(capabilities?.insertion)}:${Boolean(capabilities?.microphone)}:${Boolean(capabilities?.system_audio)}:${Boolean(capabilities?.input_monitoring)}`;
    if (status.dataset.state === state) return;
    status.dataset.state = state;
    status.classList.toggle('has-permission-issue', !capabilities?.shortcut || !capabilities?.insertion || (IS_MAC_CLIENT && !capabilities?.microphone));
    status.hidden = false;
    status.replaceChildren();
    [
      { ready: capabilities?.shortcut, label: 'Shortcut' },
      { ready: capabilities?.insertion, label: 'Text insertion' },
      ...(IS_MAC_CLIENT ? [
        { ready: capabilities?.microphone, label: 'Microphone' },
        { ready: capabilities?.input_monitoring, label: 'Input Monitoring', optional: capabilities?.shortcut }
      ] : [])
    ].forEach(({ ready, label, optional }) => {
      const item = document.createElement('span');
      item.className = 'shortcut-status-item' + (ready || optional ? ' is-ready' : ' needs-permission');
      item.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"></circle>${ready ? '<path d="m8 12 3 3 5-6"></path>' : '<path d="M12 7v6m0 4h.01"></path>'}</svg>`;
      item.append(document.createTextNode(label + (ready ? ' ready' : optional ? ' optional' : ' permission needed')));
      status.appendChild(item);
    });
    if (IS_MAC_CLIENT && (!capabilities?.shortcut || !capabilities?.insertion || !capabilities?.microphone)) {
      const button = document.createElement('button');
      button.type = 'button';
      button.id = 'macos-permissions-button';
      button.className = 'fluent-button';
      button.textContent = 'Grant Desktop Access';
      button.addEventListener('click', () => invoke('request_desktop_permissions'));
      status.appendChild(button);
      const help = document.createElement('span');
      help.className = 'shortcut-permission-help';
      help.textContent = 'Allow SpeekTo.Me in System Settings → Privacy & Security. Already enabled but still blocked? Repair saved permissions, then approve the fresh entry. If Shortcut stays unavailable, quit and reopen the app.';
      status.appendChild(help);
      if (!capabilities?.shortcut || !capabilities?.insertion) {
        const repair = document.createElement('button');
        repair.type = 'button';
        repair.id = 'macos-repair-permissions-button';
        repair.className = 'fluent-button';
        repair.textContent = 'Repair saved permissions';
        repair.title = 'Clear denied keyboard access records for SpeekTo.Me so macOS can ask for fresh approval.';
        repair.addEventListener('click', async () => {
          repair.disabled = true;
          try {
            await invoke('repair_desktop_permissions');
            help.textContent = 'Old denied keyboard permission records cleared. Enable SpeekTo.Me in the macOS permission pane, then return here. Microphone approval is preserved.';
          } catch (error) {
            help.textContent = 'Permission repair failed: ' + String(error);
          } finally {
            repair.disabled = false;
          }
        });
        status.appendChild(repair);
      }
    }
  } catch (error) {
    console.warn('[LINUX] Capability status is unavailable:');
    if (!status.dataset.state) {
      status.hidden = false;
      status.textContent = 'Permission status is unavailable.';
    }
  } finally {
    linuxCapabilityStatusPending = false;
  }
}

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
