import React, { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cards, clientCSS } from "virtual:desktop-client-ui";
import demoCSS from "./DesktopDictationDemo.css?raw";
import MeetingDemo from "./MeetingDemo";
import meetingCSS from "./MeetingDemo.css?raw";

const EXAMPLES = {
  dictation: {
    before: "",
    spoken:
      "hey maya um quick update on the launch the new onboarding flow is ready for review can you take a look before friday thanks",
    after:
      "Hey Maya, quick update on the launch. The new onboarding flow is ready for review. Can you take a look before Friday? Thanks!",
    label: "Write an email",
    title: "Launch update",
    hint: "Speak naturally. Your words appear in the active app.",
  },
  command: {
    before: "the onboarding is done can you check it by friday thanks",
    spoken: "make this more professional and format it as a short email",
    after:
      "Hi Maya,\n\nThe onboarding flow is ready. Please share your feedback by Friday.\n\nThank you!",
    label: "Rewrite with your voice",
    title: "Launch update",
    hint: "Select text, then double-tap your hotkey to give a command.",
  },
};
// Add future modes here so tab navigation and per-mode settings stay consistent.
const MODES = [{ id: 'dictation', label: 'Voice dictation' }, { id: 'command', label: 'AI Command Mode' }, { id: 'meetings', label: 'Meetings' }];
const STAGES = { dictation: ['Ready', 'Listening', 'Processing', 'Done'], command: ['Select', 'Command', 'Processing', 'Done'] };

// The markup and styling come directly from windows-client/frontend at build time.
const ClientFeatureCard = memo(function ClientFeatureCard({ mode, enabled, onToggle }) {
  const ref = useRef(null);
  const markup = useMemo(() => ({ __html: cards[mode] }), [mode]);
  useLayoutEffect(() => {
    const input = ref.current.querySelector("input");
    input.checked = enabled;
    input.setAttribute(
      "aria-label",
      mode === "dictation"
        ? "Stream text as you speak in this demo"
        : "Enable voice commands in this demo",
    );
    const change = () => onToggle(input.checked);
    input.addEventListener("change", change);
    return () => input.removeEventListener("change", change);
  }, [mode, enabled, onToggle]);
  return (
    <div
      className="client-feature"
      ref={ref}
      dangerouslySetInnerHTML={markup}
    />
  );
});

function Demo() {
  const [mode, setMode] = useState("dictation");
  const [settings, setSettings] = useState({ dictation: true, command: true });
  const enabled = settings[mode];
  const editor = useRef(null);
  const originalText = useRef(null);
  const [pointer, setPointer] = useState({ x: 0, y: 0 });
  const [elapsed, setElapsed] = useState(0);
  const [playing, setPlaying] = useState(
    () => !window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  const [reducedMotion, setReducedMotion] = useState(
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  const root = useRef(null);
  const visible = useRef(true);
  const example = EXAMPLES[mode] || EXAMPLES.dictation;
  const speechStart = mode === 'command' ? 2400 : 900;
  const stage = elapsed < speechStart ? 0 : elapsed < 6100 ? 1 : elapsed < 7600 ? 2 : 3;
  const progress = Math.min(1, Math.max(0, (elapsed - speechStart) / (6100 - speechStart)));
  const words = example.spoken.split(" ");
  const spoken = words.slice(0, Math.ceil(words.length * progress)).join(" ");
  const complete = stage === 3;
  const disabled = mode === "command" && !enabled;
  const output = complete && !disabled ? example.after : mode === 'command' ? example.before : '';
  const selectionCount = mode === 'command' && enabled && !complete ? Math.min(example.before.length, Math.max(0, Math.ceil((elapsed - 1200) / 850 * example.before.length))) : 0;
  const showPointer = mode === 'command' && enabled && elapsed >= 700 && elapsed < 2250;
  const stages = STAGES[mode];

  useLayoutEffect(() => {
    if (!showPointer) return;
    const character = originalText.current?.children[Math.max(0, selectionCount - 1)];
    if (!character) return;
    const target = character.getBoundingClientRect();
    const bounds = editor.current.getBoundingClientRect();
    setPointer({ x: target.left - bounds.left + (selectionCount ? target.width : 0), y: target.bottom - bounds.top - 4 });
  }, [showPointer, selectionCount, mode]);

  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const change = () => {
      setReducedMotion(query.matches);
      if (query.matches) {
        setPlaying(false);
        setElapsed(7600);
      }
    };
    query.addEventListener("change", change);
    if (query.matches) setElapsed(7600);
    return () => query.removeEventListener("change", change);
  }, []);
  useEffect(() => {
    const observer = new IntersectionObserver(([entry]) => {
      visible.current = entry.isIntersecting;
    });
    observer.observe(root.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!playing || disabled || mode === 'meetings') return;
    const timer = setInterval(() => {
      if (!visible.current || document.hidden) return;
      setElapsed((value) => (value + 100 >= 11200 ? 0 : value + 100));
    }, 100);
    return () => clearInterval(timer);
  }, [playing, disabled, mode]);
  const reset = useCallback(() => {
    setElapsed(reducedMotion ? 7600 : 0);
    setPlaying(!reducedMotion);
  }, [reducedMotion]);
  const handleToggle = useCallback((value) => {
    setSettings(current => ({ ...current, [mode]: value }));
    reset();
  }, [reset, mode]);
  const switchMode = (next) => {
    setMode(next);
    reset();
  };

  return (
    <div
      className="desktop-demo"
      ref={root}
      data-playing={playing && !disabled}
    >
      <div className="demo-window-bar">
        <div className="demo-app-brand">
          <img src="/assets/logo.svg" alt="" />
          SpeekTo.Me
        </div>
        <span className="demo-preview-badge">Interactive preview</span>
      </div>
      <div className="demo-content">
        <div
          className="demo-mode-tabs"
          role="tablist"
          aria-label="Try desktop features"
        >
          {MODES.map((item, index) => <button
            key={item.id}
            type="button"
            id={`demo-tab-${item.id}`}
            role="tab"
            aria-selected={mode === item.id}
            aria-controls="demo-feature-panel"
            tabIndex={mode === item.id ? 0 : -1}
            onClick={() => switchMode(item.id)}
            onKeyDown={event => {
              let next;
              if (event.key === 'ArrowRight') next = (index + 1) % MODES.length;
              if (event.key === 'ArrowLeft') next = (index + MODES.length - 1) % MODES.length;
              if (event.key === 'Home') next = 0;
              if (event.key === 'End') next = MODES.length - 1;
              if (next !== undefined) { event.preventDefault(); switchMode(MODES[next].id); event.currentTarget.parentElement.children[next].focus(); }
            }}
          >{item.label}</button>)}
        </div>
        <div
          id="demo-feature-panel"
          role="tabpanel"
          aria-labelledby={`demo-tab-${mode}`}
        >
          {mode === 'meetings' ? <MeetingDemo /> : <><ClientFeatureCard
            mode={mode}
            enabled={enabled}
            onToggle={handleToggle}
          />
          <div className="demo-workspace">
            <div className="demo-editor-toolbar">
              <span className="demo-editor-icon" aria-hidden="true">
                ↗
              </span>
              <span>{example.label}</span>
              <span className="demo-draft-label">Draft</span>
            </div>
            <div className="demo-editor-meta">
              <span>To</span>
              <span>Maya</span>
              <span>Subject</span>
              <strong>{example.title}</strong>
            </div>
            <div className="demo-editor-body" ref={editor} data-selecting={showPointer && elapsed < 1200}>
              {mode === 'command' && !complete || disabled ? (
                <span ref={originalText} aria-label={example.before}>{[...example.before].map((character, index) => <span key={index} aria-hidden="true" className={index < selectionCount ? 'demo-selected-text' : undefined}>{character}</span>)}</span>
              ) : output ? <span>{output}</span> : <span className="demo-editor-placeholder">Your transcription will appear here when you finish speaking.</span>}
              {showPointer && <svg className="demo-selection-pointer" style={{ '--pointer-x': `${pointer.x}px`, '--pointer-y': `${pointer.y}px` }} width="23" height="28" viewBox="0 0 23 28" aria-hidden="true"><path d="M2 2v21l5-5 4 8 4-2-4-8h8Z" fill="#40101d" stroke="white" strokeWidth="2" strokeLinejoin="round" /></svg>}
            </div>
            <div className={`demo-overlay phase-${stage}`}>
              {/* Browser adaptation of the native 62x24, five-bar Windows recording pill. */}
              <div className="demo-recording-pill" aria-hidden="true">
                {complete ? (
                  <svg
                    width="20"
                    height="20"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="m5 12 4 4L19 6" />
                  </svg>
                ) : (
                  <div className="demo-pill-wave">
                    {[0, 1, 2, 3, 4].map((bar) => (
                      <i key={bar} style={{ "--bar": bar }} />
                    ))}
                  </div>
                )}
              </div>
              <div className="demo-voice-preview">
                <span className="demo-preview-label">
                  {disabled
                    ? "Voice commands paused"
                    : stage === 0
                      ? mode === "command"
                        ? selectionCount === example.before.length ? "Double-tap your hotkey" : "Select text in your app"
                        : "Hold your hotkey to speak"
                      : stage === 1
                        ? mode === "command"
                          ? "Voice command"
                          : "Listening to your voice"
                        : stage === 2
                          ? "Applying your words"
                          : "Inserted into your app"}
                </span>
                <span className="demo-preview-speech">
                  {disabled
                    ? "Turn on AI Command Mode above to try it."
                    : stage === 0
                      ? example.hint
                      : stage === 1
                        ? mode === "command" || enabled ? <>{spoken}<span className="demo-insertion-cursor" aria-hidden="true" /></> : "Listening…"
                        : stage === 2
                          ? mode === "command" ? "Applying your command…" : "Finishing your transcription…"
                          : "Ready for your next thought."}
                </span>
              </div>
            </div>
          </div></>}
        </div>
        {mode !== 'meetings' && <><div className="demo-playback">
          <div className="demo-steps" aria-label="Demo progress">
            {stages.map((label, index) => (
              <span
                key={label}
                className={
                  stage > index || stage === stages.length - 1 ? "finished" : stage === index ? "current" : ""
                }
              >
                <i aria-hidden="true">{(stage > index || stage === stages.length - 1) && <svg viewBox="0 0 12 12" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="m2.5 6 2.2 2.2L9.5 3.5" /></svg>}</i>
                {label}
              </span>
            ))}
          </div>
          <div className="demo-playback-actions">
            <button
              type="button"
              aria-label={playing ? "Pause product demo" : "Play product demo"}
              disabled={disabled}
              onClick={() => setPlaying((value) => !value)}
            >
              {playing ? (
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 16 16"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path d="M4 3h3v10H4zm5 0h3v10H9z" />
                </svg>
              ) : (
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 16 16"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path d="m5 2 9 6-9 6z" />
                </svg>
              )}
            </button>
            <button
              type="button"
              aria-label="Replay product demo"
              disabled={disabled}
              onClick={reset}
            >
              ↻
            </button>
          </div>
        </div>
        <span className="demo-sr-only" role="status">
          {disabled ? "Voice commands paused" : stages[stage]}
        </span>
        <p className="demo-footnote">Sample text • No microphone needed</p></>}
      </div>
    </div>
  );
}

export default function DesktopDictationDemo() {
  const host = useRef(null);
  const [shadowRoot, setShadowRoot] = useState(null);
  useEffect(() => {
    setShadowRoot(
      host.current.shadowRoot || host.current.attachShadow({ mode: "open" }),
    );
  }, []);
  return (
    <div
      ref={host}
      style={{ width: "100%", minWidth: 0 }}
      aria-label="Interactive SpeekTo.Me desktop client preview"
    >
      {shadowRoot &&
        createPortal(
          <>
            <style>{clientCSS + demoCSS + meetingCSS}</style>
            <Demo />
          </>,
          shadowRoot,
        )}
    </div>
  );
}
