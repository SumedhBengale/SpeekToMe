// Base URL of the SpeekTo.Me API. Local development talks to a local server;
// production uses VITE_API_URL or, without it, the site's own origin.
export const API_BASE = import.meta.env.VITE_API_URL || (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1'
  ? 'http://localhost:8000'
  : '');
