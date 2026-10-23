import React from 'react';
import './DashboardLoader.css';

export default function DashboardLoader() {
  return (
    <main className="dashboard-loading" aria-live="polite" aria-busy="true">
      <div className="dashboard-loading-card">
        <a className="dashboard-loading-brand" href="/" aria-label="SpeekTo.Me home">
          <img src="/logo.svg" alt="" />
          <span>SpeekTo<span>.</span>Me</span>
        </a>
        <div className="dashboard-loading-pill" role="img" aria-label="SpeekTo.Me is getting ready">
          <i /><i /><i /><i /><i />
        </div>
        <h1>Getting your words ready.</h1>
        <p>Your dashboard is loading securely.</p>
        <div className="dashboard-loading-track" aria-hidden="true"><span /></div>
      </div>
    </main>
  );
}
