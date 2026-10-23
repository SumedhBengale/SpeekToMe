import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import './VocabularyExample.css';
import DemoSteps from './DemoSteps';

const SPOKEN = 'I was using speak to me to draft an email when Maya came over to speak to me.';
const SPEECH_START = 2150;
const SPEECH_END = SPEECH_START + SPOKEN.length * 25;
const POLISH_START = SPEECH_END + 300;
const FIXED_AT = POLISH_START + 750;
const COMPLETE_AT = FIXED_AT + 4200;

export default function VocabularyExample() {
  const panel = useRef(null);
  const term = useRef(null);
  const mic = useRef(null);
  const matched = useRef(null);
  const preserved = useRef(null);
  const visible = useRef(false);
  const [pointer, setPointer] = useState({ x: 0, y: 0 });
  const [elapsed, setElapsed] = useState(0);
  const [paused, setPaused] = useState(false);
  const [reduced, setReduced] = useState(() => window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  const fixed = reduced || elapsed >= FIXED_AT;
  const finished = reduced || elapsed >= COMPLETE_AT;
  const polishing = !fixed && elapsed >= POLISH_START;
  const speaking = elapsed >= SPEECH_START && elapsed < SPEECH_END;
  const typed = SPOKEN.slice(0, Math.max(0, Math.floor((elapsed - SPEECH_START) / 25)));
  const stage = finished ? 'done' : elapsed < 1300 ? 'vocabulary' : elapsed < 1900 ? 'to-mic' : elapsed < SPEECH_START ? 'pressing' : !fixed ? polishing ? 'polishing' : 'speaking' : elapsed < FIXED_AT + 1900 ? 'matched' : 'preserved';
  const selectingVocabulary = !reduced && elapsed >= 500 && elapsed < 1300;
  const selectingMatch = !finished && elapsed >= FIXED_AT + 700 && elapsed < FIXED_AT + 1900;
  const selectingPreserved = !finished && elapsed >= FIXED_AT + 2500;
  const selectionProgress = Math.max(0, Math.min(1, (elapsed - (stage === 'vocabulary' ? 500 : stage === 'matched' ? FIXED_AT + 700 : FIXED_AT + 2500)) / 400));
  const pointerVisible = !reduced && !finished && (elapsed < SPEECH_START + 100 || fixed);

  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    const change = () => setReduced(query.matches);
    const observer = new IntersectionObserver(([entry]) => { visible.current = entry.isIntersecting; }, { threshold: 0.25 });
    observer.observe(panel.current);
    query.addEventListener('change', change);
    return () => { observer.disconnect(); query.removeEventListener('change', change); };
  }, []);

  useEffect(() => {
    if (finished || paused) return;
    const timer = setInterval(() => {
      if (visible.current && !document.hidden) setElapsed(value => Math.min(COMPLETE_AT, value + 50));
    }, 50);
    return () => clearInterval(timer);
  }, [finished, paused]);

  useLayoutEffect(() => {
    if (!pointerVisible) return;
    const target = (stage === 'matched' ? matched : stage === 'preserved' ? preserved : stage === 'vocabulary' ? term : mic).current;
    if (!target) return;
    const update = () => {
      const bounds = panel.current.getBoundingClientRect();
      const rect = target.getBoundingClientRect();
      setPointer({ x: rect.left - bounds.left + rect.width * (['vocabulary', 'matched', 'preserved'].includes(stage) ? selectionProgress : 0.5), y: rect.top - bounds.top + rect.height * 0.7 });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(panel.current);
    return () => observer.disconnect();
  }, [stage, pointerVisible, selectionProgress]);

  const completedSteps = finished ? 4 : elapsed >= FIXED_AT + 1900 ? 3 : elapsed >= SPEECH_END ? 2 : elapsed >= 1300 ? 1 : 0;

  return (
    <section className="section vocabulary-section" aria-labelledby="vocabulary-title">
      <div className="container vocabulary-container">
        <div className="writing-heading">
          <p className="vocabulary-eyebrow">PERSONAL VOCABULARY</p>
          <h2 id="vocabulary-title">Same sound.<br /><span className="heading-accent">The right words.</span></h2>
          <p>SpeekToMe and “speak to me” sound alike. Your saved vocabulary and the sentence around it tell them apart.</p>
        </div>
        <div className="vocabulary-demo" ref={panel} data-phase={stage} data-paused={paused}>
          <div className="vocabulary-saved">
            <div className="vocabulary-saved-label">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 3h14a2 2 0 0 1 2 2v16H6a2 2 0 0 1-2-2V3Zm0 14h16M8 7h7M8 11h5" /></svg>
              <span>Saved vocabulary</span>
            </div>
            <span className="vocabulary-term"><span ref={term} style={{ '--selection-progress': `${selectionProgress * 100}%` }} className={selectingVocabulary ? 'vocabulary-selection' : undefined}>SpeekToMe</span><span aria-hidden="true">✓</span></span>
          </div>
          <div className="vocabulary-input">
            <button ref={mic} className="command-mic vocabulary-mic" type="button" aria-label="Start vocabulary dictation demo" aria-pressed={speaking} disabled={reduced} onClick={() => { setElapsed(SPEECH_START); setPaused(false); }}>
              <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><rect x="9" y="2" width="6" height="12" rx="3" /><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8" /></svg>
            </button>
            <span>Sample voice input</span>
          </div>
          <div className="vocabulary-output">
            <div className="writing-label">{fixed ? 'Written in your app' : 'Your spoken words'}{fixed && <span className="writing-complete" aria-hidden="true">✓</span>}</div>
            <p className={polishing ? 'vocabulary-glow' : undefined}>
              {fixed ? <>I was using <mark ref={matched} style={{ '--selection-progress': `${selectionProgress * 100}%` }} className={`vocabulary-match${selectingMatch ? ' vocabulary-selection' : ''}`}>SpeekToMe</mark> to draft an email when Maya came over to <span ref={preserved} style={{ '--selection-progress': `${selectionProgress * 100}%` }} className={`vocabulary-ordinary${selectingPreserved ? ' vocabulary-selection' : ''}`}>speak to me</span>.</> : typed || <span className="writing-placeholder">Your sentence will appear here.</span>}{speaking && !paused && <span className="writing-cursor" aria-hidden="true" />}
            </p>
          </div>
          <div className="vocabulary-explanation">
            <DemoSteps completed={completedSteps} label="Vocabulary demo steps" steps={[
              'Save “SpeekToMe” in your vocabulary.',
              'Click the mic and speak your sentence.',
              'Use “SpeekToMe” for the app name.',
              'Keep “speak to me” as an everyday phrase.'
            ]} />
          </div>
          <div className="vocabulary-footer">
            <div className="vocabulary-playback">
              {!finished && <button type="button" onClick={() => setPaused(value => !value)} aria-label={`${paused ? 'Play' : 'Pause'} vocabulary example`}>{paused ? 'Play' : 'Pause'}</button>}
              <button type="button" disabled={reduced} onClick={() => { setElapsed(0); setPaused(false); }} aria-label="Replay vocabulary example">↻ Replay</button>
            </div>
          </div>
          {pointerVisible && <svg className={`vocabulary-pointer${selectingVocabulary || selectingMatch || selectingPreserved ? ' is-selecting' : ''}`} style={{ left: pointer.x, top: pointer.y }} width="23" height="28" viewBox="0 0 23 28" aria-hidden="true"><path d="M2 2v21l5-5 4 8 4-2-4-8h8Z" fill="#40101d" stroke="white" strokeWidth="2" strokeLinejoin="round" /></svg>}
        </div>
      </div>
    </section>
  );
}
