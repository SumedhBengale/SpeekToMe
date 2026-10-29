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
| `createSpeekMascot(svg, options)` | The animated rig. Options: `mode`, `faceColor`, `followPointer`, and `voice`, a function from time in ms to a voice level 0–1 (defaults to `simulatedVoice`). |
| `speekSvg(options)` | Static SVG string. Options: `mode`, `faceColor`, `viewBox`, `size`. |
| `SPEEK_MODES` | Every mood name. |
| `simulatedVoice` | A speech-like level for pages without a microphone. |
| `SPEEK_COLORS` | Body, cheek and the two face colours. |
| `SPEEK_VIEWBOX` | `0 0 1254 1254`: room for hops, raised ears and thought dots. |
| `SPEEK_ICON_VIEWBOX` | `172 215 910 910`: crops to the resting mascot for logo-sized slots. |

Static icons are generated, never drawn by hand. After changing the geometry,
run `node shared/speek/build-icons.js`; it rewrites every SVG listed in it:
`website/public/assets/speek.svg` (the website's logo and favicon),
`windows-client/frontend/logo.svg` (the desktop app's in-app logo and
favicon) and `windows-client/src-tauri/icons/app-icon.svg`. Then rebuild the
desktop app icons from that source:

```bash
cd windows-client && cargo tauri icon src-tauri/icons/app-icon.svg
```

Keep the desktop files it writes to `src-tauri/icons` (the PNGs, `icon.ico`
and `icon.icns`) and delete the `android` and `ios` folders it adds. Every icon
is the resting `idle` mood, Speek's smile; the app icon has a little margin
around it.

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

The overlays are drawn natively in Rust, so the rig is ported to
`windows-client/src-tauri/src/speek.rs`: the same shapes, moods, springs and
timings, stepped at the web's 60 Hz whatever the overlay's frame rate. The M
logo and pointer following are not ported. `overlay_paint.rs` draws the frame
for Windows, Linux and macOS alike, anti-aliasing each pill, stroke and the
tail from its signed distance.

**Keep the two in step.** A change to `speek.js` needs the same change in
`speek.rs`. To check them, render both: the Rust painter at a large scale and
`speekSvg` for the same mood should match to the anti-aliased edge, and
stepping the JS rig with a fake clock should give the same numbers as
`Rig::advance` frame for frame.

The tray icon on every platform is drawn by the same painter at startup
(`overlay_paint::speek_icon`): at the system's small-icon size on Windows,
64px on Linux and macOS, which scale it to the bar. The window and taskbar
icons come from the generated app icons, and Speek keeps its own colours in
every app theme.

Speek replaces the waveform bars inside the pill. Its resting box fills 22 of
the pill's 24px (`SPEEK_HEIGHT`), centred; raised ears and hops reach into the
4px margin around the pill. In the meeting pill Speek sits at the left, before
the label.

| Overlay | Mood |
| --- | --- |
| Pill appears | `surprised` for 350ms |
| Recording, meetings | `listen`; the ears, eyes and tail follow the real microphone level |
| Polishing | `think` |
| Finished, while the changed words settle | `happy` |

At 100% display scaling the face is about 22px tall: the ears, eyes and the
happy smile read, which is why listening shows the voice in the ears rather
than the mouth. At 200% every expression is clear.

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
| `listen` | Recording | Concentrating: slightly narrowed eyes, furrowed brows, a short flat mouth; eyes follow the pointer | Ears carry the voice (below) | Pulses longer with the voice level |
| `think` | Transcribing | Eyes up and drifting, one brow raised, lopsided "hmm", three pulsing thought dots | Left ear flops, head tilts | Taps on a 700ms beat |
| `happy` | Text pasted | ^ ^ eyes, big smile, rosy cheeks | Ears wiggle, squash-and-stretch hop | Wags hard, then keeps a gentle wag |
| `admire` | Showing the result | Happy eyes looking right at the text; after 2s glances back at the viewer | Head tilts toward the text, one small nod | Slow happy sway |
| `surprised` | Hotkey pressed | Tall eyes, raised brows, "o" mouth | Ears shoot up, quick jump | Shoots out and quivers |
| `sleepy` | Idle for a long time | Closed eyes, tiny mouth, rising bubbles | Ears droop, slow deep breathing | Droops and rests |
| `oops` | Something failed | Small eyes, worried brows, frown | Ears flat, quick shake | Curls in short and fidgets |

### Listening to the voice

`listen` takes a voice level from 0 to 1: the real microphone on the desktop
(the overlay's smoothed volume through `voice()`, a square root above the
noise floor), `simulatedVoice` on the website.

- **Ears.** Each ear's share of the level is the level times its own rhythm
  (two sines per ear, 0.2–1), so the ears flop independently but always
  with the voice. An ear stretches upward from its pivot by up to 110 units
  and tilts out by up to 14°, and sways side to side by up to ±12° times the
  level (about 0.85 and 1 Hz for the left and right ear). Above 60% level both
  ears flare out a further 10° at full volume.
- **Eyes and brows.** Loud speech widens the eyes: with `peak` = level²,
  each eye grows from 100×170 to 116×260 and the brows lift 30 units, easing
  the furrow.
- **Silence.** Everything settles to the concentrating face; an ear still
  twitches every few seconds, as in `idle`.

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
