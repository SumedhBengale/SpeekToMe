// Starts the app once every script has loaded.

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

// Only poll while pending server jobs are visible. Processing survives this UI.
setInterval(() => {
  const pane = document.getElementById('tab-meetings');
  if (document.hidden || !pane?.classList.contains('active')) return;
  if ((window.allMeetings || []).some(m => ['queued', 'processing'].includes(m.processing_status))) loadMeetings(true);
}, 10000);
