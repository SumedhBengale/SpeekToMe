import React, { useEffect, useRef } from 'react';

const SITE_KEY = import.meta.env.VITE_TURNSTILE_SITE_KEY || 'YOUR_TURNSTILE_SITE_KEY';

export default function TurnstileWidget({ onVerify, resetTrigger }) {
  const containerRef = useRef(null);
  const widgetIdRef = useRef(null);

  useEffect(() => {
    let intervalId;

    const renderWidget = () => {
      if (window.turnstile && containerRef.current) {
        if (widgetIdRef.current !== null) {
          try {
            window.turnstile.remove(widgetIdRef.current);
          } catch (e) {}
        }
        try {
          widgetIdRef.current = window.turnstile.render(containerRef.current, {
            sitekey: SITE_KEY,
            size: 'flexible',
            action: 'turnstile-spin-v2',
            callback: (token) => {
              if (onVerify) onVerify(token);
            },
            'expired-callback': () => {
              if (onVerify) onVerify('');
            },
            'error-callback': () => {
              if (onVerify) onVerify('');
            }
          });
        } catch (e) {
          console.error("Turnstile render error:", e);
        }
        clearInterval(intervalId);
      }
    };

    renderWidget();
    intervalId = setInterval(renderWidget, 300);

    return () => {
      clearInterval(intervalId);
      if (window.turnstile && widgetIdRef.current !== null) {
        try {
          window.turnstile.remove(widgetIdRef.current);
        } catch (e) {}
      }
    };
  }, []);

  useEffect(() => {
    if (resetTrigger && window.turnstile && widgetIdRef.current !== null) {
      try {
        window.turnstile.reset(widgetIdRef.current);
      } catch (e) {}
    }
  }, [resetTrigger]);

  return (
    <div
      ref={containerRef}
      className="cf-turnstile"
      data-sitekey={SITE_KEY}
      data-action="turnstile-spin-v2"
      style={{ display: 'flex', justifyContent: 'center', margin: '14px 0' }}
    />
  );
}
