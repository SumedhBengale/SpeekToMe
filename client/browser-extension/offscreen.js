let audioCtx = null;
let micStream = null;
let micSource = null;
let audioWorkletNode = null;
let monitorGain = null;
let pcmSamples = [];
let sampleRate = 48000;
const MAX_DICTATION_SAMPLES = 12_000_000;

// Meeting capture state
let meetingMixer = null;
let meetingRecorder = null;
let meetingChunks = [];
let capturedTabs = new Map(); // tabId -> { stream, sourceNode }

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.target !== 'offscreen') return false;

  // createDocument() can resolve before this script has finished loading.
  // Background waits for this synchronous response before sending work.
  if (message.action === 'ping') {
    sendResponse({ success: true, ready: true });
    return false;
  }

  // A microphone permission prompt can outlive the message request. Return a
  // synchronous acknowledgement and report the eventual result separately so
  // the response channel is never held open across getUserMedia().
  if (message.action === 'start_dictation') {
    sendResponse({ success: true, accepted: true });
    startDictation(message.requestId)
      .then(() => notifyDictationStatus(message.requestId, 'started'))
      .catch((err) => notifyDictationStatus(message.requestId, 'failed', err.message));
    return false;
  }

  if (message.action === 'stop_dictation') {
    sendResponse({ success: true, accepted: true });
    stopDictation()
      .then((result) => notifyDictationAudio(message.requestId, result))
      .catch((err) => notifyDictationStatus(message.requestId, 'failed', err.message));
    return false;
  }

  const handleMessage = async () => {
    switch (message.action) {
      case 'start_meeting':
        await startMeeting();
        return { success: true };

      case 'add_tab_stream':
        await addTabStream(message.tabId, message.streamId);
        return { success: true };

      case 'remove_tab_stream':
        await removeTabStream(message.tabId);
        return { success: true };

      case 'stop_meeting':
        await stopMeeting();
        return { success: true };
    }
  };

  handleMessage()
    .then((res) => sendResponse(res || { success: true }))
    .catch((err) => sendResponse({ success: false, error: err.message }));

  return true; // Keep message channel open for async response
});

function notifyBackground(message) {
  chrome.runtime.sendMessage(
    { target: 'background', ...message },
    () => {
      // This is a notification; explicitly consume any missing-response
      // lastError so Chrome does not report an unhandled message rejection.
      void chrome.runtime.lastError;
    }
  );
}

function notifyDictationStatus(requestId, status, error = null) {
  notifyBackground({
    action: 'offscreen_dictation_status',
    requestId,
    status,
    ...(error ? { error } : {})
  });
}

function notifyDictationAudio(requestId, result) {
  notifyBackground({
    action: 'dictation_audio_ready',
    requestId,
    success: true,
    audioBuffer: result.samples.buffer,
    sampleRate: result.sampleRate,
    channels: result.channels
  });
}

// ==========================================
// Dictation (Microphone) Recording Logic
// ==========================================
async function startDictation() {
  try {
    await startDictationCapture();
  } catch (err) {
    await cleanupDictation();
    throw err;
  }
}

async function startDictationCapture() {
  if (micStream || audioWorkletNode) {
    throw new Error('Dictation is already recording.');
  }

  pcmSamples = [];
  audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  sampleRate = audioCtx.sampleRate;

  micStream = await navigator.mediaDevices.getUserMedia({ audio: true });
  micSource = audioCtx.createMediaStreamSource(micStream);

  if (!audioCtx.audioWorklet) {
    throw new Error('AudioWorklet is unavailable in this browser.');
  }

  await audioCtx.audioWorklet.addModule(chrome.runtime.getURL('audio-worklet-processor.js'));
  audioWorkletNode = new AudioWorkletNode(audioCtx, 'dictation-processor');
  audioWorkletNode.port.onmessage = (event) => {
    const inputBuffer = new Float32Array(event.data);
    const remaining = MAX_DICTATION_SAMPLES - pcmSamples.length;
    for (let i = 0; i < inputBuffer.length && i < remaining; i++) {
      const s = Math.max(-1, Math.min(1, inputBuffer[i]));
      const i16 = s < 0 ? s * 0x8000 : s * 0x7FFF;
      pcmSamples.push(Math.round(i16));
    }
  };

  micSource.connect(audioWorkletNode);
  // The worklet needs an output connection to continue processing,
  // but dictation must never play the microphone back through the speakers.
  monitorGain = audioCtx.createGain();
  monitorGain.gain.value = 0;
  audioWorkletNode.connect(monitorGain);
  monitorGain.connect(audioCtx.destination);
}

async function cleanupDictation() {
  if (audioWorkletNode) {
    audioWorkletNode.disconnect();
  }
  if (monitorGain) {
    monitorGain.disconnect();
  }
  if (micSource) {
    micSource.disconnect();
  }
  if (micStream) {
    micStream.getTracks().forEach(track => track.stop());
  }

  const context = audioCtx;
  audioCtx = null;
  micStream = null;
  micSource = null;
  audioWorkletNode = null;
  monitorGain = null;
  pcmSamples = [];

  if (context) {
    await context.close();
  }
}

async function stopDictation() {
  if (!audioCtx && !micStream && !audioWorkletNode) {
    return { samples: new Int16Array(), sampleRate, channels: 1 };
  }

  const finalizedSamples = Int16Array.from(pcmSamples);
  const rate = sampleRate;
  await cleanupDictation();

  return {
    samples: finalizedSamples,
    sampleRate: rate,
    channels: 1
  };
}

// ==========================================
// Meeting (Multi-Tab) Capture Logic
// ==========================================
async function startMeeting() {
  meetingChunks = [];
  capturedTabs.clear();

  audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  meetingMixer = audioCtx.createGain();
  
  // Destination node to record from the mixer
  const destination = audioCtx.createMediaStreamDestination();
  meetingMixer.connect(destination);

  // Configure MediaRecorder to compress mixed streams
  let mimeType = 'audio/webm;codecs=opus';
  if (!MediaRecorder.isTypeSupported(mimeType)) {
    mimeType = 'audio/ogg;codecs=opus';
  }
  if (!MediaRecorder.isTypeSupported(mimeType)) {
    mimeType = 'audio/aac';
  }

  meetingRecorder = new MediaRecorder(destination.stream, { mimeType });
  meetingRecorder.ondataavailable = (event) => {
    if (event.data.size > 0) {
      meetingChunks.push(event.data);
    }
  };
  
  meetingRecorder.onstop = async () => {
    const blob = new Blob(meetingChunks, { type: mimeType });
    const arrayBuffer = await blob.arrayBuffer();
    const bytes = new Uint8Array(arrayBuffer);
    
    // Broadcast finalized meeting audio bytes to background worker
    chrome.runtime.sendMessage({
      target: 'background',
      action: 'meeting_audio_complete',
      bytes: Array.from(bytes),
      mimeType: mimeType
    });
  };

  meetingRecorder.start(250); // Slice recording every 250ms
}

async function addTabStream(tabId, streamId) {
  if (!audioCtx || !meetingMixer) return;
  
  // Stop existing stream if already captured
  if (capturedTabs.has(tabId)) {
    await removeTabStream(tabId);
  }

  // Retrieve captured tab media stream via chromeMediaSource ID
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: {
      mandatory: {
        chromeMediaSource: 'tab',
        chromeMediaSourceId: streamId
      }
    },
    video: false
  });

  const sourceNode = audioCtx.createMediaStreamSource(stream);
  sourceNode.connect(meetingMixer);
  
  // Crucial: keep the audio playing back to the speaker so the user can still hear it!
  sourceNode.connect(audioCtx.destination);

  capturedTabs.set(tabId, { stream, sourceNode });
  console.log(`[OFFSCREEN] Successfully started capturing tab ${tabId}`);
}

async function removeTabStream(tabId) {
  const item = capturedTabs.get(tabId);
  if (item) {
    item.sourceNode.disconnect();
    item.stream.getTracks().forEach(track => track.stop());
    capturedTabs.delete(tabId);
    console.log(`[OFFSCREEN] Disconnected capture stream for tab ${tabId}`);
  }
}

async function stopMeeting() {
  if (meetingRecorder && meetingRecorder.state !== 'inactive') {
    meetingRecorder.stop();
  }
  
  // Clean up all captured tabs
  for (const tabId of capturedTabs.keys()) {
    await removeTabStream(tabId);
  }

  if (audioCtx) {
    await audioCtx.close();
  }
}
