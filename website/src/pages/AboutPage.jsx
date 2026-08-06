import React from 'react';
import { Link } from 'react-router-dom';

export default function AboutPage() {
  return (
    <div className="about-page">
      {/* 1. HERO SECTION WITH STATS */}
      <section className="about-hero" style={{ padding: '72px 6% 48px', textAlign: 'center' }}>
        <div className="container" style={{ maxWidth: '1150px', margin: '0 auto' }}>
          <h1 style={{ fontSize: '46px', fontWeight: '800', lineHeight: '1.2', marginBottom: '24px', letterSpacing: '-0.5px' }}>
            Building the native <span className="heading-accent">voice layer</span> for your OS.
          </h1>
          <p className="subtitle text-muted" style={{ fontSize: '19px', lineHeight: '1.65', maxWidth: '820px', margin: '0 auto' }}>
            At SpeekTo.Me, our mission is to eliminate typing friction completely. Typing limits your speed of thought — we built a lightweight desktop client that turns natural speech into instant, formatted text directly inside any app.
          </p>

          {/* Stat Badges Row */}
          <div className="about-grid-3" style={{ marginTop: '44px' }}>
            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '18px', padding: '20px 24px', boxShadow: '4px 4px 0px #240a12', textAlign: 'center' }}>
              <div style={{ fontSize: '32px', fontWeight: '800', color: '#240a12' }}>&lt; 200ms</div>
              <div style={{ fontSize: '13px', fontWeight: '600', color: 'rgba(36,10,18,0.65)', textTransform: 'uppercase', letterSpacing: '0.5px', marginTop: '4px' }}>Audio Pipeline</div>
            </div>
            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '18px', padding: '20px 24px', boxShadow: '4px 4px 0px #240a12', textAlign: 'center' }}>
              <div style={{ fontSize: '32px', fontWeight: '800', color: '#240a12' }}>100%</div>
              <div style={{ fontSize: '13px', fontWeight: '600', color: 'rgba(36,10,18,0.65)', textTransform: 'uppercase', letterSpacing: '0.5px', marginTop: '4px' }}>Zero-Log Privacy</div>
            </div>
            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '18px', padding: '20px 24px', boxShadow: '4px 4px 0px #240a12', textAlign: 'center' }}>
              <div style={{ fontSize: '32px', fontWeight: '800', color: '#240a12' }}>3x Faster</div>
              <div style={{ fontSize: '13px', fontWeight: '600', color: 'rgba(36,10,18,0.65)', textTransform: 'uppercase', letterSpacing: '0.5px', marginTop: '4px' }}>Vs Keyboard Typing</div>
            </div>
          </div>
        </div>
      </section>

      {/* 2. MISSION QUOTE BANNER */}
      <section className="section section-bordered" style={{ background: 'var(--bg-alt)', padding: '64px 6%', borderTop: '2px solid #240a12', borderBottom: '2px solid #240a12' }}>
        <div className="container text-center" style={{ maxWidth: '1100px', margin: '0 auto' }}>
          <p style={{ fontSize: '24px', fontWeight: '600', fontStyle: 'italic', lineHeight: '1.5', color: 'var(--color-text-dark)' }}>
            "The keyboard is a bottleneck. Your thoughts move at the speed of speech — your tools should keep up."
          </p>
        </div>
      </section>

      {/* 3. VISUAL ARCHITECTURE PIPELINE */}
      <section className="section" style={{ padding: '88px 6%' }}>
        <div className="container" style={{ maxWidth: '1200px', margin: '0 auto' }}>
          <div className="section-header text-center" style={{ marginBottom: '52px' }}>
            <h2>How we achieve <span className="heading-accent">ultra-low latency</span></h2>
            <p className="subtitle" style={{ margin: '0 auto', maxWidth: '700px' }}>Every step of our pipeline is engineered for zero lag — from mic capture to instant paste.</p>
          </div>

          {/* Interactive Pipeline Cards — 4 Columns on Desktop */}
          <div className="about-grid-4">
            
            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '20px', padding: '28px 24px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ fontSize: '11px', fontWeight: '800', color: '#065f46', background: '#d1fae5', padding: '4px 12px', borderRadius: '10px', display: 'inline-block', marginBottom: '14px' }}>STEP 01</div>
              <h4 style={{ fontSize: '18px', fontWeight: '700', marginBottom: '10px' }}>Hotkey Capture</h4>
              <p style={{ fontSize: '14px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.6' }}>Right Ctrl key hook intercepts audio recording at native OS kernel level.</p>
            </div>

            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '20px', padding: '28px 24px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ fontSize: '11px', fontWeight: '800', color: '#1e40af', background: '#dbeafe', padding: '4px 12px', borderRadius: '10px', display: 'inline-block', marginBottom: '14px' }}>STEP 02</div>
              <h4 style={{ fontSize: '18px', fontWeight: '700', marginBottom: '10px' }}>Opus Compression</h4>
              <p style={{ fontSize: '14px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.6' }}>Microphone stream compressed in real time saving 99% network payload.</p>
            </div>

            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '20px', padding: '28px 24px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ fontSize: '11px', fontWeight: '800', color: '#92400e', background: '#fef3c7', padding: '4px 12px', borderRadius: '10px', display: 'inline-block', marginBottom: '14px' }}>STEP 03</div>
              <h4 style={{ fontSize: '18px', fontWeight: '700', marginBottom: '10px' }}>AI STT & Formatting</h4>
              <p style={{ fontSize: '14px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.6' }}>Whisper models transcribe and strip filler words in under 200 milliseconds.</p>
            </div>

            <div style={{ background: '#ffffff', border: '2px solid #240a12', borderRadius: '20px', padding: '28px 24px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ fontSize: '11px', fontWeight: '800', color: '#7e22ce', background: '#f3e8ff', padding: '4px 12px', borderRadius: '10px', display: 'inline-block', marginBottom: '14px' }}>STEP 04</div>
              <h4 style={{ fontSize: '18px', fontWeight: '700', marginBottom: '10px' }}>Active Injection</h4>
              <p style={{ fontSize: '14px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.6' }}>Text is pasted directly into your active window cursor location seamlessly.</p>
            </div>

          </div>
        </div>
      </section>

      {/* 4. CORE PRINCIPLES */}
      <section className="section section-bordered" style={{ background: '#ffffff', padding: '88px 6%', borderTop: '2px solid #240a12', borderBottom: '2px solid #240a12' }}>
        <div className="container" style={{ maxWidth: '1200px', margin: '0 auto' }}>
          <div className="section-header text-center" style={{ marginBottom: '52px' }}>
            <h2>Core engineering <span className="heading-accent">principles</span></h2>
          </div>

          {/* Core Principles — 3 Columns on Desktop */}
          <div className="about-grid-3">

            
            <div style={{ background: 'var(--bg-canvas)', border: '2px solid #240a12', borderRadius: '20px', padding: '36px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ width: '46px', height: '46px', borderRadius: '12px', background: '#ffd1dc', border: '1.5px solid #240a12', display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: '20px' }}>
                <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
                </svg>
              </div>
              <h3 style={{ fontSize: '21px', fontWeight: '700', marginBottom: '12px' }}>Speed Above All</h3>
              <p style={{ fontSize: '15px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.65' }}>
                We obsess over milliseconds. Audio sampling, compression, and paste triggers are optimized for minimal latency.
              </p>
            </div>

            <div style={{ background: 'var(--bg-canvas)', border: '2px solid #240a12', borderRadius: '20px', padding: '36px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ width: '46px', height: '46px', borderRadius: '12px', background: '#d1fae5', border: '1.5px solid #240a12', display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: '20px' }}>
                <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                  <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                </svg>
              </div>
              <h3 style={{ fontSize: '21px', fontWeight: '700', marginBottom: '12px' }}>Privacy First</h3>
              <p style={{ fontSize: '15px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.65' }}>
                Your audio stream is processed ephemerally and discarded immediately. We do not store or train on your personal audio.
              </p>
            </div>

            <div style={{ background: 'var(--bg-canvas)', border: '2px solid #240a12', borderRadius: '20px', padding: '36px', boxShadow: '4px 4px 0px #240a12' }}>
              <div style={{ width: '46px', height: '46px', borderRadius: '12px', background: '#ffe1e5', border: '1.5px solid #240a12', display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: '20px' }}>
                <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10" />
                  <line x1="12" y1="8" x2="12" y2="12" />
                  <line x1="12" y1="16" x2="12.01" y2="16" />
                </svg>
              </div>
              <h3 style={{ fontSize: '21px', fontWeight: '700', marginBottom: '12px' }}>Invisible Design</h3>
              <p style={{ fontSize: '15px', color: 'rgba(36,10,18,0.75)', lineHeight: '1.65' }}>
                The best dictation tool is invisible. SpeekTo.Me runs quietly in your tray, appearing only when summoned.
              </p>
            </div>

          </div>
        </div>
      </section>

      {/* 5. TEAM & STUDIO SPOTLIGHT CARD */}
      <section className="section text-center" style={{ padding: '88px 6%' }}>
        <div
          className="container"
          style={{
            maxWidth: '960px',
            margin: '0 auto',
            background: '#ffffff',
            border: '3px solid #240a12',
            borderRadius: '28px',
            padding: '56px 48px',
            boxShadow: '6px 6px 0px #240a12'
          }}
        >
          <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '1.2px', fontWeight: '800', color: '#240a12', background: '#ffd1dc', padding: '5px 14px', borderRadius: '16px', border: '1.5px solid #240a12', display: 'inline-block', marginBottom: '18px' }}>
            ENGINEERED BY SYNAPTICAN
          </span>
          <h2 style={{ fontSize: '36px', fontWeight: '800', color: '#240a12', marginBottom: '16px', letterSpacing: '-0.5px' }}>
            Built for everyone who writes.
          </h2>
          <p style={{ fontSize: '17px', color: 'rgba(36, 10, 18, 0.75)', lineHeight: '1.65', maxWidth: '720px', margin: '0 auto 32px' }}>
            Whether you're drafting emails, taking notes, writing documents, or coding — SpeekTo.Me is crafted by Synaptican to give everyone lightweight, privacy-first voice dictation.
          </p>

          <Link to="/pricing" className="btn btn-primary btn-lg" style={{ fontSize: '16px', padding: '14px 32px' }}>
            Join SpeekTo.Me Pro Today →
          </Link>
        </div>
      </section>
    </div>
  );
}
