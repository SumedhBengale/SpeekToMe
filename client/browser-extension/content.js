// Listen for transcription events from the background worker
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.action === "insert_transcription") {
    const text = message.text;
    const typingMode = message.typingMode;
    insertText(text, typingMode);
    sendResponse({ success: true });
  }
});

function insertText(text, typingMode) {
  const activeEl = document.activeElement;
  if (!activeEl) {
    console.warn("[SPEEKTOME] No active elements focused on the page.");
    return;
  }

  const isInput = activeEl.tagName === "INPUT" || activeEl.tagName === "TEXTAREA";
  const isContentEditable = activeEl.isContentEditable || activeEl.getAttribute("contenteditable") === "true";

  if (!isInput && !isContentEditable) {
    console.warn("[SPEEKTOME] Focused element is not a text entry field.");
    return;
  }

  if (typingMode) {
    simulateTyping(activeEl, text, isInput);
  } else {
    pasteText(activeEl, text, isInput);
  }
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

function simulateTyping(el, text, isInput) {
  let index = 0;
  function typeChar() {
    if (index >= text.length) return;
    const char = text[index];
    index++;

    // Fire keydown
    const keydownEvent = new KeyboardEvent("keydown", { key: char, charCode: char.charCodeAt(0), bubbles: true });
    el.dispatchEvent(keydownEvent);

    if (isInput) {
      const start = el.selectionStart;
      const end = el.selectionEnd;
      const val = el.value;
      el.value = val.substring(0, start) + char + val.substring(end);
      el.selectionStart = el.selectionEnd = start + 1;
      el.dispatchEvent(new Event("input", { bubbles: true }));
    } else {
      const selection = window.getSelection();
      if (selection.rangeCount > 0) {
        const range = selection.getRangeAt(0);
        const textNode = document.createTextNode(char);
        range.insertNode(textNode);
        range.setStartAfter(textNode);
        range.setEndAfter(textNode);
        selection.removeAllRanges();
        selection.addRange(range);
      } else {
        el.textContent += char;
      }
      el.dispatchEvent(new Event("input", { bubbles: true }));
    }

    // Fire keyup
    const keyupEvent = new KeyboardEvent("keyup", { key: char, charCode: char.charCodeAt(0), bubbles: true });
    el.dispatchEvent(keyupEvent);

    // 2ms delay between characters to simulate high-speed typing
    setTimeout(typeChar, 2);
  }
  typeChar();
}
