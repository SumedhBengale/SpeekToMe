import React from 'react';

export default function CommandUsage({ usage }) {
  const date = usage?.resets_at ? new Date(usage.resets_at).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '';
  const remaining = Math.max(0, Math.min(100, usage?.remaining_percent ?? 0));
  const used = 100 - remaining;
  const percentUsed = percent => percent > 0 && percent < 1 ? '<1% used' : `${Math.floor(percent)}% used`;
  const exhausted = usage && (usage.input_remaining <= 0 || usage.output_remaining <= 0);
  const status = !usage ? 'Usage unavailable. Refresh to try again.'
    : !usage.enabled ? 'Available with an active Pro subscription.'
    : `${percentUsed(used)}${exhausted ? ' · Monthly limit reached' : ''}`;
  return (
    <section className="card command-allowance-card">
      <h3 className="card-title">Command Mode</h3>
      <p role="status">{status}</p>
      {usage?.enabled && <>
        <progress max="100" value={used} aria-label="Command Mode monthly usage limit used" />
        <p className="command-usage-note">{usage.renewal_pending ? 'Allowance refreshes when your subscription renewal is confirmed.' : `Resets ${date}`}</p>
      </>}
    </section>
  );
}
