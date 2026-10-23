import React from 'react';
import { Link } from 'react-router-dom';

export default function Footer() {
  return (
    <footer className="footer">
      <div className="container">
        <div className="footer-content" style={{ display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: '32px' }}>
          <div className="footer-brand" style={{ maxWidth: '360px' }}>
            <Link to="/" className="nav-logo" style={{ marginBottom: '16px', display: 'inline-flex' }}>
              <img src="/assets/logo.svg" alt="SpeekTo.Me Logo" className="brand-logo-icon" />
              <span>SpeekTo.<span className="brand-accent">Me</span></span>
            </Link>
            <p className="text-muted" style={{ fontSize: '14px', lineHeight: '1.6' }}>
              The fastest, smartest way to type with your voice. Built with ultra-low latency audio streaming and AI formatting.
            </p>
          </div>

          <div className="footer-links-group" style={{ display: 'flex', gap: '48px', flexWrap: 'wrap' }}>
            <div>
              <h4 style={{ fontSize: '15px', fontWeight: '600', marginBottom: '16px' }}>Navigation</h4>
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '10px' }}>
                <li><Link to="/" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Home</Link></li>
                <li><Link to="/about" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>About Us</Link></li>
                <li><Link to="/pricing" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Pricing</Link></li>
                <li><Link to="/dashboard" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Dashboard</Link></li>
              </ul>
            </div>

            <div>
              <h4 style={{ fontSize: '15px', fontWeight: '600', marginBottom: '16px' }}>Legal</h4>
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '10px' }}>
                <li><Link to="/privacy" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Privacy Policy</Link></li>
                <li><Link to="/refund" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Refund Policy</Link></li>
                <li><Link to="/terms" onClick={() => window.scrollTo(0, 0)} style={{ color: 'var(--color-text-muted)', fontSize: '14px' }}>Terms of Service</Link></li>
              </ul>
            </div>

          </div>
        </div>

        <div className="footer-bottom" style={{ marginTop: '48px', paddingTop: '24px', borderTop: '1px solid rgba(23, 23, 25, 0.1)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '16px', fontSize: '13px', color: 'var(--color-text-muted)' }}>
          <p>© {new Date().getFullYear()} SpeekTo.Me — Built by Synaptican. All rights reserved.</p>
          <p>Speech-to-Text Ultra Low Latency Dictation</p>
        </div>
      </div>
    </footer>
  );
}
