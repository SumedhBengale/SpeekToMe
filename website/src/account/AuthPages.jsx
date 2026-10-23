// Sign-in, sign-up, two-factor and password-reset pages of the account area.
// Each page receives the account state and actions that App owns as `view`.
import TurnstileWidget from '../components/TurnstileWidget';

export function TwoFactorPage({ view }) {
  const {
    authError,
    handle2faVerify,
    loading,
    setAuthError,
    setRoute,
    setTempToken,
    setTotpCode,
    totpCode,
  } = view;
  return (
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
  );
}

export function LoginPage({ view }) {
  const {
    authError,
    email,
    handleGoogleLogin,
    handleLogin,
    loading,
    name,
    password,
    resetTurnstile,
    setAuthError,
    setEmail,
    setForgotEmail,
    setPassword,
    setResetEmailError,
    setResetEmailSuccess,
    setRoute,
    setTurnstileToken,
    successMessage,
    turnstileToken,
  } = view;
  return (
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

        <button onClick={handleGoogleLogin} className="btn google-auth-btn" style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: '8px' }}>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M12.24 10.285V13.4h6.887c-.58 2.716-2.85 4.67-5.918 4.67-3.568 0-6.46-2.892-6.46-6.46s2.892-6.46 6.46-6.46c1.614 0 3.08.595 4.22 1.57l2.4-2.4C17.96 2.68 15.26 1.7 12.24 1.7 6.58 1.7 2 6.28 2 11.94s4.58 10.24 10.24 10.24c5.93 0 9.87-4.17 9.87-10.04 0-.67-.07-1.33-.19-1.855H12.24z" /></svg> Sign in with Google
        </button>

        <div className="auth-divider-advanced">
          <div className="auth-divider-line"></div>
          <span className="auth-divider-badge">or continue with email</span>
          <div className="auth-divider-line"></div>
        </div>


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
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
              <label style={{ margin: 0 }}>Password</label>
              <button
                type="button"
                onClick={() => { setRoute('forgot-password'); setAuthError(''); setResetEmailSuccess(''); setResetEmailError(''); setForgotEmail(email); }}
                className="auth-toggle-link"
                style={{ fontSize: '12px' }}
              >
                Forgot password?
              </button>
            </div>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              className="auth-input"
              required
            />
          </div>
          <TurnstileWidget
            onVerify={setTurnstileToken}
            resetTrigger={resetTurnstile}
          />
          <button type="submit" className="btn btn-primary auth-submit-btn" disabled={loading || !turnstileToken}>
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
  );
}

export function ForgotPasswordPage({ view }) {
  const {
    email,
    forgotEmail,
    handleSendPasswordReset,
    name,
    password,
    resetEmailError,
    resetEmailSuccess,
    resetTurnstile,
    sendingResetEmail,
    setAuthError,
    setForgotEmail,
    setResetEmailError,
    setResetEmailSuccess,
    setRoute,
    setTurnstileToken,
    turnstileToken,
  } = view;
  return (
    <main className="auth-page">
      <div className="auth-box">
        <div className="auth-header">
          <h2>Reset Password</h2>
          <p>Enter your email address to receive a secure password reset link.</p>
        </div>

        {resetEmailSuccess && (
          <div
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
            {resetEmailSuccess}
          </div>
        )}
        {resetEmailError && <div className="auth-error">{resetEmailError}</div>}

        <form onSubmit={handleSendPasswordReset}>
          <div className="auth-form-control">
            <label>Email Address</label>
            <input
              type="email"
              value={forgotEmail}
              onChange={(e) => setForgotEmail(e.target.value)}
              placeholder="name@example.com"
              className="auth-input"
              required
            />
          </div>
          <TurnstileWidget
            onVerify={setTurnstileToken}
            resetTrigger={resetTurnstile}
          />
          <button type="submit" className="btn btn-primary auth-submit-btn" disabled={sendingResetEmail || !turnstileToken}>
            {sendingResetEmail ? <div className="spinner"></div> : 'Send Password Reset Link'}
          </button>
        </form>

        <div className="auth-toggle">
          <button onClick={() => { setRoute('login'); setResetEmailError(''); setResetEmailSuccess(''); setAuthError(''); }} className="auth-toggle-link">
            Back to Sign In
          </button>
        </div>
      </div>
    </main>
  );
}

export function ResetPasswordPage({ view }) {
  const {
    authError,
    confirmNewPassword,
    handleResetPasswordSubmit,
    newPassword,
    password,
    resetTurnstile,
    resettingPassword,
    setAuthError,
    setConfirmNewPassword,
    setNewPassword,
    setRoute,
    setTurnstileToken,
    turnstileToken,
  } = view;
  return (
    <main className="auth-page">
      <div className="auth-box">
        <div className="auth-header">
          <h2>Choose New Password</h2>
          <p>Set a new password for your SpeekTo.Me account.</p>
        </div>

        {authError && <div className="auth-error">{authError}</div>}

        <form onSubmit={handleResetPasswordSubmit}>
          <div className="auth-form-control">
            <label>New Password</label>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              placeholder="At least 8 characters (letters & numbers)"
              className="auth-input"
              required
            />
          </div>
          <div className="auth-form-control">
            <label>Confirm New Password</label>
            <input
              type="password"
              value={confirmNewPassword}
              onChange={(e) => setConfirmNewPassword(e.target.value)}
              placeholder="Re-enter new password"
              className="auth-input"
              required
            />
          </div>
          <TurnstileWidget
            onVerify={setTurnstileToken}
            resetTrigger={resetTurnstile}
          />
          <button type="submit" className="btn btn-primary auth-submit-btn" disabled={resettingPassword || !turnstileToken}>
            {resettingPassword ? <div className="spinner"></div> : 'Update Password'}
          </button>
        </form>

        <div className="auth-toggle">
          <button onClick={() => { setRoute('login'); setAuthError(''); }} className="auth-toggle-link">
            Back to Sign In
          </button>
        </div>
      </div>
    </main>
  );
}

export function SignupPage({ view }) {
  const {
    authError,
    email,
    handleGoogleLogin,
    handleSignup,
    loading,
    name,
    password,
    resetTurnstile,
    setAuthError,
    setEmail,
    setName,
    setPassword,
    setRoute,
    setTurnstileToken,
    turnstileToken,
  } = view;
  return (
    <main className="auth-page">
      <div className="auth-box">
        <div className="auth-header">
          <h2>Create Account</h2>
          <p>Get started with SpeekTo.Me transcription services.</p>
        </div>

        {authError && <div className="auth-error">{authError}</div>}

        <button onClick={handleGoogleLogin} className="btn google-auth-btn" style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: '8px' }}>
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M12.24 10.285V13.4h6.887c-.58 2.716-2.85 4.67-5.918 4.67-3.568 0-6.46-2.892-6.46-6.46s2.892-6.46 6.46-6.46c1.614 0 3.08.595 4.22 1.57l2.4-2.4C17.96 2.68 15.26 1.7 12.24 1.7 6.58 1.7 2 6.28 2 11.94s4.58 10.24 10.24 10.24c5.93 0 9.87-4.17 9.87-10.04 0-.67-.07-1.33-.19-1.855H12.24z" /></svg> Sign up with Google
        </button>

        <div className="auth-divider-advanced">
          <div className="auth-divider-line"></div>
          <span className="auth-divider-badge">or create account with email</span>
          <div className="auth-divider-line"></div>
        </div>


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
          <TurnstileWidget
            onVerify={setTurnstileToken}
            resetTrigger={resetTurnstile}
          />
          <button type="submit" className="btn btn-primary auth-submit-btn" disabled={loading || !turnstileToken}>
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
  );
}
