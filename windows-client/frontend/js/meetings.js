// Meetings: recording controls, live transcript, history and detail views.

let recordingInterval = null;
let timerInterval = null;
let meetingSeconds = 0;
let currentMeetingFile = null;
let activeMeetingId = null;
let meetingDetailGeneration=0;
function resetMeetingAudio(){
 const player=document.getElementById("meeting-audio-player");if(!player)return;
 player.pause();if(player.src?.startsWith("blob:"))URL.revokeObjectURL(player.src);player.removeAttribute("src");player.load();
}
let liveMeetingTurnsMap = new Map();

function setupMeetingsUI() {
  const startRecBtn = document.getElementById('meetings-start-rec-btn');
  const stopRecBtn = document.getElementById('meetings-stop-rec-btn');
  const cancelRecBtn = document.getElementById('meetings-cancel-rec-btn');
  const backBtn = document.getElementById('meetings-back-btn');
  const deleteBtn = document.getElementById('meeting-delete-btn');
  const searchInput = document.getElementById('meetings-search-input');
  
  if (startRecBtn) startRecBtn.addEventListener('click', () => openMeetingSetup());
  setupMeetingSetupPage();
  if (stopRecBtn) stopRecBtn.addEventListener('click', stopMeetingRecording);
  if (cancelRecBtn) cancelRecBtn.addEventListener('click', cancelMeetingRecording);
  if (backBtn) backBtn.addEventListener('click', () => {
    const player = document.getElementById('meeting-audio-player');
    player.pause();
    player.src = '';
    showDashboardView();
    loadMeetings();
  });
  if (deleteBtn) deleteBtn.addEventListener('click', confirmDeleteMeeting);
  
  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      const query = e.target.value.toLowerCase();
      if (!window.allMeetings) return;
      const filtered = window.allMeetings.filter(m => 
        m.title.toLowerCase().includes(query) || 
        (m.transcript && m.transcript.toLowerCase().includes(query)) ||
        (m.summary && m.summary.toLowerCase().includes(query))
      );
      renderMeetingsList(filtered);
    });
  }

  // Close meeting card dropdowns when clicking outside
  document.addEventListener('click', (e) => {
    if (!e.target.closest('.meeting-card-menu-wrapper') && !e.target.closest('.meeting-detail-menu-wrapper')) {
      document.querySelectorAll('.meeting-card-dropdown').forEach(d => d.classList.add('hidden'));
    }
  });

  // Setup real-time meeting events from Tauri Rust backend
  if (window.__TAURI__ && window.__TAURI__.event) {
    listen('meeting-turn-update', (event) => {
      handleLiveMeetingTurn(event.payload);
    });
    listen('meeting-session-ready', () => {
      const badge = document.getElementById('meetings-live-status-badge');
      if (badge) {
        badge.textContent = "Live transcript on";
        badge.style.color = "#57bf7c";
      }
    });
    listen('meeting-recording-stopped', async () => {
      clearInterval(timerInterval);
      clearInterval(recordingInterval);
      currentMeetingFile = null;
      showDashboardView();
      await loadMeetings(true);
    });
    listen('meeting-levels', ({ payload }) => {
      const [mic, loopback] = payload;
      // Perceptual dynamic scaling (0-1000 RMS -> 0-100% meter bar)
      const percent = level => level > 0 ? Math.min(100, Math.max(3, Math.round(Math.pow(level / 250, 0.5) * 100))) : 0;
      for (const id of ['mic-level-fill', 'setup-mic-level-fill']) {
        const fill = document.getElementById(id);
        if (fill) fill.style.width = `${percent(mic)}%`;
      }
      for (const id of ['loopback-level-fill', 'setup-loopback-level-fill']) {
        const fill = document.getElementById(id);
        if (fill) fill.style.width = `${percent(loopback)}%`;
      }
      if (testSoundPeak !== null) testSoundPeak = Math.max(testSoundPeak, loopback);
    });
    listen('meeting-error', (event) => {
      const badge = document.getElementById('meetings-live-status-badge');
      if (badge) {
        // The recording continues locally and is fully transcribed afterwards.
        badge.textContent = "Recording; live transcript paused";
        badge.style.color = "#ff453a";
      }
    });
    listen('open-meeting-view', async () => {
      console.log("[MEETING EVENT] open-meeting-view received from backend");
      try {
        const activeMeeting = await invoke('get_active_meeting');
        if (activeMeeting && activeMeeting.is_recording) {
          rehydrateActiveMeeting(activeMeeting);
        } else {
          tabItems.forEach(nav => nav.classList.remove('active'));
          tabPanes.forEach(pane => pane.classList.remove('active'));
          const meetingsTabNav = document.querySelector('.nav-item[data-tab="meetings"]');
          const meetingsTabPane = document.getElementById('tab-meetings');
          if (meetingsTabNav) meetingsTabNav.classList.add('active');
          if (meetingsTabPane) meetingsTabPane.classList.add('active');
          showDashboardView();
          loadMeetings();
        }
      } catch (err) {
        console.warn("[MEETING EVENT] open-meeting-view handling failed:");
      }
    });
  }
}

function showDashboardView() {
  leaveMeetingSetup();
  meetingDetailGeneration++;resetMeetingAudio();
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.remove('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showRecordingView() {
  leaveMeetingSetup();
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.remove('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showSetupView() {
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
  document.getElementById('meetings-setup-view').classList.remove('hidden');
}

function showDetailView() {
  leaveMeetingSetup();
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.add('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.remove('hidden');
  document.getElementById('meetings-processing-view').classList.add('hidden');
}

function showProcessingView() {
  leaveMeetingSetup();
  const mainHdr = document.getElementById('meetings-main-header');
  if (mainHdr) mainHdr.classList.remove('hidden');
  document.getElementById('meetings-dashboard-view').classList.add('hidden');
  document.getElementById('meetings-recording-view').classList.add('hidden');
  document.getElementById('meetings-detail-view').classList.add('hidden');
  document.getElementById('meetings-processing-view').classList.remove('hidden');
}

function showMeetingsLoading() {
  const loadingEl = document.getElementById('meetings-loading-state');
  const emptyEl = document.getElementById('meetings-empty-msg');
  const listEl = document.getElementById('meetings-list');

  if (loadingEl) {
    loadingEl.classList.remove('hidden');
    loadingEl.style.display = 'flex';
  }
  if (emptyEl) {
    emptyEl.classList.add('hidden');
    emptyEl.style.display = 'none';
  }
  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
}

function hideMeetingsLoading() {
  const loadingEl = document.getElementById('meetings-loading-state');
  if (loadingEl) {
    loadingEl.classList.add('hidden');
    loadingEl.style.display = 'none';
  }
}

function showMeetingsEmpty(message) {
  hideMeetingsLoading();
  const emptyEl = document.getElementById('meetings-empty-msg');
  const emptyText = document.getElementById('meetings-empty-text');
  const listEl = document.getElementById('meetings-list');

  if (listEl) {
    listEl.classList.add('hidden');
    listEl.style.display = 'none';
    listEl.innerHTML = '';
  }
  if (emptyEl) {
    if (emptyText && message) {
      emptyText.textContent = message;
    } else if (message) {
      emptyEl.textContent = message;
    }
    emptyEl.classList.remove('hidden');
    emptyEl.style.display = 'flex';
  }
}

let currentMeetingsReqId = 0;

async function loadMeetings(quiet = false) {
  const listGrid = document.getElementById('meetings-list');
  if (!listGrid) return;

  const reqId = ++currentMeetingsReqId;
  invoke('cloud_storage_usage').then(usage=>{
    if(reqId!==currentMeetingsReqId)return;
    const el=document.getElementById('meeting-cloud-storage');if(!el)return;
    el.textContent=usage.enabled?`Encrypted cloud audio: ${((usage.used_bytes+usage.reserved_bytes)/1e9).toFixed(2)} GB of ${(usage.limit_bytes/1e9).toFixed(0)} GB used. Local recordings are kept on this device.`:'Cloud audio backup is currently unavailable. Local recordings are kept on this device.';
    if(usage.purge_after)el.textContent+=` Cloud audio is scheduled for deletion on ${new Date(usage.purge_after*1000).toLocaleDateString()} unless your paid plan is renewed.`;
  }).catch(()=>{const el=document.getElementById('meeting-cloud-storage');if(el&&reqId===currentMeetingsReqId)el.textContent='Cloud storage usage could not be loaded.';});
  const token = currentConfig?.signed_in ? true : null;

  if (!token) {
    showMeetingsEmpty('Please sign in to view and record meeting notes.');
    return;
  }

  // Display the loading state while server fetch is in progress
  if (!quiet) showMeetingsLoading();

  try {
    const res = await apiRequest('GET', '/meetings');

    if (reqId !== currentMeetingsReqId) return; // Stale request, ignore

    if (!res.ok) throw new Error("Server error " + res.status);
    const meetings = await res.json();
    
    if (reqId !== currentMeetingsReqId) return; // Stale request, ignore

    window.allMeetings = Array.isArray(meetings) ? meetings : [];
    renderMeetingsList(window.allMeetings);
    await appendPendingLocalMeetings();
  } catch (err) {
    if (reqId !== currentMeetingsReqId) return;
    console.error("Failed to load meetings:");
    if (!quiet) showMeetingsEmpty('Error connecting to SpeekTo.Me server. Please check your connection.');
    await appendPendingLocalMeetings();
  }
}

function formatMeetingDate(timestamp) {
  if (!timestamp) return "N/A";
  try {
    let d;
    if (typeof timestamp === 'number') {
      d = new Date(timestamp < 10000000000 ? timestamp * 1000 : timestamp);
    } else {
      const num = Number(timestamp);
      if (!isNaN(num) && num > 0) {
        d = new Date(num < 10000000000 ? num * 1000 : num);
      } else {
        d = new Date(timestamp);
      }
    }
    if (isNaN(d.getTime())) return String(timestamp);
    const day = d.getDate();
    const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sept', 'Oct', 'Nov', 'Dec'];
    const month = months[d.getMonth()] || d.toLocaleDateString('en-US', { month: 'short' });
    const year = d.getFullYear();
    return `${day} ${month} ${year}`;
  } catch (_) {
    return String(timestamp);
  }
}

function getMeetingSpeakerCount(m) {
  if (!m) return 1;
  if (m.speaker_count && typeof m.speaker_count === 'number') {
    return Math.max(1, m.speaker_count);
  }
  let segments = [];
  if (Array.isArray(m.segments)) {
    segments = m.segments;
  } else if (m.segments_json) {
    try {
      segments = typeof m.segments_json === 'string' ? JSON.parse(m.segments_json) : m.segments_json;
    } catch (_) {}
  }
  if (Array.isArray(segments) && segments.length > 0) {
    const speakerSet = new Set();
    segments.forEach(s => {
      if (s && s.speaker && typeof s.speaker === 'string') {
        const clean = s.speaker.trim();
        if (clean) speakerSet.add(clean);
      }
    });
    if (speakerSet.size > 0) {
      return speakerSet.size;
    }
  }
  return 1;
}

async function deleteMeetingById(id) {
  if (!id) return;
  const confirmed = await showConfirmDialog({
    title: "Delete Meeting Note?",
    message: "Are you sure you want to delete this meeting note? This action cannot be undone.",
    confirmText: "Delete",
    cancelText: "Cancel",
    isDanger: true
  });
  if (!confirmed) return;

  const token = currentConfig?.signed_in ? true : null;
  try {
    const res = await apiRequest('DELETE', `/meetings/${encodeURIComponent(id)}`);
    if (!res.ok) throw new Error("Failed to delete");
    if (activeMeetingId === id) {
      activeMeetingId = null;
    }
    showToast("Meeting note deleted successfully.", "SpeekTo.Me", "info");
    loadMeetings();
  } catch (err) {
    console.error("[diagnostic event: private values omitted]");
    await showAlertDialog({
      title: "Failed to Delete",
      message: "Failed to delete meeting note: " + err,
      type: "error"
    });
  }
}

function renderMeetingsList(meetings) {
  hideMeetingsLoading();
  const emptyMsg = document.getElementById('meetings-empty-msg');
  const listGrid = document.getElementById('meetings-list');
  if (!listGrid) return;
  listGrid.innerHTML = '';

  if (!meetings || meetings.length === 0) {
    const searchInput = document.getElementById('meetings-search-input');
    const query = searchInput ? searchInput.value.trim() : '';
    if (query) {
      showMeetingsEmpty(`No meeting notes found matching "${query}".`);
    } else {
      showMeetingsEmpty('No meeting notes recorded yet. Click "New Meeting" to start!');
    }
    return;
  }

  if (emptyMsg) {
    emptyMsg.classList.add('hidden');
    emptyMsg.style.display = 'none';
  }
  listGrid.classList.remove('hidden');
  listGrid.style.display = 'grid';

  meetings.forEach(m => {
    const card = document.createElement('div');
    card.className = 'meeting-card';
    card.setAttribute('data-meeting-id', m.id);

    // Clicking card opens meeting details
    card.addEventListener('click', (e) => {
      if (e.target.closest('.meeting-card-menu-wrapper')) return;
      viewMeeting(m.id);
    });

    // 1. Top row: Badge + Title + Action Button
    const topRow = document.createElement('div');
    topRow.className = 'meeting-card-top';

    const leftGroup = document.createElement('div');
    leftGroup.className = 'meeting-card-left';

    const iconBadge = document.createElement('div');
    iconBadge.className = 'meeting-card-icon-badge';
    iconBadge.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
      <polyline points="14 2 14 8 20 8"></polyline>
      <line x1="16" y1="13" x2="8" y2="13"></line>
      <line x1="16" y1="17" x2="8" y2="17"></line>
      <line x1="10" y1="9" x2="8" y2="9"></line>
    </svg>`;

    const title = document.createElement('div');
    title.className = 'meeting-card-title';
    title.textContent = m.title || 'Recording...';
    title.title = m.title || 'Recording...';

    leftGroup.appendChild(iconBadge);
    leftGroup.appendChild(title);
    if (m.processing_status && m.processing_status !== 'completed') {
      const status = document.createElement('span');
      status.textContent = m.processing_status === 'failed' ? 'Processing failed' : m.processing_status === 'processing' ? 'Processing' : 'Queued';
      status.className = 'meeting-processing-status';
      leftGroup.appendChild(status);
    }

    // Three dots menu wrapper
    const menuWrapper = document.createElement('div');
    menuWrapper.className = 'meeting-card-menu-wrapper';

    const menuBtn = document.createElement('button');
    menuBtn.type = 'button';
    menuBtn.className = 'meeting-card-menu-btn';
    menuBtn.title = 'More options';
    menuBtn.setAttribute('aria-label', 'More options');
    menuBtn.innerHTML = `<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <circle cx="5" cy="12" r="1.75"></circle>
      <circle cx="12" cy="12" r="1.75"></circle>
      <circle cx="19" cy="12" r="1.75"></circle>
    </svg>`;

    // Dropdown
    const dropdown = document.createElement('div');
    dropdown.className = 'meeting-card-dropdown hidden';

    const viewItem = document.createElement('button');
    viewItem.type = 'button';
    viewItem.className = 'meeting-dropdown-item';
    viewItem.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7z"></path>
      <circle cx="12" cy="12" r="3"></circle>
    </svg><span>View Note</span>`;
    viewItem.addEventListener('click', (e) => {
      e.stopPropagation();
      dropdown.classList.add('hidden');
      viewMeeting(m.id);
    });

    const deleteItem = document.createElement('button');
    deleteItem.type = 'button';
    deleteItem.className = 'meeting-dropdown-item danger';
    deleteItem.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <polyline points="3 6 5 6 21 6"></polyline>
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
    </svg><span>Delete Note</span>`;
    deleteItem.addEventListener('click', (e) => {
      e.stopPropagation();
      dropdown.classList.add('hidden');
      deleteMeetingById(m.id);
    });

    dropdown.appendChild(viewItem);
    if (m.processing_status === 'failed' && m.client_id) {
      const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'meeting-dropdown-item'; retry.textContent = 'Retry processing';
      retry.addEventListener('click', async e => {
        e.stopPropagation(); retry.disabled = true;
        try {
          const response = await apiRequest('POST', `/v1/meetings/jobs/${encodeURIComponent(m.client_id)}/retry`);
          if (!response.ok) throw new Error(await response.text());
          await loadMeetings(true);
        } catch (error) { showToast(String(error), 'Retry failed', 'error'); retry.disabled = false; }
      });
      dropdown.appendChild(retry);
    }
    dropdown.appendChild(deleteItem);

    menuBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      const isCurrentlyHidden = dropdown.classList.contains('hidden');
      document.querySelectorAll('.meeting-card-dropdown').forEach(d => d.classList.add('hidden'));
      if (isCurrentlyHidden) {
        dropdown.classList.remove('hidden');
      }
    });

    menuWrapper.appendChild(menuBtn);
    menuWrapper.appendChild(dropdown);

    topRow.appendChild(leftGroup);
    topRow.appendChild(menuWrapper);

    // 2. Bottom Row: Calendar Date, Clock Duration, Users Speaker Count
    const bottomRow = document.createElement('div');
    bottomRow.className = 'meeting-card-bottom';

    // Date item
    const dateItem = document.createElement('div');
    dateItem.className = 'meeting-meta-item';
    dateItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
      <line x1="16" y1="2" x2="16" y2="6"></line>
      <line x1="8" y1="2" x2="8" y2="6"></line>
      <line x1="3" y1="10" x2="21" y2="10"></line>
    </svg><span class="meeting-meta-text">${formatMeetingDate(m.timestamp)}</span>`;

    // Duration item
    const durItem = document.createElement('div');
    durItem.className = 'meeting-meta-item';
    durItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="12" cy="12" r="10"></circle>
      <polyline points="12 6 12 12 16 14"></polyline>
    </svg><span class="meeting-meta-text">${formatDuration(m.duration)}</span>`;

    // Speakers item
    const speakerCount = getMeetingSpeakerCount(m);
    const speakerItem = document.createElement('div');
    speakerItem.className = 'meeting-meta-item';
    speakerItem.innerHTML = `<svg class="meeting-meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
      <circle cx="9" cy="7" r="4"></circle>
      <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
      <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
    </svg><span class="meeting-meta-text">${speakerCount} ${speakerCount === 1 ? 'speaker' : 'speakers'}</span>`;

    bottomRow.appendChild(dateItem);
    bottomRow.appendChild(durItem);
    bottomRow.appendChild(speakerItem);

    card.appendChild(topRow);
    card.appendChild(bottomRow);

    listGrid.appendChild(card);
  });
}

async function viewMeeting(id) {
  const detailGeneration=++meetingDetailGeneration;resetMeetingAudio();
  showProcessingView();
  const isCurrent=()=>detailGeneration===meetingDetailGeneration&&Boolean(currentConfig?.signed_in);
  try {
    const res = await apiRequest('GET', `/meetings/${encodeURIComponent(id)}`);
    if (!res.ok) throw new Error("Failed to load meeting details");
    const m = await res.json();
    if(!isCurrent())return;
    activeMeetingId = id;

    showDetailView();

    // Date & Duration formatters matching template ("7 Sept 2026", "1h 3m 24s")
    const formatMeetingDate = (dateStr) => {
      if (!dateStr) return "N/A";
      try {
        const d = new Date(dateStr);
        if (isNaN(d.getTime())) return dateStr;
        const day = d.getDate();
        const monthNames = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sept", "Oct", "Nov", "Dec"];
        return `${day} ${monthNames[d.getMonth()]} ${d.getFullYear()}`;
      } catch (_) {
        return dateStr;
      }
    };

    const formatMeetingDurationDetailed = (seconds) => {
      if (!seconds || seconds <= 0) return "0s";
      const h = Math.floor(seconds / 3600);
      const m = Math.floor((seconds % 3600) / 60);
      const s = Math.round(seconds % 60);
      if (h > 0) return `${h}h ${m}m ${s}s`;
      if (m > 0) return `${m}m ${s}s`;
      return `${s}s`;
    };

    const formatCurrentTimeDisplay = (sec) => {
      if (isNaN(sec) || sec < 0) sec = 0;
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      const s = Math.floor(sec % 60);
      if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
      return `${m}:${String(s).padStart(2, '0')}`;
    };

    const formatTotalTimeDisplay = (sec) => {
      if (isNaN(sec) || sec <= 0) return '0:00';
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      const s = Math.floor(sec % 60);
      if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
      return `${m}:${String(s).padStart(2, '0')}`;
    };

    // Populate Overview Metadata Row
    const titleEl = document.getElementById('meeting-detail-title');
    if (titleEl) titleEl.textContent = m.title || 'Untitled Meeting';

    const dateEl = document.getElementById('meeting-detail-date');
    if (dateEl) dateEl.textContent = formatMeetingDate(m.timestamp);

    // Parse segments
    let segments = [];
    try {
      segments = JSON.parse(m.segments_json);
    } catch (_) {
      segments = [];
    }

    // Determine local user name if available (from DOM or window state)
    const domUserName = userNameEl && userNameEl.textContent ? userNameEl.textContent.trim() : '';

    let maxSegEnd = 0;
    if (segments && segments.length > 0) {
      segments.forEach(s => {
        if (s.end && s.end > maxSegEnd) maxSegEnd = s.end;
      });
    }
    let totalDuration = Math.max(m.duration || 0, maxSegEnd, 1);

    const durEl = document.getElementById('meeting-detail-duration');
    if (durEl) durEl.textContent = formatMeetingDurationDetailed(totalDuration);
    const cloudStatus=document.getElementById('meeting-cloud-status');
    const downloadButton=document.getElementById('meeting-cloud-download');
    if(downloadButton){downloadButton.disabled=m.cloud_audio_status!=='ready';downloadButton.onclick=async()=>{downloadButton.disabled=true;try{const path=await invoke('download_meeting_audio',{meetingId:id});if(isCurrent()&&cloudStatus)cloudStatus.textContent=`Audio saved to ${path}`;}catch{if(isCurrent()&&cloudStatus)cloudStatus.textContent='Audio could not be downloaded. Your local recordings are preserved.';}finally{if(isCurrent())downloadButton.disabled=false;}};}

    if(cloudStatus)cloudStatus.textContent=({ready:'Encrypted cloud backup available.',storage_full:'Cloud storage is full. This recording is kept locally but has not been backed up.',local_file_unavailable:'The server could not find audio to back up. The local recording may still be available.',deleted:'The cloud audio copy was deleted.',unavailable:'Cloud audio backup is unavailable.',not_archived:'Encrypted cloud backup needs an active Pro plan. This recording is kept on this device.',pending:'Encrypted cloud backup is pending.'})[m.cloud_audio_status]||'';


    const uniqueSpeakers = new Set((segments || []).map(s => getMeetingSpeakerIdentity(s, domUserName).id));
    const spkCount = Math.max(1, uniqueSpeakers.size);
    const speakersEl = document.getElementById('meeting-detail-speakers');
    if (speakersEl) speakersEl.textContent = `${spkCount} speaker${spkCount === 1 ? '' : 's'}`;

    // Resolve the exact local meeting and account before trying its encrypted cloud copy.
    const player = document.getElementById('meeting-audio-player');
    try {
      const ticket = await invoke('get_local_meeting_audio', { meetingId: id });
      if (!isCurrent()) return;
      player.preload = 'metadata';
      player.onerror = () => { if (isCurrent() && cloudStatus) cloudStatus.textContent = 'Audio playback failed. Download the cloud recording to play it externally.'; };
      player.src = ticket;
    } catch (_) {
      if (!isCurrent()) return;
      if (cloudStatus) cloudStatus.textContent = 'Recording could not be opened. Check your connection or download the cloud audio to play it externally.';
    }
    if (!isCurrent()) return;
    // Custom Audio Player Controls Elements
    const audioPlayBtn = document.getElementById('audio-play-btn');
    const audioPlayIcon = document.getElementById('audio-play-icon');
    const audioPauseIcon = document.getElementById('audio-pause-icon');
    const audioTimeCurrent = document.getElementById('audio-time-current');
    const audioTimeTotal = document.getElementById('audio-time-total');
    const audioScrubberTrack = document.getElementById('audio-scrubber-track');
    const audioScrubberFill = document.getElementById('audio-scrubber-fill');
    const audioScrubberThumb = document.getElementById('audio-scrubber-thumb');
    const audioVolBtn = document.getElementById('audio-vol-btn');
    const audioSpeedSelect = document.getElementById('audio-speed-select');

    if (audioTimeCurrent) audioTimeCurrent.textContent = '0:00';
    if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(totalDuration);
    if (audioScrubberFill) audioScrubberFill.style.width = '0%';
    if (audioScrubberThumb) audioScrubberThumb.style.left = '0%';

    const updateAudioPlayState = () => {
      if (!audioPlayIcon || !audioPauseIcon) return;
      if (player.paused) {
        audioPlayIcon.classList.remove('hidden');
        audioPauseIcon.classList.add('hidden');
      } else {
        audioPlayIcon.classList.add('hidden');
        audioPauseIcon.classList.remove('hidden');
      }
    };

    if (audioPlayBtn) {
      audioPlayBtn.onclick = () => {
        if (player.paused) {
          player.play().catch(() => { if (isCurrent() && cloudStatus) cloudStatus.textContent = 'Audio playback could not start. Try again or download the recording.'; });
        } else {
          player.pause();
        }
      };
    }

    player.onplay = updateAudioPlayState;
    player.onpause = updateAudioPlayState;
    player.onended = () => {
      updateAudioPlayState();
      if (audioScrubberFill) audioScrubberFill.style.width = '0%';
      if (audioScrubberThumb) audioScrubberThumb.style.left = '0%';
    };
    updateAudioPlayState();

    if (audioVolBtn) {
      audioVolBtn.onclick = () => {
        player.muted = !player.muted;
        audioVolBtn.style.opacity = player.muted ? '0.4' : '1.0';
      };
    }

    if (audioSpeedSelect) {
      audioSpeedSelect.value = "1.0";
      player.playbackRate = 1.0;
      audioSpeedSelect.onchange = (e) => {
        player.playbackRate = parseFloat(e.target.value);
      };
    }

    let isDraggingScrubber = false;
    const seekFromScrubber = (e) => {
      const effDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0) ? player.duration : totalDuration;
      if (!audioScrubberTrack || effDuration <= 0) return;
      const rect = audioScrubberTrack.getBoundingClientRect();
      const clickX = e.clientX - rect.left;
      const pct = Math.max(0, Math.min(1, clickX / rect.width));
      player.currentTime = pct * effDuration;
      if (audioScrubberFill) audioScrubberFill.style.width = `${pct * 100}%`;
      if (audioScrubberThumb) audioScrubberThumb.style.left = `${pct * 100}%`;
      if (audioTimeCurrent) audioTimeCurrent.textContent = formatCurrentTimeDisplay(player.currentTime);
    };

    if (audioScrubberTrack) {
      audioScrubberTrack.onmousedown = (e) => {
        isDraggingScrubber = true;
        seekFromScrubber(e);
      };
    }

    window.onmousemove = (e) => {
      if (isDraggingScrubber) seekFromScrubber(e);
    };

    window.onmouseup = () => {
      if (isDraggingScrubber) isDraggingScrubber = false;
    };

    // Detail 3-dots Dropdown Menu
    const detailMenuBtn = document.getElementById('meeting-detail-menu-btn');
    const detailDropdown = document.getElementById('meeting-detail-dropdown');
    const detailDeleteOpt = document.getElementById('meeting-detail-delete-opt');
    if (detailMenuBtn && detailDropdown) {
      detailMenuBtn.onclick = (e) => {
        e.stopPropagation();
        detailDropdown.classList.toggle('hidden');
      };
    }
    if (detailDeleteOpt) {
      detailDeleteOpt.onclick = (e) => {
        e.stopPropagation();
        if (detailDropdown) detailDropdown.classList.add('hidden');
        confirmDeleteMeeting();
      };
    }

    // Rename: the title becomes an inline field; Enter saves, Escape cancels.
    const renameOpt = document.getElementById('meeting-detail-rename-opt');
    const titleForm = document.getElementById('meeting-title-form');
    const titleEdit = document.getElementById('meeting-title-edit');
    const titleSave = document.getElementById('meeting-title-save');
    const titleCancel = document.getElementById('meeting-title-cancel');
    const titleStatus = document.getElementById('meeting-title-status');
    const endTitleEdit = () => {
      titleForm.hidden = true;
      titleEl.hidden = false;
      titleStatus.textContent = '';
    };
    endTitleEdit();
    if (renameOpt) {
      renameOpt.onclick = (e) => {
        e.stopPropagation();
        if (detailDropdown) detailDropdown.classList.add('hidden');
        titleEdit.value = m.title || '';
        titleEl.hidden = true;
        titleForm.hidden = false;
        titleEdit.focus();
        titleEdit.select();
      };
    }
    titleCancel.onclick = endTitleEdit;
    titleEdit.onkeydown = (e) => { if (e.key === 'Escape') endTitleEdit(); };
    titleForm.onsubmit = async (e) => {
      e.preventDefault();
      const title = titleEdit.value.trim();
      if (!title) { titleStatus.textContent = 'Enter a name.'; return; }
      if (title === m.title) { endTitleEdit(); return; }
      titleSave.disabled = true;
      titleStatus.textContent = 'Saving…';
      try {
        const response = await apiRequest('PUT', `/meetings/${encodeURIComponent(id)}/title`, { title });
        if (!response.ok) throw new Error(await response.text() || 'The name could not be saved.');
        const saved = await response.json();
        if (!isCurrent()) return;
        m.title = saved.title;
        titleEl.textContent = saved.title;
        endTitleEdit();
      } catch (error) {
        if (isCurrent()) titleStatus.textContent = error.message || 'The name could not be saved. Try again.';
      } finally {
        titleSave.disabled = false;
      }
    };

    // Setup active tab (Timeline by default)
    const tabBtns = document.querySelectorAll('.detail-tab-btn');
    const panes = document.querySelectorAll('.detail-tab-pane');
    tabBtns.forEach(b => b.classList.remove('active'));
    panes.forEach(p => p.classList.remove('active'));
    const defaultTimelineBtn = document.querySelector('.detail-tab-btn[data-detail-tab="timeline"]') || document.querySelector('.detail-tab-btn');
    const defaultTimelinePane = document.getElementById('detail-pane-timeline') || document.getElementById('detail-pane-transcript');
    if (defaultTimelineBtn) defaultTimelineBtn.classList.add('active');
    if (defaultTimelinePane) defaultTimelinePane.classList.add('active');

    // Helper function to render or update timeline when audio duration is resolved
    const doRenderTimeline = () => {
      const audioDuration = player && player.duration && !isNaN(player.duration) && player.duration > 0 ? player.duration : 0;
      totalDuration = audioDuration > 0 ? audioDuration : (m.duration > 0 ? m.duration : Math.max(maxSegEnd, 1));
      
      const durLabel = document.getElementById('meeting-detail-duration');
      if (durLabel) durLabel.textContent = formatMeetingDurationDetailed(totalDuration);

      if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(totalDuration);
      
      renderMeetingTimeline(segments, totalDuration, player, domUserName);
    };

    if (player) {
      player.onloadedmetadata = () => {
        console.log("[diagnostic event: private values omitted]");
        doRenderTimeline();
      };
      player.ondurationchange = () => {
        console.log("[diagnostic event: private values omitted]");
        doRenderTimeline();
      };
    }

    // 1. RENDER AUDIO TIMELINE VIEW
    doRenderTimeline();

    // 2. RENDER TRANSCRIPT VIEW
    const transcriptTextEl = document.getElementById('meeting-transcript-text');
    transcriptTextEl.innerHTML = '';

    renderMeetingTranscript(segments, player, domUserName, m.transcript);

    let isUserHoveringTranscript = false;
    transcriptTextEl.onmouseenter = () => { isUserHoveringTranscript = true; };
    transcriptTextEl.onmouseleave = () => { isUserHoveringTranscript = false; };
    transcriptTextEl.onwheel = () => { isUserHoveringTranscript = true; };

    let lastActiveSpan = null;
    let isUserHoveringTable = false;
    let lastActiveTableRow = null;

    player.ontimeupdate = () => {
      const time = player.currentTime;
      const effectiveDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0)
        ? player.duration
        : totalDuration;

      // 1. Digital Time Counter in Audio Bar
      if (audioTimeCurrent) audioTimeCurrent.textContent = formatCurrentTimeDisplay(time);
      if (audioTimeTotal) audioTimeTotal.textContent = formatTotalTimeDisplay(effectiveDuration);

      // 2. Scrubber Track Fill & Thumb
      if (!isDraggingScrubber && audioScrubberFill && audioScrubberThumb && effectiveDuration > 0) {
        const pct = Math.max(0, Math.min(100, (time / effectiveDuration) * 100));
        audioScrubberFill.style.width = `${pct}%`;
        audioScrubberThumb.style.left = `${pct}%`;
      }

      // 3. Timeline Playhead Needle & Auto-Scroll
      const playhead = document.getElementById('timeline-playhead');
      const scrollArea = document.getElementById('timeline-scroll-area');
      const scrollCanvas = document.getElementById('timeline-scroll-canvas');

      if (playhead && effectiveDuration > 0) {
        const pct = Math.max(0, Math.min(100, (time / effectiveDuration) * 100));
        playhead.style.left = `${pct}%`;

        // Auto-scroll timeline when zoomed
        if (scrollArea && scrollCanvas && scrollCanvas.offsetWidth > scrollArea.offsetWidth && !player.paused) {
          const playheadPx = (pct / 100) * scrollCanvas.offsetWidth;
          const targetScroll = playheadPx - (scrollArea.offsetWidth / 2);
          scrollArea.scrollLeft = targetScroll;
        }
      }

      // 4. Update Timeline Active Segment Boxes
      const segmentBoxes = document.querySelectorAll('.segment-box');
      segmentBoxes.forEach(box => {
        const start = parseFloat(box.dataset.start);
        const end = parseFloat(box.dataset.end);
        if (time >= start && time <= end) {
          box.classList.add('active');
        } else {
          box.classList.remove('active');
        }
      });

      // 5. Update Turn-by-Turn Inspector Table Active Row
      const tableRows = document.querySelectorAll('.turn-table-row');
      let currentActiveRow = null;
      tableRows.forEach(row => {
        const start = parseFloat(row.dataset.start);
        const end = parseFloat(row.dataset.end);
        if (time >= start && time <= end) {
          row.classList.add('active-turn-row');
          currentActiveRow = row;
        } else {
          row.classList.remove('active-turn-row');
        }
      });

      if (currentActiveRow && currentActiveRow !== lastActiveTableRow) {
        lastActiveTableRow = currentActiveRow;
        if (!isUserHoveringTable) {
          currentActiveRow.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }

      // 6. Update Transcript Active Spans
      const spans = transcriptTextEl.querySelectorAll('.transcript-segment');
      let currentActiveSpan = null;

      spans.forEach(span => {
        const start = parseFloat(span.dataset.start);
        const end = parseFloat(span.dataset.end);
        if (time >= start && time <= end) {
          span.classList.add('active');
          currentActiveSpan = span;
        } else {
          span.classList.remove('active');
        }
      });

      if (currentActiveSpan && currentActiveSpan !== lastActiveSpan) {
        lastActiveSpan = currentActiveSpan;
        if (!isUserHoveringTranscript) {
          currentActiveSpan.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }
    };

    // Inspector table hover lock
    const inspectorScroll = document.querySelector('.inspector-table-scroll');
    if (inspectorScroll) {
      inspectorScroll.onmouseenter = () => { isUserHoveringTable = true; };
      inspectorScroll.onmouseleave = () => { isUserHoveringTable = false; };
      inspectorScroll.onwheel = () => { isUserHoveringTable = true; };
    }

    // Render summary and actions
    document.getElementById('meeting-summary-text').innerHTML = parseMarkdownToHTML(m.summary);
    document.getElementById('meeting-actions-text').innerHTML = parseMarkdownToHTML(m.action_items);

    renderMeetingSpeakerEditor(m, (updated) => {
      Object.assign(m, updated);
      segments = JSON.parse(updated.segments_json || '[]');
      const scrollArea = document.getElementById('timeline-scroll-area');
      const scrollLeft = scrollArea?.scrollLeft || 0;
      const transcriptScroll = transcriptTextEl.scrollTop;
      doRenderTimeline();
      if (scrollArea) scrollArea.scrollLeft = scrollLeft;
      renderMeetingTranscript(segments, player, domUserName, m.transcript);
      transcriptTextEl.scrollTop = transcriptScroll;
      lastActiveSpan = null;
      lastActiveTableRow = null;
      document.getElementById('meeting-summary-text').innerHTML = parseMarkdownToHTML(m.summary);
      document.getElementById('meeting-actions-text').innerHTML = parseMarkdownToHTML(m.action_items);
      const search = document.getElementById('timeline-search-speaker');
      if (search?.value) search.dispatchEvent(new Event('input'));
    });

    // Detail Tabs switching
    const currentTabBtns = document.querySelectorAll('.detail-tab-btn');
    currentTabBtns.forEach(btn => {
      const newBtn = btn.cloneNode(true);
      btn.parentNode.replaceChild(newBtn, btn);
      newBtn.addEventListener('click', () => {
        const activeBtns = document.querySelectorAll('.detail-tab-btn');
        const activePanes = document.querySelectorAll('.detail-tab-pane');
        activeBtns.forEach(b => b.classList.remove('active'));
        activePanes.forEach(p => p.classList.remove('active'));
        
        newBtn.classList.add('active');
        const targetPane = newBtn.getAttribute('data-detail-tab');
        const paneEl = document.getElementById(`detail-pane-${targetPane}`);
        if (paneEl) paneEl.classList.add('active');
      });
    });

  } catch (err) {
    console.error("[diagnostic event: private values omitted]");
    showToast(err.message, "Failed to load meeting", "error");
    showDashboardView();
  }
}

function renderMeetingSpeakerEditor(meeting, onSaved) {
  const editor = document.getElementById('meeting-speaker-editor');
  const fields = document.getElementById('meeting-speaker-fields');
  const form = document.getElementById('meeting-speaker-form');
  const save = document.getElementById('meeting-speaker-save');
  const status = document.getElementById('meeting-speaker-status');
  if (!editor || !fields || !form || !save || !status) return;
  editor.open = false;
  editor.hidden = !(meeting.speakers?.length);
  fields.replaceChildren();
  status.textContent = '';
  save.disabled = true;
  const inputs = new Map();
  for (const speaker of meeting.speakers || []) {
    const row = document.createElement('label');
    row.className = 'meeting-speaker-edit-row';
    const source = document.createElement('span');
    source.textContent = speaker.default_name + (speaker.is_me ? ' (microphone)' : '');
    const dot = document.createElement('span');
    dot.className = 'meeting-speaker-dot';
    dot.style.backgroundColor = getMeetingSpeakerTheme({id: speaker.id, isMe: speaker.is_me}).color;
    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'fluent-input';
    input.maxLength = 80;
    input.value = meeting.speaker_names?.[speaker.id] || '';
    input.placeholder = speaker.default_name;
    input.autocomplete = 'off';
    input.dataset.speakerId = speaker.id;
    input.oninput = () => {
      const dirty = [...inputs].some(([id, field]) => field.value.trim() !== (meeting.speaker_names?.[id] || ''));
      save.disabled = !dirty;
      status.textContent = dirty ? 'Unsaved changes' : '';
    };
    row.append(dot, source, input);
    fields.appendChild(row);
    inputs.set(speaker.id, input);
  }
  let saving = false;
  form.onsubmit = async (event) => {
    event.preventDefault();
    if (saving || save.disabled) return;
    saving = true;
    save.disabled = true;
    for (const input of inputs.values()) input.disabled = true;
    status.textContent = 'Saving…';
    const names = Object.fromEntries([...inputs].filter(([,input]) => input.value.trim()).map(([id,input]) => [id,input.value.trim()]));
    try {
      const response = await apiRequest('PUT', `/meetings/${encodeURIComponent(meeting.id)}/speakers`, {speaker_names: names, previous_names: meeting.speaker_names || {}});
      if (!response.ok) throw new Error(await response.text() || 'Could not save names. Try again.');
      const updated = await response.json();
      if (String(activeMeetingId) !== String(meeting.id)) return;
      meeting.speaker_names = updated.speaker_names;
      for (const [id,input] of inputs) input.value = updated.speaker_names[id] || '';
      onSaved(updated);
      status.textContent = 'Names saved';
    } catch (error) {
      if (String(activeMeetingId) !== String(meeting.id)) return;
      status.textContent = error.message || 'Could not save names. Try again.';
      save.disabled = false;
    } finally {
      saving = false;
      for (const input of inputs.values()) input.disabled = false;
    }
  };
}

// Dedicated High-Precision Visualizer & Timeline Renderer
function renderMeetingTimeline(segments, totalDuration, player, currentUserName) {
  const legendContainer = document.getElementById('timeline-speaker-legend');
  const headersList = document.getElementById('timeline-headers-list');
  const rulerTrack = document.getElementById('timeline-ruler-track');
  const lanesBody = document.getElementById('timeline-lanes-body');
  const scrollCanvas = document.getElementById('timeline-scroll-canvas');
  const scrollArea = document.getElementById('timeline-scroll-area');
  const inspectorTbody = document.getElementById('timeline-inspector-tbody');
  const turnsLabel = document.getElementById('inspector-total-turns-label');
  const searchInput = document.getElementById('timeline-search-speaker');

  if (!headersList || !lanesBody || !rulerTrack) return;

  // Clear previous content
  headersList.innerHTML = '';
  rulerTrack.innerHTML = '';
  if (legendContainer) legendContainer.innerHTML = '';
  if (inspectorTbody) inspectorTbody.innerHTML = '';
  lanesBody.innerHTML = `
    <div class="timeline-playhead" id="timeline-playhead">
      <div class="timeline-playhead-line"></div>
    </div>
  `;

  const formatOffset = (seconds) => {
    if (isNaN(seconds) || seconds < 0) return '0:00';
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    const sStr = s < 10 ? '0' + s : s;
    if (h > 0) {
      const mStr = m < 10 ? '0' + m : m;
      return `${h}:${mStr}:${sStr}`;
    }
    return `${m}:${sStr}`;
  };

  // Sync with actual audio player duration when metadata loads
  if (player && player.duration && player.duration > 0 && !isNaN(player.duration)) {
    totalDuration = player.duration;
  }

  if (turnsLabel) turnsLabel.textContent = `${(segments || []).length}`;

  if (!segments || segments.length === 0) {
    lanesBody.innerHTML += '<div style="padding: 30px; text-align: center; color: var(--text-muted); font-size: 13px; font-style: italic;">No audio segment data available for this recording.</div>';
    return;
  }

  // Channel and stable speaker ID are shared with the transcript and inspector.
  const speakerGroups = new Map();
  segments.forEach((seg, idx) => {
    const identity = getMeetingSpeakerIdentity(seg, currentUserName);
    if (!speakerGroups.has(identity.id)) {
      speakerGroups.set(identity.id, { ...identity, name: identity.label, segments: [], totalTime: 0 });
    }
    const group = speakerGroups.get(identity.id);
    group.segments.push({ ...seg, turnIndex: idx + 1 });
    group.totalTime += Math.max(0, (seg.end || 0) - (seg.start || 0));
  });

  // Sort speakers: Local user (isMe) ALWAYS at Row 0 (Top Track)
  const sortedSpeakers = Array.from(speakerGroups.values()).sort((a, b) => {
    if (a.isMe && !b.isMe) return -1;
    if (!a.isMe && b.isMe) return 1;
    return a.name.localeCompare(b.name, undefined, { numeric: true });
  });

  window.__currentSpeakerThemes = {};
  sortedSpeakers.forEach(spk => {
    const theme = getMeetingSpeakerTheme(spk);
    window.__currentSpeakerThemes[spk.id] = theme;

    // Add to Legend
    if (legendContainer) {
      const pill = document.createElement('span');
      pill.className = 'legend-pill';
      pill.style.backgroundColor = theme.bg;
      pill.style.borderColor = theme.border;
      pill.style.color = theme.textColor;
      pill.innerHTML = `<span style="width: 6px; height: 6px; border-radius: 50%; background-color: ${theme.color}; display: inline-block;"></span><span>${escapeHtml(theme.label)}</span>`;
      legendContainer.appendChild(pill);
    }
  });

  // Render Time Ruler Ticks at clean human intervals
  const steps = [5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];
  let step = 600;
  for (const s of steps) {
    if (totalDuration / s <= 8) {
      step = s;
      break;
    }
  }

  for (let t = 0; t <= totalDuration; t += step) {
    const percent = (t / totalDuration) * 100;
    const tickEl = document.createElement('div');
    tickEl.className = 'timeline-ruler-tick';
    tickEl.style.left = `${percent}%`;
    tickEl.innerHTML = `
      <span class="timeline-ruler-tick-label">${formatOffset(t)}</span>
      <div class="timeline-ruler-tick-line"></div>
    `;
    rulerTrack.appendChild(tickEl);
  }

  // Interactive Drag-to-Scrub on Ruler & Lanes
  let isScrubbing = false;
  const handleScrub = (e) => {
    const effectiveTotalDuration = (player && player.duration && !isNaN(player.duration) && player.duration > 0) ? player.duration : totalDuration;
    if (!scrollCanvas || effectiveTotalDuration <= 0) return;
    const rect = scrollCanvas.getBoundingClientRect();
    const clickX = e.clientX - rect.left;
    const pct = Math.max(0, Math.min(1, clickX / rect.width));
    const seekTime = pct * effectiveTotalDuration;
    player.currentTime = seekTime;
    
    // Immediate visual update
    const playhead = document.getElementById('timeline-playhead');
    if (playhead) playhead.style.left = `${pct * 100}%`;
    const scrubberFill = document.getElementById('audio-scrubber-fill');
    const scrubberThumb = document.getElementById('audio-scrubber-thumb');
    if (scrubberFill) scrubberFill.style.width = `${pct * 100}%`;
    if (scrubberThumb) scrubberThumb.style.left = `${pct * 100}%`;
    const audioTimeCurrent = document.getElementById('audio-time-current');
    if (audioTimeCurrent) {
      const m = Math.floor(seekTime / 60);
      const s = Math.floor(seekTime % 60);
      audioTimeCurrent.textContent = `${m}:${String(s).padStart(2, '0')}`;
    }
  };

  const startScrub = (e) => {
    isScrubbing = true;
    handleScrub(e);
  };

  rulerTrack.onmousedown = startScrub;
  lanesBody.onmousedown = startScrub;

  window.addEventListener('mousemove', (e) => {
    if (isScrubbing) handleScrub(e);
  });

  window.addEventListener('mouseup', () => {
    if (isScrubbing) isScrubbing = false;
  });

  // Render Tracks & Segment Boxes
  sortedSpeakers.forEach((spk) => {
    const theme = window.__currentSpeakerThemes[spk.id];
    const talkPercent = Math.round((spk.totalTime / totalDuration) * 100);

    // 1. Left Sticky Header Item
    const headerItem = document.createElement('div');
    headerItem.className = 'timeline-header-item';
    headerItem.innerHTML = `
      <div class="timeline-header-swatch" style="background-color: ${theme.color};"></div>
      <div class="timeline-header-info">
        <div class="timeline-header-name" title="${escapeHtml(theme.label)}">${escapeHtml(theme.label)}</div>
        <div class="timeline-header-meta">${spk.segments.length} turns &bull; ${talkPercent}% talk time</div>
      </div>
    `;
    headersList.appendChild(headerItem);

    // 2. Right Track Lane Row
    const laneRow = document.createElement('div');
    laneRow.className = 'timeline-lane-row';
    laneRow.dataset.speaker = spk.id;

    spk.segments.forEach(seg => {
      const leftPercent = (seg.start / totalDuration) * 100;
      const widthPercent = ((seg.end - seg.start) / totalDuration) * 100;

      const box = document.createElement('div');
      box.className = 'segment-box';
      box.style.left = `${leftPercent}%`;
      box.style.width = `max(2px, ${widthPercent}%)`;
      box.style.minWidth = '2px';
      box.style.backgroundColor = theme.bg;
      box.style.border = `1px solid ${theme.color}`;
      box.dataset.start = seg.start;
      box.dataset.end = seg.end;
      box.dataset.speaker = spk.id;
      box.title = `${theme.label}: ${seg.start.toFixed(2)}s - ${seg.end.toFixed(2)}s (${(seg.end - seg.start).toFixed(2)}s)\n"${seg.text || ''}"`;

      // Click to seek & play
      box.onclick = (e) => {
        e.stopPropagation();
        player.currentTime = seg.start;
        player.play();
      };

      laneRow.appendChild(box);
    });

    lanesBody.appendChild(laneRow);
  });

  // Render Turn-by-Turn Inspector Table
  if (inspectorTbody) {
    segments.forEach((seg, idx) => {
      const identity = getMeetingSpeakerIdentity(seg, currentUserName);
      const spkName = identity.label;
      const theme = window.__currentSpeakerThemes[identity.id] || {
        color: 'var(--text-muted)',
        border: 'var(--border-subtle)',
        bg: 'var(--bg-card-subtle)',
        textColor: 'var(--text-primary)',
        label: spkName
      };

      const duration = Math.max(0, seg.end - seg.start);
      const leftPct = (seg.start / totalDuration) * 100;
      const widthPct = Math.max((duration / totalDuration) * 100, 1.5);

      const tr = document.createElement('tr');
      tr.className = 'turn-table-row';
      tr.dataset.turnIdx = idx;
      tr.dataset.start = seg.start;
      tr.dataset.end = seg.end;
      tr.dataset.speaker = spkName;
      tr.dataset.speakerId = identity.id;

      tr.innerHTML = `
        <td style="font-family: monospace; color: var(--text-muted);">#${String(idx + 1).padStart(2, '0')}</td>
        <td>
          <span class="inspector-speaker-pill" style="background-color: ${theme.bg}; border: 1px solid ${theme.border}; color: ${theme.textColor};">
            <span style="width: 6px; height: 6px; border-radius: 50%; background-color: ${theme.color}; display: inline-block;"></span>
            ${escapeHtml(theme.label)}
          </span>
        </td>
        <td>
          <span class="inspector-start-badge">${seg.start.toFixed(2)}s</span>
        </td>
        <td style="font-family: monospace; color: var(--text-muted);">${seg.end.toFixed(2)}s</td>
        <td style="font-family: monospace; color: var(--text-secondary); font-weight: 600;">${duration.toFixed(2)}s</td>
        <td>
          <div class="mini-timeline-bar">
            <div class="mini-timeline-fill" style="left: ${leftPct}%; width: ${widthPct}%; background-color: ${theme.color};"></div>
          </div>
        </td>
        <td style="color: var(--text-primary); max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(seg.text || '')}">
          ${seg.text ? escapeHtml(seg.text) : '<span style="color: var(--text-muted); font-style: italic;">(No speech text)</span>'}
        </td>
      `;

      tr.onclick = () => {
        player.currentTime = seg.start;
        player.play();
      };

      inspectorTbody.appendChild(tr);
    });
  }

  // Filter input logic
  if (searchInput) {
    searchInput.oninput = (e) => {
      const q = e.target.value.toLowerCase().trim();
      document.querySelectorAll('.turn-table-row').forEach(row => {
        const spk = (row.dataset.speaker || '').toLowerCase();
        const text = (row.querySelector('td:last-child')?.textContent || '').toLowerCase();
        if (!q || spk.includes(q) || text.includes(q)) {
          row.style.display = '';
        } else {
          row.style.display = 'none';
        }
      });
    };
  }
}

// Speaker numbers (including zero) are remote diarizer IDs, never a local-user shortcut.
function getMeetingSpeakerIdentity(segment, currentUserName = '') {
  if (segment.speaker_key && segment.speaker_display_name) {
    return { id: segment.speaker_key, label: segment.speaker_display_name, isMe: segment.speaker_key === 'mic:speaker_local' };
  }
  const channel = String(segment.channel || '').toLowerCase();
  const sourceId = String(segment.speaker_id || '').trim();
  const rawName = String(segment.speaker || segment.speaker_name || '').replace(/[\[\]]/g, '').trim();
  const clean = rawName.toLowerCase();
  const numbered = clean.match(/^(?:speaker|spk)[ _-]*(\d+)$/) || sourceId.match(/^spk[_-]?(\d+)$/i);
  const remoteChannel = channel === 'system' || channel === 'loopback';
  const localAlias = ['me', 'you', 'local speaker'].includes(clean);
  const exactOwnerName = currentUserName && clean === currentUserName.toLowerCase().trim();
  const isMe = channel === 'mic' || (!remoteChannel && !numbered &&
    (sourceId === 'speaker_local' || (!sourceId && (localAlias || exactOwnerName))));
  if (isMe) return { id: 'mic:speaker_local', label: 'Me', isMe: true };
  const id = sourceId || (numbered ? `spk_${numbered[1]}` : (clean || 'unknown'));
  let label = rawName || (numbered ? `Speaker ${numbered[1]}` : 'Unknown speaker');
  if (localAlias) label += ' (System)';
  return { id: `system:${id}`, label, isMe: false };
}

function getMeetingSpeakerTheme(identity) {
  if (identity.isMe) return {
    color: 'var(--speaker-mic-color, var(--accent-color))',
    border: 'var(--speaker-mic-border, var(--border-focus))',
    bg: 'var(--speaker-mic-bg, var(--accent-tint))',
    textColor: 'var(--text-primary)', label: identity.label
  };
  const color = getMeetingSpeakerColor(identity.id);
  return { color, border: `${color}55`, bg: `${color}22`, textColor: 'var(--text-primary)', label: identity.label };
}

function renderMeetingTranscript(segments, player, currentUserName, fallbackText) {
  const container = document.getElementById('meeting-transcript-text');
  if (!container) return;
  container.replaceChildren();
  if (!segments || !segments.length) {
    container.textContent = fallbackText || 'No transcript text available.';
    return;
  }
  let previousId = null;
  let blockText = null;
  for (const segment of segments) {
    const identity = getMeetingSpeakerIdentity(segment, currentUserName);
    const theme = getMeetingSpeakerTheme(identity);
    if (identity.id !== previousId) {
      previousId = identity.id;
      const block = document.createElement('div');
      block.className = `meeting-speaker-block ${identity.isMe ? 'is-me' : 'is-remote'}`;
      block.dataset.speakerId = identity.id;
      block.style.borderLeftColor = theme.color;
      const header = document.createElement('div');
      header.className = 'meeting-speaker-header';
      const avatar = document.createElement('div');
      avatar.className = 'meeting-speaker-avatar';
      avatar.textContent = identity.isMe ? 'ME' : getMeetingSpeakerInitials(identity.label);
      avatar.style.backgroundColor = theme.color;
      const label = document.createElement('span');
      label.className = 'meeting-speaker-name';
      label.textContent = identity.label;
      const time = document.createElement('span');
      time.className = 'meeting-speaker-time';
      time.textContent = formatMeetingTimeOffset(segment.start);
      header.append(avatar, label, time);
      blockText = document.createElement('div');
      blockText.className = 'meeting-speaker-text';
      block.append(header, blockText);
      container.appendChild(block);
    }
    const text = document.createElement('span');
    text.className = 'transcript-segment';
    text.dataset.start = segment.start;
    text.dataset.end = segment.end;
    text.textContent = (segment.text || '') + ' ';
    text.onclick = () => { player.currentTime = segment.start; player.play(); };
    blockText.appendChild(text);
  }
}

function formatMeetingTimeOffset(seconds) {
  seconds = Math.max(0, Number(seconds) || 0);
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const sec = String(Math.floor(seconds % 60)).padStart(2, '0');
  return h ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
}

// Global helper functions for speaker avatars, initials & theme colors
function getMeetingSpeakerInitials(name) {
  if (!name) return 'SP';
  const cleanName = name.replace(/[\[\]]/g, '').trim();
  const lower = cleanName.toLowerCase();
  const numbered = lower.match(/^(?:speaker|spk)[ _-]*(\d+)$/);
  if (numbered) return `S${numbered[1]}`;
  if (lower === 'you' || lower === 'me') return 'ME';
  const parts = cleanName.split(' ');
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  return cleanName.slice(0, 2).toUpperCase();
}

function getMeetingSpeakerColor(name) {
  const colors = ['#2563eb', '#d97706', '#7c3aed', '#059669', '#e11d48', '#0284c7', '#4f46e5', '#0d9488'];
  const clean = String(name || '').toLowerCase().replace(/^system:/, '').trim();
  const numbered = clean.match(/^(?:speaker|spk)[ _-]*(\d+)$/);
  if (numbered) return colors[Number(numbered[1]) % colors.length];
  let hash = 0;
  for (let i = 0; i < clean.length; i++) hash = clean.charCodeAt(i) + ((hash << 5) - hash);
  return colors[Math.abs(hash) % colors.length];
}

function handleLiveMeetingTurn(turn) {
  if (!turn || !turn.id) return;
  const turnsList = document.getElementById('meetings-live-turns-list');
  const waitingText = document.getElementById('meetings-live-waiting-text');
  if (waitingText) waitingText.style.display = 'none';
  if (!turnsList) return;

  const identity = getMeetingSpeakerIdentity(turn, currentUserInfo?.name || '');
  const isMe = identity.isMe;
  const formatOffset = (seconds) => {
    if (isNaN(seconds) || seconds < 0) return '0:00';
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return `${m}:${s < 10 ? '0' + s : s}`;
  };

  const displayName = identity.label;
  const avatarText = isMe ? 'ME' : getMeetingSpeakerInitials(displayName);
  const avatarBg = getMeetingSpeakerTheme(identity).color;

  let turnEl = document.getElementById(`live-${turn.id}`);
  if (!turnEl) {
    turnEl = document.createElement('div');
    turnEl.id = `live-${turn.id}`;
    turnEl.dataset.startTime = turn.start_time != null ? String(turn.start_time) : "0";
    turnEl.className = `live-turn-item ${isMe ? 'is-me' : 'is-remote'}`;
    turnEl.style.borderLeftColor = avatarBg;

    const header = document.createElement('div');
    header.className = 'live-turn-header';

    const avatar = document.createElement('div');
    avatar.className = 'live-turn-avatar';
    avatar.textContent = avatarText;
    avatar.style.backgroundColor = avatarBg;

    const spkSpan = document.createElement('span');
    spkSpan.className = 'live-turn-speaker';
    spkSpan.textContent = displayName;

    const timeSpan = document.createElement('span');
    timeSpan.className = 'live-turn-time';
    timeSpan.textContent = formatOffset(turn.start_time);

    header.appendChild(avatar);
    header.appendChild(spkSpan);
    header.appendChild(timeSpan);
    turnEl.appendChild(header);

    const textEl = document.createElement('div');
    textEl.className = 'live-turn-text';
    turnEl.appendChild(textEl);

    // Insert in chronological order by start_time
    const existingItems = Array.from(turnsList.querySelectorAll('.live-turn-item'));
    const turnStart = parseFloat(turn.start_time || 0);
    const nextItem = existingItems.find(item => {
      const itemStart = parseFloat(item.dataset.startTime || 0);
      return itemStart > turnStart;
    });
    if (nextItem) {
      turnsList.insertBefore(turnEl, nextItem);
    } else {
      turnsList.appendChild(turnEl);
    }
  } else {
    // If start_time was updated
    if (turn.start_time != null) {
      turnEl.dataset.startTime = String(turn.start_time);
    }
    // Dynamically update speaker name, avatar, and border if refined by Sortformer diarization
    turnEl.className = `live-turn-item ${isMe ? 'is-me' : 'is-remote'}`;
    turnEl.style.borderLeftColor = avatarBg;
    const avatar = turnEl.querySelector('.live-turn-avatar');
    if (avatar) {
      avatar.textContent = avatarText;
      avatar.style.backgroundColor = avatarBg;
    }
    const spkSpan = turnEl.querySelector('.live-turn-speaker');
    if (spkSpan) {
      spkSpan.textContent = displayName;
    }
    const timeSpan = turnEl.querySelector('.live-turn-time');
    if (timeSpan && turn.start_time != null) {
      timeSpan.textContent = formatOffset(turn.start_time);
    }
  }

  const textEl = turnEl.querySelector('.live-turn-text');
  if (textEl) {
    textEl.textContent = turn.text;
    if (!turn.is_final) {
      textEl.classList.add('interim');
    } else {
      textEl.classList.remove('interim');
    }
  }

  // Smooth autoscroll down
  turnsList.scrollTop = turnsList.scrollHeight;
}

function rehydrateActiveMeeting(activeMeeting) {
  if (!activeMeeting || !activeMeeting.is_recording) return;
  console.log("[MEETING RESTORE] Rehydrating active meeting session:");
  currentMeetingFile = activeMeeting.filepath;

  // 1. Switch to Meetings tab
  tabItems.forEach(nav => nav.classList.remove('active'));
  tabPanes.forEach(pane => pane.classList.remove('active'));
  const meetingsTabNav = document.querySelector('.nav-item[data-tab="meetings"]');
  const meetingsTabPane = document.getElementById('tab-meetings');
  if (meetingsTabNav) meetingsTabNav.classList.add('active');
  if (meetingsTabPane) meetingsTabPane.classList.add('active');

  // 2. Switch to Recording view
  showRecordingView();

  // 3. Restore title if set
  const titleInput = document.getElementById('meeting-title-input');
  if (titleInput) {
    if (activeMeeting.title && activeMeeting.title !== 'Recording...') {
      titleInput.value = activeMeeting.title;
    } else {
      titleInput.value = '';
    }
  }

  // 4. Restore timer to elapsed seconds
  meetingSeconds = Math.floor(activeMeeting.elapsed_seconds || 0);
  const timerEl = document.getElementById('meetings-timer');
  if (timerEl) {
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }

  // 5. Update status badge
  const badge = document.getElementById('meetings-live-status-badge');
  if (badge) {
    badge.textContent = "Recording locally";
    badge.style.color = "#57bf7c";
  }

  // 6. Restore turns list
  const turnsList = document.getElementById('meetings-live-turns-list');
  if (turnsList) {
    turnsList.innerHTML = '';
    if (activeMeeting.turns && activeMeeting.turns.length > 0) {
      activeMeeting.turns.forEach(turn => {
        handleLiveMeetingTurn(turn);
      });
    } else {
      turnsList.innerHTML = `
        <div id="meetings-live-waiting-text" class="live-waiting-text">
          Recording microphone and system audio. The live transcript appears here as people speak; speakers are identified after the meeting ends.
        </div>
      `;
    }
  }

  // 7. Restart intervals (timer clock + audio level polling)
  clearInterval(timerInterval);
  clearInterval(recordingInterval);

  timerInterval = setInterval(() => {
    meetingSeconds++;
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    if (timerEl) timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }, 1000);

  // Level meters are driven by 'meeting-levels' events from the desktop process.
}

// The New Meeting setup page. The meters run from a level-only preview of the
// meeting's own captures; nothing is recorded until Start meeting.
let testSoundPeak = null; // loopback peak while the test sound plays
let meetingSetupOpen = false;

function setupMeetingSetupPage() {
  const form = document.getElementById('meeting-setup-form');
  const name = document.getElementById('meeting-setup-name');
  const description = document.getElementById('meeting-setup-description');
  const count = document.getElementById('meeting-setup-description-count');
  const start = document.getElementById('meeting-setup-start');
  const cancel = document.getElementById('meeting-setup-cancel');
  const testSound = document.getElementById('meeting-setup-test-sound');
  if (!form || !name || !description || !start || !cancel || !testSound) return;
  name.addEventListener('input', () => { start.disabled = !name.value.trim(); });
  description.addEventListener('input', () => {
    if (count) count.textContent = `${description.value.length} / 2000`;
  });
  cancel.addEventListener('click', () => { showDashboardView(); loadMeetings(); });
  testSound.addEventListener('click', playMeetingTestSound);
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    if (!start.disabled) startMeetingRecording(name.value.trim(), description.value.trim());
  });
  // Leaving the Meetings tab closes the devices too.
  document.querySelectorAll('.nav-item[data-tab]').forEach(item => {
    item.addEventListener('click', () => {
      if (item.getAttribute('data-tab') !== 'meetings') leaveMeetingSetup();
    });
  });
}

function setMeetingSetupError(message) {
  const error = document.getElementById('meeting-setup-error');
  if (!error) return;
  error.textContent = message || '';
  error.hidden = !message;
}

async function openMeetingSetup({ keepFields = false } = {}) {
  if (!currentConfig?.signed_in) {
    showToast("Please sign in to record meeting notes.", "Sign-in Required", "info");
    return;
  }
  const name = document.getElementById('meeting-setup-name');
  const description = document.getElementById('meeting-setup-description');
  const count = document.getElementById('meeting-setup-description-count');
  const start = document.getElementById('meeting-setup-start');
  const status = document.getElementById('meeting-setup-sound-status');
  if (!keepFields) {
    name.value = '';
    description.value = '';
    if (count) count.textContent = '0 / 2000';
    setMeetingSetupError('');
  }
  start.disabled = !name.value.trim();
  if (status) status.textContent = '';
  showSetupView();
  meetingSetupOpen = true;
  name.focus();
  try {
    const devices = await invoke('start_meeting_preview');
    if (!meetingSetupOpen) { invoke('stop_meeting_preview').catch(() => {}); return; }
    if (devices && !devices.system_audio && status) {
      status.textContent = devices.system_audio_error || "System audio isn't being captured, so other participants won't be recorded.";
    }
  } catch (err) {
    if (meetingSetupOpen) setMeetingSetupError(String(err));
  }
}

// Stops the preview whenever the setup page is left, however it is left.
function leaveMeetingSetup() {
  const view = document.getElementById('meetings-setup-view');
  if (view) view.classList.add('hidden');
  if (!meetingSetupOpen) return;
  meetingSetupOpen = false;
  testSoundPeak = null;
  invoke('stop_meeting_preview').catch(() => {});
}

// Plays the chime and reports whether the system audio meter heard it.
async function playMeetingTestSound() {
  const button = document.getElementById('meeting-setup-test-sound');
  const status = document.getElementById('meeting-setup-sound-status');
  button.disabled = true;
  if (status) status.textContent = 'Playing…';
  testSoundPeak = 0;
  try {
    await invoke('play_meeting_test_sound');
    await new Promise(resolve => setTimeout(resolve, 300)); // let the last levels arrive
    if (!status) return;
    if (testSoundPeak > 20) {
      status.textContent = 'System audio is being captured.';
    } else {
      status.textContent = /Mac/i.test(navigator.platform)
        ? "System audio wasn't captured. Allow SpeekTo.Me in System Settings → Privacy & Security → Screen & System Audio Recording."
        : "System audio wasn't captured. Check that sound plays from your usual output device.";
    }
  } catch (err) {
    if (status) status.textContent = String(err);
  } finally {
    testSoundPeak = null;
    button.disabled = false;
  }
}

async function startMeetingRecording(title, description) {
  const startButton = document.getElementById('meeting-setup-start');
  if (startButton) startButton.disabled = true;
  setMeetingSetupError('');
  showRecordingView(); // also closes the preview, freeing the devices
  const titleInput = document.getElementById('meeting-title-input');
  titleInput.value = title;

  const turnsList = document.getElementById('meetings-live-turns-list');
  if (turnsList) {
    turnsList.innerHTML = `
      <div id="meetings-live-waiting-text" class="live-waiting-text">
        Recording microphone and system audio. The live transcript appears here as people speak; speakers are identified after the meeting ends.
      </div>
    `;
  }
  const badge = document.getElementById('meetings-live-status-badge');
  if (badge) {
    badge.textContent = "Starting recording...";
    badge.style.color = "#888";
  }

  const timerEl = document.getElementById('meetings-timer');
  timerEl.textContent = '00:00:00';
  meetingSeconds = 0;

  try {
    currentMeetingFile = await invoke('start_meeting', { title, description: description || null });
    if (badge) { badge.textContent="Recording locally"; badge.style.color="#4ec9b0"; }
  } catch (err) {
    console.error("[MEETING FRONTEND ERROR] Failed to start meeting recording:");
    // Clear anything half-started, then return to the setup page with the
    // name and description still filled in.
    try { await invoke('cancel_meeting'); } catch (_) {}
    currentMeetingFile = null;
    await openMeetingSetup({ keepFields: true });
    setMeetingSetupError(`The meeting couldn't start: ${err}`);
    return;
  }

  timerInterval = setInterval(() => {
    meetingSeconds++;
    const hrs = String(Math.floor(meetingSeconds / 3600)).padStart(2, '0');
    const mins = String(Math.floor((meetingSeconds % 3600) / 60)).padStart(2, '0');
    const secs = String(meetingSeconds % 60).padStart(2, '0');
    timerEl.textContent = `${hrs}:${mins}:${secs}`;
  }, 1000);

  // Level meters are driven by 'meeting-levels' events from the desktop process.
}

async function stopMeetingRecording() {
  console.log("[MEETING FRONTEND] stopMeetingRecording clicked");
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  
  showProcessingView();
  
  const titleInput = document.getElementById('meeting-title-input');
  let title = titleInput.value.trim();
  if (!title) {
    title = `Meeting on ${new Date().toLocaleDateString()} ${new Date().toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}`;
  }
  console.log("[MEETING FRONTEND] Stopping meeting with title:");

  let recordingSaved = false;
  try {
    console.log("[MEETING FRONTEND] Invoking stop_meeting command on Tauri backend...");
    const res = await invoke('stop_meeting', {title});
    recordingSaved = true;
    currentMeetingFile = null;
    console.log("[MEETING FRONTEND] stop_meeting returned:");
    let m = null;
    try {
      m = JSON.parse(res);
      console.log("[MEETING FRONTEND] Parsed meeting object from stop_meeting:");
    } catch (_) {
      console.log("[MEETING FRONTEND] stop_meeting returned audio filepath. Uploading audio to server via upload_meeting...");
      const resultJson = await invoke('upload_meeting', { title, filepath: res });
      console.log("[MEETING FRONTEND] upload_meeting returned:");
      m = JSON.parse(resultJson);
    }

    if (m && ['queued','processing','analyzing'].includes(m.status)) {
      showDashboardView();
      loadMeetings();
      await showAlertDialog({title:"Meeting uploaded",message:"Your meeting is queued for transcription and speaker identification. You can close the app; results will be saved to your meeting history.",type:"info"});
    } else if (m && m.id) {
      console.log("[MEETING FRONTEND] Successfully finalized meeting note! ID:");
      showDashboardView();
      loadMeetings();
      viewMeeting(m.id);
    } else {
      throw new Error("Invalid meeting response from server");
    }
  } catch (err) {
    console.error("[MEETING FRONTEND ERROR] Failed to finalize meeting note:");
    currentMeetingFile = null;
    if (recordingSaved) {
    showToast("Your recording is saved on this device and will upload automatically when available. " + String(err), "Recording saved; upload pending", "warning");
    } else {
      showToast("Could not finish saving the recording: " + String(err), "Recording save problem", "error");
    }
    showDashboardView();
    loadMeetings();
  }
}

async function cancelMeetingRecording() {
  console.log("[MEETING FRONTEND] cancelMeetingRecording called");
  clearInterval(timerInterval);
  clearInterval(recordingInterval);
  try {
    await invoke('cancel_meeting');
  } catch (e) {}
  currentMeetingFile = null;
  showDashboardView();
  loadMeetings();
}

async function confirmDeleteMeeting() {
  if (!activeMeetingId) return;
  const confirmed = await showConfirmDialog({
    title: "Delete Meeting Note?",
    message: "Are you sure you want to delete this meeting note? This action cannot be undone.",
    confirmText: "Delete",
    cancelText: "Cancel",
    isDanger: true
  });
  if (!confirmed) return;
  
  const token = currentConfig?.signed_in ? true : null;
  try {
    const res = await apiRequest('DELETE', `/meetings/${encodeURIComponent(activeMeetingId)}`);
    if (!res.ok) throw new Error("Failed to delete");
    activeMeetingId = null;
    showDashboardView();
    loadMeetings();
  } catch (err) {
    console.error("[diagnostic event: private values omitted]");
    await showAlertDialog({
      title: "Failed to Delete",
      message: "Failed to delete meeting note: " + err,
      type: "error"
    });
  }
}

async function appendPendingLocalMeetings() {
  try {
    const local = await invoke('list_local_meetings');
    const grid = document.getElementById('meetings-list'); if (!grid) return;
    grid.querySelectorAll('.local-pending-meeting').forEach(card => card.remove());
    const remote = new Set((window.allMeetings || []).map(meeting => meeting.client_id));
    for (const meeting of local) {
      if (remote.has(meeting.id) || !['recorded','uploading','failed','processing_failed'].includes(meeting.status)) continue;
      document.getElementById('meetings-empty-msg')?.classList.add('hidden'); grid.classList.remove('hidden'); grid.style.display='grid';
      const card=document.createElement('div'); card.className='meeting-card local-pending-meeting';
      const title=document.createElement('div'); title.className='meeting-card-title'; title.textContent=meeting.title || 'Local recording'; card.appendChild(title);
      const status=document.createElement('div'); status.textContent=meeting.status === 'uploading' ? 'Uploading audio' : 'Audio saved locally. Upload pending.'; card.appendChild(status);
      if (meeting.error) {const error=document.createElement('div'); error.textContent=meeting.error; card.appendChild(error);}
      const retry=document.createElement('button'); retry.type='button'; retry.className='fluent-button'; retry.textContent='Retry upload'; retry.disabled=meeting.status==='uploading';
      retry.addEventListener('click',async()=>{retry.disabled=true;try{await invoke('retry_upload_meeting',{filepath:meeting.filepath});await loadMeetings(true);}catch(error){showToast(String(error),'Upload pending','error');retry.disabled=false;}});
      card.appendChild(retry); grid.appendChild(card);
    }
  } catch(error) {console.warn('Cannot list pending local meeting audio');}
}
