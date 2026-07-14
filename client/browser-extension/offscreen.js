let audioCtx = null;
let micStream = null;
let scriptNode = null;
let pcmSamples = [];
let sampleRate = 48000;

// Meeting capture state
let meetingMixer = null;
let meetingRecorder = null;
let meetingChunks = [];
let capturedTabs = new Map(); // tabId -> { stream, sourceNode }

chrome.runtime.onMessage.addListener(async (message, sender, sendResponse) => {
  if (message.target !== 'offscreen') return;

  switch (message.action) {
    case 'start_dictation':
      try {
        await startDictation();
        sendResponse({ success: true });
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;

    case 'stop_dictation':
      try {
        const result = await stopDictation();
        sendResponse(result);
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;

    case 'start_meeting':
      try {
        await startMeeting();
        sendResponse({ success: true });
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;

    case 'add_tab_stream':
      try {
        await addTabStream(message.tabId, message.streamId);
        sendResponse({ success: true });
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;

    case 'remove_tab_stream':
      try {
        await removeTabStream(message.tabId);
        sendResponse({ success: true });
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;

    case 'stop_meeting':
      try {
        await stopMeeting();
        sendResponse({ success: true });
      } catch (err) {
        sendResponse({ success: false, error: err.message });
      }
      break;
  }
});

// ==========================================
// Dictation (Microphone) Recording Logic
// ==========================================
async function startDictation() {
  pcmSamples = [];
  audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  sampleRate = audioCtx.sampleRate;

  micStream = await navigator.mediaDevices.getUserMedia({ audio: true });
  const source = audioCtx.createMediaStreamSource(micStream);

  // ScriptProcessor collects Float32 samples from microphone in real-time
  scriptNode = audioCtx.createScriptProcessor(4096, 1, 1);
  scriptNode.onaudioprocess = (event) => {
    const inputBuffer = event.inputBuffer.getChannelData(0);
    // Convert Float32 to Int16 PCM
    for (let i = 0; i < inputBuffer.length; i++) {
      const s = Math.max(-1, Math.min(1, inputBuffer[i]));
      const i16 = s < 0 ? s * 0x8000 : s * 0x7FFF;
      pcmSamples.push(Math.round(i16));
    }
  };

  source.connect(scriptNode);
  scriptNode.connect(audioCtx.destination);
}

async function stopDictation() {
  if (scriptNode) {
    scriptNode.disconnect();
  }
  if (micStream) {
    micStream.getTracks().forEach(track => track.stop());
  }
  const finalizedSamples = pcmSamples;
  const rate = sampleRate;
  
  if (audioCtx) {
    await audioCtx.close();
  }
  
  return {
    success: true,
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
