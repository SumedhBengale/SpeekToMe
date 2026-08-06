import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import LiveFormattingDemo from '../components/LiveFormattingDemo';
import TurnstileWidget from '../components/TurnstileWidget';


const BRAND_LOGOS = [
  { name: 'ChatGPT', file: 'chatgpt.avif' },
  { name: 'Claude', file: 'claude.avif' },
  { name: 'Notion', file: 'notion.avif' },
  { name: 'Slack', file: 'slack.webp' },
  { name: 'VS Code', file: 'vscode.webp' },
  { name: 'Cursor IDE', file: 'cursor-ide.webp' },
  { name: 'Obsidian', file: 'obsidian.webp' },
  { name: 'Linear', file: 'linear.avif' },
  { name: 'Arc Browser', file: 'arc.avif' },
  { name: 'Gmail', file: 'gmail.webp' },
  { name: 'Perplexity', file: 'perplexity.avif' },
  { name: 'Raycast', file: 'raycast.webp' },
  { name: 'Superhuman', file: 'superhuman.avif' },
  { name: 'Evernote', file: 'evernote.avif' },
  { name: 'Canva', file: 'canva.avif' },
  { name: 'Google Keep', file: 'google-keep.avif' },
  { name: 'Grammarly', file: 'grammarly.avif' },
  { name: 'WhatsApp', file: 'whatsapp.avif' },
  { name: 'Telegram', file: 'telegram.avif' },
  { name: 'iMessage', file: 'imessage.avif' },
  { name: 'Zoom', file: 'zoom.avif' },
  { name: 'MS Teams', file: 'teams.webp' },
  { name: 'Things', file: 'things.webp' },
  { name: 'Todoist', file: 'todoist.avif' }
];

const FAQS = [
  {
    q: "How is the transcription latency so low?",
    a: "By downmixing audio locally to 16kHz mono and encoding to Opus, we reduce transmission payloads by 99%. Small payloads travel faster over networks. Combined with high-speed cloud transcription infrastructure, transcription completes almost instantly."
  },
  {
    q: "Is transcription unlimited on Pro?",
    a: "Yes! SpeekTo.Me Pro includes unlimited AI audio transcription without daily session caps. Standard fair-use guidelines apply to safeguard server infrastructure, but normal human dictation is uncapped."
  },
  {
    q: "Is my voice recording data stored permanently?",
    a: "No. We process all audio memory buffers ephemerally. Once our transcription engine finishes translating the Ogg/Opus container to text, the audio clip is immediately deleted from our servers. We never train models on your data."
  },
  {
    q: "Can I customize how the AI formats my voice?",
    a: "Yes. Inside the client dashboard, you can define custom AI instructions to prompt the AI (e.g. 'Write as Python code comments' or 'Keep it in concise bullet points')."
  },
  {
    q: "Which languages are supported?",
    a: "Our transcription engine supports 99+ languages. SpeekTo.Me inherits this multilingual capability automatically. You can dictate in English, Spanish, French, German, Japanese, and many more languages — no configuration needed."
  },
  {
    q: "Do I need an internet connection?",
    a: "Yes, an active internet connection is required. SpeekTo.Me streams compressed audio to our cloud API for transcription and formatting. However, the payload is extremely small (161KB for 60s of audio) so even slow mobile connections work well."
  }
];

export default function HomePage() {
  const [activeFaq, setActiveFaq] = useState(null);
  const [newsletterEmail, setNewsletterEmail] = useState('');
  const [subscribed, setSubscribed] = useState(false);
  const [subscribing, setSubscribing] = useState(false);
  const [subscribeError, setSubscribeError] = useState('');
  const [turnstileToken, setTurnstileToken] = useState('');
  const [resetTurnstile, setResetTurnstile] = useState(0);
  const [isKeyPressed, setIsKeyPressed] = useState(false);
  const [keyStatusText, setKeyStatusText] = useState('Press and hold the key');

  const API_BASE = import.meta.env.VITE_API_URL || (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1' 
    ? 'http://localhost:8000' 
    : '');

  const handleNewsletterSubmit = async (e) => {
    e.preventDefault();
    if (!newsletterEmail) return;
    setSubscribing(true);
    setSubscribeError('');
    try {
      const res = await fetch(`${API_BASE}/newsletter/subscribe`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ 
          email: newsletterEmail,
          'cf-turnstile-response': turnstileToken 
        })
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || 'Failed to subscribe');
      }
      setSubscribed(true);
    } catch (err) {
      setSubscribeError(err.message || 'Failed to subscribe');
      setResetTurnstile(prev => prev + 1);
    } finally {
      setSubscribing(false);
    }
  };

  const handleKeyDown = () => {
    setIsKeyPressed(true);
    setKeyStatusText('Now you can speak...');
  };

  const handleKeyUp = () => {
    setIsKeyPressed(false);
    setKeyStatusText('Text entered!');
    setTimeout(() => {
      setKeyStatusText('Press and hold the key');
    }, 2500);
  };



  return (
    <div className="home-page">
      {/* 1. HERO SECTION */}
      <header className="hero">
        <div className="hero-text reveal visible">
          <div className="hero-badge-top">
            <span className="hero-badge-dot"></span>
            <span>AI-POWERED VOICE DICTATION</span>
          </div>
          <h1>Write <span className="heading-accent">3x faster</span> with your voice.</h1>
          <p className="text-large" style={{ marginTop: '16px', marginBottom: '28px' }}>
            Stop typing and think out loud. SpeekTo.Me turns your voice into instant, polished text inside any app — from Slack and emails to VS Code and Notion, with zero latency.
          </p>

          <div className="hero-actions" style={{ marginBottom: '32px' }}>
            <Link to="/pricing" className="btn btn-primary btn-lg" style={{ padding: '14px 28px', fontSize: '15px' }}>Subscribe for $2/mo →</Link>
            <Link to="/about" className="btn btn-secondary btn-lg" style={{ padding: '14px 24px', fontSize: '15px' }}>How it works</Link>
          </div>

          <div className="os-badge-container">
            <span className="os-label">Supported on</span>
            <div className="os-badges">
              <span className="os-badge" style={{ background: '#d1fae5', color: '#065f46', borderColor: '#a7f3d0' }}>
                <svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor">
                  <path d="M0 3.449L9.75 2.1v9.45H0V3.449zM0 12.45h9.75v9.45L0 20.551v-8.101zM10.95 1.936L24 0v11.55H10.95V1.936zM10.95 12.45H24v11.55l-13.05-1.936v-9.614z" />
                </svg> Windows (Live)
              </span>
              <span className="os-badge">
                <svg viewBox="0 0 170 170" width="15" height="15" fill="currentColor">
                  <path d="M150.37 130.25c-2.45 5.66-5.35 10.87-8.71 15.66-4.58 6.53-8.33 11.05-11.22 13.56-4.48 4.12-9.28 6.23-14.42 6.35-3.69 0-8.14-1.05-13.32-3.18-5.19-2.12-9.97-3.17-14.34-3.17-4.58 0-9.49 1.05-14.75 3.17-5.26 2.13-9.5 3.24-12.74 3.35-4.37.13-9.13-1.9-14.28-6.1-3.48-2.84-7.38-7.7-11.72-14.56-4.83-7.62-8.99-16.71-12.48-27.27-3.48-10.56-5.23-20.96-5.23-31.21 0-14.71 3.73-26.88 11.19-36.5 7.46-9.62 16.73-14.48 27.81-14.59 5.66 0 11.45 1.63 17.38 4.9 5.93 3.26 10.15 4.9 12.66 4.9 2.01 0 6.07-1.69 12.18-5.07 6.1-3.39 11.83-5.01 17.2-4.9 14.15.22 24.96 5.4 32.43 15.53-11.38 6.89-17 16.42-16.88 28.58.11 9.8 3.84 17.9 11.19 24.31 7.34 6.42 16.03 10.02 26.06 10.8-2.31 6.86-5.22 13.56-8.72 20.12zm-35.03-125.7c0 8.01-2.85 15.25-8.56 21.72-5.71 6.47-12.69 10.14-20.93 11-1.09-9.14 2.1-17.17 9.56-24.1 6.42-6.02 13.91-9.14 22.48-9.36.43 3.65.65 6.9.65 10.74z" />
                </svg> macOS
              </span>
              <span className="os-badge">
                <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2"><rect x="5" y="2" width="14" height="20" rx="2" ry="2" /><line x1="12" y1="18" x2="12" y2="18" /></svg> iOS
              </span>
              <span className="os-badge">
                <svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor"><path d="M17.523 15.3c-.551 0-.996-.445-.996-.997 0-.552.445-.997.996-.997.552 0 .997.445.997.997 0 .552-.445.997-.997.997m-11.046 0c-.551 0-.996-.445-.996-.997 0-.552.445-.997.996-.997.552 0 .997.445.997.997 0 .552-.445.997-.997.997m11.43-6.205l1.972-3.415a.417.417 0 00-.152-.567.417.417 0 00-.568.152l-1.996 3.456C15.586 7.915 13.86 7.42 12 7.42c-1.86 0-3.586.495-5.163 1.304L4.84 5.268a.417.417 0 00-.568-.152.417.417 0 00-.152.567l1.97 3.415C2.695 10.997.428 14.369 0 18.257h24c-.428-3.888-2.695-7.26-6.093-9.162" /></svg> Android
              </span>
            </div>
          </div>
        </div>

        {/* Hero Right Visualizer — Live Formatting Demo (Raw -> Redaction -> Polished Text) */}
        <div className="hero-visualizer reveal-right visible" style={{ flex: 1, minWidth: '320px' }}>
          <LiveFormattingDemo />
        </div>


      </header>

      {/* 2. BRANDS SHOWCASE — CURVED DUAL MARQUEE SHOWCASE */}
      <section className="marquee-section-wrapper section-alt section-bordered">
        <div className="container text-center" style={{ marginBottom: '40px' }}>
          <h2>Types directly into <span className="heading-accent">any application</span>.</h2>
          <p className="subtitle" style={{ margin: '0 auto' }}>Works seamlessly with all your favorite desktop & web software.</p>
        </div>

        {/* Top Curved Ribbon */}
        <div className="marquee-container marquee-curved-1">
          <div className="marquee-track">
            {[...BRAND_LOGOS.slice(0, 12), ...BRAND_LOGOS.slice(0, 12), ...BRAND_LOGOS.slice(0, 12)].map((b, idx) => (
              <div key={idx} className="marquee-card">
                <img src={`/assets/brands/${b.file}`} alt={b.name} />
                <span>{b.name}</span>
              </div>
            ))}
          </div>
        </div>

        {/* Bottom Counter-Rotating Curved Ribbon with Generous Vertical Gap */}
        <div className="marquee-container marquee-curved-2">
          <div className="marquee-track-reverse">
            {[...BRAND_LOGOS.slice(12), ...BRAND_LOGOS.slice(12), ...BRAND_LOGOS.slice(12)].map((b, idx) => (
              <div key={idx} className="marquee-card">
                <img src={`/assets/brands/${b.file}`} alt={b.name} />
                <span>{b.name}</span>
              </div>
            ))}
          </div>
        </div>
      </section>




      {/* 3. ONE KEY TO RULE THEM ALL (RIGHT CTRL KEY VISUAL) */}
      <section className="section section-alt section-bordered">
        <div className="container">
          <div className="split-section reverse" style={{ display: 'flex', alignItems: 'center', gap: '48px', flexWrap: 'wrap' }}>
            <div className="split-content reveal-right" style={{ flex: 1, minWidth: '280px' }}>
              <h2>One key to <span className="heading-accent">rule them all</span>.</h2>
              <p className="text-large" style={{ marginTop: '16px', marginBottom: '24px', lineHeight: '1.6' }}>
                SpeekTo.Me intercepts your Right Ctrl Key at the OS level — no app switching, no menu navigation. Hold to speak, release to paste. It works in any text field, in any application, on any screen.
              </p>

              <ul className="feature-list" style={{ listStyle: 'none', padding: 0, display: 'flex', flexDirection: 'column', gap: '12px', fontSize: '15px' }}>
                <li style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                  <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span> Instant OS-level hook
                </li>
                <li style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                  <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span> Zero app context switching
                </li>
                <li style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                  <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span> Configurable key bindings
                </li>
              </ul>
            </div>

            <div className="split-visual reveal-left" style={{ flex: 1, minWidth: '280px', display: 'flex', justifyContent: 'center' }}>
              <div className="keyboard-key-container">
                <button
                  className={`key-3d-button ${isKeyPressed ? 'pressed' : ''}`}
                  onMouseDown={handleKeyDown}
                  onMouseUp={handleKeyUp}
                  onMouseLeave={() => setIsKeyPressed(false)}
                  onTouchStart={handleKeyDown}
                  onTouchEnd={handleKeyUp}
                  aria-label="Test Right Ctrl keypress"
                >
                  <span className="key-3d-subtext">Press & Hold</span>
                  <span className="key-3d-title">Right Ctrl</span>
                </button>
                <div className="key-3d-status">
                  {keyStatusText}
                </div>
                <span style={{ fontSize: '11px', color: 'rgba(26, 26, 26, 0.45)', textTransform: 'uppercase', letterSpacing: '0.8px', marginTop: '10px', fontWeight: '600' }}>
                  Interactive Visual Simulation
                </span>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* 3.5. SUPPORTED PLATFORMS SECTION */}

      <section className="section section-bordered platforms-section" style={{ background: '#ffffff', padding: '72px 6%' }}>
        <div className="container" style={{ maxWidth: '1000px', margin: '0 auto' }}>
          <div className="section-header text-center" style={{ marginBottom: '44px' }}>
            <h2>Available across <span className="heading-accent">all your platforms</span></h2>
            <p className="subtitle" style={{ margin: '0 auto' }}>Dictate everywhere you work — desktop, mobile, and browser extensions.</p>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '20px' }}>

            {/* Windows — LIVE */}
            <div style={{ background: 'var(--bg-canvas)', border: '2px solid #240a12', borderRadius: '20px', padding: '20px 24px', boxShadow: '4px 4px 0px #240a12', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#0078d4', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                  <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
                    <path d="M0 3.449L9.75 2.1v9.45H0V3.449zM0 12.45h9.75v9.45L0 20.551v-8.101zM10.95 1.936L24 0v11.55H10.95V1.936zM10.95 12.45H24v11.55l-13.05-1.936v-9.614z" />
                  </svg>
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Windows Desktop</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>Native Win32 Client</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#d1fae5', color: '#065f46', border: '1px solid #a7f3d0', padding: '4px 10px', borderRadius: '14px', fontWeight: '700', letterSpacing: '0.5px' }}>
                LIVE NOW
              </span>
            </div>

            {/* macOS — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(36,10,18,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/macos.png" alt="macOS Client" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>macOS Client</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>Apple Silicon & Intel</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>


            {/* iOS — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(36,10,18,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                  <img src="/assets/brands/ios.png" alt="iOS Client" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>iOS Mobile</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>iPhone & iPad App</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Android — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(36,10,18,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/android.png" alt="Android Mobile" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Android Mobile</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>Phone & Tablet App</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Google Chrome — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(36,10,18,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/chrome.png" alt="Google Chrome" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Chrome Extension</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>Browser Web Extension</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Safari — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(36,10,18,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(36,10,18,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/safari.png" alt="Apple Safari" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Safari Extension</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(36,10,18,0.65)' }}>macOS & iOS Extension</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>




          </div>
        </div>
      </section>



      {/* 4. LIVE DEMO — BEFORE/AFTER */}
      <section className="section section-alt section-bordered demo-section">
        <div className="container">
          <div className="section-header text-center">
            <h2>From messy speech to <span className="heading-accent">polished text</span>.</h2>
            <p className="subtitle" style={{ margin: '0 auto' }}>Watch how SpeekTo.Me transforms raw dictation into clean, professional writing in real time.</p>
          </div>

          <LiveFormattingDemo />
        </div>
      </section>


      {/* 5. HOW IT WORKS — 3 STEPS */}
      <section className="section">
        <div className="container">
          <div className="section-header text-center">
            <h2>How it works in <span className="heading-accent">three steps</span>.</h2>
            <p className="subtitle" style={{ margin: '0 auto' }}>Using SpeekTo.Me is designed to feel like a natural extension of your keyboard.</p>
          </div>


          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '24px', marginTop: '48px' }}>
            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-btn-primary)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>1</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Trigger Global Hotkey</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Press and hold the Right Ctrl Key anywhere on your system to begin recording.
              </p>
            </div>

            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-btn-primary)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>2</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Speak Naturally</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Talk at your normal pace. The client captures your voice and packages it securely for fast transmission.
              </p>
            </div>

            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-btn-primary)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>3</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Release to Type</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Release the hotkey. Our transcription system converts and formats your audio instantly, pasting the text into your active window.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* 6. FAQ SECTION */}
      <section className="section section-alt section-bordered">
        <div className="container" style={{ maxWidth: '800px', margin: '0 auto' }}>
          <div className="section-header text-center">
            <h2>Frequently Asked <span className="heading-accent">Questions</span></h2>
            <p className="subtitle" style={{ margin: '0 auto' }}>Got questions about limits, privacy, or setup? We have answers.</p>
          </div>


          <div className="faq-accordion-list">
            {FAQS.map((faq, idx) => {
              const isOpen = activeFaq === idx;
              return (
                <div key={idx} className={`faq-item-advanced ${isOpen ? 'open' : ''}`}>
                  <button
                    className="faq-header-button"
                    onClick={() => setActiveFaq(isOpen ? null : idx)}
                    aria-expanded={isOpen}
                  >
                    <span>{faq.q}</span>
                    <span className="faq-icon-wrapper">
                      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                        <line x1="12" y1="5" x2="12" y2="19" />
                        <line x1="5" y1="12" x2="19" y2="12" />
                      </svg>
                    </span>
                  </button>
                  <div className="faq-content-grid">
                    <div className="faq-content-inner">
                      <div className="faq-answer-body">
                        {faq.a}
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

        </div>
      </section>

      {/* 7. NEWSLETTER SECTION — LIGHT CARD WITH BORDER ACCENT */}
      <section className="section newsletter-section" style={{ background: '#ffffff', borderTop: '2px solid var(--border-color)', borderBottom: '2px solid var(--border-color)', padding: '64px 6%' }}>
        <div className="container" style={{ maxWidth: '640px', margin: '0 auto', textAlign: 'center' }}>
          <h3 style={{ color: 'var(--color-text-dark)', fontSize: '28px', marginBottom: '12px' }}>Stay in the loop.</h3>
          <p className="text-muted" style={{ fontSize: '15px', marginBottom: '24px' }}>
            Get product updates, tips, and early access to new features delivered to your inbox.
          </p>
          <form
            onSubmit={handleNewsletterSubmit}
            style={{ display: 'flex', gap: '12px', justifyContent: 'center', flexWrap: 'wrap' }}
          >
            <input
              type="email"
              placeholder="you@example.com"
              value={newsletterEmail}
              onChange={(e) => setNewsletterEmail(e.target.value)}
              disabled={subscribed || subscribing}
              required
              style={{
                padding: '12px 18px',
                borderRadius: '12px',
                border: '2px solid #240a12',
                width: '280px',
                fontSize: '14px',
                background: '#ffffff',
                boxShadow: '2px 2px 0px #240a12'
              }}
            />
            <button type="submit" className="btn btn-primary" disabled={subscribed || subscribing || !turnstileToken}>
              {subscribed ? '✓ Subscribed!' : subscribing ? 'Subscribing...' : 'Subscribe'}
            </button>
          </form>
          {!subscribed && (
            <TurnstileWidget 
              onVerify={setTurnstileToken} 
              resetTrigger={resetTurnstile} 
            />
          )}
          {subscribeError && (
            <p style={{ color: '#dc2626', fontSize: '14px', marginTop: '12px' }}>
              {subscribeError}
            </p>
          )}
        </div>
      </section>

      {/* 8. CALL TO ACTION BANNER — VIBRANT COLORFUL CARD */}
      <section className="cta-banner" style={{ padding: '80px 6%', background: 'var(--bg-canvas)', borderBottom: '2px solid #240a12' }}>
        <div
          className="container text-center"
          style={{
            maxWidth: '880px',
            margin: '0 auto',
            background: 'linear-gradient(135deg, #fff0f3 0%, #ffd1dc 45%, #ffe1e5 100%)',
            border: '3px solid #240a12',
            borderRadius: '32px',
            padding: '64px 40px',
            boxShadow: '8px 8px 0px #240a12',
            position: 'relative',
            overflow: 'hidden'
          }}
        >
          {/* Subtle Ambient Glow */}
          <div style={{ position: 'absolute', top: '-50%', left: '-50%', width: '200%', height: '200%', background: 'radial-gradient(circle at 50% 50%, rgba(255, 209, 220, 0.4) 0%, transparent 60%)', pointerEvents: 'none' }}></div>

          <h2 style={{ fontSize: '42px', fontWeight: '800', color: '#240a12', marginBottom: '18px', letterSpacing: '-0.5px', position: 'relative' }}>
            Speak naturally. <span className="heading-accent">Write effortlessly.</span>
          </h2>
          <p style={{ fontSize: '18px', color: 'rgba(36, 10, 18, 0.8)', lineHeight: '1.65', maxWidth: '640px', margin: '0 auto 36px', position: 'relative' }}>
            Join thousands of creators, engineers, and founders using SpeekTo.Me to dictate text 3x faster with instant AI formatting.
          </p>
          <div style={{ display: 'flex', gap: '16px', justifyContent: 'center', flexWrap: 'wrap', position: 'relative' }}>
            <Link to="/pricing" className="btn btn-primary btn-lg" style={{ fontSize: '16px', padding: '16px 36px', boxShadow: '4px 4px 0px #240a12' }}>
              Subscribe for $2/mo →
            </Link>
            <Link to="/about" className="btn btn-secondary btn-lg" style={{ fontSize: '16px', padding: '16px 30px', boxShadow: '4px 4px 0px #240a12' }}>
              Explore Features
            </Link>
          </div>
        </div>
      </section>



    </div>
  );
}
