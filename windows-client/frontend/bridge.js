// Polyfill window.__TAURI__ if it is missing (i.e. running in a Web Extension or browser context)
if (!window.__TAURI__) {
  console.log("[BRIDGE] Web Extension mode detected. Polyfilling window.__TAURI__...");
  window.__TAURI__ = {
    core: {
      invoke: async (command, args = {}) => {
        return new Promise((resolve, reject) => {
          chrome.runtime.sendMessage({ action: command, ...args }, (response) => {
            if (chrome.runtime.lastError) {
              reject(chrome.runtime.lastError.message);
            } else if (response && response.error) {
              reject(response.error);
            } else {
              resolve(response);
            }
          });
        });
      }
    },
    event: {
      listen: async (eventName, callback) => {
        const listener = (message) => {
          if (message && message.event === eventName) {
            callback({ payload: message.payload });
          }
        };
        chrome.runtime.onMessage.addListener(listener);
        // Return unlisten function
        return () => {
          chrome.runtime.onMessage.removeListener(listener);
        };
      }
    }
  };
} else {
  console.log("[BRIDGE] Tauri native mode detected. Skipping polyfill.");
}
