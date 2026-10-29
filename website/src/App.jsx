import React, { useState, useEffect, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import Navbar from './components/Navbar';
import Footer from './components/Footer';
import CommandUsage from './components/CommandUsage';
import HomePage from './pages/HomePage';
import AboutPage from './pages/AboutPage';
import PricingPage from './pages/PricingPage';
import PrivacyPage from './pages/PrivacyPage';
import RefundPage from './pages/RefundPage';
import TermsPage from './pages/TermsPage';
import ScrollToTop from './components/ScrollToTop';
import TurnstileWidget from './components/TurnstileWidget';
import logoSvg from '/assets/speek.svg';
import { TwoFactorPage, LoginPage, ForgotPasswordPage, ResetPasswordPage, SignupPage } from './account/AuthPages';
import DashboardPage from './account/DashboardPage';
import BillingPage from './account/BillingPage';
import AdminPage from './account/AdminPage';


import { SUPPORTED_LANGUAGES, getLanguageName } from './utils/languages';

import { API_BASE } from './utils/api';

export default function App() {
  const location = useLocation();
  const exchangeStarted = useRef(false);

  const [token, setToken] = useState(localStorage.getItem('speektome_token') || '');
  const [user, setUser] = useState(null);
  const [userLanguages, setUserLanguages] = useState(['en']);
  const [savingLangs, setSavingLangs] = useState(false);
  const [logs, setLogs] = useState([]);
  const [route, setRoute] = useState('loading');
  const [loading, setLoading] = useState(false);
  const [authError, setAuthError] = useState('');
  const [successMessage, setSuccessMessage] = useState('');
  const [turnstileToken, setTurnstileToken] = useState('');
  const [resetTurnstile, setResetTurnstile] = useState(0);

  // Auth Form inputs
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');

  // 2FA state
  const [tempToken, setTempToken] = useState('');
  const [totpCode, setTotpCode] = useState('');

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

  // Profile, Password Reset & Account Deletion states
  const [profileSummary, setProfileSummary] = useState(null);
  const [loadingSummary, setLoadingSummary] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [deleteConfirmText, setDeleteConfirmText] = useState('');
  const [deletingAccount, setDeletingAccount] = useState(false);
  const [resetEmailSuccess, setResetEmailSuccess] = useState('');
  const [resetEmailError, setResetEmailError] = useState('');
  const [sendingResetEmail, setSendingResetEmail] = useState(false);
  const [forgotEmail, setForgotEmail] = useState('');
  const [resetTokenParam, setResetTokenParam] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmNewPassword, setConfirmNewPassword] = useState('');
  const [resettingPassword, setResettingPassword] = useState(false);

  // 1. Initial Authentication & Redirect check
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const exchangeCode = params.get('code');
    if (exchangeCode) {
      if (exchangeStarted.current) return;
      exchangeStarted.current = true;
      // Remove the one-time code before loading third-party assets or navigating.
      window.history.replaceState({}, document.title, window.location.pathname);
      fetch(`${API_BASE}/auth/exchange`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code: exchangeCode }) })
        .then(async res => { if (!res.ok) throw new Error('Sign-in expired. Please try again.'); return res.json(); })
        .then(data => {
          if (data.require_2fa) { setTempToken(data.token); setRoute('2fa'); }
          else { localStorage.setItem('speektome_token', data.token); setToken(data.token); }
        })
        .catch(() => { setRoute('login'); setAuthError('Sign-in expired. Please try again.'); });
      return;
    }
    const isVerified = params.get('verified');
    const verifiedEmail = params.get('email');
    const queryResetToken = params.get('reset_token');

    // Sessions arrive only through the one-time code above. A session token
    // in the URL is never accepted: a link could sign a visitor into someone
    // else's account, and URLs end up in history and logs.
    if (params.has('token') || params.has('temp_token')) {
      window.history.replaceState({}, document.title, window.location.pathname);
    }
    if (queryResetToken) {
      setResetTokenParam(queryResetToken);
      setRoute('reset-password');
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
    } else if (route !== '2fa' && route !== 'signup' && route !== 'reset-password' && route !== 'forgot-password') {
      const params = new URLSearchParams(window.location.search);
      if (!params.get('temp_token')) {
        setRoute('login');
      }
    }
  }, [token]);

  useEffect(() => {
    if (!token || route !== 'dashboard') return;
    let disposed = false;
    const refreshAllowance = async () => {
      if (document.visibilityState === 'hidden') return;
      try {
        const res = await fetch(`${API_BASE}/auth/me`, { headers: { Authorization: `Bearer ${token}` } });
        if (res.ok) {
          const data = await res.json();
          if (!disposed) setUser(data);
        }
      } catch { /* Preserve the last successful account snapshot while offline. */ }
    };
    refreshAllowance();
    window.addEventListener('focus', refreshAllowance);
    return () => { disposed = true; window.removeEventListener('focus', refreshAllowance); };
  }, [token, route, activeSubTab]);

  const fetchUserProfile = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/auth/me`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setUser(data);
        if (data.languages && Array.isArray(data.languages)) {
          setUserLanguages(data.languages);
        }
        setRoute('dashboard');
        fetchUsageLogs();
        fetchVocabulary();
        fetchMeetings();
        fetchProfileSummary();
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

  const fetchProfileSummary = async () => {
    if (!token) return;
    setLoadingSummary(true);
    try {
      const res = await fetch(`${API_BASE}/auth/profile/summary`, {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        const data = await res.json();
        setProfileSummary(data);
      }
    } catch (err) {
      console.error("Failed to load profile summary:", err);
    } finally {
      setLoadingSummary(false);
    }
  };

  const handleToggleLanguage = async (code) => {
    let updated;
    if (userLanguages.includes(code)) {
      if (userLanguages.length <= 1) {
        setAuthError("At least 1 language must be selected.");
        return;
      }
      updated = userLanguages.filter(c => c !== code);
    } else {
      if (userLanguages.length >= 5) {
        setAuthError("You can select at most 5 spoken languages.");
        return;
      }
      updated = [...userLanguages, code];
    }
    setUserLanguages(updated);
    setAuthError('');
    setSavingLangs(true);
    try {
      const res = await fetch(`${API_BASE}/auth/languages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ languages: updated })
      });
      if (res.ok) {
        const data = await res.json();
        setUserLanguages(data.languages || updated);
        setSuccessMessage("Spoken languages updated successfully!");
      } else {
        const errText = await res.text();
        setAuthError(errText || "Failed to update language settings.");
      }
    } catch (err) {
      setAuthError("Could not connect to server to update languages.");
    } finally {
      setSavingLangs(false);
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

  const handleSendPasswordReset = async (e) => {
    if (e) e.preventDefault();
    setResetEmailError('');
    setResetEmailSuccess('');
    const targetEmail = user ? user.email : forgotEmail;
    if (!targetEmail) {
      setResetEmailError('Please specify an email address.');
      return;
    }
    if (!turnstileToken) {
      setResetEmailError('Please complete the verification check.');
      return;
    }
    setSendingResetEmail(true);
    try {
      const res = await fetch(`${API_BASE}/auth/forgot-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          email: targetEmail,
          'cf-turnstile-response': turnstileToken
        })
      });
      const data = await res.json();
      if (res.ok) {
        setResetEmailSuccess(data.message || 'If an account exists with this email, a password reset link has been sent.');
      } else {
        setResetEmailError(data.error || 'Failed to send password reset email.');
      }
    } catch (err) {
      setResetEmailError('Could not connect to server.');
    } finally {
      setSendingResetEmail(false);
      setTurnstileToken('');
      setResetTurnstile(prev => prev + 1);
    }
  };

  const handleResetPasswordSubmit = async (e) => {
    e.preventDefault();
    setAuthError('');
    if (newPassword !== confirmNewPassword) {
      setAuthError('Passwords do not match.');
      return;
    }
    if (!turnstileToken) {
      setAuthError('Please complete the verification check.');
      return;
    }
    setResettingPassword(true);
    try {
      const res = await fetch(`${API_BASE}/auth/reset-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          token: resetTokenParam,
          password: newPassword,
          'cf-turnstile-response': turnstileToken
        })
      });
      const data = await res.json();
      if (res.ok) {
        setSuccessMessage(data.message || 'Password updated successfully! Please sign in with your new password.');
        setRoute('login');
        setResetTokenParam('');
        setNewPassword('');
        setConfirmNewPassword('');
      } else {
        setAuthError(data.error || 'Failed to reset password.');
      }
    } catch (err) {
      setAuthError('Could not connect to server.');
    } finally {
      setResettingPassword(false);
      setTurnstileToken('');
      setResetTurnstile(prev => prev + 1);
    }
  };

  const handleDeleteAccount = async () => {
    if (deleteConfirmText.trim() !== 'DELETE') return;
    setDeletingAccount(true);
    setAuthError('');
    try {
      const res = await fetch(`${API_BASE}/auth/account`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` }
      });
      if (res.ok) {
        setShowDeleteModal(false);
        setDeleteConfirmText('');
        handleLogoutLocal();
        setSuccessMessage('Your account and all associated data have been permanently deleted.');
      } else {
        const errText = await res.text();
        setAuthError(errText || 'Failed to delete account.');
      }
    } catch (err) {
      setAuthError('Could not connect to server.');
    } finally {
      setDeletingAccount(false);
    }
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
        body: JSON.stringify({
          email,
          password,
          'cf-turnstile-response': turnstileToken
        })
      });
      const data = await res.json();
      if (res.ok) {
        if (data.require_2fa) {
          setTempToken(data.temp_token);
          setRoute('2fa');
        } else {
          localStorage.setItem('speektome_token', data.token);
          setToken(data.token);
          setEmail('');
          setPassword('');
        }
      } else {
        setAuthError(data.error || 'Invalid credentials');
        setResetTurnstile(prev => prev + 1);
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
      setResetTurnstile(prev => prev + 1);
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
        setAuthError(data.error || 'Invalid 2FA code');
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
        body: JSON.stringify({
          email,
          password,
          name,
          'cf-turnstile-response': turnstileToken
        })
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
        setResetTurnstile(prev => prev + 1);
      }
    } catch (err) {
      setAuthError('Cannot connect to SpeekTo.Me authentication server.');
      setResetTurnstile(prev => prev + 1);
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
      const planAmount = data.amount ? `${data.currency} ${(data.amount / 100).toFixed(2)}` : 'the configured plan';
      const options = {
        key: data.razorpay_key_id,
        subscription_id: data.subscription_id,
        name: 'SpeekTo.Me',
        description: `SpeekTo.Me Pro (${planAmount}/mo)`,
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
    const s = Number(seconds) || 0;
    if (s <= 0) return '0.0h';
    if (s >= 730 && s <= 10000) return 'Unlimited';
    const hours = s / 3600;
    if (hours >= 730) return 'Unlimited';
    return `${hours.toFixed(1)}h`;
  };

  const formatUsageParts = (usedSec, totalSec, isPaid = false) => {
    const safeUsed = Math.max(0, Number(usedSec) || 0);
    const rawTotalSec = Math.max(0, Number(totalSec) || 3600);
    
    const totalHours = rawTotalSec / 3600;

    let usedStr = '0s';
    if (safeUsed < 60) {
      usedStr = `${Math.round(safeUsed)}s`;
    } else if (safeUsed < 3600) {
      usedStr = `${Math.round(safeUsed / 60)}m`;
    } else {
      usedStr = `${(safeUsed / 3600).toFixed(1)}h`;
    }

    const isUnlimited = isPaid && (totalHours >= 730);
    let totalStr = '1h';
    if (isUnlimited) {
      totalStr = 'Unlimited';
    } else if (totalHours < 1.0) {
      const totalMins = Math.round(rawTotalSec / 60);
      totalStr = totalMins > 0 ? `${totalMins}m` : `${Math.round(rawTotalSec)}s`;
    } else if (Number.isInteger(totalHours)) {
      totalStr = `${Math.round(totalHours)}h`;
    } else {
      totalStr = `${totalHours.toFixed(1)}h`;
    }

    const fupHours = Math.round(totalHours);
    const fupTooltip = `Fair Usage Limit: ${fupHours.toLocaleString()} hours / month.`;

    return { usedStr, totalStr, isUnlimited, fupTooltip, display: `${usedStr} / ${totalStr}` };
  };

  const formatUsageFraction = (usedSec, totalSec) => {
    return formatUsageParts(usedSec, totalSec).display;
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

  // Render marketing pages for public routes
  const renderMarketingLayout = (Component) => (
    <>
      <ScrollToTop />
      <Navbar token={token} user={user} onLogout={handleLogoutLocal} />
      <Component />
      <Footer />
    </>
  );

  const currentPath = location.pathname.toLowerCase();
  if (currentPath === '/') return renderMarketingLayout(HomePage);
  if (currentPath === '/about') return renderMarketingLayout(AboutPage);
  if (currentPath === '/pricing') return renderMarketingLayout(PricingPage);
  if (currentPath === '/privacy') return renderMarketingLayout(PrivacyPage);
  if (currentPath === '/refund') return renderMarketingLayout(RefundPage);
  if (currentPath === '/terms') return renderMarketingLayout(TermsPage);

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

  // State and actions shared with the account pages.
  const view = {
    activeMeeting,
    activeSubTab,
    adminSubTab,
    adminUsers,
    authError,
    cacheRateLimits,
    cacheVocabKeys,
    confirmNewPassword,
    deleteConfirmText,
    deletingAccount,
    editPlanActive,
    editPlanTier,
    editRateLimit,
    editingUser,
    email,
    fetchAdminUsers,
    fetchCacheData,
    fetchProfileSummary,
    fetchWebhookLogs,
    forgotEmail,
    formatDate,
    formatSeconds,
    formatUsageParts,
    handle2faVerify,
    handleAddVocab,
    handleDeleteAccount,
    handleDeleteMeeting,
    handleDeleteVocab,
    handleGoogleLogin,
    handleInvalidateVocabCache,
    handleLogin,
    handleOpenEditUser,
    handleResetPasswordSubmit,
    handleResetRateLimitCache,
    handleSaveEditUser,
    handleSendPasswordReset,
    handleSignup,
    handleToggleLanguage,
    handleUpgrade,
    loading,
    logs,
    meetings,
    name,
    newPassword,
    newVocabTerm,
    password,
    profileSummary,
    resetEmailError,
    resetEmailSuccess,
    resetTurnstile,
    resettingPassword,
    savingLangs,
    sendingResetEmail,
    setActiveMeeting,
    setActiveSubTab,
    setAdminSubTab,
    setMeetings,
    setAuthError,
    setConfirmNewPassword,
    setDeleteConfirmText,
    setEditPlanActive,
    setEditPlanTier,
    setEditRateLimit,
    setEditingUser,
    setEmail,
    setForgotEmail,
    setName,
    setNewPassword,
    setNewVocabTerm,
    setPassword,
    setResetEmailError,
    setResetEmailSuccess,
    setRoute,
    setShowCancelModal,
    setShowDeleteModal,
    setTempToken,
    setTotpCode,
    setTurnstileToken,
    showDeleteModal,
    successMessage,
    token,
    totpCode,
    turnstileToken,
    user,
    userLanguages,
    vocabTerms,
    webhookLogs,
  };

  return (
    <div className="dashboard-container">
      <ScrollToTop />
      {/* 1. Header */}

      <header className="dashboard-header">
        <a href="/" className="logo" style={{ textDecoration: 'none', display: 'inline-flex', alignItems: 'center' }}>
          <img src={logoSvg} alt="SpeekTo.Me Logo" style={{ width: '26px', height: '26px', marginRight: '8px', verticalAlign: 'middle', display: 'inline-block' }} />
          SpeekTo<span className="logo-dot">.</span>Me
        </a>
        <nav className="nav-links">
          {user ? (
            <>
              <span
                className="nav-user"
                onClick={() => { setActiveSubTab('profile'); setRoute('dashboard'); setActiveMeeting(null); fetchProfileSummary(); }}
                style={{ cursor: 'pointer', transition: 'opacity 0.2s ease' }}
                title="Click to view Profile"
              >
                <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" style={{ marginRight: '6px', verticalAlign: 'middle', display: 'inline-block' }}><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" /></svg>
                {user.name} ({user.email})
              </span>
              {user.is_admin && (
                <button
                  onClick={() => setRoute(route === 'admin' ? 'dashboard' : 'admin')}
                  className="btn btn-primary btn-sm"
                  style={{ background: route === 'admin' ? '#034f46' : 'var(--color-primary)', display: 'inline-flex', alignItems: 'center', gap: '6px' }}
                >
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" /></svg> Admin Panel
                </button>
              )}
              <button onClick={() => setRoute(route === 'dashboard' ? 'billing' : 'dashboard')} className="btn btn-primary btn-sm">
                {route === 'dashboard' ? (
                  <>
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ marginRight: '6px', verticalAlign: 'middle', display: 'inline-block' }}><rect x="1" y="4" width="22" height="16" rx="2" ry="2" /><line x1="1" y1="10" x2="23" y2="10" /></svg>
                    Manage Subscription
                  </>
                ) : (
                  <>
                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ marginRight: '6px', verticalAlign: 'middle', display: 'inline-block' }}><line x1="18" y1="20" x2="18" y2="10" /><line x1="12" y1="20" x2="12" y2="4" /><line x1="6" y1="20" x2="6" y2="14" /></svg>
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
      {route === '2fa' && <TwoFactorPage view={view} />}

      {/* 2. Login Page */}
      {route === 'login' && <LoginPage view={view} />}

      {/* Forgot Password Page */}
      {route === 'forgot-password' && <ForgotPasswordPage view={view} />}

      {/* Reset Password Submission Page */}
      {route === 'reset-password' && <ResetPasswordPage view={view} />}

      {/* 3. Signup Page */}
      {route === 'signup' && <SignupPage view={view} />}

      {/* 4. Main Dashboard View */}
      {route === 'dashboard' && user && <DashboardPage view={view} />}

      {/* 5. Billing Tiers Overview Page */}
      {route === 'billing' && user && <BillingPage view={view} />}

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
      {route === 'admin' && user && user.is_admin && <AdminPage view={view} />}
    </div>
  );
}
