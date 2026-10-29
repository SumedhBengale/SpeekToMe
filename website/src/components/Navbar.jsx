import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import SpeekMascot from './SpeekMascot';
export default function Navbar({ token, onLogout }) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [logoHover, setLogoHover] = useState(false);
  const location = useLocation();
  const toggleRef = useRef(null);
  useEffect(() => { setMobileOpen(false); }, [location.pathname]);
  useEffect(() => {
    const close = (event) => { if (event.key === 'Escape') { setMobileOpen(false); toggleRef.current?.focus(); } };
    if (mobileOpen) document.addEventListener('keydown', close);
    return () => document.removeEventListener('keydown', close);
  }, [mobileOpen]);
  const closeMenu = () => setMobileOpen(false);
  return <nav className="navbar" aria-label="Main navigation">
    <Link to="/" className="nav-logo" onClick={closeMenu} onPointerEnter={() => setLogoHover(true)} onPointerLeave={() => setLogoHover(false)}><SpeekMascot className="brand-logo-icon" framing="icon" faceColor="#240a12" mode={logoHover ? 'happy' : 'idle'} /><span>SpeekTo.<span className="brand-accent">Me</span></span></Link>
    <button ref={toggleRef} type="button" className={`nav-hamburger ${mobileOpen ? 'open' : ''}`} aria-label={mobileOpen ? 'Close navigation' : 'Open navigation'} aria-expanded={mobileOpen} aria-controls="main-navigation" onClick={() => setMobileOpen(!mobileOpen)}><span /><span /><span /></button>
    <div id="main-navigation" className={`nav-menu ${mobileOpen ? 'open' : ''}`}>
      <ul className="nav-links">{[['/', 'Home'], ['/about', 'About Us'], ['/pricing', 'Pricing']].map(([path, label]) => <li className={`nav-item ${location.pathname === path ? 'active' : ''}`} key={path}><Link to={path} aria-current={location.pathname === path ? 'page' : undefined} onClick={closeMenu}>{label}</Link></li>)}</ul>
      <div className="nav-actions">{token ? <><Link to="/dashboard" className="btn btn-primary nav-btn" onClick={closeMenu}>Dashboard</Link><button type="button" className="btn btn-secondary nav-btn" onClick={() => { onLogout?.(); closeMenu(); }}>Sign Out</button></> : <><Link to="/login" className="btn btn-secondary nav-btn" onClick={closeMenu}>Sign In</Link><Link to="/pricing" className="btn btn-primary nav-btn" onClick={closeMenu}>Get Started</Link></>}</div>
    </div>
  </nav>;
}
