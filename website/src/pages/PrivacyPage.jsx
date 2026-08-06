import React from 'react';

export default function PrivacyPage() {
  return (
    <div className="editorial-container" style={{ maxWidth: '800px', margin: '40px auto', padding: '0 24px' }}>
      <h1>Privacy Policy</h1>
      <p className="text-muted" style={{ marginBottom: '24px' }}>Last updated: July 2, 2026</p>
      
      <p>At <strong>SpeekTo.Me</strong>, we value your privacy. This policy outlines how we handle audio recordings and transcribed text when you use our desktop application and cloud API services.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>1. Data Collection & Processing</h2>
      <p>To perform transcription, our application records audio locally from your microphone only when you actively trigger the hotkey. This audio is immediately compressed using secure local processing.</p>
      <p>This compressed audio is sent via secure encrypted requests directly to our backend server. We do not store raw audio recordings on our servers. Once the transcription request is completed, the audio data is discarded immediately from temporary memory.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>2. Transcription and Enhancement</h2>
      <p>Audio transcription is processed via secure, state-of-the-art translation systems. If text formatting or enhancement is enabled, the resulting transcript is processed using modern language models to clean grammar and punctuation. Both services process the data in real-time under strict confidentiality agreements. None of your data is ever used to train machine learning models.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>3. Usage Logging</h2>
      <p>To enforce rate limits and subscription quotas, we log metadata about requests in a secure database. This includes: user ID, timestamp, transcribed audio duration (in seconds), transcription and AI formatting latency statistics, and request status.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>4. Changes to This Policy</h2>
      <p>We may update this policy occasionally. Any changes will be posted on this page.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>5. Contact Us</h2>
      <p>For questions regarding privacy or data handling, contact us at: <a href="mailto:contact@synaptican.com" style={{ color: 'var(--color-text-dark)', fontWeight: '600' }}>contact@synaptican.com</a>.</p>
    </div>
  );
}
