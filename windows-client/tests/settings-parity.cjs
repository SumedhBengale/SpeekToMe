// Run with jsdom available on NODE_PATH, as for meeting-speaker-names.cjs.
const { JSDOM } = require('jsdom');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const frontend = path.join(__dirname, '../frontend');

async function settingsFor(platform) {
  const dom = new JSDOM(fs.readFileSync(path.join(frontend, 'index.html'), 'utf8'), {
    url: 'https://test.invalid', runScripts: 'outside-only'
  });
  const w = dom.window;
  // Deliberately disagree with the native platform to catch UA-based detection.
  Object.defineProperty(w.navigator, 'platform', { value: platform === 'linux' ? 'Win32' : 'Linux' });
  Object.defineProperty(w.navigator, 'userAgent', { value: 'unrelated WebView' });
  const add = w.document.addEventListener.bind(w.document);
  w.document.addEventListener = (type, ...args) => { if (type !== 'DOMContentLoaded') add(type, ...args); };
  Object.defineProperty(w.document, 'readyState', { value: 'loading' });
  const calls = [];
  w.__TAURI__ = {
    core: { invoke: async (command, args) => {
      calls.push(command);
      if (command === 'save_config') calls.push({ config: JSON.parse(JSON.stringify(args.config)) });
      if (command === 'get_desktop_platform') return platform;
      if (command === 'get_linux_capabilities') return { shortcut: true, insertion: false };
      if (command === 'check_hotkey_conflicts') return { conflicts: [], verified: true };
      return null;
    } },
    event: { listen: async () => () => {} }
  };
  w.setInterval = () => 0;
  w.eval(fs.readFileSync(path.join(frontend, 'main.js'), 'utf8') + `
    window.settingsTest = {
      configureDesktopSettings, loadLinuxCapabilityStatus, applyConfigToUI,
      gatherConfigFromUI, checkIfConfigDirty,
      load(config) { currentConfig = config; applyConfigToUI(config); }
    };
  `);
  await w.settingsTest.configureDesktopSettings();
  const d = w.document;
  assert.equal(d.documentElement.dataset.platform, platform);
  const keyLabel = platform === 'linux' ? 'Meta' : 'Windows';
  for (const id of ['hotkey-select', 'onboard-hotkey-select']) {
    assert.equal(d.getElementById(id).options[0].textContent, `Ctrl + ${keyLabel} (Recommended)`);
    assert.equal(d.getElementById(id).options[0].value, 'Ctrl + Win');
  }
  await w.settingsTest.loadLinuxCapabilityStatus();
  assert.equal(calls.includes('get_linux_capabilities'), platform === 'linux');
  assert.equal(d.getElementById('linux-capability-status').hidden, platform !== 'linux');
  const config = {
    languages: ['en', 'hi'], active_language: 'hi', api_url: 'https://test.invalid',
    session_token: 'fixture-only', hotkey: 'Ctrl + Shift + K', hotkey_mode: 'toggle',
    autostart: true, typing_mode: true, sound_feedback: false,
    model: 'whisper-large-v3-turbo', enhance: false, custom_prompt: 'Keep this prompt.',
    transcription_mode: 'stream', command_mode: false, theme: 'forest',
    pill_position_mode: 'custom', pill_predefined_position: 'Top Center',
    pill_custom_x: -20, pill_custom_y: 0, pill_bg_color: 0xFAF9FF, pill_wave_color: 0x120A24
  };
  w.settingsTest.load(config);
  const plain = value => JSON.parse(JSON.stringify(value));
  const normal = plain(w.settingsTest.gatherConfigFromUI(false));
  const onboarding = plain(w.settingsTest.gatherConfigFromUI(true));
  assert.deepEqual(normal, config, `${platform}: settings must round-trip without losing fields or y=0`);
  assert.deepEqual(onboarding, normal, `${platform}: onboarding and settings must save the same schema`);
  assert.equal(w.settingsTest.checkIfConfigDirty(config, normal), false);
  // Every shared control must affect the same field on either screen.
  for (const prefix of ['', 'onboard-']) {
    d.getElementById(prefix + 'hotkey-mode-select').value = 'hold';
    d.getElementById(prefix + 'theme-select').value = 'classic_dark';
    for (const id of ['autostart-check', 'typing-mode-check', 'streaming-mode-check']) {
      d.getElementById(prefix + id).checked = false;
    }
    d.getElementById(prefix + 'sound-feedback-check').checked = true;
  }
  const changed = plain(w.settingsTest.gatherConfigFromUI(false));
  assert.deepEqual(changed, plain(w.settingsTest.gatherConfigFromUI(true)));
  assert.equal(changed.hotkey_mode, 'hold');
  assert.equal(changed.autostart, false);
  assert.equal(changed.typing_mode, false);
  assert.equal(changed.sound_feedback, true);
  assert.equal(changed.transcription_mode, 'batch');
  assert.equal(changed.enhance, false);
  assert.equal(changed.custom_prompt, config.custom_prompt);
  assert.equal(w.settingsTest.checkIfConfigDirty(config, changed), true);
  // Invalid coordinate input retains the saved coordinate rather than moving the overlay.
  d.getElementById('pill-custom-x').value = '';
  assert.equal(w.settingsTest.gatherConfigFromUI(false).pill_custom_x, -20);
  await w.saveAndApply();
  assert.deepEqual(calls.find(call => typeof call === 'object').config, changed,
    `${platform}: Save & Apply must send the same complete payload to Rust`);
  assert.equal(d.getElementById('save-btn').disabled, false);
  const controls = [...d.querySelectorAll('input,select,button')].map(el => el.id);
  dom.window.close();
  return { normal, changed, controls };
}

(async () => {
  const windows = await settingsFor('windows');
  const linux = await settingsFor('linux');
  assert.deepEqual(linux, windows, 'Both native platforms must expose the same controls and saved settings');
  console.log('Windows/Linux settings parity, onboarding, native platform labels, permissions and round-trip checks passed.');
})().catch(error => { console.error(error); process.exitCode = 1; });
