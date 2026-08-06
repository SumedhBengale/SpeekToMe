import React from 'react';

export default function TermsPage() {
  return (
    <div className="editorial-container" style={{ maxWidth: '800px', margin: '40px auto', padding: '0 24px' }}>
      <h1>Terms & Conditions</h1>
      <p className="text-muted" style={{ marginBottom: '24px' }}>Last updated: July 2, 2026</p>

      <p>Welcome to <strong>SpeekTo.Me</strong>, a product built and operated by <strong>Synaptican</strong>. By downloading our desktop client or using our services, you agree to comply with and be bound by these terms.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>1. Subscription and Fees</h2>
      <p>We offer a single subscription tier of <strong>$2 per month</strong>. Subscriptions are billed on a recurring monthly basis. You can cancel at any time through your dashboard portal. No refunds are provided for partial billing cycles.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>2. Subscription Usage & Fair-Use Policy</h2>
      <p>We provide unlimited audio transcription for active <strong>SpeekTo.Me Pro</strong> subscriptions. However, to ensure fair service quality, protect infrastructure stability, and prevent automated abuse, Synaptican reserves the right to modify, adjust, or change the transcription quota or apply fair-use limits for paid plans at our sole discretion at any time.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>3. Prohibited Abuse</h2>
      <p>You agree not to bypass, hack, or overload our API endpoint. Rate limits (15 requests per minute) are enforced to safeguard API stability. Any malicious traffic, bots, or reverse-engineering of the desktop client will result in immediate termination of your account without a refund.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>4. Limitation of Liability</h2>
      <p>SpeekTo.Me is provided "as is". Synaptican makes no warranties that the service will be entirely error-free or uninterrupted. We are not liable for any lost data or formatting inaccuracies in transcripts.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>5. Contact Us</h2>
      <p>For support or billing inquiries, email: <a href="mailto:contact@synaptican.com" style={{ color: 'var(--color-text-dark)', fontWeight: '600' }}>contact@synaptican.com</a>.</p>
    </div>
  );
}
