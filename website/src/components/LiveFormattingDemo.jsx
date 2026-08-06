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
    <div className="live-demo-wrapper" style={{ marginTop: '40px' }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '24px' }}>

        {/* RAW DICTATION CARD */}
        <div style={{
          background: '#ffffff',
          border: '2px solid #1a1a1a',
          borderRadius: '24px',
          padding: '28px',
          boxShadow: '4px 4px 0px #1a1a1a',
          display: 'flex',
          flexDirection: 'column',
          justify: 'space-between',
          position: 'relative'
        }}>
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px', fontWeight: '700' }}>
                <span style={{ width: '10px', height: '10px', borderRadius: '50%', background: '#ef4444', display: 'inline-block', animation: 'pulse 1.5s infinite' }}></span>
                <span>RAW VOICE DICTATION</span>
              </div>
            </div>

            <div style={{
              fontSize: '15px',
              lineHeight: '1.7',
              color: 'rgba(26, 26, 26, 0.75)',
              fontFamily: 'monospace',
              background: '#f9fafb',
              padding: '16px',
              borderRadius: '16px',
              border: '1px solid #e5e7eb',
              minHeight: '130px'
            }}>
              "{displayedText}"
              {phase === 'raw' && <span style={{ fontWeight: 'bold', color: '#ef4444', animation: 'blink 0.8s infinite' }}>|</span>}
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginTop: '16px', fontSize: '12px', color: 'rgba(26, 26, 26, 0.5)' }}>
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" /><path d="M19 10v2a7 7 0 0 1-14 0v-2" /></svg>
            <span>Unfiltered speech input with stutters & fillers</span>
          </div>
        </div>

        {/* AI REFINEMENT LIVE TRANSFORM CARD */}
        <div style={{
          background: phase === 'polished' ? '#f0fdf4' : '#ffffff',
          border: phase === 'polished' ? '2px solid #16a34a' : '2px solid #1a1a1a',
          borderRadius: '24px',
          padding: '28px',
          boxShadow: phase === 'polished' ? '4px 4px 0px #16a34a' : '4px 4px 0px #1a1a1a',
          transition: 'all 0.4s ease',
          display: 'flex',
          flexDirection: 'column',
          justify: 'space-between'
        }}>
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px', fontWeight: '700', color: phase === 'polished' ? '#15803d' : '#1a1a1a' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" /></svg>
                <span>AI FORMATTED OUTPUT</span>
              </div>

              <span style={{
                fontSize: '11px',
                background: phase === 'polished' ? '#d1fae5' : phase === 'editing' ? '#fef3c7' : '#f3f4f6',
                color: phase === 'polished' ? '#065f46' : phase === 'editing' ? '#92400e' : '#6b7280',
                padding: '3px 10px',
                borderRadius: '12px',
                fontWeight: '700',
                transition: 'all 0.3s ease'
              }}>
                {phase === 'raw' && 'Waiting for audio...'}
                {phase === 'editing' && '⚡ Live AI Cleaning...'}
                {phase === 'polished' && '✓ Ready & Pasted!'}
              </span>
            </div>

            <div style={{
              fontSize: '15px',
              lineHeight: '1.7',
              fontWeight: '500',
              color: '#1a1a1a',
              background: '#ffffff',
              padding: '16px',
              borderRadius: '16px',
              border: phase === 'polished' ? '1px solid #86efac' : '1px solid #e5e7eb',
              minHeight: '130px',
              position: 'relative',
              overflow: 'hidden'
            }}>
              {phase === 'raw' && (
                <span style={{ opacity: 0.4, fontStyle: 'italic' }}>
                  AI output will appear here as speech is processed...
                </span>
              )}

              {phase === 'editing' && (
                <div>
                  <span style={{ textDecoration: 'line-through', color: '#ef4444', background: '#fee2e2', padding: '0 4px', borderRadius: '4px', marginRight: '4px' }}>umm so basically the the</span>
                  <span>project timeline needs to be updated. I think we should </span>
                  <span style={{ textDecoration: 'line-through', color: '#ef4444', background: '#fee2e2', padding: '0 4px', borderRadius: '4px', marginRight: '4px' }}>uhh</span>
                  <span>move the deadline to </span>
                  <span style={{ textDecoration: 'line-through', color: '#ef4444', background: '#fee2e2', padding: '0 4px', borderRadius: '4px', marginRight: '4px' }}>like</span>
                  <span>next Friday...</span>
                </div>
              )}

              {phase === 'polished' && (
                <div style={{ animation: 'fadeIn 0.4s ease' }}>
                  {POLISHED_TEXT}
                </div>
              )}
            </div>
          </div>

          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '16px' }}>
            <span style={{ fontSize: '12px', color: 'rgba(26, 26, 26, 0.6)', fontWeight: '500' }}>
              {phase === 'polished' ? 'Instant paste into active text cursor' : 'Grammar & filler removal active'}
            </span>
            <button
              onClick={() => { setPhase('raw'); setDisplayedText(''); setCharIndex(0); }}
              style={{
                background: 'transparent',
                border: '1px solid #d1d5db',
                borderRadius: '8px',
                padding: '4px 10px',
                fontSize: '11px',
                fontWeight: '600',
                cursor: 'pointer',
                color: '#374151'
              }}
            >
              ↻ Replay Demo
            </button>
          </div>
        </div>

      </div>
    </div>
  );
}
