import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import './WritingExamples.css';
import SpokenText from './SpokenText';

const EXAMPLES = [
  { id: 'email', label: 'An email', spoken: 'hey maya um the onboarding flow is ready could you review it before friday thanks', transcription: 'Hey Maya, the onboarding flow is ready. Could you review it before Friday? Thanks!' },
  { id: 'update', label: 'A team update', spoken: 'quick update uh the new onboarding is ready design review is tomorrow and we are aiming for friday', transcription: 'Quick update: the new onboarding is ready, design review is tomorrow, and we are aiming for Friday.' },
  { id: 'notes', label: 'A personal note', spoken: 'i need to um review the onboarding copy check the signup flow and send maya the final draft', transcription: 'I need to review the onboarding copy, check the signup flow, and send Maya the final draft.' },
];

const COMMAND_EXAMPLES = [
  { id: 'translate', label: 'Translate', before: 'Hi Sofia, thanks for your feedback. We’ve updated the design and will send you the final version tomorrow.', spoken: 'translate this into spanish', transcription: 'Hola Sofía, gracias por tus comentarios. Hemos actualizado el diseño y te enviaremos la versión final mañana.' },
  { id: 'format', label: 'Format', before: 'Launch plan: Maya will review the copy on Tuesday. Alex will test the signup flow on Wednesday. We’ll publish the update on Friday.', spoken: 'format this as a bulleted list with the dates first', transcription: '• Tuesday: Maya reviews the copy.\n• Wednesday: Alex tests the signup flow.\n• Friday: Publish the update.' },
  { id: 'rewrite', label: 'Rewrite', before: 'Your payment didn’t go through. Update your card or you won’t be able to use your account.', spoken: 'make this friendlier and keep it short', transcription: 'We couldn’t process your payment. Please update your card to keep your account active.' },
];

function TranscriptionExample({ example, command = false }) {
  const [reducedMotion, setReducedMotion] = useState(() => window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  const [elapsed, setElapsed] = useState(0);
  const [paused, setPaused] = useState(false);
  const card = useRef(null);
  const visible = useRef(false);
  const prefix = command ? 'command' : 'writing';
  const playback = command ? 'command' : 'transcription';
  const finishAt = example.spoken.length * 35 + 800;
  const finished = reducedMotion || elapsed >= finishAt;
  const typed = reducedMotion ? example.spoken : example.spoken.slice(0, Math.floor(elapsed / 35));
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    const change = () => setReducedMotion(query.matches);
    query.addEventListener('change', change);
    return () => query.removeEventListener('change', change);
  }, []);
  useEffect(() => {
    const observer = new IntersectionObserver(([entry]) => { visible.current = entry.isIntersecting; });
    observer.observe(card.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (finished || paused) return;
    const timer = setInterval(() => { if (visible.current && !document.hidden) setElapsed(value => Math.min(finishAt, value + 70)); }, 70);
    return () => clearInterval(timer);
  }, [finishAt, finished, paused]);
  return (
    <div className="writing-card" ref={card} id={`${prefix}-example-output`} role="tabpanel" aria-labelledby={`${prefix}-tab-${example.id}`}>
      <div className="writing-card-header"><h3>{example.label}</h3><span>{command ? 'AI Command Mode' : 'Sample dictation'}</span></div>
      {command && <div className="writing-original"><div className="writing-label">Selected text</div><p>{example.before}</p></div>}
      <div className="writing-speech">
        <div className="writing-label"><span className={`writing-recording-dot ${!finished && !paused ? 'active' : ''}`} aria-hidden="true" />{command ? 'You say a command' : 'You say'}</div>
        <p>{typed ? <SpokenText text={typed} strikeFillers={finished} /> : <span className="writing-placeholder">Start speaking…</span>}{!finished && !paused && <span className="writing-cursor" aria-hidden="true" />}</p>
      </div>
      <div className={`writing-transcription ${finished ? 'ready' : ''}`}>
        <div className="writing-label">{command ? 'Result' : 'Transcription'}{finished && <span className="writing-complete" aria-hidden="true">✓</span>}</div>
        <p>{finished ? example.transcription : <span className="writing-placeholder">{command ? 'Your edited text will appear here.' : 'Your transcription will appear here.'}</span>}</p>
      </div>
      <div className="writing-card-footer">
        <span role="status">{finished ? command ? 'Your command applied to the selected text.' : 'Filler words removed. Punctuation added.' : paused ? 'Paused' : 'Typing your spoken words…'}</span>
        <div className="writing-playback">
          {!finished && <button type="button" onClick={() => setPaused(value => !value)} aria-label={paused ? `Play ${playback} example` : `Pause ${playback} example`}>{paused ? 'Play' : 'Pause'}</button>}
          <button type="button" disabled={reducedMotion} onClick={() => { setElapsed(0); setPaused(false); }} aria-label={`Replay ${playback} example`}>↻ Replay</button>
        </div>
      </div>
    </div>
  );
}

function CommandExample({ example }) {
  const [reducedMotion, setReducedMotion] = useState(() => window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  const [elapsed, setElapsed] = useState(0);
  const [paused, setPaused] = useState(false);
  const [pointer, setPointer] = useState({ x: 0, y: 0 });
  const card = useRef(null);
  const editor = useRef(null);
  const text = useRef(null);
  const visible = useRef(false);
  const speechStart = 2700;
  const speechEnd = speechStart + example.spoken.length * 40;
  const finishAt = speechEnd + 650;
  const finished = reducedMotion || elapsed >= finishAt;
  const selecting = elapsed >= 1200 && elapsed < 2200;
  const selectedCount = elapsed < 1200 ? 0 : Math.min(example.before.length, Math.ceil((elapsed - 1200) / 1000 * example.before.length));
  const typed = reducedMotion ? example.spoken : example.spoken.slice(0, Math.max(0, Math.floor((elapsed - speechStart) / 40)));
  const phase = finished ? 'done' : elapsed < 700 ? 'original' : elapsed < 1200 ? 'pointing' : selecting ? 'selecting' : elapsed < speechStart ? 'selected' : elapsed < speechEnd ? 'speaking' : 'applying';
  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    const change = () => setReducedMotion(query.matches);
    query.addEventListener('change', change);
    const observer = new IntersectionObserver(([entry]) => { visible.current = entry.isIntersecting; });
    observer.observe(card.current);
    return () => { query.removeEventListener('change', change); observer.disconnect(); };
  }, []);
  useEffect(() => {
    if (finished || paused) return;
    const timer = setInterval(() => { if (visible.current && !document.hidden) setElapsed(value => Math.min(finishAt, value + 70)); }, 70);
    return () => clearInterval(timer);
  }, [finished, paused, finishAt]);
  useLayoutEffect(() => {
    if (finished) return;
    const character = text.current.children[Math.max(0, selectedCount - 1)];
    if (!character) return;
    const target = character.getBoundingClientRect();
    const bounds = editor.current.getBoundingClientRect();
    setPointer({ x: target.left - bounds.left + (selectedCount ? target.width : 0), y: target.bottom - bounds.top - 4 });
  }, [selectedCount, finished]);
  const status = finished ? 'Selected text updated.' : paused ? 'Paused' : phase === 'original' || phase === 'pointing' || phase === 'selecting' ? 'Select the text you want to change.' : phase === 'selected' ? 'Double-tap your hotkey.' : phase === 'speaking' ? 'Say your command…' : 'Applying your command…';
  return (
    <div className="writing-card command-card" ref={card} id="command-example-output" role="tabpanel" aria-labelledby={`command-tab-${example.id}`} data-phase={phase} data-paused={paused}>
      <div className="writing-card-header"><h3>{example.label}</h3><span>AI Command Mode</span></div>
      <div className="command-editor" ref={editor}>
        <div className="writing-label">{finished ? 'Updated text' : selectedCount === example.before.length ? 'Text selected' : 'Text in your app'}{finished && <span className="writing-complete" aria-hidden="true">✓</span>}</div>
        <p ref={text} className={finished ? 'command-result' : 'command-existing'} aria-label={finished ? undefined : example.before}>
          {finished ? example.transcription : [...example.before].map((character, index) => <span key={index} aria-hidden="true" className={index < selectedCount ? 'command-highlight' : undefined}>{character}</span>)}
        </p>
        {!finished && elapsed >= 700 && elapsed < 2400 && <svg className="command-pointer" style={{ '--pointer-x': `${pointer.x}px`, '--pointer-y': `${pointer.y}px` }} width="23" height="28" viewBox="0 0 23 28" aria-hidden="true"><path d="M2 2v21l5-5 4 8 4-2-4-8h8Z" fill="#40101d" stroke="white" strokeWidth="2" strokeLinejoin="round" /></svg>}
      </div>
      <div className="writing-speech command-speech">
        <div className="writing-label"><span className={`writing-recording-dot ${phase === 'speaking' && !paused ? 'active' : ''}`} aria-hidden="true" />Voice command</div>
        <p>{typed || <span className="writing-placeholder">{elapsed < speechStart ? 'Select text, then speak your command…' : 'Start speaking…'}</span>}{phase === 'speaking' && !paused && <span className="writing-cursor" aria-hidden="true" />}</p>
      </div>
      <div className="writing-card-footer">
        <span role="status">{status}</span>
        <div className="writing-playback">
          {!finished && <button type="button" onClick={() => setPaused(value => !value)} aria-label={`${paused ? 'Play' : 'Pause'} command example`}>{paused ? 'Play' : 'Pause'}</button>}
          <button type="button" disabled={reducedMotion} onClick={() => { setElapsed(0); setPaused(false); }} aria-label="Replay command example">↻ Replay</button>
        </div>
      </div>
    </div>
  );
}

function ExamplesSection({ command = false }) {
  const [selected, setSelected] = useState(0);
  const examples = command ? COMMAND_EXAMPLES : EXAMPLES;
  const prefix = command ? 'command' : 'writing';
  return (
    <section className={`section writing-examples ${command ? 'command-examples' : ''}`} aria-labelledby={`${prefix}-examples-title`}>
      <div className="container writing-container">
        <div className="writing-heading">
          <h2 id={`${prefix}-examples-title`}>{command ? <>Select your text.<br /><span className="heading-accent">Say what to change.</span></> : <>Speak naturally.<br /><span className="heading-accent">Keep your words.</span></>}</h2>
          <p>{command ? 'Translate, format, or rewrite text that’s already in your app. Select it and say what you need.' : 'See how everyday speech becomes clear text, without changing what you meant.'}</p>
        </div>
        <div className="writing-tabs" role="tablist" aria-label={command ? 'Command mode examples' : 'Writing examples'}>
          {examples.map((example, index) => <button type="button" role="tab" key={example.id} id={`${prefix}-tab-${example.id}`} aria-selected={selected === index} aria-controls={`${prefix}-example-output`} tabIndex={selected === index ? 0 : -1} onClick={() => setSelected(index)} onKeyDown={event => {
            let next;
            if (event.key === 'ArrowRight') next = (index + 1) % examples.length;
            if (event.key === 'ArrowLeft') next = (index + examples.length - 1) % examples.length;
            if (event.key === 'Home') next = 0;
            if (event.key === 'End') next = examples.length - 1;
            if (next !== undefined) { event.preventDefault(); setSelected(next); event.currentTarget.parentElement.children[next].focus(); }
          }}>{example.label}</button>)}
        </div>
        {command ? <CommandExample key={examples[selected].id} example={examples[selected]} /> : <TranscriptionExample key={examples[selected].id} example={examples[selected]} />}
        {command && <p className="writing-command-note">Select text in your app, then double-tap your hotkey to give a command.</p>}
      </div>
    </section>
  );
}

export default function WritingExamples() {
  return <><ExamplesSection /><ExamplesSection command /></>;
}
