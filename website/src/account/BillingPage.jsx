// Subscription management page.
// Each page receives the account state and actions that App owns as `view`.

export default function BillingPage({ view }) {
  const {
    email,
    handleUpgrade,
    loading,
    logs,
    setShowCancelModal,
    user,
  } = view;
  return (
    <main className="dashboard-main">
      <div className="welcome-section">
        <h1 className="welcome-title">Choose Your Subscription Plan</h1>
        <p>Get the dictation limits you need to be productive directly on your desktop.</p>
      </div>

      <div className="pricing-grid">
        {/* Free Plan */}
        <div className={`plan-card ${user.plan_tier === 'free' ? 'active' : ''}`}>
          <div className="plan-header">
            <span className="plan-badge plan-badge-free">Free Tier</span>
            <h3>SpeekTo.Me Starter</h3>
            <p>Perfect for testing out dictation and speed.</p>
            <div className="plan-price">$0<span>/forever</span></div>

            <ul className="plan-features" style={{ listStyle: 'none', padding: 0 }}>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>1 hour / month transcription limit</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Fast AI voice dictation</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Global keyboard shortcut trigger</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Standard latency queue</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Cross-platform desktop & mobile support (coming soon)</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Basic community support</span>
              </li>
            </ul>
          </div>

          <div className="plan-card-footer">
            {user.plan_tier === 'free' ? (
              <button className="btn btn-current-plan" disabled>
                ✓ Current Active Plan
              </button>
            ) : (
              <button
                onClick={() => setShowCancelModal(true)}
                className="btn btn-danger"
                disabled={loading}
              >
                Downgrade to Starter Plan
              </button>
            )}
          </div>
        </div>

        {/* Pro Plan */}
        <div className={`plan-card plan-card-pro ${user.plan_tier === 'pro' ? 'active' : ''}`}>
          <div className="plan-header">
            <span className="plan-badge">Most Popular</span>
            <h3>SpeekTo.Me Pro</h3>
            <p>For power users who dictate essays, logs, and emails.</p>
            <div className="plan-price">$2.00<span>/month</span></div>

            <ul className="plan-features" style={{ listStyle: 'none', padding: 0 }}>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span><strong>Unlimited AI Voice Transcription</strong></span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span><strong>Advanced AI Enhancement & Smart Formatting</strong></span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>High-priority transcription queues (Ultra-low latency)</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Custom AI system prompt instructions</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Cross-platform desktop & mobile support (coming soon)</span>
              </li>
              <li style={{ display: 'flex', alignItems: 'flex-start', gap: '10px', marginBottom: '12px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--color-success)', marginTop: '3px', flexShrink: 0 }}><polyline points="20 6 9 17 4 12" /></svg>
                <span>Priority 24/7 email & chat support</span>
              </li>
            </ul>
          </div>

          <div className="plan-card-footer">
            {user.plan_tier === 'pro' ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', width: '100%' }}>
                <button className="btn btn-current-plan" disabled>
                  ✓ Current Active Plan (Subscribed)
                </button>
                <button
                  onClick={() => setShowCancelModal(true)}
                  className="btn btn-danger btn-sm"
                  disabled={loading}
                >
                  Cancel Subscription
                </button>
              </div>
            ) : (
              <button onClick={handleUpgrade} className="btn btn-primary btn-upgrade-cta">
                💳 Upgrade to Pro ($2.00/mo)
              </button>
            )}
          </div>
        </div>
      </div>
    </main>
  );
}
