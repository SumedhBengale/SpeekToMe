console.log('[SPEEKTOME] Content script loaded. Hotkey listener is active.');

// Content scripts are deliberately not hotkey listeners. Chrome commands are
// delivered directly to background.js, which also works when this script is
// unavailable (for example on chrome:// pages and the Chrome Web Store).
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "insert_transcription") {
    sendResponse(insertText(message.text));
    return false;
  }

  if (message.action === 'dictation_status') {
    setDictationPill(message.state, message.detail);
    sendResponse({ success: true });
    return false;
  }

  return false;
});

function insertText(text) {
  const activeEl = document.activeElement;
  if (!activeEl) {
    console.warn("[SPEEKTOME] No active elements focused on the page.");
    return { success: false, error: 'Focus a text field before dictating.' };
  }

  const isInput = activeEl.tagName === "INPUT" || activeEl.tagName === "TEXTAREA";
  const isContentEditable = activeEl.isContentEditable || activeEl.getAttribute("contenteditable") === "true";

  if (!isInput && !isContentEditable) {
    console.warn("[SPEEKTOME] Focused element is not a text entry field.");
    return { success: false, error: 'Focus a text field before dictating.' };
  }

  pasteText(activeEl, text, isInput);
  return { success: true };
}

function pasteText(el, text, isInput) {
  if (isInput) {
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const val = el.value;
    el.value = val.substring(0, start) + text + val.substring(end);
    el.selectionStart = el.selectionEnd = start + text.length;
    // Dispatch input/change events so frontend frameworks (React, Vue, Angular) register changes
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  } else {
    const selection = window.getSelection();
    if (selection.rangeCount > 0) {
      const range = selection.getRangeAt(0);
      range.deleteContents();
      const textNode = document.createTextNode(text);
      range.insertNode(textNode);
      range.setStartAfter(textNode);
      range.setEndAfter(textNode);
      selection.removeAllRanges();
      selection.addRange(range);
    } else {
      el.textContent += text;
    }
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }
}

let pillHideTimer = null;

function setDictationPill(state, detail = '') {
  let pill = document.getElementById('speektome-dictation-pill');
  if (!pill) {
    pill = document.createElement('div');
    pill.id = 'speektome-dictation-pill';
    pill.setAttribute('role', 'status');
    pill.style.cssText = [
      'position:fixed', 'z-index:2147483647', 'left:50%', 'bottom:28px',
      'transform:translateX(-50%)', 'display:none', 'align-items:center',
      'gap:9px', 'padding:10px 16px', 'border-radius:999px',
      'font:600 13px/1.2 system-ui,sans-serif', 'color:#fff',
      'background:#40101d', 'box-shadow:0 6px 24px rgba(0,0,0,.28)',
      'pointer-events:none'
    ].join(';');
    document.documentElement.appendChild(pill);
  }

  const colors = {
    recording: '#b3261e',
    processing: '#40101d',
    error: '#7a1f1f',
    complete: '#155724',
    idle: '#40101d'
  };
  pill.style.background = colors[state] || colors.processing;
  pill.textContent = detail || (
    state === 'recording' ? 'Listening…'
      : state === 'complete' ? 'Dictation complete'
        : state === 'idle' ? 'No speech detected'
          : 'Processing…'
  );
  pill.style.display = 'flex';

  if (pillHideTimer) clearTimeout(pillHideTimer);
  if (state !== 'recording') {
    pillHideTimer = setTimeout(() => {
      pill.style.display = 'none';
    }, state === 'error' ? 2400 : 1400);
  }
}
