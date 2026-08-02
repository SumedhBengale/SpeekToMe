// Browser-extension API boundary. Configuration and commands are handled by
// the extension service worker through chrome.storage.local and runtime RPC.
window.SpeekToMeExtension = {
  invoke(command, args = {}) {
    return new Promise((resolve, reject) => {
      chrome.runtime.sendMessage({ action: command, ...args }, (response) => {
        if (chrome.runtime.lastError) {
          reject(new Error(chrome.runtime.lastError.message));
        } else if (response?.error) {
          reject(new Error(response.error));
        } else {
          resolve(response);
        }
      });
    });
  },

  listen(eventName, callback) {
    const listener = (message) => {
      if (message?.event === eventName) {
        callback({ payload: message.payload });
      }
    };

    chrome.runtime.onMessage.addListener(listener);
    return Promise.resolve(() => chrome.runtime.onMessage.removeListener(listener));
  }
};
