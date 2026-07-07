import React, { useState, useEffect } from 'react';

const API_BASE = window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1' 
  ? 'http://localhost:8000' 
  : 'http://localhost:8000'; // Fallback to local dev API or current host in production

export default function App() {
  const [token, setToken] = useState(localStorage.getItem('speektome_token') || '');
  const [user, setUser] = useState(null);
  const [logs, setLogs] = useState([]);
  const [route, setRoute] = useState('loading');
  const [loading, setLoading] = useState(false);
  const [authError, setAuthError] = useState('');
  const [successMessage, setSuccessMessage] = useState('');
  
  // Auth Form inputs
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');

  // 1. Initial Authentication & Redirect check
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const queryToken = params.get('token');
    const isVerified = params.get('verified');
    const verifiedEmail = params.get('email');
    
    if (queryToken) {
      localStorage.setItem('speektome_token', queryToken);
      setToken(queryToken);
      window.history.replaceState({}, document.title, window.location.pathname);
    } else if (isVerified === 'true') {
      setSuccessMessage(`Email verified successfully${verifiedEmail ? ' for ' + verifiedEmail : ''}! You can now sign in.`);
      window.history.replaceState({}, document.title, window.location.pathname);
    }
  }, []);

  // 1.5. Load Razorpay Checkout SDK Script
  useEffect(() => {
    const script = document.createElement('script');
    script.src = 'https://checkout.razorpay.com/v1/checkout.js';
    script.async = true;
    document.body.appendChild(script);
    return () => {
      document.body.removeChild(script);
    };
  }, []);

  // 2. Load user profile whenever token changes
  useEffect(() => {
    if (token) {
      fetchUserProfile();
    } else {
      setRoute('login');
    }
  }, [token]);

  const fetchUserProfile = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/me`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setUser(data);
        setRoute('dashboard');
        fetchUsageLogs();
      } else {
        // Token invalid or expired
        handleLogoutLocal();
      }
    } catch (err) {
      console.error("Failed to load user info:", err);
      setRoute('dashboard'); // Fallback to let dashboard load if server is temporarily unreachable
    } finally {
      setLoading(false);
    }
  };

  const fetchUsageLogs = async () => {
    try {
      const res = await fetch(`${API_BASE}/usage/history`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setLogs(data);
      }
    } catch (err) {
      console.error("Failed to fetch logs:", err);
    }
  };

  const handleLogoutLocal = () => {
    localStorage.removeItem('speektome_token');
    setToken('');
    setUser(null);
    setLogs([]);
    setRoute('login');
  };

  const handleLogout = async () => {
    try {
      await fetch(`${API_BASE}/auth/logout`, {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${token}` }
      });
    } catch (err) {
      console.error("Logout failed on server:", err);
    }
    handleLogoutLocal();
  };

  const handleLogin = async (e) => {
    e.preventDefault();
    setAuthError('');
    setSuccessMessage('');
    if (!email || !password) {
      setAuthError('Email and password are required.');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password })
      });
      const data = await res.json();
      if (res.ok) {
        localStorage.setItem('speektome_token', data.token);
        setToken(data.token);
        setEmail('');
        setPassword('');
      } else {
        setAuthError(data.error || 'Invalid credentials');
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const handleSignup = async (e) => {
    e.preventDefault();
    setAuthError('');
    setSuccessMessage('');
    if (!email || !password || !name) {
      setAuthError('All fields are required.');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/signup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password, name })
      });
      const data = await res.json();
      if (res.ok) {
        if (data.status === 'verification_required') {
          setSuccessMessage(data.message || 'Account created! Please check your email to verify.');
          setRoute('login'); // Switch back to login page
          setEmail('');
          setPassword('');
          setName('');
        } else if (data.token) {
          localStorage.setItem('speektome_token', data.token);
          setToken(data.token);
          setEmail('');
          setPassword('');
          setName('');
        }
      } else {
        setAuthError(data.error || 'Registration failed.');
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const handleGoogleLogin = () => {
    const dashboardURL = window.location.origin + window.location.pathname;
    window.location.href = `${API_BASE}/auth/login?redirect_to=${encodeURIComponent(dashboardURL)}`;
  };

  const handleUpgrade = async () => {
    setAuthError('');
    setSuccessMessage('');
    try {
      const res = await fetch(`${API_BASE}/billing/create-subscription`, {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (!res.ok) {
        const errorText = await res.text();
        setAuthError(errorText || 'Failed to initiate checkout.');
        return;
      }
      const data = await res.json();
      if (data.mock_mode) {
        // Fallback: Redirect to neobrutalist simulator page
        const dashboardURL = window.location.origin + window.location.pathname;
        window.location.href = `${API_BASE}/mock-payment?token=${token}&redirect_to=${encodeURIComponent(dashboardURL)}`;
        return;
      }

      // Real Razorpay subscription popup modal
      const options = {
        key: data.razorpay_key_id,
        subscription_id: data.subscription_id,
        name: 'SpeekTo.Me',
        description: 'SpeekTo.Me Pro ($2.00/mo)',
        handler: async function (response) {
          try {
            const verifyRes = await fetch(`${API_BASE}/billing/verify-subscription`, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${token}`
              },
              body: JSON.stringify({
                razorpay_payment_id: response.razorpay_payment_id,
                razorpay_subscription_id: response.razorpay_subscription_id,
                razorpay_signature: response.razorpay_signature
              })
            });
            if (verifyRes.ok) {
              setSuccessMessage("Successfully upgraded to SpeekTo.Me Pro!");
              fetchUserProfile();
              setRoute('dashboard');
            } else {
              const errData = await verifyRes.json();
              setAuthError(errData.error || "Payment verification failed.");
            }
          } catch (verifyErr) {
            setAuthError("Failed to verify payment with server.");
          }
        },
        prefill: {
          name: user ? user.name : '',
          email: user ? user.email : ''
        },
        theme: {
          color: '#034f46'
        }
      };

      const rzp = new window.Razorpay(options);
      rzp.open();
    } catch (err) {
      console.error("Upgrade error:", err);
      setAuthError("Could not connect to billing server.");
    }
  };

  // Helper formatting routines
  const formatSecondsToHours = (seconds) => {
    if (!seconds || seconds <= 0) return '0.0h';
    return `${(seconds / 3600).toFixed(1)}h`;
  };

  const formatSeconds = (seconds) => {
    if (!seconds || seconds <= 0) return '0s';
    if (seconds < 60) return `${Math.round(seconds)}s`;
    const m = Math.floor(seconds / 60);
    const s = Math.round(seconds % 60);
    return `${m}m ${s}s`;
  };

  const formatDate = (dateStr) => {
    if (!dateStr) return 'N/A';
    const d = new Date(dateStr);
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
  };

  // Render loading state
  if (route === 'loading' || loading && !user) {
    return (
      <div className="dashboard-container">
        <div className="loading-container">
          <div className="spinner"></div>
          <h2>Loading SpeekTo.Me Dashboard...</h2>
        </div>
      </div>
    );
  }

  return (
    <div className="dashboard-container">
      {/* 1. Header */}
      <header class="dashboard-header">
        <a href="/" className="logo">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{marginRight: '6px', verticalAlign: 'middle', display: 'inline-block'}}><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/><path d="M19 10v2a7 7 0 0 1-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>SpeekTo.Me<span className="logo-dot">.</span>
        </a>
        <nav className="nav-links">
          {user ? (
            <>
              <span className="nav-user"><svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" style={{marginRight: '6px', verticalAlign: 'middle', display: 'inline-block'}}><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>{user.name} ({user.email})</span>
              <button onClick={() => setRoute(route === 'dashboard' ? 'billing' : 'dashboard')} className="btn btn-primary btn-sm">
                {route === 'dashboard' ? (
                  <>
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{marginRight: '6px', verticalAlign: 'middle', display: 'inline-block'}}><rect x="1" y="4" width="22" height="16" rx="2" ry="2"/><line x1="1" y1="10" x2="23" y2="10"/></svg>
                    Manage Subscription
                  </>
                ) : (
                  <>
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{marginRight: '6px', verticalAlign: 'middle', display: 'inline-block'}}><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
                    Back to Stats
                  </>
                )}
              </button>
              <button onClick={handleLogout} className="btn btn-danger btn-sm">Sign Out</button>
            </>
          ) : (
            <a href="/" className="btn btn-sm">Home</a>
          )}
        </nav>
      </header>

      {/* 2. Login Page */}
      {route === 'login' && (
        <main className="auth-page">
          <div className="auth-box">
            <div className="auth-header">
              <h2>Welcome Back</h2>
              <p>Sign in to manage your SpeekTo.Me subscription.</p>
            </div>

            {authError && <div className="auth-error">{authError}</div>}
            {successMessage && (
              <div 
                className="auth-success" 
                style={{ 
                  background: '#d1fae5', 
                  border: '2px solid #1a1a1a', 
                  padding: '12px 14px', 
                  borderRadius: '12px', 
                  color: '#1a1a1a', 
                  fontSize: '13.5px', 
                  fontWeight: '500', 
                  marginBottom: '20px', 
                  textAlign: 'center',
                  boxShadow: '2px 2px 0px #1a1a1a'
                }}
              >
                {successMessage}
              </div>
            )}

            <button onClick={handleGoogleLogin} className="btn google-auth-btn">
              🔑 Sign in with Google
            </button>

            <div className="divider">or use credentials</div>

            <form onSubmit={handleLogin}>
              <div className="auth-form-control">
                <label>Email Address</label>
                <input 
                  type="email" 
                  value={email} 
                  onChange={(e) => setEmail(e.target.value)} 
                  placeholder="name@example.com" 
                  className="auth-input"
                  required
                />
              </div>
              <div className="auth-form-control">
                <label>Password</label>
                <input 
                  type="password" 
                  value={password} 
                  onChange={(e) => setPassword(e.target.value)} 
                  placeholder="••••••••" 
                  className="auth-input"
                  required
                />
              </div>
              <button type="submit" className="btn btn-primary auth-submit-btn">
                {loading ? <div className="spinner"></div> : 'Sign In'}
              </button>
            </form>

            <div className="auth-toggle">
              Don't have an account?{' '}
              <button onClick={() => { setRoute('signup'); setAuthError(''); }} className="auth-toggle-link">
                Sign Up
              </button>
            </div>
          </div>
        </main>
      )}

      {/* 3. Signup Page */}
      {route === 'signup' && (
        <main className="auth-page">
          <div className="auth-box">
            <div className="auth-header">
              <h2>Create Account</h2>
              <p>Get started with SpeekTo.Me transcription services.</p>
            </div>

            {authError && <div className="auth-error">{authError}</div>}

            <button onClick={handleGoogleLogin} className="btn google-auth-btn">
              🔑 Sign up with Google
            </button>

            <div className="divider">or create credentials</div>

            <form onSubmit={handleSignup}>
              <div className="auth-form-control">
                <label>Full Name</label>
                <input 
                  type="text" 
                  value={name} 
                  onChange={(e) => setName(e.target.value)} 
                  placeholder="John Doe" 
                  className="auth-input"
                  required
                />
              </div>
              <div className="auth-form-control">
                <label>Email Address</label>
                <input 
                  type="email" 
                  value={email} 
                  onChange={(e) => setEmail(e.target.value)} 
                  placeholder="name@example.com" 
                  className="auth-input"
                  required
                />
              </div>
              <div className="auth-form-control">
                <label>Password</label>
                <input 
                  type="password" 
                  value={password} 
                  onChange={(e) => setPassword(e.target.value)} 
                  placeholder="••••••••" 
                  className="auth-input"
                  required
                />
              </div>
              <button type="submit" className="btn btn-primary auth-submit-btn">
                {loading ? <div className="spinner"></div> : 'Create Account'}
              </button>
            </form>

            <div className="auth-toggle">
              Already have an account?{' '}
              <button onClick={() => { setRoute('login'); setAuthError(''); }} className="auth-toggle-link">
                Sign In
              </button>
            </div>
          </div>
        </main>
      )}

      {/* 4. Main Dashboard View */}
      {route === 'dashboard' && user && (
        <main className="dashboard-main">
          <div className="welcome-section">
            <h1 className="welcome-title">Hey, {user.name.split(' ')[0]} 👋</h1>
            <p>Here is your current SpeekTo.Me usage and transcription analytics.</p>
          </div>

          <div className="grid-dashboard">
            {/* Left Column: Logs */}
            <div className="dashboard-col">
              <div className="card">
                <h3 className="card-title">🎙️ Recent Transcriptions</h3>
                {logs.length === 0 ? (
                  <p style={{ color: 'rgba(26, 26, 26, 0.5)', padding: '20px 0', textAlign: 'center' }}>
                    No recent dictations found. Open SpeekTo.Me on your desktop to make your first transcription!
                  </p>
                ) : (
                  <div className="logs-table-container">
                    <table className="logs-table">
                      <thead>
                        <tr>
                          <th>Date</th>
                          <th>Duration</th>
                          <th>Speed</th>
                          <th>Status</th>
                        </tr>
                      </thead>
                      <tbody>
                        {logs.map((log, idx) => (
                          <tr key={idx}>
                            <td>{formatDate(log.timestamp)}</td>
                            <td>{formatSeconds(log.audio_duration)}</td>
                            <td>{log.total_time ? `${log.total_time.toFixed(1)}s` : 'N/A'}</td>
                            <td>
                              <span className={`badge-status ${log.status === 'success' ? 'badge-success' : 'badge-failed'}`}>
                                {log.status}
                              </span>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            </div>

            {/* Right Column: Usage Limits & Subscription */}
            <div className="dashboard-col">
              <div className="card">
                <h3 className="card-title">📊 Usage Analytics</h3>
                
                <div className="usage-summary">
                  <div className="usage-stats">
                    <span>Usage Limit ({user.plan_tier.toUpperCase()})</span>
                    <span>{formatSecondsToHours(user.duration_seconds)} / {user.plan_tier === 'pro' ? '50.0h' : '1.0h'}</span>
                  </div>
                  
                  <div className="progress-bar-container">
                    <div 
                      className="progress-bar-fill" 
                      style={{ 
                        width: `${Math.min(
                          (user.duration_seconds / (user.plan_tier === 'pro' ? 50 * 3600 : 1 * 3600)) * 100, 
                          100
                        )}%` 
                      }}
                    ></div>
                  </div>
                  
                  <p style={{ fontSize: '13px', color: 'rgba(26, 26, 26, 0.5)', marginTop: '8px' }}>
                    {user.plan_tier === 'free' 
                      ? 'Upgrade to SpeekTo.Me Pro to get 50 hours of dictation limit per month and access to enhanced LLM formatting.' 
                      : 'Your Pro plan subscription is active and will renew monthly.'}
                  </p>
                </div>
              </div>

              <div className="card" style={{ background: 'var(--color-btn-accent)' }}>
                <h3 className="card-title" style={{ borderBottomColor: 'var(--border-color)', display: 'flex', alignItems: 'center' }}>
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" style={{marginRight: '6px'}}><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>
                  Plan Tier: {user.plan_tier.toUpperCase()}
                </h3>
                <p style={{ fontSize: '14px', marginBottom: '16px', fontWeight: '500' }}>
                  {user.plan_active 
                    ? `Active since ${formatDate(user.plan_activated_at)}` 
                    : 'No active paid subscription.'}
                </p>
                {!user.plan_active ? (
                  <button onClick={() => setRoute('billing')} className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{marginRight: '6px', verticalAlign: 'middle', display: 'inline-block'}}><path d="M4.5 16.5c-1.5 1.25-2.5 3.5-2.5 3.5s2.25-1 3.5-2.5M12 2C8 2 5 5 5 9c0 1.66.84 3.5 1.5 4.5L12 22l5.5-8.5c.66-1 1.5-2.84 1.5-4.5 0-4-3-7-7-7z"/><path d="M12 6a3 3 0 1 0 0 6 3 3 0 0 0 0-6z"/></svg>
                    Upgrade to Pro ($2.00/mo)
                  </button>
                ) : (
                  <button onClick={() => setRoute('billing')} className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff' }}>
                    Manage Billing Details
                  </button>
                )}
              </div>
            </div>
          </div>
        </main>
      )}

      {/* 5. Billing Tiers Overview Page */}
      {route === 'billing' && user && (
        <main className="dashboard-main">
          <div className="welcome-section">
            <h1 className="welcome-title">Choose Your Subscription Plan</h1>
            <p>Get the dictation limits you need to be productive directly on your desktop.</p>
          </div>

          <div className="pricing-grid">
            {/* Free Plan */}
            <div className={`plan-card ${user.plan_tier === 'free' ? 'active' : ''}`}>
              <div className="plan-header">
                <h3>SpeekTo.Me Starter</h3>
                <p>Perfect for testing out dictation and speed.</p>
                <div className="plan-price">$0<span>/forever</span></div>
                
                <ul className="plan-features" style={{ listStyle: 'none', padding: 0 }}>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    1 hour / month transcription limit
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    5 minutes / day daily session limit
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    Fast AI voice dictation
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    Global keyboard shortcut trigger
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px', opacity: 0.65 }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-text-dark)', marginRight: '8px', flexShrink: 0}}><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    No custom formatting presets
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px', opacity: 0.65 }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-text-dark)', marginRight: '8px', flexShrink: 0}}><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                    No intelligent formatting engine
                  </li>
                </ul>
              </div>

              <div>
                {user.plan_tier === 'free' ? (
                  <button className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff', cursor: 'default' }} disabled>
                    Current Active Plan
                  </button>
                ) : (
                  <button className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff', cursor: 'default' }} disabled>
                    Downgrades require contacting support
                  </button>
                )}
              </div>
            </div>

            {/* Pro Plan */}
            <div className={`plan-card ${user.plan_tier === 'pro' ? 'active' : ''}`}>
              <div className="plan-header">
                <h3>SpeekTo.Me Pro</h3>
                <p>For power users who dictate essays, logs, and emails.</p>
                <div className="plan-price">$2.00<span>/month</span></div>

                <ul className="plan-features" style={{ listStyle: 'none', padding: 0 }}>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    50 hours / month transcription limit
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    1 hour / day daily usage limit
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    High-priority transcription queues
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    Intelligent AI formatting & correction
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    Custom formatting prompts and presets
                  </li>
                  <li style={{ display: 'flex', alignItems: 'center', marginBottom: '8px' }}>
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" style={{color: 'var(--color-success)', marginRight: '8px', flexShrink: 0}}><polyline points="20 6 9 17 4 12"/></svg>
                    Cross-platform desktop & mobile support
                  </li>
                </ul>
              </div>

              <div>
                {user.plan_tier === 'pro' ? (
                  <button className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff', cursor: 'default' }} disabled>
                    Current Active Plan (Subscribed)
                  </button>
                ) : (
                  <button onClick={handleUpgrade} className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
                    💳 Upgrade to Pro ($2.00/mo)
                  </button>
                )}
              </div>
            </div>
          </div>
        </main>
      )}
    </div>
  );
}
