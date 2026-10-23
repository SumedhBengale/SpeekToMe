import CommandAllowanceTerms from '../components/CommandAllowanceTerms';
import React from 'react';
import { Link } from 'react-router-dom';

export default function PricingPage() {
  const [activeFaq, setActiveFaq] = React.useState(null);

  return (

    <div className="pricing-page">
      {/* 1. PRICING HERO */}
      <section className="pricing-wrapper" style={{ padding: '80px 6% 40px', textAlign: 'center' }}>
        <div className="reveal">
          <h1>Simple, transparent <span className="heading-accent">pricing</span>.</h1>
          <p className="subtitle text-muted" style={{ fontSize: '18px', maxWidth: '600px', margin: '16px auto 0' }}>
            Get access to the ultra-fast voice client. No hidden fees. Cancel anytime.
          </p>
        </div>

        {/* 2. PRICING CARDS */}
        <div className="pricing-cards" style={{ display: 'flex', gap: '24px', justifyContent: 'center', flexWrap: 'wrap', marginTop: '36px' }}>
          {/* Free Tier Card */}
          <div className="pricing-free-card reveal stagger-1" style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '20px', padding: '28px 24px', width: '100%', maxWidth: '340px', textAlign: 'left', position: 'relative' }}>
            <span className="pricing-badge pricing-badge-free" style={{ background: '#f3f4f6', padding: '4px 12px', borderRadius: '12px', fontSize: '11px', fontWeight: '600' }}>Free Tier</span>
            <h2 style={{ fontSize: '22px', marginTop: '12px', marginBottom: '6px' }}>Starter</h2>
            <div className="price-title-row" style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '20px' }}>
              <span className="currency" style={{ fontSize: '20px', fontWeight: '700' }}>$</span>
              <span className="amount" style={{ fontSize: '42px', fontWeight: '800' }}>0</span>
              <span className="period text-muted" style={{ fontSize: '13px' }}>/ forever</span>
            </div>

            <div className="plan-features-header" style={{ fontWeight: '600', marginBottom: '14px', fontSize: '13.5px' }}>What's included</div>
            <ul className="pricing-features-list" style={{ listStyle: 'none', padding: 0, margin: '0 0 24px 0', display: 'flex', flexDirection: 'column', gap: '10px', fontSize: '13.5px' }}>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>1 hour / month transcription limit</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Fast AI voice dictation</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Global keyboard shortcut trigger</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Standard latency queue</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Cross-platform support (coming soon)</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Basic community support</span>
              </li>
            </ul>

            <Link to="/dashboard" className="btn btn-secondary btn-pricing-cta" style={{ display: 'block', textAlign: 'center', width: '100%', padding: '10px 16px' }}>Try Free</Link>
          </div>

          {/* Pro Plan Card */}
          <section className="flat-pricing-card reveal stagger-2" style={{ background: '#fff', border: 'var(--border-flat)', borderRadius: '20px', padding: '28px 24px', width: '100%', maxWidth: '350px', textAlign: 'left', boxShadow: 'var(--shadow-lg)', position: 'relative' }}>
            <span className="pricing-badge" style={{ background: 'var(--color-btn-primary)', color: 'var(--color-text-light)', padding: '4px 12px', borderRadius: '12px', fontSize: '11px', fontWeight: '600' }}>Most Popular</span>
            <h2 style={{ fontSize: '22px', marginTop: '12px', marginBottom: '6px' }}>Pro</h2>


            <div className="price-title-row" style={{ display: 'flex', alignItems: 'baseline', gap: '4px', marginBottom: '20px' }}>
              <span className="currency" style={{ fontSize: '20px', fontWeight: '700' }}>$</span>
              <span className="amount" style={{ fontSize: '42px', fontWeight: '800' }}>2</span>
              <span className="period text-muted" style={{ fontSize: '13px' }}>/ month</span>
            </div>

            <CommandAllowanceTerms />
            <div className="plan-features-header" style={{ fontWeight: '600', marginBottom: '14px', fontSize: '13.5px' }}>Everything in Starter, plus</div>
            <ul className="pricing-features-list" style={{ listStyle: 'none', padding: 0, margin: '0 0 24px 0', display: 'flex', flexDirection: 'column', gap: '10px', fontSize: '13.5px' }}>

              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span><strong>Unlimited AI Voice Transcription</strong></span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span><strong>Advanced AI Enhancement & Smart Formatting</strong></span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span><strong>Guaranteed High-priority under load</strong></span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Generous monthly Command Mode usage</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Customizable AI system prompt instructions</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Cross-platform desktop & mobile support (coming soon)</span>
              </li>
              <li style={{ display: 'flex', gap: '8px', alignItems: 'flex-start' }}>
                <span style={{ color: 'var(--color-success)', fontWeight: 'bold' }}>✓</span>
                <span>Priority 24/7 email & chat support</span>
              </li>
            </ul>

            <Link to="/dashboard" className="btn btn-primary btn-pricing-cta btn-lg" style={{ display: 'block', textAlign: 'center', width: '100%' }}>Subscribe to Pro →</Link>
          </section>
        </div>
      </section>

      {/* 3. FEATURE COMPARISON TABLE */}
      <section className="comparison-section section-alt section-bordered" style={{ padding: '80px 6%' }}>
        <div className="container" style={{ maxWidth: '900px', margin: '0 auto' }}>
          <div className="section-header text-center" style={{ marginBottom: '40px' }}>
            <h2>Full feature <span className="heading-accent">breakdown</span>.</h2>
          </div>

          <div style={{ overflowX: 'auto', background: '#fff', border: 'var(--border-flat)', borderRadius: '24px', padding: '24px' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '14px' }}>
              <thead>
                <tr style={{ borderBottom: 'var(--border-light)' }}>
                  <th style={{ padding: '16px' }}>Feature</th>
                  <th style={{ padding: '16px' }}>Starter</th>
                  <th style={{ padding: '16px' }}>Pro ($2/mo)</th>
                </tr>
              </thead>
              <tbody>
                <tr style={{ borderBottom: '1px solid #eee' }}>
                  <td style={{ padding: '16px' }}><strong>Monthly Transcription Limit</strong></td>
                  <td style={{ padding: '16px' }}>1 hour/month</td>
                  <td style={{ padding: '16px', color: 'var(--color-success)', fontWeight: 'bold' }}>✓ Unlimited</td>
                </tr>
                <tr style={{ borderBottom: '1px solid #eee' }}>
                  <td style={{ padding: '16px' }}><strong>AI Enhancement & Formatting</strong></td>
                  <td style={{ padding: '16px' }}>Basic cleanup</td>
                  <td style={{ padding: '16px', color: 'var(--color-success)', fontWeight: 'bold' }}>✓ Advanced AI Enhancement</td>
                </tr>
                <tr style={{ borderBottom: '1px solid #eee' }}>
                  <td style={{ padding: '16px' }}><strong>Transcription Speed</strong></td>
                  <td style={{ padding: '16px' }}>Standard queue</td>
                  <td style={{ padding: '16px', color: 'var(--color-success)', fontWeight: 'bold' }}>✓ Ultra-low latency priority</td>
                </tr>
                <tr>
                  <td style={{ padding: '16px' }}><strong>Custom System Prompts</strong></td>
                  <td style={{ padding: '16px' }}>—</td>
                  <td style={{ padding: '16px', color: 'var(--color-success)', fontWeight: 'bold' }}>✓ Full Customization</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* 4. PRICING FAQ */}
      <section className="section faq-section" style={{ padding: '80px 6%' }}>
        <div className="container" style={{ maxWidth: '800px', margin: '0 auto' }}>
          <div className="section-header text-center" style={{ marginBottom: '40px' }}>
            <h2>Pricing <span className="heading-accent">questions</span>.</h2>
          </div>

          <div className="faq-accordion-list">
            {[
              { q: "How does billing work?", a: <span>You're billed $2 per month from the date you subscribe. You get unlimited AI voice transcription with no hidden fees or daily limits. You can cancel anytime directly from your dashboard — no contracts, no lock-in.</span> },
              { q: "Can I get a refund?", a: <span>If you're not satisfied with SpeekTo.Me Pro or encounter any issues, please reach out to our support team. We review refund requests thoughtfully on a case-by-case basis to ensure our customers are taken care of. See our <Link to="/refund" style={{ color: 'var(--color-text-dark)', fontWeight: '600' }}>Refund Policy</Link> for details.</span> },

              { q: "Is transcription really unlimited on Pro?", a: <span>Yes! SpeekTo.Me Pro includes unlimited AI audio transcription without daily session caps. Standard fair-use guidelines apply to safeguard high-performance infrastructure, but normal human dictation has no cap.</span> },
              { q: "Is there a free plan?", a: <span>Yes! The Starter tier gives you 1 hour per month of basic transcription with no daily usage caps. It's perfect for testing out SpeekTo.Me before upgrading to Pro. No credit card required.</span> }
            ].map((faq, idx) => {
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


      {/* 5. BOTTOM CTA — HIGH CONTRAST UNIQUE CARD */}
      <section className="cta-banner" style={{ padding: '80px 6%', background: 'var(--bg-canvas)', borderBottom: 'var(--border-light)' }}>
        <div
          className="container text-center"
          style={{
            maxWidth: '840px',
            margin: '0 auto',
            background: '#ffffff',
            border: 'var(--border-light)',
            borderRadius: '28px',
            padding: '56px 36px',
            boxShadow: 'var(--shadow-sm)'
          }}
        >
          <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '1.2px', fontWeight: '800', color: '#065f46', background: '#d1fae5', padding: '6px 16px', borderRadius: '20px', border: '1.5px solid #a7f3d0', display: 'inline-block', marginBottom: '20px' }}>
            ⚡ SPEEKTO.ME PRO UNLIMITED
          </span>
          <h2 style={{ fontSize: '38px', fontWeight: '800', color: 'var(--color-text-dark)', marginBottom: '16px', letterSpacing: '-0.5px' }}>
            Transform the way you write.
          </h2>
          <p style={{ fontSize: '17px', color: 'rgba(23, 23, 25, 0.75)', lineHeight: '1.6', maxWidth: '600px', margin: '0 auto 32px' }}>
            Unlock unlimited high-speed AI dictation across all your desktop & web apps for just $2/month. Cancel anytime.
          </p>
          <Link to="/dashboard" className="btn btn-primary btn-lg" style={{ fontSize: '16px', padding: '14px 32px' }}>
            Subscribe to Pro →
          </Link>
        </div>
      </section>

    </div>
  );
}
