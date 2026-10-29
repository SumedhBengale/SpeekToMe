import { useEffect, useRef, useState } from 'react';
import './VoiceToWords.css';
import DemoSteps from './DemoSteps';
import SpeekMascot from './SpeekMascot';

// Each polished example comes from its raw transcript; there is no second draft.
const SPELLING = {
  lauch: 'launch', reveiw: 'review', feedbak: 'feedback', tomorows: "tomorrow's",
  presntation: 'presentation', coffe: 'coffee', enogh: 'enough', ill: "I'll",
  desings: 'designs', actualy: 'actually', lets: "let's", thoght: 'thought',
  coments: 'comments', thursday: 'Thursday', friday: 'Friday', sevn: 'seven',
  befor: 'before', idea: 'ideas', thru: 'through', maya: 'Maya', i: 'I'
};

function polishTranscript(raw, pause) {
  const corrected = raw
    .replace(/\b(?:umm|aah|uh)\b\s*/g, '')
    .replace(/\b[a-z]+\b/g, word => SPELLING[word] || word)
    .replace(/\s*\.\.\.\s*/g, `${pause} `)
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/(^|[.!?]\s+)([a-z])/g, (_, prefix, letter) => prefix + letter.toUpperCase())
    .replace(/\b(Hey Maya|Hi team)\b/g, '$1,');
  const lastSentence = corrected.split(/[.!?]/).at(-1).trim();
  const ending = /^(can|could|would|will|should|do|does|is|are)\b/i.test(lastSentence) ? '?' : '.';
  return `${corrected}${ending}`;
}

const EXAMPLES = [
  { raw: 'hey maya umm the lauch is ready for reveiw... can you check the homepage and send me your feedbak before lunch', pause: '.' },
  { raw: 'can we aah move tomorows meeting to the afternoon... i need a little more time to finish the presntation', pause: '?' },
  { raw: 'umm remind me to pick up coffe on the way home... and check if we have enogh milk for the weekend', pause: ',' },
  { raw: 'thanks for the feedbak... uh the simpler version makes sense... ill update the desings and share them with the team', pause: '.' },
  { raw: 'i think umm we should start with a small test... see what people actualy use and then decide what to build next', pause: ',' },
  { raw: 'lets save this thoght for our next brainstorm... aah it could be a better way to explain the product to new users', pause: '.' },
  { raw: 'hi team umm the latest desings are ready... please leave your coments by thursday so we can finish this week', pause: '.' },
  { raw: 'book a table for uh four this friday at sevn... somewhere quiet so we can actualy hear each other talk', pause: ',' },
  { raw: 'quick note befor i forget umm... lets try the simpler onboarding first and keep the extra settings for later', pause: ':' },
  { raw: 'i have a few idea about the new homepage... aah let me talk you thru them and we can pick a direction together', pause: '.' }
].map(example => ({ ...example, clean: polishTranscript(example.raw, example.pause) }));

const MASCOT_MODES = { raw: 'listen', 'raw-complete': 'think', processing: 'think', reveal: 'happy', polished: 'admire' };

export default function VoiceToWords() {
  const waveRef = useRef(null);
  const [transcript, setTranscript] = useState({ example: 0, count: 0, stage: 'raw', steps: 0 });

  useEffect(() => {
    const canvas = waveRef.current;
    const context = canvas.getContext('2d');
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
    let inView = false;
    let animation = null;
    let timer = null;
    let example = 0;
    let count = 0;
    let stage = 'raw';
    let elapsed = 0;
    let previousTime = 0;
    let width = 0;
    let height = 0;

    const paint = () => {
      context.clearRect(0, 0, width, height);
      const colors = ['rgba(255, 145, 185, .50)', 'rgba(245, 45, 120, .60)', 'rgba(225, 27, 99, .45)'];
      colors.forEach((color, layer) => {
        context.beginPath();
        // The original liquid silhouette: mirrored, translucent lobes.
        // Staggered packets travel right and taper as they enter the pill.
        for (const sign of [1, -1]) {
          for (let step = 0; step <= 180; step += 1) {
            const x = sign === 1 ? step / 180 : 1 - step / 180;
            let amplitude = 0;
            for (let packet = 0; packet < 3; packet += 1) {
              const progress = ((elapsed / 6500 + packet / 3 + layer * 0.065) % 1);
              const center = progress * 1.6 - 0.3;
              const local = (x - center) / (0.13 + layer * 0.018);
              const attenuation = (4 / (4 + local * local)) ** 4;
              amplitude += Math.abs(Math.sin(local * 1.7 - elapsed / 1800 + layer)) * attenuation;
            }
            const taper = Math.sin(Math.PI * x) ** 0.65;
            const y = height / 2 + sign * amplitude * taper * height * 0.34;
            if (sign === 1 && step === 0) context.moveTo(x * width, y);
            else context.lineTo(x * width, y);
          }
        }
        context.closePath();
        context.fillStyle = color;
        context.fill();
      });
    };

    const draw = time => {
      elapsed += previousTime ? Math.min(time - previousTime, 64) : 16;
      previousTime = time;
      paint();
      animation = requestAnimationFrame(draw);
    };
    const advance = () => {
      timer = null;
      let delay = 18;
      if (stage === 'raw') {
        if (count < EXAMPLES[example].raw.length) {
          count += 1;
          const character = EXAMPLES[example].raw[count - 1];
          delay = character === '.' ? 65 : character === ' ' ? 24 : 18;
          if (count === EXAMPLES[example].raw.length) {
            stage = 'raw-complete';
            delay = 1100;
          }
        } else {
          stage = 'processing';
          delay = 650;
        }
      } else if (stage === 'raw-complete') {
        stage = 'processing';
        delay = 650;
      } else if (stage === 'processing') {
        stage = 'reveal';
        delay = 650;
      } else if (stage === 'reveal') {
        stage = 'polished';
        delay = 3200;
      } else {
        example = (example + 1) % EXAMPLES.length;
        count = 0;
        stage = 'raw';
        delay = 180;
      }
      setTranscript(current => ({ example, count, stage, steps: current.steps + 1 }));
      timer = setTimeout(advance, delay);
    };
    const syncPlayback = () => {
      if (inView && !document.hidden && !motion.matches) {
        if (animation == null) animation = requestAnimationFrame(draw);
        if (timer == null) timer = setTimeout(advance, 250);
      } else {
        cancelAnimationFrame(animation);
        animation = null;
        previousTime = 0;
        clearTimeout(timer);
        timer = null;
        if (motion.matches) {
          stage = 'polished';
          setTranscript(current => ({ example, count, stage, steps: current.steps + 1 }));
          paint();
        }
      }
    };
    const resize = () => {
      width = canvas.clientWidth;
      height = canvas.clientHeight;
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(width * ratio);
      canvas.height = Math.round(height * ratio);
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      paint();
    };
    const sizeObserver = new ResizeObserver(resize);
    const visibilityObserver = new IntersectionObserver(([entry]) => {
      inView = entry.isIntersecting;
      syncPlayback();
    });
    sizeObserver.observe(canvas);
    visibilityObserver.observe(canvas);
    motion.addEventListener('change', syncPlayback);
    document.addEventListener('visibilitychange', syncPlayback);
    return () => {
      cancelAnimationFrame(animation);
      clearTimeout(timer);
      sizeObserver.disconnect();
      visibilityObserver.disconnect();
      motion.removeEventListener('change', syncPlayback);
      document.removeEventListener('visibilitychange', syncPlayback);
    };
  }, []);

  const processing = transcript.stage === 'processing' || transcript.stage === 'reveal';
  const polished = transcript.stage === 'reveal' || transcript.stage === 'polished';
  // Speek dozes until the demo first plays, then startles awake and listens.
  const mascotMode = transcript.steps === 0 ? 'sleepy' : transcript.steps < 30 ? 'surprised' : MASCOT_MODES[transcript.stage];

  return (
    <figure className="voice-to-words" aria-label="Raw speech flows through SpeekTo.Me and becomes polished text.">
      <div className={`voice-scene stage-${transcript.stage}`} aria-hidden="true" data-example={transcript.example} data-typed-characters={transcript.count} data-raw-length={EXAMPLES[transcript.example].raw.length}>
        <canvas className="voice-liquid" ref={waveRef} />
        <div className="voice-transcript">
          <span className="voice-part-label">YOUR WORDS</span>
          <p className="voice-sentence">
            <span className="voice-sentence-text">{polished ? EXAMPLES[transcript.example].clean : transcript.stage === 'raw' ? EXAMPLES[transcript.example].raw.slice(0, transcript.count) : EXAMPLES[transcript.example].raw}</span>{transcript.stage === 'raw' && <span className="voice-cursor" />}
          </p>
          <div className="voice-progress"><DemoSteps compact label="Voice to text steps" completed={transcript.stage === 'polished' ? 3 : polished ? 2 : processing ? 1 : 0} steps={['Speak', 'Polish', 'Ready']} /></div>
        </div>
        <div className="voice-brand">
          <SpeekMascot className="voice-mascot" mode={mascotMode} faceColor="#240a12" followPointer />
          <span className="voice-brand-name">SpeekTo.<span>Me</span></span>
        </div>
        <span className="voice-input-label voice-part-label">YOUR VOICE</span>
      </div>
    </figure>
  );
}
