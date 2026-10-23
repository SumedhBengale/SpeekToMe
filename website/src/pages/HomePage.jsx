import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import WritingExamples from '../components/WritingExamples';
import VocabularyExample from '../components/VocabularyExample';
import DesktopDictationDemo from '../components/DesktopDictationDemo';
import VoiceToWords from '../components/VoiceToWords';
import AppRibbon from '../components/AppRibbon';
import TurnstileWidget from '../components/TurnstileWidget';


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

  return (
    <div className="home-page">
      <header className="hero-section">
        <div className="hero">
          <div className="hero-text">
            <div className="hero-eyebrow"><span /> SPEAK FREELY. WRITE ANYWHERE.</div>
            <h1 className="hero-heading"><span className="hero-heading-lead">Your thoughts, </span><span className="hero-heading-flow">in full flow.</span></h1>
            <p className="hero-description">
              Turn your voice into clear, polished text in the apps you already use.
              Use your hotkey, speak, and let your words flow.
            </p>
            <div className="hero-actions">
              <Link to="/pricing" className="btn btn-primary btn-lg">Get started <span aria-hidden="true">↗</span></Link>
              <a href="#product-demo" className="hero-how-link">See it in action <span aria-hidden="true">↓</span></a>
            </div>
            <p className="hero-plan-note">Unlimited transcription on Pro. <strong>$2 /month.</strong></p>
            <p className="hero-availability">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" aria-hidden="true"><path d="M0 3.449L9.75 2.1v9.45H0V3.449zM0 12.45h9.75v9.45L0 20.551v-8.101zM10.95 1.936L24 0v11.55H10.95V1.936zM10.95 12.45H24v11.55l-13.05-1.936v-9.614z" /></svg>
              Available for Windows <span>·</span> More platforms on the way
            </p>
          </div>
          <VoiceToWords />
        </div>
      </header>

      <AppRibbon />

      <section className="section product-demo-section" id="product-demo" aria-labelledby="product-demo-title">
        <div className="container">
          <div className="section-header text-center">
            <p className="demo-section-eyebrow">MAKE YOURSELF AT HOME</p>
            <h2 id="product-demo-title">A little less typing.<br /><span className="heading-accent">A lot more flow.</span></h2>
            <p>Explore dictation, voice commands, and meetings in the interactive preview.</p>
          </div>
          <div className="hero-preview">
            <div className="hero-visualizer"><DesktopDictationDemo /></div>
            <p className="hero-allowance-note">Command Mode has a separate <Link to="/pricing#command-mode-allowance">monthly allowance</Link>.</p>
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

          <div className="platform-cards">

            {/* Windows — LIVE */}
            <div style={{ background: 'var(--bg-canvas)', border: 'var(--border-light)', borderRadius: '20px', padding: '20px 24px', boxShadow: 'var(--shadow-sm)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#0078d4', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                  <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
                    <path d="M0 3.449L9.75 2.1v9.45H0V3.449zM0 12.45h9.75v9.45L0 20.551v-8.101zM10.95 1.936L24 0v11.55H10.95V1.936zM10.95 12.45H24v11.55l-13.05-1.936v-9.614z" />
                  </svg>
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Windows Desktop</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>Native Win32 Client</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#d1fae5', color: '#065f46', border: '1px solid #a7f3d0', padding: '4px 10px', borderRadius: '14px', fontWeight: '700', letterSpacing: '0.5px' }}>
                LIVE NOW
              </span>
            </div>

            {/* macOS — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(23,23,25,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/macos.png" alt="macOS Client" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>macOS Client</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>Apple Silicon & Intel</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>


            {/* iOS — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(23,23,25,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                  <img src="/assets/brands/ios.png" alt="iOS Client" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>iOS Mobile</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>iPhone & iPad App</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Android — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(23,23,25,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/android.png" alt="Android Mobile" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Android Mobile</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>Phone & Tablet App</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Google Chrome — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(23,23,25,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/chrome.png" alt="Google Chrome" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Chrome Extension</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>Browser Web Extension</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>

            {/* Safari — COMING SOON */}
            <div style={{ background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.2)', borderRadius: '20px', padding: '20px 24px', boxShadow: '2px 2px 0px rgba(23,23,25,0.08)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                <div style={{ width: '42px', height: '42px', borderRadius: '12px', background: '#ffffff', border: '1.5px solid rgba(23,23,25,0.15)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, padding: '4px' }}>
                  <img src="/assets/brands/safari.png" alt="Apple Safari" style={{ width: '100%', height: '100%', objectFit: 'contain' }} />
                </div>
                <div>
                  <h4 style={{ margin: 0, fontSize: '16px', fontWeight: '700' }}>Safari Extension</h4>
                  <span style={{ fontSize: '12.5px', color: 'rgba(23,23,25,0.65)' }}>macOS & iOS Extension</span>
                </div>
              </div>
              <span style={{ fontSize: '10.5px', background: '#f3f4f6', color: '#4b5563', border: '1px solid #e5e7eb', padding: '4px 10px', borderRadius: '14px', fontWeight: '600' }}>
                COMING SOON
              </span>
            </div>




          </div>
        </div>
      </section>



      <WritingExamples />
      <VocabularyExample />


      {/* 5. HOW IT WORKS — 3 STEPS */}
      <section className="section" id="how-it-works">
        <div className="container">
          <div className="section-header text-center">
            <h2>How it works in <span className="heading-accent">three steps</span>.</h2>
            <p className="subtitle" style={{ margin: '0 auto' }}>Using SpeekTo.Me is designed to feel like a natural extension of your keyboard.</p>
          </div>


          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 280px), 1fr))', gap: '24px', marginTop: '48px' }}>
            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-brand-tint)', color: 'var(--color-brand-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>1</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Use your hotkey</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Use your configured hotkey to start dictation in any app.
              </p>
            </div>

            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-brand-tint)', color: 'var(--color-brand-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>2</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Speak Naturally</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Talk at your normal pace. The client captures your voice and packages it securely for fast transmission.
              </p>
            </div>

            <div style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '32px' }}>
              <div style={{ width: '40px', height: '40px', borderRadius: '12px', background: 'var(--color-brand-tint)', color: 'var(--color-brand-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: '700', marginBottom: '16px' }}>3</div>
              <h3 style={{ fontSize: '20px', marginBottom: '12px' }}>Turn speech into text</h3>
              <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
                Finish dictating. SpeekTo.Me turns your speech into text and pastes it into your active window.
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
      <section className="section newsletter-section" style={{ background: '#ffffff', borderTop: 'var(--border-light)', borderBottom: 'var(--border-light)', padding: '64px 6%' }}>
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
              aria-label="Email address for product updates"
              value={newsletterEmail}
              onChange={(e) => setNewsletterEmail(e.target.value)}
              disabled={subscribed || subscribing}
              required
              style={{
                padding: '12px 18px',
                borderRadius: '12px',
                border: 'var(--border-light)',
                width: '280px',
                fontSize: '14px',
                background: '#ffffff',
                boxShadow: 'var(--shadow-sm)'
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

      {/* 8. CALL TO ACTION */}
      <section className="cta-banner" style={{ padding: '80px 6%', background: 'var(--bg-canvas)', borderBottom: 'var(--border-light)' }}>
        <div
          className="container text-center"
          style={{
            maxWidth: '880px',
            margin: '0 auto',
            background: 'var(--bg-alt)',
            border: 'var(--border-light)',
            borderRadius: '32px',
            padding: '64px 40px',
            boxShadow: 'var(--shadow-sm)',
            position: 'relative',
            overflow: 'hidden'
          }}
        >

          <h2 style={{ fontSize: '42px', fontWeight: '800', color: 'var(--color-text-dark)', marginBottom: '18px', letterSpacing: '-0.5px', position: 'relative' }}>
            Speak naturally. <span className="heading-accent">Write effortlessly.</span>
          </h2>
          <p style={{ fontSize: '18px', color: 'rgba(23, 23, 25, 0.8)', lineHeight: '1.65', maxWidth: '640px', margin: '0 auto 36px', position: 'relative' }}>
            Join thousands of creators, engineers, and founders using SpeekTo.Me to dictate text 3x faster with instant AI formatting.
          </p>
          <div style={{ display: 'flex', gap: '16px', justifyContent: 'center', flexWrap: 'wrap', position: 'relative' }}>
            <Link to="/pricing" className="btn btn-primary btn-lg" style={{ fontSize: '16px', padding: '16px 36px', boxShadow: 'var(--shadow-sm)' }}>
              Subscribe for $2/mo →
            </Link>
            <Link to="/about" className="btn btn-secondary btn-lg" style={{ fontSize: '16px', padding: '16px 30px', boxShadow: 'var(--shadow-sm)' }}>
              Explore Features
            </Link>
          </div>
        </div>
      </section>



    </div>
  );
}
