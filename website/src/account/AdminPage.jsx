// Administrator panel: users, plans, rate limits, webhooks and caches.
// Each page receives the account state and actions that App owns as `view`.

export default function AdminPage({ view }) {
  const {
    adminSubTab,
    adminUsers,
    authError,
    cacheRateLimits,
    cacheVocabKeys,
    editPlanActive,
    editPlanTier,
    editRateLimit,
    editingUser,
    email,
    fetchAdminUsers,
    fetchCacheData,
    fetchWebhookLogs,
    formatSeconds,
    handleInvalidateVocabCache,
    handleOpenEditUser,
    handleResetRateLimitCache,
    handleSaveEditUser,
    loading,
    logs,
    name,
    setAdminSubTab,
    setEditPlanActive,
    setEditPlanTier,
    setEditRateLimit,
    setEditingUser,
    successMessage,
    user,
    webhookLogs,
  } = view;
  return (
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
                  <th>Payment / Invoice</th>
                  <th>User ID</th>
                  <th>Sub Status</th>
                  <th>Result</th>
                </tr>
              </thead>
              <tbody>
                {webhookLogs.length === 0 ? (
                  <tr>
                    <td colSpan="8" style={{ textAlign: 'center', opacity: 0.5, padding: '24px' }}>No webhook events received yet.</td>
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
                    <td style={{ fontSize: '11px', opacity: 0.7 }}>
                      {log.payment_id || log.invoice_id ? (
                        <>
                          {log.payment_id && <div>pay: {log.payment_id}</div>}
                          {log.invoice_id && <div>inv: {log.invoice_id}</div>}
                        </>
                      ) : '—'}
                    </td>
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
              <h3 className="card-title" style={{ margin: 0, display: 'flex', alignItems: 'center', gap: '8px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" /><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" /></svg>
                Active User Vocabulary Cache
              </h3>
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
  );
}
