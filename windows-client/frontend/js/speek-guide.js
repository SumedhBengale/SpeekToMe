// Speek guides first-run setup: an animated host beside a speech bubble that
// follows what the setup page shows. It only reads the page and toggles the
// existing controls, so setup works unchanged if this module fails to load.
// See docs/desktop-onboarding.md.
import { createSpeekMascot, SPEEK_COLORS } from './vendor/speek.js';

const root = document.getElementById('onboarding');
const card = root?.querySelector('.onboarding-card');
const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
const invoke = (command, args) => window.__TAURI__?.core?.invoke(command, args);
const TYPE_MS = 22;

// Yes/no steps answer with two replies bound to the step's existing checkbox.
const REPLIES = {
  5: { check: 'onboard-typing-mode-check', on: 'Type it out key by key', off: 'Paste it all at once', offFirst: true },
  6: { check: 'onboard-command-mode-check', on: 'Yes, take orders', off: 'Not now' },
  7: { check: 'onboard-streaming-mode-check', on: 'Show my words live', off: 'After I stop talking' },
  8: { check: 'onboard-autostart-check', on: 'Yes, start with my computer', off: 'I’ll open it myself' }
};

const byId = id => document.getElementById(id);
const visible = el => Boolean(el) && !el.classList.contains('hidden') && !el.hidden && el.style.display !== 'none';
const name = (text, cls = 'speek-name') => ({ text, cls });

function context() {
  const step = Number(root.querySelector('.onboarding-step.active')?.id.replace('onboard-step-', '')) || 1;
  const keys = [...(byId('onboard-shortcut-keys')?.querySelectorAll('kbd') || [])].map(key => key.textContent);
  const first = (byId('onboard-user-name')?.textContent || '').trim().split(/\s+/)[0];
  return {
    step,
    shortcut: keys.join(' + ') || 'your shortcut',
    hold: byId('onboard-hotkey-mode-select')?.value !== 'toggle',
    pro: !byId('onboard-command-mode-check')?.disabled,
    first: first && first !== 'User' ? first : '',
    // core.js sets the platform flags; module code sees its top-level `let`s.
    mac: typeof IS_MAC_CLIENT !== 'undefined' && IS_MAC_CLIENT
  };
}

// What Speek says and how it feels about the page as it is right now.
const answered = new Set();
let shortcutSeen = '';
function scene() {
  const c = context();
  const press = `${c.hold ? 'hold' : 'tap'} ${c.shortcut}`;
  switch (c.step) {
    case 1: {
      const error = byId('onboard-auth-error');
      if (visible(byId('onboard-auth-logging-in'))) return { key: '1-wait', mood: 'think', parts: ['I opened your browser. Approve the sign-in there, and I’ll be right here.'] };
      if (visible(byId('onboard-auth-logged-in'))) return { key: `1-in-${c.first}`, mood: 'happy', parts: [c.first ? `Nice to meet you, ${c.first}! ` : 'You’re signed in! ', 'Ready to set things up?'] };
      if (visible(error) && error.textContent.trim()) return { key: '1-error', mood: 'oops', parts: ['Hmm, that didn’t work. Check the details and try again?'] };
      return { key: '1-out', mood: 'idle', parts: ['I turn what you say into text, anywhere you type. Let’s sign you in first.'] };
    }
    case 2:
      if (c.pro) return { key: '2-pro', mood: 'happy', parts: ['You’re on Pro, so all my tricks are ready. Let’s keep going!'] };
      return { key: '2-free', mood: 'admire', parts: ['You’re on the Free plan, with an hour of dictation a month. Pro unlocks my best tricks: live typing, Command Mode and meetings.'] };
    case 3: {
      const rows = [...byId('onboard-access-status').querySelectorAll('.setup-check')];
      const ready = rows.map(row => row.classList.contains('is-ready'));
      const testing = byId('onboard-mic-test')?.disabled;
      const result = byId('onboard-mic-result')?.textContent || '';
      const missing = [!ready[0] && 'use your shortcut', !ready[1] && 'type into your apps'].filter(Boolean);
      if (testing) return { key: '3-test', mood: 'listen', poll: true, parts: ['Say something! I’m listening. Nothing is saved or sent.'] };
      if (rows.length && ready.every(Boolean)) return { key: '3-ready', mood: 'happy', parts: ['All set! I can hear you, and I can type for you.'] };
      if (/little sound/.test(result)) return { key: '3-quiet', mood: 'oops', parts: ['That was very quiet. Is the right microphone selected?'] };
      if (/could not|No default/i.test(result)) return { key: '3-mic-error', mood: 'oops', parts: ['I couldn’t open your microphone. Check your device and privacy settings.'] };
      if (/sound detected/.test(result)) return { key: `3-heard-${missing}`, mood: 'happy', parts: [`Got you loud and clear! Now I just need permission to ${missing.join(' and ')}.`] };
      return { key: '3-ask', mood: 'idle', parts: ['To hear you and type for you, I need a little access. Then let’s check your microphone.'] };
    }
    case 4: {
      if (byId('onboard-hotkey-record')?.classList.contains('is-recording')) return { key: '4-record', mood: 'surprised', parts: ['Press the keys you want me to listen for. Esc cancels.'] };
      if (byId('onboard-hotkey-record-hint')?.classList.contains('has-error')) return { key: '4-error', mood: 'oops', parts: ['Hmm, that one won’t work. Try another?'] };
      if (answered.has('4-gesture')) return { key: `4-gesture-${c.hold}`, mood: 'happy', parts: c.hold ? ['Hold to talk, like a walkie-talkie. Got it!'] : ['Tap to start, and tap again when you’re done. Got it!'] };
      if (shortcutSeen && shortcutSeen !== c.shortcut) return { key: `4-set-${c.shortcut}`, mood: 'happy', parts: [name(c.shortcut, 'speek-keys'), ' it is!'] };
      return { key: '4-ask', mood: 'idle', parts: ['How would you like to call me? Pick a shortcut, then choose how you’ll press it.'] };
    }
    case 5: {
      if (!answered.has(5)) return { key: '5-ask', mood: 'think', parts: ['When you’re done talking, should I paste it all at once, or type it out key by key?'] };
      const typing = byId('onboard-typing-mode-check').checked;
      return { key: `5-${typing}`, mood: 'happy', parts: [typing ? 'Typing it out, then.' : 'Pasting it is. Quick and clean.'] };
    }
    case 6: {
      if (!c.pro) return { key: '6-free', mood: 'idle', parts: ['Taking orders is a Pro trick. You can still dictate everything, so let’s leave it off.'] };
      if (!answered.has(6)) return { key: '6-ask', mood: 'think', parts: ['I can take orders too. Select some text and tell me what to do, like “make this shorter.”'] };
      const on = byId('onboard-command-mode-check').checked;
      return { key: `6-${on}`, mood: on ? 'happy' : 'idle', parts: [on ? 'Orders it is. I’m all ears.' : 'No problem. You can turn it on in Settings later.'] };
    }
    case 7: {
      if (!c.pro) return { key: '7-free', mood: 'idle', parts: ['On Free, your words arrive once you stop talking. Pro shows them live as you speak.'] };
      if (!answered.has(7)) return { key: '7-ask', mood: 'think', parts: ['Want to watch your words appear while you’re still talking?'] };
      const on = byId('onboard-streaming-mode-check').checked;
      return { key: `7-${on}`, mood: 'happy', parts: [on ? 'Live it is. Watch them roll in!' : 'Okay, I’ll hand them over when you’re done.'] };
    }
    case 8: {
      if (!answered.has(8)) return { key: '8-ask', mood: 'sleepy', parts: ['Should I wake up when your computer does?'] };
      const on = byId('onboard-autostart-check').checked;
      return { key: `8-${on}`, mood: on ? 'happy' : 'idle', parts: [on ? 'I’ll be up when you are.' : 'Okay, open me whenever you need me.'] };
    }
    case 9:
      return { key: '9', mood: 'listen', parts: ['Which languages do you speak? I can follow up to five, and I’ll listen for the starred one.'] };
    case 10: {
      const text = byId('onboard-practice-text')?.value.trim() || '';
      if (text) {
        const quote = text.length > 70 ? `${text.slice(0, 67).trimEnd()}…` : text;
        return { key: `10-said-${text}`, mood: 'admire', poll: true, parts: [name(`“${quote}”`, 'speek-quote'), ' Look at that!'] };
      }
      return { key: `10-${press}`, mood: 'listen', poll: true, parts: [`Your turn! Click the box, ${press}, and say anything.`] };
    }
    default:
      return { key: `11-${press}`, mood: 'happy', parts: [`We’re all set! I’ll wait in your ${c.mac ? 'menu bar' : 'system tray'}. Just ${press} whenever you want to talk.`] };
  }
}

// The stage: Speek, the bubble and a dot per step.
const stage = document.createElement('div');
stage.className = 'speek-stage';
stage.innerHTML = `
  <div class="speek-host"><svg class="speek-mascot" viewBox="0 0 1254 1254" aria-hidden="true"></svg></div>
  <div class="speek-bubble"><p class="speek-line" aria-hidden="true"></p><p class="speek-sr" aria-live="polite"></p></div>`;
const line = stage.querySelector('.speek-line');
const spoken = stage.querySelector('.speek-sr');
const bubble = stage.querySelector('.speek-bubble');
const dots = document.createElement('div');
dots.className = 'speek-dots';
dots.setAttribute('aria-hidden', 'true');

let mascot = null;
let level = 0;
let current = '';
let typing = null;

function render(parts, { animate = !motion.matches } = {}) {
  clearTimeout(typing);
  line.replaceChildren();
  spoken.textContent = parts.map(part => (typeof part === 'string' ? part : part.text)).join('');
  bubble.classList.remove('is-new');
  void bubble.offsetWidth;
  bubble.classList.add('is-new');
  const spans = parts.map(part => {
    const span = document.createElement('span');
    if (typeof part !== 'string') span.className = part.cls;
    line.append(span);
    return [span, typeof part === 'string' ? part : part.text];
  });
  if (!animate) {
    spans.forEach(([span, text]) => { span.textContent = text; });
    return Promise.resolve();
  }
  // Type the line out; a click on the bubble shows it all at once.
  return new Promise(resolve => {
    let index = 0, offset = 0;
    const finish = () => { clearTimeout(typing); spans.forEach(([span, text]) => { span.textContent = text; }); bubble.onclick = null; resolve(); };
    bubble.onclick = finish;
    const tick = () => {
      if (index >= spans.length) return finish();
      const [span, text] = spans[index];
      offset += 1;
      span.textContent = text.slice(0, offset);
      if (offset >= text.length) { index += 1; offset = 0; }
      typing = setTimeout(tick, TYPE_MS);
    };
    tick();
  });
}

function say({ key, mood, parts }) {
  if (key === current) return;
  current = key;
  mascot?.setMode(mood);
  render(parts);
}

let polling = null;
function poll(on) {
  if (on && !polling) {
    polling = setInterval(async () => {
      try { level = Number(await invoke('get_voice_level')) || 0; } catch { level = 0; }
    }, 60);
  } else if (!on && polling) {
    clearInterval(polling);
    polling = null;
    level = 0;
  }
}

function syncReplies() {
  for (const [step, reply] of Object.entries(REPLIES)) {
    const check = byId(reply.check);
    const group = byId(`speek-replies-${step}`);
    if (!check || !group) continue;
    for (const button of group.querySelectorAll('button')) {
      const value = button.dataset.value === 'on';
      const pressed = String(check.checked === value);
      if (button.getAttribute('aria-pressed') !== pressed) button.setAttribute('aria-pressed', pressed);
      if (button.disabled !== (check.disabled && value)) button.disabled = check.disabled && value;
    }
  }
}

function syncDots() {
  const { step } = context();
  if (dots.childElementCount !== 11) dots.replaceChildren(...Array.from({ length: 11 }, () => document.createElement('span')));
  [...dots.children].forEach((dot, index) => {
    dot.classList.toggle('is-current', index + 1 === step);
    dot.classList.toggle('is-done', index + 1 < step);
  });
}

let introRunning = false;
let scheduled = false;
function update() {
  scheduled = false;
  if (introRunning || root.classList.contains('hidden')) { poll(false); return; }
  const { step } = context();
  if (step === 4 && !shortcutSeen) shortcutSeen = context().shortcut;
  if (step !== 4) shortcutSeen = '';
  const now = scene();
  poll(Boolean(now.poll));
  syncReplies();
  syncDots();
  say(now);
}
// A microtask rather than an animation frame or timer: changes apply right
// after the page updates, even while the window is in the background.
function schedule() {
  if (!scheduled) { scheduled = true; queueMicrotask(update); }
}

// First meeting: Speek fades in, says hi, then turns "Speek To Me" into the app's name.
async function intro() {
  introRunning = true;
  let skipped = motion.matches;
  const wait = ms => (skipped ? Promise.resolve() : new Promise(resolve => setTimeout(resolve, ms)));
  const skip = () => { skipped = true; };
  stage.addEventListener('click', skip);
  root.classList.add('speek-intro');
  mascot.setMode('sleepy');
  await wait(650);
  mascot.setMode('surprised');
  root.classList.add('speek-intro-talk');
  await render(['Hi! I’m ', name('Speek')], { animate: !skipped });
  await wait(200);
  line.querySelector('.speek-name')?.classList.add('is-glowing');
  mascot.setMode('happy');
  await wait(1300);
  mascot.setMode('admire');
  render([name('You can ', 'speek-word speek-lead'), name('Speek', 'speek-word'), name('To', 'speek-word'), name('Me', 'speek-word speek-me')], { animate: false });
  line.classList.add('is-phrase');
  await wait(1500);
  line.classList.add('is-joined');
  mascot.setMode('happy');
  await wait(700);
  line.classList.add('is-brand');
  await wait(1400);
  line.classList.remove('is-phrase', 'is-joined', 'is-brand');
  stage.removeEventListener('click', skip);
  // Glide Speek and the bubble from the centre of the card to their places.
  const movers = [stage.querySelector('.speek-host'), bubble];
  const before = movers.map(el => el.getBoundingClientRect().top);
  root.classList.remove('speek-intro', 'speek-intro-talk');
  if (!skipped) {
    movers.forEach((el, index) => {
      const shift = before[index] - el.getBoundingClientRect().top;
      el.animate([{ transform: `translateY(${shift}px)` }, { transform: 'none' }], { duration: 450, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)' });
    });
  }
  introRunning = false;
  current = '';
  update();
}

function start() {
  if (!root || !card) return;
  try {
    mascot = createSpeekMascot(stage.querySelector('.speek-mascot'), {
      mode: 'idle', faceColor: SPEEK_COLORS.faceOnDark, followPointer: true, voice: () => level
    });
  } catch {
    return;
  }
  card.querySelector('.onboarding-header')?.after(stage);
  card.querySelector('.step-tracker')?.append(dots);
  root.classList.add('has-speek');

  for (const [step, reply] of Object.entries(REPLIES)) {
    const check = byId(reply.check);
    const choice = check?.closest('.setup-choice');
    if (!choice) continue;
    const group = document.createElement('div');
    group.className = 'speek-replies';
    group.id = `speek-replies-${step}`;
    group.setAttribute('role', 'group');
    group.setAttribute('aria-label', 'Your answer');
    const options = reply.offFirst ? ['off', 'on'] : ['on', 'off'];
    for (const value of options) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'speek-reply';
      button.dataset.value = value;
      button.textContent = reply[value];
      button.addEventListener('click', () => {
        if (check.disabled && value === 'on') return;
        answered.add(Number(step));
        if (check.checked !== (value === 'on')) {
          check.checked = value === 'on';
          check.dispatchEvent(new Event('change', { bubbles: true }));
        }
        schedule();
      });
      group.append(button);
    }
    choice.before(group);
  }
  root.addEventListener('change', event => {
    if (event.target?.name === 'onboard-gesture') answered.add('4-gesture');
    schedule();
  });
  root.addEventListener('input', schedule);

  // Changes to the guide's own nodes (bubble, dots, replies) never reschedule it.
  const ours = node => stage.contains(node) || dots.contains(node) || Boolean(node.closest?.('.speek-replies') || node.parentElement?.closest('.speek-replies'));
  new MutationObserver(records => {
    if (records.every(record => ours(record.target))) return;
    schedule();
  }).observe(root, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class', 'hidden', 'disabled', 'style'] });

  // The intro plays when setup opens on its first page; returning users skip it.
  let introduced = false;
  new MutationObserver(() => {
    if (root.classList.contains('hidden') || introduced) return;
    introduced = true;
    if (context().step === 1 && !visible(byId('onboard-auth-logged-in'))) intro();
    else update();
  }).observe(root, { attributes: true, attributeFilter: ['class'] });
  if (!root.classList.contains('hidden')) { introduced = true; context().step === 1 ? intro() : update(); }
}

start();
