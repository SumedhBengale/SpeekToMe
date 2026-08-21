import React, { useState, useEffect } from 'react';

const RAW_TEXT = "umm so basically the the project timeline needs to be updated and i think we should uhh move the deadline to like next friday or something because the the team hasnt finished the requirements doc yet and theres been alot of back and forth about it";

const POLISHED_TEXT = "The project timeline needs to be updated. I think we should move the deadline to next Friday because the team hasn't finished the requirements document yet, and there's been a lot of back and forth about it.";

export default function LiveFormattingDemo() {
  const [phase, setPhase] = useState('raw'); // 'raw' | 'editing' | 'polished'
  const [displayedText, setDisplayedText] = useState('');
  const [charIndex, setCharIndex] = useState(0);

  // Typewriter effect for raw phase, then transition to editing -> polished
  useEffect(() => {
    let timer;
    if (phase === 'raw') {
      if (charIndex < RAW_TEXT.length) {
        timer = setTimeout(() => {
          setDisplayedText(RAW_TEXT.slice(0, charIndex + 3));
          setCharIndex((prev) => prev + 3);
        }, 20);
      } else {
        // Pause, then start AI editing phase
        timer = setTimeout(() => {
          setPhase('editing');
        }, 1200);
      }
    } else if (phase === 'editing') {
      // AI highlight sweep phase
      timer = setTimeout(() => {
        setPhase('polished');
      }, 1400);
    } else if (phase === 'polished') {
      // Hold polished state for 4s, then loop back to raw
      timer = setTimeout(() => {
        setPhase('raw');
        setDisplayedText('');
        setCharIndex(0);
      }, 4500);
    }

    return () => clearTimeout(timer);
  }, [phase, charIndex]);

  return (
    <div className="live-demo-wrapper">
      <div className="live-demo-grid">

        {/* RAW DICTATION CARD */}
        <div className="demo-card demo-card-raw">
          <div className="demo-card-content">
            <div className="demo-card-header">
              <div className="demo-card-title">
                <span className="demo-live-dot"></span>
                <span>RAW VOICE DICTATION</span>
              </div>
            </div>

            <div className="demo-text-box demo-text-raw">
              "{displayedText}"
              {phase === 'raw' && <span className="demo-cursor">|</span>}
            </div>
          </div>

          <div className="demo-card-footer">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" /><path d="M19 10v2a7 7 0 0 1-14 0v-2" /></svg>
            <span>Unfiltered speech input with stutters & fillers</span>
          </div>
        </div>

        {/* AI REFINEMENT LIVE TRANSFORM CARD */}
        <div className={`demo-card demo-card-ai ${phase === 'polished' ? 'is-polished' : ''}`}>
          <div className="demo-card-content">
            <div className="demo-card-header">
              <div className="demo-card-title demo-card-title-ai">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" /></svg>
                <span>AI FORMATTED OUTPUT</span>
              </div>

              <span className={`demo-status-pill status-${phase}`}>
                {phase === 'raw' && 'Waiting for audio...'}
                {phase === 'editing' && '⚡ Live AI Cleaning...'}
                {phase === 'polished' && '✓ Ready & Pasted!'}
              </span>
            </div>

            <div className={`demo-text-box demo-text-ai ${phase === 'polished' ? 'is-polished' : ''}`}>
              {phase === 'raw' && (
                <span className="demo-placeholder-text">
                  AI output will appear here as speech is processed...
                </span>
              )}

              {phase === 'editing' && (
                <div className="demo-editing-text">
                  <span className="demo-strike-tag">umm so basically the the</span>
                  <span>project timeline needs to be updated. I think we should </span>
                  <span className="demo-strike-tag">uhh</span>
                  <span>move the deadline to </span>
                  <span className="demo-strike-tag">like</span>
                  <span>next Friday...</span>
                </div>
              )}

              {phase === 'polished' && (
                <div className="demo-polished-text">
                  {POLISHED_TEXT}
                </div>
              )}
            </div>
          </div>

          <div className="demo-card-footer demo-card-footer-ai">
            <span className="demo-footer-info">
              {phase === 'polished' ? 'Instant paste into active text cursor' : 'Grammar & filler removal active'}
            </span>
            <button
              className="demo-replay-btn"
              onClick={() => { setPhase('raw'); setDisplayedText(''); setCharIndex(0); }}
              aria-label="Replay dictation demo"
            >
              ↻ Replay Demo
            </button>
          </div>
        </div>

      </div>
    </div>
  );
}

