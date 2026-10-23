// Account dashboard: usage, languages, vocabulary, meetings and profile.
// Each page receives the account state and actions that App owns as `view`.
import CommandUsage from '../components/CommandUsage';
import TurnstileWidget from '../components/TurnstileWidget';
import { SUPPORTED_LANGUAGES, getLanguageName } from '../utils/languages';

export default function DashboardPage({ view }) {
  const {
    activeMeeting,
    activeSubTab,
    authError,
    deleteConfirmText,
    deletingAccount,
    email,
    fetchProfileSummary,
    formatDate,
    formatSeconds,
    formatUsageParts,
    handleAddVocab,
    handleDeleteAccount,
    handleDeleteMeeting,
    handleDeleteVocab,
    handleSendPasswordReset,
    handleToggleLanguage,
    loading,
    logs,
    meetings,
    name,
    newVocabTerm,
    password,
    profileSummary,
    resetEmailError,
    resetEmailSuccess,
    resetTurnstile,
    savingLangs,
    sendingResetEmail,
    setActiveMeeting,
    setActiveSubTab,
    setAuthError,
    setDeleteConfirmText,
    setNewVocabTerm,
    setRoute,
    setShowCancelModal,
    setShowDeleteModal,
    setTurnstileToken,
    showDeleteModal,
    successMessage,
    turnstileToken,
    user,
    userLanguages,
    vocabTerms,
  } = view;
  return (
    <main className="dashboard-main">
      <div className="welcome-section">
        <h1 className="welcome-title">Welcome back, {user.name.split(' ')[0]}</h1>
        <p>Here is your current SpeekTo.Me usage and transcription analytics.</p>
        {user.notification && user.notification.trim() !== '' && (
          <div className="dashboard-announcement-card">
            <span className="dashboard-announcement-text">{user.notification.trim()}</span>
          </div>
        )}
      </div>

      {/* Sub-tab Navigation */}
      <div className="tab-navigation">
        <button
          onClick={() => { setActiveSubTab('stats'); setActiveMeeting(null); }}
          className={`btn ${activeSubTab === 'stats' ? 'btn-primary' : ''}`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="20" x2="18" y2="10" /><line x1="12" y1="20" x2="12" y2="4" /><line x1="6" y1="20" x2="6" y2="14" /></svg> Stats & Logs
        </button>
        <button
          onClick={() => { setActiveSubTab('vocab'); setActiveMeeting(null); }}
          className={`btn ${activeSubTab === 'vocab' ? 'btn-primary' : ''}`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" /><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" /></svg> Custom Vocabulary
        </button>
        <button
          onClick={() => { setActiveSubTab('languages'); setActiveMeeting(null); }}
          className={`btn ${activeSubTab === 'languages' ? 'btn-primary' : ''}`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><line x1="2" y1="12" x2="22" y2="12" /><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" /></svg> Languages ({userLanguages.length}/5)
        </button>
        {user.plan_active && (
          <button
            onClick={() => { setActiveSubTab('meetings'); }}
            className={`btn ${activeSubTab === 'meetings' ? 'btn-primary' : ''}`}
            style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
          >
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /><polyline points="10 9 9 9 8 9" /></svg> Meeting Notes
            <span className="beta-badge" title="This feature is in beta and some bugs are expected.">BETA</span>
          </button>
        )}
        <button
          onClick={() => { setActiveSubTab('profile'); setActiveMeeting(null); fetchProfileSummary(); }}
          className={`btn ${activeSubTab === 'profile' ? 'btn-primary' : ''}`}
          style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" /><circle cx="12" cy="7" r="4" /></svg> Profile
        </button>
      </div>

      {activeSubTab === 'stats' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          <CommandUsage usage={user.command_usage} />
          {/* Top Row: Usage Analytics & Plan Tier (Side by Side) */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '24px' }}>
            <div className="card" style={{ marginBottom: 0 }}>
              <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="20" x2="18" y2="10" /><line x1="12" y1="20" x2="12" y2="4" /><line x1="6" y1="20" x2="6" y2="14" /></svg> Usage Analytics
              </h3>

              <div className="usage-summary">
                <div className="usage-stats">
                  <span>Usage Limit ({user.plan_tier.toUpperCase()})</span>
                  {(() => {
                    const isPaid = Boolean(user.plan_active && user.plan_tier === 'pro');
                    const usage = formatUsageParts(user.duration_seconds, user.monthly_limit_seconds || (isPaid ? 180000 : 3600), isPaid);
                    return (
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', paddingBottom:'2px' }}>
                        {usage.usedStr} / {usage.totalStr}
                        {usage.isUnlimited && (
                          <span
                            className="fup-tooltip-badge"
                            title={usage.fupTooltip}
                            aria-label={usage.fupTooltip}
                          >
                            <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
                          </span>
                        )}
                      </span>
                    );
                  })()}
                </div>

                <div className="progress-bar-container">
                  <div
                    className="progress-bar-fill"
                    style={{
                      width: `${Math.min(
                        (user.duration_seconds / (user.monthly_limit_seconds || 3600)) * 100,
                        100
                      )}%`
                    }}
                  ></div>
                </div>

                <p style={{ fontSize: '13px', color: 'rgba(26, 26, 26, 0.5)', marginTop: '8px' }}>
                  {user.plan_tier === 'free'
                    ? 'Upgrade to SpeekTo.Me Pro to get unlimited dictation and access to Advanced AI Enhancement.'
                    : 'Your Pro plan subscription is active and will renew monthly.'}
                </p>
              </div>
            </div>

            <div className="card card-accent" style={{ marginBottom: 0 }}>
              <h3 className="card-title" style={{ display: 'flex', alignItems: 'center' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" style={{ marginRight: '6px' }}><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2" /></svg>
                Plan Tier: {user.plan_tier.toUpperCase()}
              </h3>
              <p style={{ fontSize: '14.5px', marginBottom: '20px', color: 'rgba(36, 10, 18, 0.7)' }}>
                {user.plan_active
                  ? `Active since ${formatDate(user.plan_activated_at)}`
                  : 'No active paid subscription.'}
              </p>
              {!user.plan_active ? (
                <button onClick={() => setRoute('billing')} className="btn btn-primary" style={{ width: '100%', justifyContent: 'center' }}>
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={{ marginRight: '6px', verticalAlign: 'middle', display: 'inline-block' }}><path d="M4.5 16.5c-1.5 1.25-2.5 3.5-2.5 3.5s2.25-1 3.5-2.5M12 2C8 2 5 5 5 9c0 1.66.84 3.5 1.5 4.5L12 22l5.5-8.5c.66-1 1.5-2.84 1.5-4.5 0-4-3-7-7-7z" /><path d="M12 6a3 3 0 1 0 0 6 3 3 0 0 0 0-6z" /></svg>
                  Upgrade to Pro ($2.00/mo)
                </button>
              ) : (
                <div className="card-actions">
                  <button onClick={() => setRoute('billing')} className="btn">
                    Manage Billing Details
                  </button>
                  <button
                    onClick={() => setShowCancelModal(true)}
                    className="btn btn-danger"
                    disabled={loading}
                  >
                    Cancel Subscription
                  </button>
                </div>
              )}
            </div>
          </div>

          {/* Bottom Row: Full Width Recent Transcriptions Table */}
          <div className="card" style={{ width: '100%' }}>
            <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" /><path d="M19 10v2a7 7 0 0 1-14 0v-2" /><line x1="12" y1="19" x2="12" y2="23" /><line x1="8" y1="23" x2="16" y2="23" /></svg> Recent Transcriptions
            </h3>
            {logs.length === 0 ? (
              <p style={{ color: 'rgba(26, 26, 26, 0.5)', padding: '20px 0', textAlign: 'center' }}>
                No recent dictations found. Open SpeekTo.Me on your desktop to make your first transcription!
              </p>
            ) : (
              <div className="logs-table-container">
                <table className="logs-table">
                  <thead>
                    <tr>
                      <th>Date & Time</th>
                      <th>Audio</th>
                      <th>Processing</th>
                      <th>Final Transcription</th>
                      <th className="col-status">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {logs.map((log, idx) => {
                      const textOutput = log.final_output || log.input_transcript;
                      return (
                        <tr key={idx}>
                          <td style={{ whiteSpace: 'nowrap', fontWeight: '500' }}>
                            {formatDate(log.timestamp)}
                          </td>
                          <td style={{ whiteSpace: 'nowrap' }}>
                            {formatSeconds(log.audio_duration)}
                          </td>
                          <td>
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                              <span style={{ fontWeight: '600', fontSize: '13px' }}>
                                {log.total_time ? `${log.total_time.toFixed(2)}s` : 'N/A'}
                              </span>
                              <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap' }}>
                                {Boolean(log.enhance_enabled) && (
                                  <span className="badge-pill badge-pill-accent">Enhanced</span>
                                )}
                                {log.transcription_provider && (
                                  <span className="badge-pill">{log.transcription_provider}</span>
                                )}
                              </div>
                            </div>
                          </td>
                          <td className="transcription-cell">
                            {textOutput ? (
                              <div className="transcription-text-box">
                                <div className="transcription-scroll-area">
                                  {textOutput}
                                </div>
                                <button
                                  className="btn btn-primary btn-sm transcription-copy-btn"
                                  title="Copy transcription"
                                  onClick={(e) => {
                                    navigator.clipboard.writeText(textOutput);
                                    const btn = e.currentTarget;
                                    const originalHTML = btn.innerHTML;
                                    btn.innerHTML = '✓ Copied!';
                                    btn.style.background = '#d1fae5';
                                    setTimeout(() => {
                                      btn.innerHTML = originalHTML;
                                      btn.style.background = '';
                                    }, 1500);
                                  }}
                                > Copy</button>
                              </div>
                            ) : (
                              <span style={{ color: 'rgba(36, 10, 18, 0.4)', fontStyle: 'italic', fontSize: '13px' }}>
                                No transcript
                              </span>
                            )}
                          </td>
                          <td className="col-status">
                            <span className={`badge-status ${log.status === 'success' ? 'badge-success' : 'badge-failed'}`}>
                              {log.status === 'success' ? 'Success' : 'Failed'}
                            </span>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      )}

      {activeSubTab === 'vocab' && (
        <div className="card">
          <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" /><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" /></svg>
            Custom Vocabulary Terms
          </h3>
          <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '20px' }}>
            Add specialized terms, abbreviations, acronyms, or names that you want the dictation pipeline to recognize and spell correctly.
          </p>

          {authError && <div className="auth-error" style={{ marginBottom: '20px' }}>{authError}</div>}

          <p className="setting-hint">
            {Math.min(vocabTerms.length, user.plan_active && user.plan_tier === 'pro' ? 1000 : 20)} of {user.plan_active && user.plan_tier === 'pro' ? '1,000' : '20'} terms active on your plan.
            {vocabTerms.length > (user.plan_active && user.plan_tier === 'pro' ? 1000 : 20) && ' All saved terms are preserved; only the first terms alphabetically within your plan limit are active.'}
          </p>

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

      {activeSubTab === 'languages' && (
        <div className="card">
          <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><line x1="2" y1="12" x2="22" y2="12" /><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10z" /></svg>  Spoken Language Preferences
          </h3>
          <p style={{ fontSize: '14px', color: 'var(--color-text-dark)', opacity: 0.75, marginBottom: '20px' }}>
            Select up to 5 languages you speak during voice dictations. Selecting a single primary language locks Whisper to maximum transcription accuracy. Selecting multiple languages informs Whisper's multi-lingual model without translating.
          </p>

          {authError && <div className="auth-error" style={{ marginBottom: '16px' }}>{authError}</div>}
          {successMessage && <div className="auth-success">{successMessage}</div>}

          <div className="language-selected">
            <span className="field-label">
              Currently selected ({userLanguages.length}/5)
            </span>
            <div className="language-selected-list">
              {userLanguages.map(code => (
                <button
                  key={code}
                  type="button"
                  onClick={() => handleToggleLanguage(code)}
                  className="btn btn-primary language-chip is-selected is-selected-removable"
                  title="Click to remove"
                  disabled={savingLangs}
                >
                  {getLanguageName(code)} ×
                </button>
              ))}
            </div>
          </div>

          <div className="language-all">
            <span className="field-label">
              All 99 supported languages (click to toggle)
            </span>
            <div className="language-grid">
              {SUPPORTED_LANGUAGES.map(lang => {
                const selected = userLanguages.includes(lang.code);
                return (
                  <button
                    key={lang.code}
                    type="button"
                    onClick={() => handleToggleLanguage(lang.code)}
                    disabled={savingLangs}
                    className={`btn language-chip ${selected ? 'is-selected' : ''}`}
                    aria-pressed={selected}
                  >
                    {lang.name}
                  </button>
                );
              })}
            </div>
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
                    ← Back to Meetings
                  </button>
                  <h2 className="meeting-detail-title">{activeMeeting.title}</h2>
                  <span className="meeting-card-date">Recorded on {formatDate(activeMeeting.timestamp)}</span>
                </div>
                <div style={{ display: 'flex', gap: '12px', alignItems: 'center' }}>
                  <span className="meeting-meta-badge" style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
                    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" /></svg>
                    {formatSeconds(activeMeeting.duration)}
                  </span>
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
                {/* Left Column: AI Summary & Action Items */}
                <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
                  <div className="meeting-panel-card">
                    <h3 className="meeting-section-title">Summary & Takeaways</h3>
                    <div className="meeting-text-box" style={{ maxHeight: '280px' }}>
                      {activeMeeting.summary || "No summary generated for this meeting."}
                    </div>
                  </div>

                  <div className="meeting-panel-card">
                    <h3 className="meeting-section-title">Action Items</h3>
                    <div className="meeting-text-box" style={{ maxHeight: '240px' }}>
                      {activeMeeting.action_items ? (
                        <ul className="action-items-list">
                          {activeMeeting.action_items.split('\n').filter(item => item.trim() !== '').map((item, idx) => (
                            <li key={idx}>{item.replace(/^-\s*\[\s*[x ]\s*\]\s*/, '').replace(/^-\s*/, '')}</li>
                          ))}
                        </ul>
                      ) : (
                        <span style={{ color: 'rgba(26,26,26,0.5)' }}>No action items detected.</span>
                      )}
                    </div>
                  </div>
                </div>

                {/* Right Column: Unified Meeting Chat Dialogue */}
                <div className="meeting-panel-card">
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '14px' }}>
                    <h3 className="meeting-section-title" style={{ margin: 0 }}>Conversation Dialogue</h3>
                    <span style={{ fontSize: '12px', color: 'rgba(26,26,26,0.5)', fontWeight: 500 }}>
                      Speaker Diarization
                    </span>
                  </div>

                  <div className="meeting-text-box meeting-chat-stream" style={{ maxHeight: '600px', minHeight: '400px', overflowY: 'auto' }}>
                    {(() => {
                      let segments = [];
                      try {
                        segments = JSON.parse(activeMeeting.segments_json);
                      } catch (e) { }

                      const userName = (user && user.name) ? user.name.toLowerCase() : '';

                      // 1. If we have segmented diarization, group continuous turns with start timestamp
                      if (segments && Array.isArray(segments) && segments.length > 0) {
                        const groupedTurns = [];
                        let currentTurn = null;

                        segments.forEach((seg) => {
                          const spk = seg.speaker || 'Speaker';
                          if (!currentTurn || currentTurn.speaker !== spk) {
                            currentTurn = {
                              speaker: spk,
                              start: seg.start || 0,
                              end: seg.end || 0,
                              text: seg.text || ''
                            };
                            groupedTurns.push(currentTurn);
                          } else {
                            currentTurn.text += ' ' + (seg.text || '');
                            currentTurn.end = seg.end || currentTurn.end;
                          }
                        });

                        return (
                          <div className="segments-list">
                            {groupedTurns.map((turn, idx) => {
                              const speakerName = turn.speaker || 'Speaker';
                              const cleanLowerSpeaker = speakerName.toLowerCase().replace(/[\[\]]/g, '').trim();
                              const isRemote = cleanLowerSpeaker.includes('remote') || cleanLowerSpeaker.includes('system') || cleanLowerSpeaker.includes('participant') || cleanLowerSpeaker.includes('caller');
                              const isMe = !isRemote && (cleanLowerSpeaker === 'you' || cleanLowerSpeaker === 'me' || (userName && cleanLowerSpeaker.includes(userName)) || !cleanLowerSpeaker.includes('speaker 2'));
                              const initials = speakerName.replace(/[\[\]]/g, '').split(' ').filter(Boolean).map(n => n[0]).join('').slice(0, 2).toUpperCase() || 'SP';

                              return (
                                <div key={idx} className={`segment-item ${isRemote ? 'is-remote' : 'is-me'}`}>
                                  <div className="segment-header">
                                    <div className={`segment-avatar ${isRemote ? 'is-remote-avatar' : ''}`}>
                                      {initials}
                                    </div>
                                    <span className="segment-speaker-name">{speakerName}</span>
                                    <span className="segment-time">
                                      {formatSeconds(turn.start || 0)}
                                    </span>
                                  </div>
                                  <div className="segment-text">{turn.text}</div>
                                </div>
                              );
                            })}
                          </div>
                        );
                      }

                      // 2. Fallback: Parse transcript text if segments_json is not present
                      const rawTranscript = activeMeeting.transcript || "";
                      if (!rawTranscript.trim()) {
                        return <p style={{ color: 'rgba(26,26,26,0.5)', textAlign: 'center', padding: '40px 0' }}>No transcript found.</p>;
                      }

                      const lines = rawTranscript.split('\n').map(l => l.trim()).filter(Boolean);
                      const dialogueTurns = [];
                      const lineRegex = /^([^:\n]{1,50}):\s*(.+)$/;

                      lines.forEach((line) => {
                        const match = line.match(lineRegex);
                        if (match) {
                          dialogueTurns.push({
                            speaker: match[1].trim(),
                            text: match[2].trim()
                          });
                        } else {
                          if (dialogueTurns.length > 0) {
                            dialogueTurns[dialogueTurns.length - 1].text += ' ' + line;
                          } else {
                            dialogueTurns.push({
                              speaker: 'Speaker',
                              text: line
                            });
                          }
                        }
                      });

                      return (
                        <div className="segments-list">
                          {dialogueTurns.map((turn, idx) => {
                            const speakerName = turn.speaker || 'Speaker';
                            const cleanLowerSpeaker = speakerName.toLowerCase().replace(/[\[\]]/g, '').trim();
                            const isRemote = cleanLowerSpeaker.includes('remote') || cleanLowerSpeaker.includes('system') || cleanLowerSpeaker.includes('participant') || cleanLowerSpeaker.includes('caller');
                            const isMe = !isRemote && (cleanLowerSpeaker === 'you' || cleanLowerSpeaker === 'me' || (userName && cleanLowerSpeaker.includes(userName)) || !cleanLowerSpeaker.includes('speaker 2'));
                            const initials = speakerName.replace(/[\[\]]/g, '').split(' ').filter(Boolean).map(n => n[0]).join('').slice(0, 2).toUpperCase() || 'SP';

                            return (
                              <div key={idx} className={`segment-item ${isRemote ? 'is-remote' : 'is-me'}`}>
                                <div className="segment-header">
                                  <div className={`segment-avatar ${isRemote ? 'is-remote-avatar' : ''}`}>
                                    {initials}
                                  </div>
                                  <span className="segment-speaker-name">{speakerName}</span>
                                </div>
                                <div className="segment-text">{turn.text}</div>
                              </div>
                            );
                          })}
                        </div>
                      );
                    })()}
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <div className="card">
              <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /><polyline points="10 9 9 9 8 9" /></svg>
                Recorded Meetings
                <span className="beta-badge" title="This feature is in beta and some bugs are expected.">BETA</span>
              </h3>
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
                    >
                      <div className="meeting-card-top">
                        <div className="meeting-card-icon-wrap">
                          <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></svg>
                        </div>
                        <span className="meeting-card-date">{formatDate(meeting.timestamp)}</span>
                      </div>
                      
                      <h4 className="meeting-card-title">{meeting.title}</h4>
                      
                      <p className="meeting-card-summary">
                        {meeting.summary || "Click to view full transcript and summaries."}
                      </p>

                      <div className="meeting-card-bottom">
                        <div className="meeting-card-duration">
                          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" /></svg>
                          <span>{formatSeconds(meeting.duration)}</span>
                        </div>
                        <span className="meeting-card-arrow">
                          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><line x1="5" y1="12" x2="19" y2="12" /><polyline points="12 5 19 12 12 19" /></svg>
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* Profile SubTab */}
      {activeSubTab === 'profile' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {/* 1. Account Details Card */}
          <div className="card" style={{ marginBottom: 0 }}>
            <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" /><circle cx="12" cy="7" r="4" /></svg>
              Account Overview
            </h3>
            <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '16px' }}>
              Basic account credentials and subscription status associated with your SpeekTo.Me account.
            </p>

            <div className="profile-info-grid">
              <div className="profile-info-item">
                <span className="profile-info-label">Full Name</span>
                <span className="profile-info-value">{user.name}</span>
              </div>
              <div className="profile-info-item">
                <span className="profile-info-label">Email Address</span>
                <span className="profile-info-value">{user.email}</span>
              </div>
              <div className="profile-info-item">
                <span className="profile-info-label">Plan Tier</span>
                <span className="profile-info-value" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <span className={`badge-status ${user.plan_active ? 'badge-success' : 'badge-failed'}`}>
                    {user.plan_tier.toUpperCase()} {user.plan_active ? '(ACTIVE)' : '(FREE)'}
                  </span>
                </span>
              </div>
              <div className="profile-info-item">
                <span className="profile-info-label">Member Since</span>
                <span className="profile-info-value">{formatDate(profileSummary?.created_at || user.plan_activated_at)}</span>
              </div>
              <div className="profile-info-item">
                <span className="profile-info-label">Authentication Method</span>
                <span className="profile-info-value">
                  {profileSummary?.has_google_auth && profileSummary?.has_password
                    ? 'Google OAuth + Password'
                    : profileSummary?.has_google_auth
                    ? 'Google OAuth'
                    : 'Email & Password'}
                </span>
              </div>
              <div className="profile-info-item">
                <span className="profile-info-label">Spoken Languages ({userLanguages.length}/5)</span>
                <span className="profile-info-value" style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginTop: '2px' }}>
                  {userLanguages.map(code => (
                    <span key={code} className="badge-pill">{getLanguageName(code)}</span>
                  ))}
                </span>
              </div>
            </div>
          </div>

          {/* 2. Summarized Activity & Usage Dashboard */}
          <div className="card" style={{ marginBottom: 0 }}>
            <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="18" y1="20" x2="18" y2="10" /><line x1="12" y1="20" x2="12" y2="4" /><line x1="6" y1="20" x2="6" y2="14" /></svg>
              Usage & Activity Summary
            </h3>
            <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '20px' }}>
              Consolidated transcription, meeting recording, and AI usage statistics inferred from your account history.
            </p>

            <div className="profile-stats-grid">
              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z" /><path d="M19 10v2a7 7 0 0 1-14 0v-2" /><line x1="12" y1="19" x2="12" y2="23" /><line x1="8" y1="23" x2="16" y2="23" /></svg>
                  Dictations
                </span>
                <span className="stat-card-value">{profileSummary?.total_transcriptions ?? logs.length}</span>
                <span className="stat-card-sub">Total voice inputs processed</span>
              </div>

              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" /></svg>
                  Total Audio Dictated
                </span>
                <span className="stat-card-value">{formatSeconds(profileSummary?.total_audio_duration_seconds ?? 0)}</span>
                <span className="stat-card-sub">All-time audio processed</span>
              </div>

              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></svg>
                  Words Transcribed
                </span>
                <span className="stat-card-value">{(profileSummary?.total_words_transcribed ?? 0).toLocaleString()}</span>
                <span className="stat-card-sub">Estimated words delivered</span>
              </div>

              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" /><circle cx="9" cy="7" r="4" /><path d="M23 21v-2a4 4 0 0 0-3-3.87" /><path d="M16 3.13a4 4 0 0 1 0 7.75" /></svg>
                  Meetings Recorded
                </span>
                <span className="stat-card-value">{profileSummary?.total_meetings ?? meetings.length}</span>
                <span className="stat-card-sub">{formatSeconds(profileSummary?.total_meeting_duration_seconds ?? 0)} total duration</span>
              </div>

              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" /><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" /></svg>
                  Custom Vocabulary
                </span>
                <span className="stat-card-value">{profileSummary?.total_vocabulary_terms ?? vocabTerms.length}</span>
                <span className="stat-card-sub">Domain-specific custom terms</span>
              </div>

              <div className="stat-card">
                <span className="stat-card-title">
                  <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" /></svg>
                  AI Enhancements
                </span>
                <span className="stat-card-value">{profileSummary?.total_enhancements ?? 0}</span>
                <span className="stat-card-sub">AI structured transcriptions</span>
              </div>
            </div>

            {/* Current Monthly Quota meter */}
            <div className="quota-meter">
              <div className="usage-stats">
                <span>Current Month Usage Quota</span>
                {(() => {
                  const isPaid = Boolean(user.plan_active && user.plan_tier === 'pro');
                  const usage = formatUsageParts(user.duration_seconds, user.monthly_limit_seconds || (isPaid ? 180000 : 3600), isPaid);
                  return (
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
                      {usage.usedStr} / {usage.totalStr}
                      {usage.isUnlimited && (
                        <span
                          className="fup-tooltip-badge"
                          title={usage.fupTooltip}
                          aria-label={usage.fupTooltip}
                        >
                          <svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
                        </span>
                      )}
                    </span>
                  );
                })()}
              </div>
              <div className="progress-bar-container">
                <div
                  className="progress-bar-fill"
                  style={{
                    width: `${Math.min(
                      (user.duration_seconds / (user.monthly_limit_seconds || 3600)) * 100,
                      100
                    )}%`
                  }}
                ></div>
              </div>
            </div>
          </div>

          {/* 3. Password & Security Card */}
          <div className="card" style={{ marginBottom: 0 }}>
            <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2" /><path d="M7 11V7a5 5 0 0 1 10 0v4" /></svg>
              Password & Security
            </h3>
            <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.6)', marginBottom: '16px' }}>
              Send a secure password reset link to your email address (<strong>{user.email}</strong>). Complete the verification below to request the link.
            </p>

            {resetEmailSuccess && (
              <div className="notice-success">
                {resetEmailSuccess}
              </div>
            )}

            {resetEmailError && (
              <div className="notice-error">
                {resetEmailError}
              </div>
            )}

            <form onSubmit={handleSendPasswordReset} style={{ maxWidth: '440px' }}>
              <TurnstileWidget
                onVerify={setTurnstileToken}
                resetTrigger={resetTurnstile}
              />
              <button
                type="submit"
                className="btn btn-primary"
                disabled={sendingResetEmail || !turnstileToken}
                style={{ width: '100%', justifyContent: 'center' }}
              >
                {sendingResetEmail ? <div className="spinner"></div> : 'Send Password Reset Email'}
              </button>
            </form>
          </div>

          {/* 4. Danger Zone Card */}
          <div className="card danger-zone-card" style={{ marginBottom: 0 }}>
            <h3 className="card-title" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>
              Danger Zone
            </h3>
            <p style={{ fontSize: '14px', color: 'rgba(26, 26, 26, 0.75)', marginBottom: '18px', lineHeight: '1.5' }}>
              Permanently delete your SpeekTo.Me account, recorded meeting notes, custom vocabulary terms, and all associated usage history. This action is irreversible.
            </p>

            <button
              onClick={() => { setShowDeleteModal(true); setDeleteConfirmText(''); setAuthError(''); }}
              className="btn btn-danger"
              style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}
            >
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" /></svg>
              Delete Account
            </button>
          </div>
        </div>
      )}

      {/* 2-Step Delete Account Modal */}
      {showDeleteModal && (
        <div className="modal-overlay">
          <div className="modal-content modal-danger">
            <h3 className="modal-title" style={{ color: '#991b1b', display: 'flex', alignItems: 'center', gap: '8px' }}>
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>
              Permanently Delete Account
            </h3>

            {authError && <div className="auth-error" style={{ marginBottom: '14px' }}>{authError}</div>}

            <p className="modal-desc">
              <strong>Confirmation 1 of 2:</strong> You are about to permanently delete your account (<strong>{user.email}</strong>). All meetings, audio recordings, custom vocabulary, and history will be wiped out immediately.
            </p>

            <div className="modal-confirm">
              <label style={{ display: 'block', fontSize: '12.5px', fontWeight: '700', marginBottom: '8px', color: '#991b1b' }}>
                Confirmation 2 of 2: To proceed, type DELETE in capital letters below:
              </label>
              <input
                type="text"
                value={deleteConfirmText}
                onChange={(e) => setDeleteConfirmText(e.target.value)}
                placeholder="DELETE"
                className="auth-input"
                style={{ width: '100%', boxSizing: 'border-box' }}
                autoFocus
              />
            </div>

            <div className="modal-actions">
              <button
                onClick={() => { setShowDeleteModal(false); setDeleteConfirmText(''); }}
                className="btn"
                disabled={deletingAccount}
              >
                Cancel
              </button>
              <button
                onClick={handleDeleteAccount}
                className="btn btn-danger"
                disabled={deleteConfirmText.trim() !== 'DELETE' || deletingAccount}
              >
                {deletingAccount ? <div className="spinner"></div> : 'Permanently Delete My Account'}
              </button>
            </div>
          </div>
        </div>
      )}
    </main>
  );
}
