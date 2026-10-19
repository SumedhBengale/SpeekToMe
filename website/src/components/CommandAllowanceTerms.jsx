import React, { useEffect, useState } from 'react';

export default function CommandAllowanceTerms() {
  const [limits, setLimits] = useState(null);
  useEffect(() => {
    const controller = new AbortController();
    const base = import.meta.env.VITE_API_URL || (['localhost', '127.0.0.1'].includes(window.location.hostname) ? 'http://localhost:8000' : '');
    fetch(`${base}/config`, { signal: controller.signal }).then(res => res.ok ? res.json() : null).then(data => {
      if (data?.command_input_monthly_tokens > 0 && data?.command_output_monthly_tokens > 0) setLimits(data);
    }).catch(() => {});
    return () => controller.abort();
  }, []);
  return <p id="command-mode-allowance" style={{ fontSize: '13px', lineHeight: 1.6 }}>
    {limits ? `Monthly Command Mode allowance: ${limits.command_input_monthly_tokens.toLocaleString()} input tokens and ${limits.command_output_monthly_tokens.toLocaleString()} output tokens. ` : 'Command Mode has separate monthly input and output token allowances. '}
    Either limit can pause usage. Allowances refresh each billing month on confirmed renewal. See your current limits, remaining usage, and reset date in your account dashboard.
  </p>;
}
