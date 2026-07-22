import React, { useState, useEffect } from 'react';

const API_BASE = import.meta.env.VITE_API_URL || (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1' 
  ? 'http://localhost:8000' 
  : ''); // Fallback to current host root if no variable is provided

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

  // 2FA state
  const [tempToken, setTempToken] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [totpSetupData, setTotpSetupData] = useState(null);

  // Admin Panel states
  const [adminUsers, setAdminUsers] = useState([]);
  const [editingUser, setEditingUser] = useState(null);
  const [editPlanTier, setEditPlanTier] = useState('free');
  const [editPlanActive, setEditPlanActive] = useState(0);
  const [editRateLimit, setEditRateLimit] = useState('');
  const [webhookLogs, setWebhookLogs] = useState([]);
  const [adminSubTab, setAdminSubTab] = useState('users');
  const [cacheRateLimits, setCacheRateLimits] = useState({});
  const [cacheVocabKeys, setCacheVocabKeys] = useState([]);

  // Dashboard Tab and details states
  const [vocabTerms, setVocabTerms] = useState([]);
  const [meetings, setMeetings] = useState([]);
  const [activeMeeting, setActiveMeeting] = useState(null);
  const [newVocabTerm, setNewVocabTerm] = useState('');
  const [activeSubTab, setActiveSubTab] = useState('stats');
  const [showCancelModal, setShowCancelModal] = useState(false);

  // 1. Initial Authentication & Redirect check
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const queryToken = params.get('token');
    const isVerified = params.get('verified');
    const verifiedEmail = params.get('email');
    const queryTempToken = params.get('temp_token');
    const require2fa = params.get('require_2fa');
    const require2faSetup = params.get('require_2fa_setup');
    
    if (queryToken) {
      localStorage.setItem('speektome_token', queryToken);
      setToken(queryToken);
      window.history.replaceState({}, document.title, window.location.pathname);
    } else if (queryTempToken && require2fa === 'true') {
      setTempToken(queryTempToken);
      setRoute('2fa');
      window.history.replaceState({}, document.title, window.location.pathname);
    } else if (queryTempToken && require2faSetup === 'true') {
      setTempToken(queryTempToken);
      start2faSetup(queryTempToken);
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
        fetchVocabulary();
        fetchMeetings();
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

  const fetchVocabulary = async () => {
    if (!token) return;
    try {
      const res = await fetch(`${API_BASE}/auth/vocabulary`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setVocabTerms(data || []);
      }
    } catch (err) {
      console.error("Failed to fetch vocabulary:", err);
    }
  };

  const handleAddVocab = async (e) => {
    e.preventDefault();
    if (!newVocabTerm.trim() || !token) return;
    setAuthError('');
    setSuccessMessage('');
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/vocabulary`, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}` 
        },
        body: JSON.stringify({ canonical_term: newVocabTerm.trim() })
      });
      if (res.ok) {
        setNewVocabTerm('');
        fetchVocabulary();
      } else {
        const errText = await res.text();
        setAuthError(errText || "Failed to add vocabulary term.");
      }
    } catch (err) {
      setAuthError("Could not connect to server.");
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteVocab = async (term) => {
    if (!token) return;
    setAuthError('');
    setSuccessMessage('');
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/vocabulary`, {
        method: 'DELETE',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}` 
        },
        body: JSON.stringify({ canonical_term: term })
      });
      if (res.ok) {
        fetchVocabulary();
      } else {
        const errText = await res.text();
        setAuthError(errText || "Failed to delete vocabulary term.");
      }
    } catch (err) {
      setAuthError("Could not connect to server.");
    } finally {
      setLoading(false);
    }
  };

  const fetchMeetings = async () => {
    if (!token) return;
    try {
      const res = await fetch(`${API_BASE}/meetings`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setMeetings(data || []);
      }
    } catch (err) {
      console.error("Failed to fetch meetings:", err);
    }
  };

  const handleDeleteMeeting = async (id) => {
    if (!token) return;
    if (!window.confirm("Are you sure you want to delete this meeting notes record? This will permanently delete the audio and summaries.")) return;
    setAuthError('');
    setSuccessMessage('');
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/meetings/${id}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        setActiveMeeting(null);
        fetchMeetings();
      } else {
        const errText = await res.text();
        setAuthError(errText || "Failed to delete meeting.");
      }
    } catch (err) {
      setAuthError("Could not connect to server.");
    } finally {
      setLoading(false);
    }
  };

  const handleLogoutLocal = () => {
    localStorage.removeItem('speektome_token');
    setToken('');
    setUser(null);
    setLogs([]);
    setVocabTerms([]);
    setMeetings([]);
    setActiveMeeting(null);
    setRoute('login');
    setActiveSubTab('stats');
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
        if (data.require_2fa) {
          setTempToken(data.temp_token);
          setRoute('2fa');
        } else if (data.require_2fa_setup) {
          setTempToken(data.temp_token);
          start2faSetup(data.temp_token);
        } else {
          localStorage.setItem('speektome_token', data.token);
          setToken(data.token);
          setEmail('');
          setPassword('');
        }
      } else {
        setAuthError(data.error || 'Invalid credentials');
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const handle2faVerify = async (e) => {
    e.preventDefault();
    setAuthError('');
    setSuccessMessage('');
    if (!totpCode || !tempToken) {
      setAuthError('Verification code is required.');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/2fa/verify`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ temp_token: tempToken, code: totpCode })
      });
      const data = await res.json();
      if (res.ok) {
        localStorage.setItem('speektome_token', data.token);
        setToken(data.token);
        setTempToken('');
        setTotpCode('');
        setRoute('dashboard');
      } else {
        if (data.require_2fa_setup) {
          start2faSetup(tempToken);
        } else {
          setAuthError(data.error || 'Invalid 2FA code');
        }
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const start2faSetup = async (tokenToUse) => {
    setLoading(true);
    setAuthError('');
    try {
      const res = await fetch(`${API_BASE}/auth/2fa/setup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ temp_token: tokenToUse || tempToken })
      });
      const data = await res.json();
      if (res.ok) {
        setTotpSetupData(data);
        setRoute('2fa_setup');
      } else {
        setAuthError(data.error || 'Failed to initiate 2FA setup.');
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  const handle2faConfirmSetup = async (e) => {
    e.preventDefault();
    setAuthError('');
    setSuccessMessage('');
    if (!totpCode || !tempToken || !totpSetupData?.secret) {
      setAuthError('Verification code is required.');
      return;
    }
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/2fa/confirm-setup`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          temp_token: tempToken,
          secret: totpSetupData.secret,
          code: totpCode
        })
      });
      const data = await res.json();
      if (res.ok) {
        localStorage.setItem('speektome_token', data.token);
        setToken(data.token);
        setTempToken('');
        setTotpCode('');
        setTotpSetupData(null);
        setRoute('dashboard');
      } else {
        setAuthError(data.error || 'Invalid 2FA verification code');
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (token && route === 'admin') {
      fetchAdminUsers();
      fetchWebhookLogs();
      fetchCacheData();
    }
  }, [token, route]);

  const fetchAdminUsers = async () => {
    setLoading(true);
    setAuthError('');
    try {
      const res = await fetch(`${API_BASE}/admin/users`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setAdminUsers(data || []);
      } else {
        const errText = await res.text();
        setAuthError(errText || 'Failed to fetch admin users list.');
      }
    } catch (err) {
      setAuthError('Failed to connect to administrative server.');
    } finally {
      setLoading(false);
    }
  };

  const fetchWebhookLogs = async () => {
    try {
      const res = await fetch(`${API_BASE}/admin/webhook-logs?limit=200`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setWebhookLogs(data || []);
      }
    } catch (err) {
      console.error('Failed to fetch webhook logs:', err);
    }
  };

  const fetchCacheData = async () => {
    try {
      const rateRes = await fetch(`${API_BASE}/admin/cache/rate-limits`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (rateRes.ok) {
        const data = await rateRes.json();
        setCacheRateLimits(data || {});
      }

      const vocabRes = await fetch(`${API_BASE}/admin/cache/vocabulary`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (vocabRes.ok) {
        const data = await vocabRes.json();
        setCacheVocabKeys(data.cached_user_ids || []);
      }
    } catch (err) {
      console.error('Failed to fetch cache management data:', err);
    }
  };

  const handleResetRateLimitCache = async (userId = '') => {
    setLoading(true);
    setAuthError('');
    setSuccessMessage('');
    try {
      const url = userId ? `${API_BASE}/admin/cache/rate-limits?user_id=${userId}` : `${API_BASE}/admin/cache/rate-limits`;
      const res = await fetch(url, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        setSuccessMessage(userId ? `Rate limit cache cleared for User ID: ${userId}` : 'All in-memory rate limit caches cleared.');
        fetchCacheData();
      } else {
        const errText = await res.text();
        setAuthError(errText || 'Failed to clear rate limit cache.');
      }
    } catch (err) {
      setAuthError('Network error clearing rate limit cache.');
    } finally {
      setLoading(false);
    }
  };

  const handleInvalidateVocabCache = async (userId = '') => {
    setLoading(true);
    setAuthError('');
    setSuccessMessage('');
    try {
      const url = userId ? `${API_BASE}/admin/cache/vocabulary?user_id=${userId}` : `${API_BASE}/admin/cache/vocabulary`;
      const res = await fetch(url, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        setSuccessMessage(userId ? `Vocabulary cache invalidated for User ID: ${userId}` : 'All in-memory vocabulary caches invalidated.');
        fetchCacheData();
      } else {
        const errText = await res.text();
        setAuthError(errText || 'Failed to invalidate vocabulary cache.');
      }
    } catch (err) {
      setAuthError('Network error invalidating vocabulary cache.');
    } finally {
      setLoading(false);
    }
  };

  const handleOpenEditUser = (u) => {
    setEditingUser(u);
    setEditPlanTier(u.plan_tier || 'free');
    setEditPlanActive(u.plan_active ? 1 : 0);
    setEditRateLimit(u.rate_limit_rpm !== null ? String(u.rate_limit_rpm) : '');
    setAuthError('');
    setSuccessMessage('');
  };

  const handleSaveEditUser = async (e) => {
    e.preventDefault();
    if (!editingUser) return;
    setLoading(true);
    setAuthError('');
    try {
      // 1. Save Plan Details
      const planRes = await fetch(`${API_BASE}/admin/users/${editingUser.id}/plan`, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}` 
        },
        body: JSON.stringify({ 
          plan_tier: editPlanTier, 
          plan_active: parseInt(editPlanActive) 
        })
      });
      if (!planRes.ok) {
        const errText = await planRes.text();
        throw new Error(errText || 'Failed to update plan settings');
      }

      // 2. Save Rate Limit
      const rpmVal = editRateLimit.trim() === '' ? null : parseInt(editRateLimit);
      const limitRes = await fetch(`${API_BASE}/admin/users/${editingUser.id}/rate-limit`, {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}` 
        },
        body: JSON.stringify({ 
          rate_limit_rpm: rpmVal 
        })
      });
      if (!limitRes.ok) {
        const errText = await limitRes.text();
        throw new Error(errText || 'Failed to update rate limits');
      }

      setEditingUser(null);
      fetchAdminUsers();
      setSuccessMessage('User updated successfully.');
    } catch (err) {
      setAuthError(err.message || 'An error occurred during updating.');
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
  const handleConfirmCancelSubscription = async () => {
    if (!token) return;
    setAuthError('');
    setSuccessMessage('');
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/billing/cancel-subscription`, {
        method: 'POST',
        headers: { 
          'Authorization': `Bearer ${token}` 
        }
      });
      if (res.ok) {
        setSuccessMessage("Your subscription was successfully cancelled. You have been returned to the Starter plan.");
        setShowCancelModal(false);
        fetchUserProfile();
        setRoute('dashboard');
      } else {
        const errText = await res.text();
        setAuthError(errText || "Failed to cancel subscription.");
      }
    } catch (err) {
      setAuthError("Could not connect to billing server.");
    } finally {
      setLoading(false);
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
              {user.is_admin && (
                <button 
                  onClick={() => setRoute(route === 'admin' ? 'dashboard' : 'admin')} 
                  className="btn btn-primary btn-sm"
                  style={{ background: route === 'admin' ? '#034f46' : 'var(--color-primary)' }}
                >
                  🛡️ Admin Panel
                </button>
              )}
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

      {/* 2FA Verification Page */}
      {route === '2fa' && (
        <main className="auth-page">
          <div className="auth-box">
            <div className="auth-header">
              <h2>Two-Factor Authentication</h2>
              <p>Enter the 6-digit verification code from your authenticator app.</p>
            </div>

            {authError && <div className="auth-error">{authError}</div>}

            <form onSubmit={handle2faVerify}>
              <div className="auth-form-control">
                <label>Verification Code</label>
                <input 
                  type="text" 
                  value={totpCode} 
                  onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))} 
                  placeholder="000000" 
                  className="auth-input"
                  style={{ textAlign: 'center', fontSize: '24px', letterSpacing: '8px' }}
                  required
                />
              </div>
              <button type="submit" className="btn btn-primary auth-submit-btn" style={{ justifyContent: 'center' }}>
                {loading ? <div className="spinner"></div> : 'Verify & Sign In'}
              </button>
            </form>

            <div className="auth-toggle">
              <button onClick={() => { setRoute('login'); setTempToken(''); setTotpCode(''); setAuthError(''); }} className="auth-toggle-link">
                Back to Sign In
              </button>
            </div>
          </div>
        </main>
      )}

      {/* 2FA Setup Page */}
      {route === '2fa_setup' && totpSetupData && (
        <main className="auth-page">
          <div className="auth-box" style={{ maxWidth: '480px' }}>
            <div className="auth-header">
              <h2>Set Up Two-Factor Authentication</h2>
              <p>Admin accounts require 2FA. Scan the QR code using Google Authenticator, Authy, or 1Password.</p>
            </div>

            {authError && <div className="auth-error">{authError}</div>}

            <div style={{ textAlign: 'center', margin: '20px 0' }}>
              <img 
                src={totpSetupData.qr_code_url} 
                alt="2FA QR Code" 
                style={{ width: '180px', height: '180px', border: '2px solid #1a1a1a', borderRadius: '8px', padding: '8px', background: '#fff' }} 
              />
              <div style={{ marginTop: '12px', fontSize: '13px', color: '#666' }}>
                Or enter key manually: <strong style={{ letterSpacing: '1px', fontFamily: 'monospace', color: '#1a1a1a', background: '#f3f4f6', padding: '2px 6px', borderRadius: '4px' }}>{totpSetupData.secret}</strong>
              </div>
            </div>

            <form onSubmit={handle2faConfirmSetup}>
              <div className="auth-form-control">
                <label>Enter 6-Digit Code from Authenticator App</label>
                <input 
                  type="text" 
                  value={totpCode} 
                  onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))} 
                  placeholder="000000" 
                  className="auth-input"
                  style={{ textAlign: 'center', fontSize: '24px', letterSpacing: '8px' }}
                  required
                />
              </div>
              <button type="submit" className="btn btn-primary auth-submit-btn" style={{ justifyContent: 'center' }}>
                {loading ? <div className="spinner"></div> : 'Confirm 2FA & Complete Login'}
              </button>
            </form>

            <div className="auth-toggle">
              <button onClick={() => { setRoute('login'); setTempToken(''); setTotpCode(''); setTotpSetupData(null); setAuthError(''); }} className="auth-toggle-link">
                Back to Sign In
              </button>
            </div>
          </div>
        </main>
      )}

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

          {/* Sub-tab Navigation */}
          <div className="tab-navigation">
            <button 
              onClick={() => { setActiveSubTab('stats'); setActiveMeeting(null); }} 
              className={`btn ${activeSubTab === 'stats' ? 'btn-primary' : ''}`}
            >
              📊 Stats & Logs
            </button>
            <button 
              onClick={() => { setActiveSubTab('vocab'); setActiveMeeting(null); }} 
              className={`btn ${activeSubTab === 'vocab' ? 'btn-primary' : ''}`}
            >
              📖 Custom Vocabulary
            </button>
            {user.plan_active && (
              <button 
                onClick={() => { setActiveSubTab('meetings'); }} 
                className={`btn ${activeSubTab === 'meetings' ? 'btn-primary' : ''}`}
              >
                📝 Meeting Notes
              </button>
            )}
          </div>

          {activeSubTab === 'stats' && (
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
                      <span>{formatSecondsToHours(user.duration_seconds)} / {formatSecondsToHours(user.monthly_limit_seconds || (user.plan_tier === 'pro' ? 50 * 3600 : 1 * 3600))}</span>
                    </div>
                    
                    <div className="progress-bar-container">
                      <div 
                        className="progress-bar-fill" 
                        style={{ 
                          width: `${Math.min(
                            (user.duration_seconds / (user.monthly_limit_seconds || (user.plan_tier === 'pro' ? 50 * 3600 : 1 * 3600))) * 100, 
                            100
                          )}%` 
                        }}
                      ></div>
                    </div>
                    
                    <p style={{ fontSize: '13px', color: 'rgba(26, 26, 26, 0.5)', marginTop: '8px' }}>
                      {user.plan_tier === 'free' 
                        ? 'Upgrade to SpeekTo.Me Pro to get 50 hours of dictation limit per month and access to enhanced AI formatting.' 
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
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                      <button onClick={() => setRoute('billing')} className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff' }}>
                        Manage Billing Details
                      </button>
                      <button 
                        onClick={() => setShowCancelModal(true)} 
                        className="btn btn-danger" 
                        style={{ width: '100%', justifyContent: 'center' }}
                        disabled={loading}
                      >
                        ❌ Cancel Subscription
                      </button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {activeSubTab === 'vocab' && (
            <div className="card">
              <h3 className="card-title">📖 Custom Vocabulary Terms</h3>
              <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '20px' }}>
                Add specialized terms, abbreviations, acronyms, or names that you want the dictation pipeline to recognize and spell correctly.
              </p>
              
              {authError && <div className="auth-error" style={{ marginBottom: '20px' }}>{authError}</div>}
              
              <form onSubmit={handleAddVocab} className="vocab-form">
                <input 
                  type="text" 
                  value={newVocabTerm} 
                  onChange={(e) => setNewVocabTerm(e.target.value)} 
                  placeholder="e.g. Synaptican, SpeekTo.Me, ChatGPT" 
                  className="auth-input"
                  required
                />
                <button type="submit" className="btn btn-primary">
                  {loading ? <div className="spinner"></div> : 'Add Term'}
                </button>
              </form>

              <div className="vocab-table-container">
                {vocabTerms.length === 0 ? (
                  <p style={{ color: 'rgba(26, 26, 26, 0.5)', padding: '20px 0', textAlign: 'center' }}>
                    No custom vocabulary terms added yet.
                  </p>
                ) : (
                  <table className="logs-table">
                    <thead>
                      <tr>
                        <th>Canonical Term</th>
                        <th>Normalized Representation</th>
                        <th style={{ textAlign: 'right' }}>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {vocabTerms.map((term, idx) => (
                        <tr key={idx}>
                          <td><strong>{term.canonical_term}</strong></td>
                          <td><code style={{ background: 'var(--bg-canvas)', padding: '2px 6px', borderRadius: '4px', border: '1px solid rgba(26,26,26,0.1)' }}>{term.normalized_term}</code></td>
                          <td style={{ textAlign: 'right' }}>
                            <button 
                              onClick={() => handleDeleteVocab(term.canonical_term)} 
                              className="btn btn-danger btn-sm"
                              style={{ padding: '4px 8px' }}
                              disabled={loading}
                            >
                              Delete
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </div>
          )}

          {activeSubTab === 'meetings' && (
            <>
              {activeMeeting ? (
                <div className="card">
                  <div className="meeting-detail-header">
                    <div>
                      <button 
                        onClick={() => setActiveMeeting(null)} 
                        className="btn btn-sm" 
                        style={{ marginBottom: '12px' }}
                      >
                        ⬅ Back to Meetings List
                      </button>
                      <h2 className="meeting-detail-title">{activeMeeting.title}</h2>
                      <span className="meeting-card-date">Recorded on {formatDate(activeMeeting.timestamp)}</span>
                    </div>
                    <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
                      <span className="meeting-meta-badge">⏱ {formatSeconds(activeMeeting.duration)}</span>
                      <button 
                        onClick={() => handleDeleteMeeting(activeMeeting.id)} 
                        className="btn btn-danger btn-sm"
                        disabled={loading}
                      >
                        Delete Meeting
                      </button>
                    </div>
                  </div>

                  {authError && <div className="auth-error" style={{ marginBottom: '20px' }}>{authError}</div>}

                  <div className="meeting-detail-grid">
                    {/* Left Column: Summary & Actions */}
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
                      <div>
                        <h3 className="meeting-section-title">💡 Meeting Summary</h3>
                        <div className="meeting-text-box">
                          {activeMeeting.summary || "No summary generated for this meeting."}
                        </div>
                      </div>

                      <div>
                        <h3 className="meeting-section-title">☑ Action Items</h3>
                        <div className="meeting-text-box">
                          {activeMeeting.action_items ? (
                            <ul className="action-items-list">
                              {activeMeeting.action_items.split('\n').filter(item => item.trim() !== '').map((item, idx) => (
                                <li key={idx}>{item.replace(/^-\s*\[\s*[x ]\s*\]\s*/, '').replace(/^-\s*/, '')}</li>
                              ))}
                            </ul>
                          ) : (
                            "No action items found."
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Right Column: Full Transcript & Segments */}
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
                      <div>
                        <h3 className="meeting-section-title">🎙 Full Transcript</h3>
                        <div className="meeting-text-box">
                          {activeMeeting.transcript || "No transcript found."}
                        </div>
                      </div>

                      <div>
                        <h3 className="meeting-section-title">📊 Audio Segments</h3>
                        <div className="meeting-text-box" style={{ maxHeight: '350px' }}>
                          {(() => {
                            let segments = [];
                            try {
                              segments = JSON.parse(activeMeeting.segments_json);
                            } catch (e) {}
                            
                            if (!segments || segments.length === 0) {
                              return <p style={{ color: 'rgba(26,26,26,0.5)' }}>No individual segment metadata available.</p>;
                            }
                            
                            return (
                              <div className="segments-list">
                                {segments.map((seg, idx) => (
                                  <div key={idx} className="segment-item">
                                    <span className="segment-time">
                                      {formatSeconds(seg.start || 0)} - {formatSeconds(seg.end || 0)}
                                    </span>
                                    <span className="segment-text">{seg.text}</span>
                                  </div>
                                ))}
                              </div>
                            );
                          })()}
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="card">
                  <h3 className="card-title">📝 Recorded Meetings</h3>
                  <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '24px' }}>
                    View details, transcripts, action items, and summaries of meetings recorded via your SpeekTo.Me desktop client.
                  </p>

                  {meetings.length === 0 ? (
                    <p style={{ color: 'rgba(26, 26, 26, 0.5)', padding: '40px 0', textAlign: 'center' }}>
                      No recorded meetings found. Open your desktop client to record and process meeting notes!
                    </p>
                  ) : (
                    <div className="meetings-grid">
                      {meetings.map((meeting) => (
                        <div 
                          key={meeting.id} 
                          onClick={() => setActiveMeeting(meeting)}
                          className="card meeting-card"
                          style={{ padding: '24px', margin: 0 }}
                        >
                          <span className="meeting-card-date">{formatDate(meeting.timestamp)}</span>
                          <h4 style={{ fontSize: '18px', marginBottom: '8px' }}>{meeting.title}</h4>
                          <p style={{ fontSize: '13px', color: 'rgba(26, 26, 26, 0.6)', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden', height: '38px', lineHeight: '19px' }}>
                            {meeting.summary || "Click to view full transcript and summaries."}
                          </p>
                          <div className="meeting-card-duration">
                            ⏱ Duration: {formatSeconds(meeting.duration)}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
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
                  <button 
                    onClick={() => setShowCancelModal(true)} 
                    className="btn btn-danger" 
                    style={{ width: '100%', justifyContent: 'center' }}
                    disabled={loading}
                  >
                    Downgrade to Starter Plan
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
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', width: '100%' }}>
                    <button className="btn" style={{ width: '100%', justifyContent: 'center', background: '#fff', cursor: 'default' }} disabled>
                      Current Active Plan (Subscribed)
                    </button>
                    <button 
                      onClick={() => setShowCancelModal(true)} 
                      className="btn btn-danger btn-sm" 
                      style={{ width: '100%', justifyContent: 'center' }}
                      disabled={loading}
                    >
                      ❌ Cancel Subscription
                    </button>
                  </div>
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

      {/* 6. Custom Confirmation Modal */}
      {showCancelModal && (
        <div className="modal-overlay" style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(36, 10, 18, 0.6)',
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          zIndex: 1000,
          backdropFilter: 'blur(4px)'
        }}>
          <div className="card" style={{
            maxWidth: '450px',
            width: '90%',
            margin: '20px',
            position: 'relative',
            background: 'var(--bg-canvas)',
            animation: 'modalSlideIn 0.2s cubic-bezier(0.16, 1, 0.3, 1)',
            boxShadow: 'var(--box-shadow-neo)'
          }}>
            <h3 className="card-title" style={{ borderBottomColor: 'var(--border-color)', fontSize: '22px' }}>
              ⚠️ Cancel Subscription?
            </h3>
            <p style={{ fontSize: '15px', lineHeight: '1.6', marginBottom: '24px', color: 'var(--color-text-dark)' }}>
              Are you sure you want to cancel your SpeekTo.Me Pro subscription? Your account will be immediately downgraded to the Starter tier, and you will not be billed further.
            </p>
            <div style={{ display: 'flex', gap: '16px', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
              <button 
                onClick={() => setShowCancelModal(false)} 
                className="btn" 
                style={{ background: '#fff' }}
                disabled={loading}
              >
                Keep Subscription
              </button>
              <button 
                onClick={handleConfirmCancelSubscription} 
                className="btn btn-danger"
                disabled={loading}
              >
                {loading ? <div className="spinner"></div> : 'Yes, Cancel Subscription'}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* 5.5. Admin Panel Page */}
      {route === 'admin' && user && user.is_admin && (
        <main className="dashboard-main">
          <div className="welcome-section" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '16px' }}>
            <div>
              <h1 className="welcome-title">🛡️ Admin Panel</h1>
              <p>Manage users, override billing/subscription tiers, and customize rate limits.</p>
            </div>
            <button onClick={() => { fetchAdminUsers(); fetchWebhookLogs(); fetchCacheData(); }} className="btn" style={{ background: '#fff' }}>
              🔄 Refresh
            </button>
          </div>

          {/* Admin Sub-tabs */}
          <div className="tab-navigation" style={{ marginBottom: '24px' }}>
            <button
              onClick={() => setAdminSubTab('users')}
              className={`btn ${adminSubTab === 'users' ? 'btn-primary' : ''}`}
            >
              👥 Users
            </button>
            <button
              onClick={() => setAdminSubTab('webhook-logs')}
              className={`btn ${adminSubTab === 'webhook-logs' ? 'btn-primary' : ''}`}
            >
              📋 Webhook Logs
            </button>
            <button
              onClick={() => setAdminSubTab('cache')}
              className={`btn ${adminSubTab === 'cache' ? 'btn-primary' : ''}`}
            >
              ⚡ Cache Control
            </button>
          </div>

          {authError && <div className="auth-error" style={{ marginBottom: '20px' }}>{authError}</div>}
          {successMessage && <div className="auth-success" style={{ marginBottom: '20px', background: '#d1fae5', color: '#1a1a1a', border: '2px solid #1a1a1a', padding: '12px 14px', borderRadius: '12px', fontWeight: '500', boxShadow: '2px 2px 0px #1a1a1a' }}>{successMessage}</div>}

          {/* Users Tab */}
          {adminSubTab === 'users' && (
            <>
          <div className="card" style={{ padding: '24px' }}>
            <h3 className="card-title" style={{ marginBottom: '20px' }}>👥 Active Users Directory</h3>
            <div className="logs-table-container">
              <table className="logs-table">
                <thead>
                  <tr>
                    <th>User ID</th>
                    <th>Name</th>
                    <th>Email</th>
                    <th>Plan Tier</th>
                    <th>Plan Status</th>
                    <th>Rate Limit</th>
                    <th>Total Transcribed</th>
                    <th style={{ textAlign: 'right' }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {adminUsers.map((u) => (
                    <tr key={u.id}>
                      <td>#{u.id}</td>
                      <td><strong>{u.name}</strong></td>
                      <td>{u.email}</td>
                      <td>
                        <span className={`badge-status ${u.plan_tier === 'pro' ? 'badge-success' : 'badge-failed'}`} style={{ textTransform: 'uppercase', background: u.plan_tier === 'pro' ? '#d1fae5' : '#f3f4f6', color: u.plan_tier === 'pro' ? '#065f46' : '#374151', border: '1px solid rgba(26,26,26,0.15)', padding: '2px 8px' }}>
                          {u.plan_tier}
                        </span>
                      </td>
                      <td>
                        <span className={`badge-status ${u.plan_active ? 'badge-success' : 'badge-failed'}`}>
                          {u.plan_active ? 'Active' : 'Inactive'}
                        </span>
                      </td>
                      <td>
                        {u.rate_limit_rpm !== null ? `${u.rate_limit_rpm} RPM` : <span style={{ opacity: 0.5 }}>Default (10)</span>}
                      </td>
                      <td>
                        <strong>{formatSeconds(u.total_usage_seconds)}</strong>
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <button 
                          onClick={() => handleOpenEditUser(u)} 
                          className="btn btn-primary btn-sm"
                          style={{ padding: '6px 12px' }}
                        >
                          ✏️ Edit Plan & Limit
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Edit User Modal */}
          {editingUser && (
            <div className="modal-overlay" style={{
              position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
              backgroundColor: 'rgba(26, 26, 26, 0.6)', display: 'flex',
              justifyContent: 'center', alignItems: 'center', zIndex: 1000,
              backdropFilter: 'blur(4px)'
            }}>
              <div className="card" style={{ maxWidth: '480px', width: '90%', margin: '20px', position: 'relative', background: '#fff' }}>
                <h3 className="card-title" style={{ fontSize: '22px' }}>
                  ⚙️ Edit User #{editingUser.id}
                </h3>
                <p style={{ fontSize: '14px', color: 'rgba(26,26,26,0.6)', marginBottom: '20px' }}>
                  Modify subscription tier and customize rate limits for <strong>{editingUser.name}</strong> ({editingUser.email}).
                </p>

                <form onSubmit={handleSaveEditUser}>
                  <div className="auth-form-control" style={{ marginBottom: '16px' }}>
                    <label style={{ display: 'block', fontWeight: 'bold', marginBottom: '6px' }}>Subscription Tier</label>
                    <select 
                      value={editPlanTier} 
                      onChange={(e) => setEditPlanTier(e.target.value)}
                      className="auth-input"
                      style={{ width: '100%', padding: '10px', borderRadius: '8px', border: '2px solid #1a1a1a', background: '#fff' }}
                    >
                      <option value="free">Starter (Free)</option>
                      <option value="pro">Pro (Paid)</option>
                    </select>
                  </div>

                  <div className="auth-form-control" style={{ marginBottom: '16px' }}>
                    <label style={{ display: 'block', fontWeight: 'bold', marginBottom: '6px' }}>Plan Status</label>
                    <select 
                      value={editPlanActive} 
                      onChange={(e) => setEditPlanActive(parseInt(e.target.value))}
                      className="auth-input"
                      style={{ width: '100%', padding: '10px', borderRadius: '8px', border: '2px solid #1a1a1a', background: '#fff' }}
                    >
                      <option value="0">Inactive / Not Subscribed</option>
                      <option value="1">Active / Subscribed</option>
                    </select>
                  </div>

                  <div className="auth-form-control" style={{ marginBottom: '24px' }}>
                    <label style={{ display: 'block', fontWeight: 'bold', marginBottom: '6px' }}>Requests Per Minute (RPM) Rate Limit</label>
                    <input 
                      type="number" 
                      value={editRateLimit} 
                      onChange={(e) => setEditRateLimit(e.target.value)}
                      placeholder="e.g. 25 (leave blank to use defaults)"
                      className="auth-input"
                      style={{ width: '100%', padding: '10px', borderRadius: '8px', border: '2px solid #1a1a1a' }}
                    />
                    <small style={{ color: 'rgba(26,26,26,0.5)', marginTop: '4px', display: 'block' }}>
                      Overrides the default user limit (10 RPM). Leave blank to clear override.
                    </small>
                  </div>

                  <div style={{ display: 'flex', gap: '16px', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                    <button 
                      type="button"
                      onClick={() => setEditingUser(null)} 
                      className="btn" 
                      style={{ background: '#fff' }}
                      disabled={loading}
                    >
                      Cancel
                    </button>
                    <button 
                      type="submit" 
                      className="btn btn-primary"
                      disabled={loading}
                    >
                      {loading ? <div className="spinner"></div> : 'Save Changes'}
                    </button>
                  </div>
                </form>
              </div>
            </div>
          )}
            </>
          )}

          {/* Webhook Logs Tab */}
          {adminSubTab === 'webhook-logs' && (
            <div className="card" style={{ padding: '24px' }}>
              <h3 className="card-title" style={{ marginBottom: '20px' }}>📋 Razorpay Webhook Event Log</h3>
              <div className="logs-table-container">
                <table className="logs-table">
                  <thead>
                    <tr>
                      <th>#</th>
                      <th>Received At</th>
                      <th>Event Type</th>
                      <th>Subscription ID</th>
                      <th>User ID</th>
                      <th>Sub Status</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {webhookLogs.length === 0 ? (
                      <tr>
                        <td colSpan="7" style={{ textAlign: 'center', opacity: 0.5, padding: '24px' }}>No webhook events received yet.</td>
                      </tr>
                    ) : webhookLogs.map((log) => (
                      <tr key={log.id}>
                        <td>#{log.id}</td>
                        <td style={{ fontSize: '12px', whiteSpace: 'nowrap' }}>{new Date(log.received_at).toLocaleString()}</td>
                        <td>
                          <code style={{ fontSize: '12px', background: '#f3f4f6', padding: '2px 6px', borderRadius: '4px' }}>
                            {log.event_type}
                          </code>
                        </td>
                        <td style={{ fontSize: '11px', opacity: 0.7 }}>{log.subscription_id || '—'}</td>
                        <td>{log.user_id ? `#${log.user_id}` : '—'}</td>
                        <td style={{ fontSize: '12px' }}>{log.status || '—'}</td>
                        <td>
                          <span
                            className={`badge-status ${log.result === 'success' ? 'badge-success' : 'badge-failed'}`}
                            title={log.result}
                          >
                            {log.result === 'success' ? '✅ success' : log.result.length > 24 ? log.result.slice(0, 24) + '…' : log.result}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Cache Management Tab */}
          {adminSubTab === 'cache' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
              <div className="card" style={{ padding: '24px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px', flexWrap: 'wrap', gap: '16px' }}>
                  <h3 className="card-title" style={{ margin: 0 }}>⚡ In-Memory Rate Limiting Counts (Fixed-Window 60s)</h3>
                  <button 
                    onClick={() => handleResetRateLimitCache()} 
                    className="btn btn-failed" 
                    style={{ fontSize: '12px' }}
                  >
                    🗑️ Reset All Rate Limits Globally
                  </button>
                </div>
                
                <div className="logs-table-container">
                  <table className="logs-table">
                    <thead>
                      <tr>
                        <th>User Identifier</th>
                        <th>Requests Made (This Minute)</th>
                        <th style={{ textAlign: 'right' }}>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {Object.keys(cacheRateLimits).length === 0 ? (
                        <tr>
                          <td colSpan="3" style={{ textAlign: 'center', opacity: 0.5, padding: '24px' }}>
                            No active rate limit entries in memory.
                          </td>
                        </tr>
                      ) : Object.keys(cacheRateLimits).map((key) => {
                        const rawUserId = key.replace('user_', '');
                        return (
                          <tr key={key}>
                            <td>
                              <strong>{key}</strong>
                            </td>
                            <td>
                              <span style={{ fontWeight: 'bold', color: cacheRateLimits[key] >= 10 ? '#dc2626' : 'inherit' }}>
                                {cacheRateLimits[key]}
                              </span>
                            </td>
                            <td style={{ textAlign: 'right' }}>
                              <button 
                                onClick={() => handleResetRateLimitCache(rawUserId)} 
                                className="btn btn-secondary" 
                                style={{ fontSize: '11px', padding: '6px 10px' }}
                              >
                                Reset User Limit
                              </button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              </div>

              <div className="card" style={{ padding: '24px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px', flexWrap: 'wrap', gap: '16px' }}>
                  <h3 className="card-title" style={{ margin: 0 }}>📖 Active User Vocabulary Cache</h3>
                  <button 
                    onClick={() => handleInvalidateVocabCache()} 
                    className="btn btn-failed" 
                    style={{ fontSize: '12px' }}
                  >
                    🗑️ Invalidate All Vocabulary Cache
                  </button>
                </div>
                
                <div className="logs-table-container">
                  <table className="logs-table">
                    <thead>
                      <tr>
                        <th>User ID</th>
                        <th>Status</th>
                        <th style={{ textAlign: 'right' }}>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {cacheVocabKeys.length === 0 ? (
                        <tr>
                          <td colSpan="3" style={{ textAlign: 'center', opacity: 0.5, padding: '24px' }}>
                            No vocabularies compiled in memory.
                          </td>
                        </tr>
                      ) : cacheVocabKeys.map((uid) => (
                        <tr key={uid}>
                          <td>
                            <strong>User #{uid}</strong>
                          </td>
                          <td>
                            <span className="badge-status badge-success">Cached in Memory</span>
                          </td>
                          <td style={{ textAlign: 'right' }}>
                            <button 
                              onClick={() => handleInvalidateVocabCache(uid)} 
                              className="btn btn-secondary" 
                              style={{ fontSize: '11px', padding: '6px 10px' }}
                            >
                              Invalidate Cache
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
          )}
        </main>
      )}
    </div>
  );
}
