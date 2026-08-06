import React, { useState } from 'react';
import { Link, useLocation } from 'react-router-dom';

export default function Navbar({ token, user, onLogout }) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const location = useLocation();

  const isActive = (path) => location.pathname === path;

  return (
    <nav className="navbar">
      <Link to="/" className="nav-logo" onClick={() => setMobileOpen(false)}>
        <img src="/assets/logo.svg" alt="SpeekTo.Me Logo" className="brand-logo-icon" />
        SpeekTo<span className="logo-dot">.</span>Me
      </Link>

      <ul className={`nav-links ${mobileOpen ? 'open' : ''}`}>
        <li className={`nav-item ${isActive('/') ? 'active' : ''}`}>
          <Link to="/" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>Home</Link>
        </li>
        <li className={`nav-item ${isActive('/about') ? 'active' : ''}`}>
          <Link to="/about" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>About Us</Link>
        </li>
        <li className={`nav-item ${isActive('/pricing') ? 'active' : ''}`}>
          <Link to="/pricing" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>Pricing</Link>
        </li>
      </ul>

      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        {token ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
            <Link to="/dashboard" className="btn btn-primary nav-btn" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>
              Dashboard
            </Link>
            <button 
              onClick={() => { onLogout && onLogout(); setMobileOpen(false); window.scrollTo(0, 0); }} 
              className="btn btn-secondary nav-btn"
              style={{ cursor: 'pointer' }}
            >
              Sign Out
            </button>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Link to="/login" className="btn btn-secondary nav-btn" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>
              Sign In
            </Link>
            <Link to="/pricing" className="btn btn-primary nav-btn" onClick={() => { setMobileOpen(false); window.scrollTo(0, 0); }}>
              Get Started
            </Link>
          </div>
        )}


        <div className={`nav-hamburger ${mobileOpen ? 'open' : ''}`} onClick={() => setMobileOpen(!mobileOpen)}>
          <span></span><span></span><span></span>
        </div>
      </div>
    </nav>
  );
}
