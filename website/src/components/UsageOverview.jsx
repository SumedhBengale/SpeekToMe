// One usage card: transcription time, Command Mode allowance and encrypted
// cloud storage. Pro transcription is unlimited under fair use, so it shows the
// time used rather than a bar against a limit nobody is meant to reach.
import React, { useEffect, useState } from 'react';
import { API_BASE } from '../utils/api';

function gigabytes(bytes) {
  const gb = (bytes || 0) / 1e9;
  return gb >= 10 ? gb.toFixed(0) : gb.toFixed(2);
}

export default function UsageOverview({ user, token, formatUsageParts }) {
  const [storage, setStorage] = useState(null);
  const [storageError, setStorageError] = useState(false);

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    fetch(`${API_BASE}/v1/storage`, { headers: { Authorization: `Bearer ${token}` } })
      .then(res => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
      .then(data => { if (!cancelled) setStorage(data); })
      .catch(() => { if (!cancelled) setStorageError(true); });
    return () => { cancelled = true; };
  }, [token]);

  const isPaid = Boolean(user.plan_active && user.plan_tier === 'pro');
  const transcription = formatUsageParts(user.duration_seconds, user.monthly_limit_seconds || (isPaid ? 180000 : 3600), isPaid);
  const transcriptionPercent = Math.min(100, ((user.duration_seconds || 0) / (user.monthly_limit_seconds || 3600)) * 100);

  const command = user.command_usage;
  const commandRemaining = Math.max(0, Math.min(100, command?.remaining_percent ?? 0));
  const commandUsed = 100 - commandRemaining;
  const commandExhausted = command && (command.input_remaining <= 0 || command.output_remaining <= 0);
  const commandReset = command?.resets_at ? new Date(command.resets_at).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '';

  const storageUsed = (storage?.used_bytes || 0) + (storage?.reserved_bytes || 0);
  const storagePercent = storage?.limit_bytes ? Math.min(100, (storageUsed / storage.limit_bytes) * 100) : 0;

  return (
    <section className="card usage-overview">
      <h3 className="card-title usage-overview-title">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><line x1="18" y1="20" x2="18" y2="10" /><line x1="12" y1="20" x2="12" y2="4" /><line x1="6" y1="20" x2="6" y2="14" /></svg>
        Usage this month
      </h3>

      <div className="usage-row">
        <div className="usage-row-head">
          <span className="usage-row-label">Transcription</span>
          <span className="usage-row-value">
            {transcription.usedStr} used
            {transcription.isUnlimited
              ? <span className="usage-unlimited" title={transcription.fupTooltip}>Unlimited</span>
              : <> of {transcription.totalStr}</>}
          </span>
        </div>
        {!transcription.isUnlimited && (
          <>
            <div className="progress-bar-container"><div className="progress-bar-fill" style={{ width: `${transcriptionPercent}%` }} /></div>
            <p className="usage-row-note">Upgrade to SpeekTo.Me Pro for unlimited dictation and meetings.</p>
          </>
        )}
      </div>

      <div className="usage-row">
        <div className="usage-row-head">
          <span className="usage-row-label">Command Mode</span>
          <span className="usage-row-value" role="status">
            {!command ? 'Unavailable' : !command.enabled ? 'Pro only' : `${commandUsed > 0 && commandUsed < 1 ? '<1' : Math.floor(commandUsed)}% used${commandExhausted ? ' · limit reached' : ''}`}
          </span>
        </div>
        {command?.enabled && (
          <>
            <div className="progress-bar-container"><div className="progress-bar-fill" style={{ width: `${commandUsed}%` }} /></div>
            <p className="usage-row-note">{command.renewal_pending ? 'Refreshes when your subscription renewal is confirmed.' : `Resets ${commandReset}`}</p>
          </>
        )}
      </div>

      <div className="usage-row">
        <div className="usage-row-head">
          <span className="usage-row-label">Cloud storage</span>
          <span className="usage-row-value">
            {storageError ? 'Unavailable'
              : !storage ? 'Loading…'
              : !storage.enabled ? 'Pro only'
              : `${gigabytes(storageUsed)} GB of ${gigabytes(storage.limit_bytes)} GB`}
          </span>
        </div>
        {storage?.enabled && (
          <>
            <div className="progress-bar-container"><div className="progress-bar-fill" style={{ width: `${storagePercent}%` }} /></div>
            <p className="usage-row-note">
              Encrypted meeting recordings.
              {storage.purge_after ? ` Scheduled for deletion on ${new Date(storage.purge_after * 1000).toLocaleDateString()} unless your Pro plan is renewed.` : ''}
            </p>
          </>
        )}
      </div>
    </section>
  );
}
