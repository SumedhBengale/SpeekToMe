# Speek mascot

Speek is the brand mark and the mascot. It is built from the five pills of the
old waveform mark: the outer two are the ears, the inner three merge into one
rounded head, and a rounded speech-bubble tail sits at the bottom left. Nothing
is drawn that is not a pill or a stroke with round caps. The website uses the
mascot everywhere the old logo was; the five-pill M (`logo` mode) still exists
in the rig but nothing shows it.

## One module

Everything lives in `shared/speek/speek.js`, a plain ES module with no
dependencies, so the website and the desktop client draw the same Speek.

```js
import { createSpeekMascot, speekSvg, SPEEK_COLORS } from './speek.js';

// Live, animated rig inside an <svg viewBox="0 0 1254 1254">.
const speek = createSpeekMascot(svg, { mode: 'idle', faceColor: SPEEK_COLORS.faceOnLight, followPointer: true });
speek.setMode('listen');
speek.destroy();

// Static markup of a mood's resting pose, for favicons and <img> icons.
const markup = speekSvg({ mode: 'idle', faceColor: SPEEK_COLORS.faceOnDark, size: 64 });
```

| Export | Use |
| --- | --- |
| `createSpeekMascot(svg, options)` | The animated rig. Options: `mode`, `faceColor`, `followPointer`. |
| `speekSvg(options)` | Static SVG string. Options: `mode`, `faceColor`, `viewBox`, `size`. |
| `SPEEK_MODES` | Every mood name. |
| `SPEEK_COLORS` | Body, cheek and the two face colours. |
| `SPEEK_VIEWBOX` | `0 0 1254 1254`: room for hops, raised ears and thought dots. |
| `SPEEK_ICON_VIEWBOX` | `172 215 910 910`: crops to the resting mascot for logo-sized slots. |

Static icons are generated, never drawn by hand. After changing the geometry,
run `node shared/speek/build-icons.js`; it rewrites every file listed in it
(today `website/public/assets/speek.svg`, which is also the favicon).

### Website

`website/vite.config.js` aliases `@speek` to the shared module and lets the dev
server read `shared/speek`. `website/src/components/SpeekMascot.jsx` wraps the
rig for React; pass `framing="icon"` to crop it for small slots.

| Place | What shows |
| --- | --- |
| Navbar | Live rig, `idle`; turns `happy` while the logo is hovered |
| Homepage hero | Live rig driven by the demo (below) |
| Footer, desktop demo window, dashboard header, favicon | `/assets/speek.svg` |

### Desktop client

The desktop frontend loads classic scripts from `windows-client/frontend`,
which Tauri serves as-is, so it cannot import from `shared/` yet. When the
client adopts Speek, copy or link `speek.js` into the frontend at build time
and load it with `<script type="module">`; add the client's icons to
`build-icons.js`.

## Geometry

| Part | Logo (M) | Mascot |
| --- | --- | --- |
| Pill 1 and 5 | Full-height legs at x 313 / 941, 98 wide, 600 tall | Ears: 190×420 pills pivoting at (407, 560) and (847, 560) |
| Pills 2 and 4 | Short pills hanging from the top at x 470 / 784, 280 tall | Merge into the head |
| Pill 3 | Middle pill at x 627, 310 tall, set lower than 2 and 4 | The head: 760×560, radius 270, centred at (627, 710) |
| Tail | Hidden | Rounded triangle: base (430, 930)–(565, 965), tip (365, 1095), 50px round-join stroke |

Colours: body `#FC5B90`, cheeks `#FFA3C2`, face strokes `faceColor`
(`#1C1520` on dark tiles, `#240a12` on the white website). Eyes, brows and the
mouth are quadratic strokes, so a pill eye can bend into a ^ or close into a
blink without changing elements.

## The M ↔ mascot morph

Only used if something sets `logo` mode. The morph is a timed tween, not a spring, so it is fast and never wobbles.
Times are `delay + duration` in milliseconds.

| Part | Logo → mascot | Mascot → logo |
| --- | --- | --- |
| Middle pill → head | 0 + 150, ease out | 50 + 150, ease in-out |
| Pills 2, 4 → head | 15 + 140, ease out | 60 + 140, ease in-out |
| Ears | 70 + 170, slight overshoot | 30 + 140, ease in-out |
| Eyes, mouth, other face | 140–185 + 150, ease out | 0 + 80 |
| Tail | 130 + 190, slight overshoot | 0 + 90 |

So the mascot is fully formed in about 0.35s and back to the M in about 0.2s.
The middle pill swells first and swallows its neighbours, the ears pop, then
the face and tail appear. Changes between two mascot moods use calm springs
(stiffness 0.16, damping 0.68).

## Moods

Each mood is a face, a body pose (ear angles, ear lift, squash, head tilt) and
a tail behaviour. The tail sits on its own spring, so it lags behind hops and
shakes and settles on its own.

| Mode | Meaning in the app | Face | Ears and body | Tail |
| --- | --- | --- | --- | --- |
| `logo` | Brand mark | None | The pill M | Hidden |
| `idle` | Ready | Pill eyes, small smile, cheeks; glances around or follows the pointer | Breathes; one ear twitches every few seconds | Sways, counterbalances the gaze, flicks every few seconds |
| `listen` | Recording | Wider eyes that follow the pointer; the mouth is a five-bar waveform | Ears up and leaning in | Pulses longer with the voice level |
| `think` | Transcribing | Eyes up and drifting, one brow raised, lopsided "hmm", three pulsing thought dots | Left ear flops, head tilts | Taps on a 700ms beat |
| `happy` | Text pasted | ^ ^ eyes, big smile, rosy cheeks | Ears wiggle, squash-and-stretch hop | Wags hard, then keeps a gentle wag |
| `admire` | Showing the result | Happy eyes looking right at the text; after 2s glances back at the viewer | Head tilts toward the text, one small nod | Slow happy sway |
| `surprised` | Hotkey pressed | Tall eyes, raised brows, "o" mouth | Ears shoot up, quick jump | Shoots out and quivers |
| `sleepy` | Idle for a long time | Closed eyes, tiny mouth, rising bubbles | Ears droop, slow deep breathing | Droops and rests |
| `oops` | Something failed | Small eyes, worried brows, frown | Ears flat, quick shake | Curls in short and fidgets |

Blinks happen every 2.2–4.8s in `idle`, `listen`, `think`, `admire`, `oops`
and `surprised`.

## Homepage hero

`VoiceToWords` replaces the old recording pill with Speek. It dozes (`sleepy`)
until the demo first plays, startles awake (`surprised`) for its first moment,
then follows the demo stages:

| Stage | Mood |
| --- | --- |
| `raw` (speech arriving) | `listen` |
| `raw-complete`, `processing` | `think` |
| `reveal` | `happy` |
| `polished` | `admire` |

With `followPointer`, Speek's eyes follow the visitor's pointer in `idle` and
`listen`.

## Motion and performance

The engine pauses its animation frame loop while the SVG is off screen or the
tab is hidden. With `prefers-reduced-motion`, it shows each mood's final pose
without animating.
