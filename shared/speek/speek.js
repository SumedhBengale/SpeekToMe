// Speek, the mascot. One module for the website and the desktop client: a live
// animated rig (createSpeekMascot) and static markup for icons (speekSvg).
// No framework or build step needed. See docs/speek-mascot.md.

const NS = 'http://www.w3.org/2000/svg';
const PINK = '#FC5B90';
const CHEEK = '#FFA3C2';
export const SPEEK_COLORS = { body: PINK, cheek: CHEEK, faceOnDark: '#1C1520', faceOnLight: '#240a12' };
// The full box leaves room for hops, thought dots and raised ears; the icon box hugs the resting mascot.
export const SPEEK_VIEWBOX = '0 0 1254 1254';
export const SPEEK_ICON_VIEWBOX = '172 215 910 910';

// Face strokes are quadratic curves: [x1, y1, qx, qy, x2, y2, strokeWidth, opacity].
// A straight stroke with round caps is a pill, so eyes and mouth bars share the logo's shape.
const pill = (cx, cy, w, h, o = 1) => {
  const d = Math.max(0.5, (h - w) / 2);
  return [cx, cy - d, cx, cy, cx, cy + d, w, o];
};
const arc = (x1, y1, qx, qy, x2, y2, w, o = 1) => [x1, y1, qx, qy, x2, y2, w, o];
const hidden = (x, y) => pill(x, y, 30, 30, 0);

// Body pills: [cx, cy, width, height, rotation, radius].
const rect = (cx, cy, w, h, rot, rx) => [cx, cy, w, h, rot, rx];
// An ear pivots inside the head; `stretch` lengthens it upward from there.
const ear = (px, py, angle, stretch = 0) => {
  const r = angle * Math.PI / 180;
  const reach = 120 + stretch / 2;
  return rect(px + reach * Math.sin(r), py - reach * Math.cos(r), 190, 420 + stretch, angle, 95);
};
const HEAD = rect(627, 710, 760, 560, 0, 270);
const LOGO = [rect(313, 630, 98, 600, 0, 49), rect(470, 470, 98, 280, 0, 49), rect(627, 605, 98, 310, 0, 49), rect(784, 470, 98, 280, 0, 49), rect(941, 630, 98, 600, 0, 49)];

// Stroke slots: 0/4 cheeks, 1/3 eyes, 2 mouth, 5/6 brows, 7/8/12/13 spare mouth strokes, 9-11 thought dots.
const STROKES = 14;
const FACE_SLOTS = new Set([1, 2, 3, 5, 6, 7, 8, 12, 13]);
const DOTS = [9, 10, 11];
function face(parts) {
  const slots = [hidden(425, 800), hidden(497, 690), hidden(627, 840), hidden(757, 690), hidden(829, 800), hidden(497, 540), hidden(757, 540), hidden(587, 840), hidden(667, 840), hidden(1060, 600), hidden(1110, 500), hidden(1150, 390), hidden(547, 840), hidden(707, 840)];
  for (const slot in parts) slots[slot] = parts[slot];
  return slots;
}
const cheeks = (o, dx = 0) => ({ 0: arc(400 + dx, 800, 425 + dx, 800, 450 + dx, 800, 50, o), 4: arc(804 + dx, 800, 829 + dx, 800, 854 + dx, 800, 50, o) });

const FACES = {
  logo: face({}),
  idle: face({ ...cheeks(0.9), 1: pill(497, 690, 96, 200), 3: pill(757, 690, 96, 200), 2: arc(587, 830, 627, 862, 667, 830, 40) }),
  sleepy: face({ ...cheeks(0.6), 1: arc(447, 700, 497, 740, 547, 700, 30), 3: arc(707, 700, 757, 740, 807, 700, 30), 2: arc(602, 842, 627, 852, 652, 842, 34), 9: pill(900, 450, 30, 30), 10: pill(900, 450, 42, 42), 11: pill(900, 450, 54, 54) }),
  surprised: face({ 1: pill(497, 670, 124, 250), 3: pill(757, 670, 124, 250), 5: arc(442, 500, 497, 468, 552, 500, 32), 6: arc(702, 500, 757, 468, 812, 500, 32), 2: pill(627, 872, 72, 96) }),
  listen: face({ ...cheeks(0.9), 1: pill(497, 682, 100, 170), 3: pill(757, 682, 100, 170), 5: arc(447, 560, 497, 552, 547, 566, 30), 6: arc(707, 566, 757, 552, 807, 560, 30), 2: arc(604, 846, 627, 849, 650, 846, 36) }),
  think: face({ 1: pill(527, 660, 88, 180), 3: pill(787, 660, 88, 180), 5: arc(462, 535, 507, 540, 552, 545, 32), 6: arc(722, 500, 770, 462, 818, 480, 32), 2: arc(610, 852, 640, 846, 672, 834, 38), 9: pill(1060, 600, 40, 40), 10: pill(1110, 500, 56, 56), 11: pill(1150, 390, 72, 72) }),
  happy: face({ ...cheeks(1), 1: arc(437, 710, 497, 600, 557, 710, 46), 3: arc(697, 710, 757, 600, 817, 710, 46), 2: arc(537, 800, 627, 920, 717, 800, 48) }),
  oops: face({ 1: pill(502, 712, 88, 150), 3: pill(752, 712, 88, 150), 5: arc(432, 592, 482, 572, 537, 550, 32), 6: arc(717, 550, 772, 572, 822, 592, 32), 2: arc(567, 872, 627, 822, 687, 872, 40) }),
  admire: face({ ...cheeks(0.9, 25), 1: pill(552, 690, 96, 190), 3: pill(812, 690, 96, 190), 2: arc(612, 830, 652, 866, 692, 830, 40) })
};

// Body pose: [left ear angle, right ear angle, ear lift, squash, head tilt].
const POSES = {
  logo: [0, 0, 0, 1, 0], idle: [10, 10, 0, 1, 0], sleepy: [52, 52, 30, 0.97, 6], surprised: [-4, -4, -70, 1.05, 0],
  listen: [4, 4, 0, 1, 2], think: [46, 8, 0, 1, -4], happy: [22, 22, -10, 1, 0], oops: [70, 70, 40, 0.98, 0], admire: [14, 6, 0, 1, 4]
};
const TAIL_LENGTH = { logo: 0, idle: 1, sleepy: 0.85, surprised: 1.15, listen: 1, think: 0.95, happy: 1.05, oops: 0.55, admire: 1 };
export const SPEEK_MODES = Object.keys(FACES);

// While listening the ears follow the voice level (0-1), each on its own
// rhythm so they flop independently. Two sines per ear, rates in 1/seconds.
const EAR_RHYTHMS = [[7.3, 11.9, 0.4], [8.4, 6.9, 5.2]];
const earRhythm = (side, time) => {
  const [a, b, phase] = EAR_RHYTHMS[side];
  const seconds = time / 1000;
  return 0.6 + 0.4 * (0.6 * Math.sin(seconds * a + phase) + 0.4 * Math.sin(seconds * b + phase * 1.7));
};
// The web has no microphone: a speech-like level for demos.
export const simulatedVoice = time => Math.abs(Math.sin(time / 180) * Math.sin(time / 470));

// The M <-> mascot morph is a fast staged tween: [delay, duration] in ms per part.
const BODY_IN = [[70, 170], [15, 140], [0, 150], [15, 140], [70, 170]];
const BODY_OUT = [[30, 140], [60, 140], [50, 150], [60, 140], [30, 140]];
const faceIn = slot => [slot === 1 || slot === 3 ? 140 : slot === 2 ? 165 : 185, 150];
const FACE_OUT = [0, 80];
const TAIL_IN = [130, 190];
const TAIL_OUT = [0, 90];
const easeOut = p => 1 - (1 - p) ** 3;
const easeBack = p => 1 + 2.9 * (p - 1) ** 3 + 1.9 * (p - 1) ** 2;
const easeInOut = p => (p < 0.5 ? 4 * p ** 3 : 1 - (-2 * p + 2) ** 3 / 2);
const progress = (elapsed, [delay, duration], ease) => ease(Math.min(1, Math.max(0, (elapsed - delay) / duration)));

const TAIL_BASE = [[430, 930], [565, 965]];
const TAIL_PIVOT = [497.5, 947.5];
const TAIL_TIP = [-132.5, 147.5];
function tailPath(length, angle) {
  const r = angle * Math.PI / 180;
  const x = TAIL_PIVOT[0] + (TAIL_TIP[0] * Math.cos(r) - TAIL_TIP[1] * Math.sin(r)) * length;
  const y = TAIL_PIVOT[1] + (TAIL_TIP[0] * Math.sin(r) + TAIL_TIP[1] * Math.cos(r)) * length;
  return `M${TAIL_BASE[0][0]} ${TAIL_BASE[0][1]}L${TAIL_BASE[1][0]} ${TAIL_BASE[1][1]}L${x} ${y}Z`;
}

const shift = (stroke, dx, dy) => {
  stroke[0] += dx; stroke[2] += dx; stroke[4] += dx;
  stroke[1] += dy; stroke[3] += dy; stroke[5] += dy;
};
const copy = rows => rows.map(row => row.slice());
const BLINKING = new Set(['idle', 'think', 'listen', 'oops', 'surprised', 'admire']);
const restingBody = mode => {
  if (mode === 'logo') return LOGO;
  const [left, right, lift] = POSES[mode];
  return [ear(407, 560 + lift, -left), HEAD, HEAD, HEAD, ear(847, 560 + lift, right)];
};
const groupTransform = (x, y, tilt, squash) => `translate(${x} ${y}) rotate(${tilt} 627 990) translate(627 990) scale(${2 - squash} ${squash}) translate(-627 -990)`;
const pillAttributes = ([cx, cy, w, h, rotation, radius]) => {
  const width = Math.max(0, w), height = Math.max(0, h);
  return { x: -width / 2, y: -height / 2, width, height, rx: Math.max(0, Math.min(radius, width / 2, height / 2)), transform: `translate(${cx} ${cy}) rotate(${rotation})` };
};
const strokePath = s => `M${s[0]} ${s[1]}Q${s[2]} ${s[3]} ${s[4]} ${s[5]}`;
const strokeColor = (slot, faceColor) => (FACE_SLOTS.has(slot) ? faceColor : DOTS.includes(slot) ? PINK : CHEEK);
// Long float artifacts only; short factors such as a 0.95 squash stay exact.
const round = value => Math.round(value * 100) / 100;

// Static markup of a mood's resting pose, for favicons, app icons and <img> tags.
export function speekSvg({ mode = 'idle', faceColor = SPEEK_COLORS.faceOnDark, viewBox = SPEEK_ICON_VIEWBOX, size } = {}) {
  if (!SPEEK_MODES.includes(mode)) throw new Error(`Unknown Speek mode: ${mode}`);
  const attributes = values => Object.entries(values).map(([name, value]) => `${name}="${value}"`).join(' ');
  const body = restingBody(mode);
  // The mascot's three inner pills overlap exactly as the head, so draw it once.
  const inner = mode === 'logo' ? [1, 3, 2] : [2];
  const parts = [
    ...[0, 4].map(part => `<rect fill="${PINK}" ${attributes(pillAttributes(body[part]))}/>`),
    TAIL_LENGTH[mode] > 0 ? `<path fill="${PINK}" stroke="${PINK}" stroke-width="50" stroke-linejoin="round" d="${tailPath(TAIL_LENGTH[mode], 0)}"/>` : '',
    ...inner.map(part => `<rect fill="${PINK}" ${attributes(pillAttributes(body[part]))}/>`),
    ...FACES[mode].map((stroke, slot) => [stroke, slot]).filter(([stroke]) => stroke[7] > 0).map(([stroke, slot]) =>
      `<path fill="none" stroke-linecap="round" stroke="${strokeColor(slot, faceColor)}" stroke-width="${stroke[6]}"${stroke[7] < 1 ? ` opacity="${stroke[7]}"` : ''} d="${strokePath(stroke)}"/>`)
  ].join('');
  const [, , , squash, tilt] = POSES[mode];
  const content = squash === 1 && tilt === 0 ? parts : `<g transform="${groupTransform(0, 0, tilt, squash)}">${parts}</g>`;
  const dimensions = size ? ` width="${size}" height="${size}"` : '';
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}"${dimensions}>${content}</svg>`.replace(/-?\d+\.\d{3,}/g, value => round(Number(value)));
}

export function createSpeekMascot(svg, { mode: initialMode = 'idle', faceColor = SPEEK_COLORS.faceOnDark, followPointer = false, voice = simulatedVoice } = {}) {
  const group = document.createElementNS(NS, 'g');
  svg.appendChild(group);
  const add = (tag, attributes) => {
    const element = document.createElementNS(NS, tag);
    for (const name in attributes) element.setAttribute(name, attributes[name]);
    group.appendChild(element);
    return element;
  };
  const pills = [];
  pills[0] = add('rect', { fill: PINK });
  pills[4] = add('rect', { fill: PINK });
  const tail = add('path', { fill: PINK, stroke: PINK, 'stroke-width': 50, 'stroke-linejoin': 'round' });
  pills[1] = add('rect', { fill: PINK });
  pills[3] = add('rect', { fill: PINK });
  pills[2] = add('rect', { fill: PINK });
  const strokes = Array.from({ length: STROKES }, (_, slot) => add('path', { fill: 'none', 'stroke-linecap': 'round', stroke: strokeColor(slot, faceColor) }));

  const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
  let mode = SPEEK_MODES.includes(initialMode) ? initialMode : 'idle';
  let modeStart = performance.now();
  let morph = null;
  let body = copy(restingBody(mode));
  let bodyVelocity = body.map(row => row.map(() => 0));
  let strokesNow = copy(FACES[mode]);
  let strokeVelocity = strokesNow.map(row => row.map(() => 0));
  const pose = POSES[mode].slice();
  let tailLength = TAIL_LENGTH[mode], tailLengthVelocity = 0, tailAngle = 0, tailAngleVelocity = 0;
  const look = { x: 0, y: 0 }, lookTarget = { x: 0, y: 0 };
  let pointerActive = false, previousHop = 0;
  let nextGlance = 0, nextBlink = 0, blinkAt = -1e9, nextTwitch = 0, twitchAt = -1e9, twitchEar = 0, nextFlick = 0, flickAt = -1e9;
  let animation = null, inView = true;

  function targets(time, elapsed) {
    const faceTarget = copy(FACES[mode]);
    const poseTarget = POSES[mode].slice();
    const stretch = [0, 0];
    const level = mode === 'listen' ? Math.max(0, Math.min(1, voice(time))) : 0;

    if (mode === 'idle' || mode === 'listen') {
      if (!morph && time > nextTwitch) { twitchAt = time; twitchEar = Math.random() < 0.5 ? 0 : 1; nextTwitch = time + 2500 + Math.random() * 3000; }
      if (time - twitchAt < 220) poseTarget[twitchEar] += Math.sin((time - twitchAt) / 220 * Math.PI) * 18;
    }
    if (mode === 'idle') {
      if (!pointerActive && time > nextGlance) {
        const glances = [[0, 0], [-1, 0], [1, 0], [0, -1], [0, 1], [-0.7, -0.6], [0.8, 0.5], [0, 0]];
        [lookTarget.x, lookTarget.y] = glances[Math.floor(Math.random() * glances.length)];
        nextGlance = time + 900 + Math.random() * 1600;
      }
      if (!morph && time > nextFlick) { flickAt = time; nextFlick = time + 3000 + Math.random() * 3500; }
      poseTarget[4] += look.x * 3;
    } else if (!pointerActive) {
      lookTarget.x = 0;
      lookTarget.y = 0;
    }
    look.x += (lookTarget.x - look.x) * 0.2;
    look.y += (lookTarget.y - look.y) * 0.2;

    if (mode === 'idle') {
      [1, 3].forEach(slot => shift(faceTarget[slot], look.x * 60, look.y * 40));
      shift(faceTarget[2], look.x * 30, look.y * 20);
      [0, 4].forEach(slot => shift(faceTarget[slot], look.x * 20, look.y * 12));
    }
    if (mode === 'listen') {
      // Concentrating: the ears carry the voice. Each stretches and flops on
      // its own rhythm; loud speech flares them out and widens the eyes.
      const flare = Math.max(0, (level - 0.6) / 0.4) * 10;
      [0, 1].forEach(side => {
        const share = level * earRhythm(side, time);
        stretch[side] = 110 * share;
        poseTarget[side] += 10 * share + flare;
      });
      const peak = level * level;
      [[1, 497], [3, 757]].forEach(([slot, x]) => { faceTarget[slot] = pill(x, 682, 100 + 16 * peak, 170 + 90 * peak); });
      [5, 6].forEach(slot => shift(faceTarget[slot], 0, -30 * peak));
      [1, 3].forEach(slot => shift(faceTarget[slot], look.x * 40, look.y * 28));
    }
    if (mode === 'think') {
      const drift = Math.sin(time / 700) * 22;
      [1, 3, 5, 6].forEach(slot => shift(faceTarget[slot], drift, 0));
    }
    if (mode === 'happy') {
      const wiggle = Math.sin(elapsed / 80) * 14 * Math.max(0, 1 - elapsed / 1200);
      poseTarget[0] += wiggle;
      poseTarget[1] -= wiggle;
    }
    if (mode === 'admire' && elapsed > 2000) {
      // Done reading: glance back at the viewer.
      [1, 3, 2].forEach(slot => shift(faceTarget[slot], slot === 2 ? -25 : -55, 0));
      [0, 4].forEach(slot => shift(faceTarget[slot], -25, 0));
      poseTarget[4] = 0;
    }

    let tailLengthTarget = TAIL_LENGTH[mode];
    let tailAngleTarget = 0;
    const fade = window => Math.max(0, 1 - elapsed / window);
    if (mode === 'idle') {
      tailAngleTarget = Math.sin(time / 900) * 6 - look.x * 8;
      if (time - flickAt < 320) tailAngleTarget += Math.sin((time - flickAt) / 320 * Math.PI * 2) * 22;
    }
    if (mode === 'listen') { tailLengthTarget = 1 + 0.22 * level; tailAngleTarget = Math.sin(time / 300) * 4 - level * 6; }
    if (mode === 'happy') tailAngleTarget = Math.sin(elapsed / 60) * 26 * Math.max(0.25, fade(1600));
    if (mode === 'sleepy') { tailAngleTarget = -20 + Math.sin(time / 1100) * 4; tailLengthTarget = 0.85 + 0.04 * Math.sin(time / 900); }
    if (mode === 'think') { const beat = (time % 700) / 700; tailAngleTarget = 6 - (beat < 0.18 ? Math.sin(beat / 0.18 * Math.PI) * 22 : 0); }
    if (mode === 'surprised') tailAngleTarget = Math.sin(elapsed / 28) * 12 * fade(500) + 14 * fade(250);
    if (mode === 'oops') { tailAngleTarget = 24 + Math.sin(elapsed / 35) * 8 * fade(600); tailLengthTarget = 0.55 + 0.05 * Math.sin(time / 180); }
    if (mode === 'admire') tailAngleTarget = Math.sin(time / 600) * 8;

    const bodyTarget = mode === 'logo' ? LOGO : [ear(407, 560 + poseTarget[2], -poseTarget[0], stretch[0]), HEAD, HEAD, HEAD, ear(847, 560 + poseTarget[2], poseTarget[1], stretch[1])];
    return { faceTarget, poseTarget, bodyTarget, tailLengthTarget, tailAngleTarget };
  }

  function step(time, snap) {
    const elapsed = time - modeStart;
    const { faceTarget, poseTarget, bodyTarget, tailLengthTarget, tailAngleTarget } = targets(time, elapsed);
    const fade = window => Math.max(0, 1 - elapsed / window);

    if (snap) {
      body = copy(bodyTarget);
      strokesNow = copy(faceTarget);
      poseTarget.forEach((value, index) => { pose[index] = value; });
      tailLength = tailLengthTarget;
      tailAngle = tailAngleTarget;
      morph = null;
    } else {
      let morphDone = true;
      for (let part = 0; part < 5; part += 1) {
        if (morph) {
          const entering = morph.direction === 'in';
          const timing = (entering ? BODY_IN : BODY_OUT)[part];
          const p = progress(elapsed, timing, entering ? (part === 0 || part === 4 ? easeBack : easeOut) : easeInOut);
          if (elapsed < timing[0] + timing[1]) morphDone = false;
          for (let k = 0; k < 6; k += 1) body[part][k] = morph.body[part][k] + (bodyTarget[part][k] - morph.body[part][k]) * p;
        } else {
          for (let k = 0; k < 6; k += 1) {
            bodyVelocity[part][k] = (bodyVelocity[part][k] + (bodyTarget[part][k] - body[part][k]) * 0.16) * 0.68;
            body[part][k] += bodyVelocity[part][k];
          }
        }
      }
      for (let slot = 0; slot < STROKES; slot += 1) {
        if (morph) {
          const timing = morph.direction === 'in' ? faceIn(slot) : FACE_OUT;
          const p = progress(elapsed, timing, easeOut);
          if (elapsed < timing[0] + timing[1]) morphDone = false;
          for (let k = 0; k < 8; k += 1) strokesNow[slot][k] = morph.face[slot][k] + (faceTarget[slot][k] - morph.face[slot][k]) * p;
        } else {
          for (let k = 0; k < 8; k += 1) {
            strokeVelocity[slot][k] = (strokeVelocity[slot][k] + (faceTarget[slot][k] - strokesNow[slot][k]) * 0.16) * 0.68;
            strokesNow[slot][k] += strokeVelocity[slot][k];
          }
        }
      }
      if (morph) {
        const timing = morph.direction === 'in' ? TAIL_IN : TAIL_OUT;
        if (elapsed < timing[0] + timing[1]) morphDone = false;
        tailLength = morph.tail + (tailLengthTarget - morph.tail) * progress(elapsed, timing, morph.direction === 'in' ? easeBack : easeOut);
      } else {
        tailLengthVelocity = (tailLengthVelocity + (tailLengthTarget - tailLength) * 0.2) * 0.65;
        tailLength += tailLengthVelocity;
      }
      if (morph && morphDone) morph = null;
      for (let k = 0; k < 5; k += 1) pose[k] += (poseTarget[k] - pose[k]) * 0.22;
    }

    let blink = 0;
    if (!snap && BLINKING.has(mode) && !morph) {
      if (time > nextBlink) { blinkAt = time; nextBlink = time + 2200 + Math.random() * 2600; }
      const since = time - blinkAt;
      if (since >= 0 && since < 200 && elapsed > 300) blink = 1 - Math.abs(since - 100) / 100;
    }

    let hopX = 0, hopY = 0, squash = pose[3];
    if (!snap && !morph) {
      if (mode === 'idle' || mode === 'listen') squash *= 1 + 0.012 * Math.sin(time / 600);
      if (mode === 'sleepy') squash *= 1 + 0.03 * Math.sin(time / 900);
      if (mode === 'happy') { hopY = -Math.abs(Math.sin(elapsed / 150)) * 45 * fade(950); squash *= 1 + 0.05 * Math.cos(elapsed / 75) * fade(950); }
      if (mode === 'surprised') hopY = -Math.abs(Math.sin(elapsed / 130)) * 70 * fade(400);
      if (mode === 'oops') hopX = Math.sin(elapsed / 35) * 16 * fade(600);
      if (mode === 'admire' && elapsed > 250 && elapsed < 650) hopY = Math.sin((elapsed - 250) / 400 * Math.PI) * 18;
    }
    if (!snap) {
      // The tail lags behind hops and shakes, then settles on its own spring.
      const swing = tailAngleTarget + (hopY - previousHop) * 1.2 - hopX * 0.4;
      previousHop = hopY;
      tailAngleVelocity = (tailAngleVelocity + (swing - tailAngle) * 0.14) * 0.8;
      tailAngle += tailAngleVelocity;
    }

    group.setAttribute('transform', groupTransform(hopX, hopY, pose[4], squash));
    tail.setAttribute('d', tailPath(Math.max(0, tailLength), tailAngle));
    tail.setAttribute('opacity', Math.max(0, Math.min(1, tailLength * 3)));
    body.forEach((shape, part) => {
      const values = pillAttributes(shape);
      for (const name in values) pills[part].setAttribute(name, values[name]);
    });
    strokes.forEach((element, slot) => {
      const stroke = strokesNow[slot].slice();
      if ((slot === 1 || slot === 3) && blink > 0) {
        const [x, y] = [stroke[2], stroke[3]];
        const closed = [x - 50, y, x, y + 10, x + 50, y, 24];
        for (let k = 0; k < 7; k += 1) stroke[k] += (closed[k] - stroke[k]) * blink;
      }
      let opacity = stroke[7];
      if (DOTS.includes(slot) && !snap) {
        const order = slot - DOTS[0];
        if (mode === 'think') opacity *= 0.25 + 0.75 * (Math.sin(time / 250 - order * 1.1) + 1) / 2;
        if (mode === 'sleepy') {
          const rise = (time / 2600 + order / 3) % 1;
          shift(stroke, rise * 40, -rise * 300);
          stroke[6] *= 0.6 + rise * 0.6;
          opacity *= Math.sin(rise * Math.PI);
        }
      }
      element.setAttribute('d', strokePath(stroke));
      element.setAttribute('stroke-width', Math.max(1, stroke[6]));
      element.setAttribute('opacity', Math.max(0, Math.min(1, opacity)));
    });
  }

  const frame = time => {
    step(time, false);
    animation = requestAnimationFrame(frame);
  };
  const sync = () => {
    if (inView && !document.hidden && !motion.matches) {
      if (animation == null) animation = requestAnimationFrame(frame);
    } else {
      cancelAnimationFrame(animation);
      animation = null;
      if (motion.matches) step(performance.now(), true);
    }
  };
  const onPointer = event => {
    const box = svg.getBoundingClientRect();
    if (!box.width) return;
    pointerActive = true;
    lookTarget.x = Math.max(-1, Math.min(1, ((event.clientX - box.left) / box.width - 0.5) * 1.6));
    lookTarget.y = Math.max(-1, Math.min(1, ((event.clientY - box.top) / box.height - 0.55) * 1.6));
  };
  const onPointerLeave = () => { pointerActive = false; };
  const observer = new IntersectionObserver(([entry]) => { inView = entry.isIntersecting; sync(); });
  observer.observe(svg);
  motion.addEventListener('change', sync);
  document.addEventListener('visibilitychange', sync);
  if (followPointer) {
    window.addEventListener('pointermove', onPointer, { passive: true });
    document.documentElement.addEventListener('pointerleave', onPointerLeave);
  }
  step(performance.now(), true);
  sync();

  return {
    setMode(next) {
      if (!SPEEK_MODES.includes(next) || next === mode) return;
      if ((next === 'logo') !== (mode === 'logo')) {
        morph = { direction: next === 'logo' ? 'out' : 'in', body: copy(body), face: copy(strokesNow), tail: tailLength };
        bodyVelocity = body.map(row => row.map(() => 0));
        strokeVelocity = strokesNow.map(row => row.map(() => 0));
        tailLengthVelocity = 0;
      }
      mode = next;
      modeStart = performance.now();
      if (motion.matches) step(modeStart, true);
    },
    destroy() {
      cancelAnimationFrame(animation);
      observer.disconnect();
      motion.removeEventListener('change', sync);
      document.removeEventListener('visibilitychange', sync);
      window.removeEventListener('pointermove', onPointer);
      document.documentElement.removeEventListener('pointerleave', onPointerLeave);
      group.remove();
    }
  };
}
