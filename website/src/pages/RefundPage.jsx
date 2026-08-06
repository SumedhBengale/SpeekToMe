import React from 'react';

export default function RefundPage() {
  return (
    <div className="editorial-container" style={{ maxWidth: '800px', margin: '40px auto', padding: '0 24px' }}>
      <h1>Refund Policy</h1>
      <p className="text-muted" style={{ marginBottom: '24px' }}>Last updated: July 2, 2026</p>
      
      <p>At <strong>SpeekTo.Me</strong>, a product built and operated by <strong>Synaptican</strong>, we strive to deliver the fastest, most reliable voice dictation tool. We understand that sometimes things don't work out as expected, or technical issues may arise. This policy outlines how we handle customer support and refund requests.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>1. Case-by-Case Support & Review</h2>
      <p>We do not provide unconditional automatic refunds. However, if you are unsatisfied with SpeekTo.Me Pro or experience technical difficulties, we encourage you to contact our support team immediately. Our team reviews all refund requests thoughtfully on a case-by-case basis to find a fair solution.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>2. Evaluation Criteria</h2>
      <p>When reviewing refund requests, our team takes into consideration:</p>
      <ul style={{ paddingLeft: '20px', margin: '12px 0' }}>
        <li>Technical difficulties or service disruptions encountered.</li>
        <li>Account setup or compatibility issues with your operating system.</li>
        <li>Timing and history of your subscription request.</li>
      </ul>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>3. Renewals and Cancellations</h2>
      <p>Our subscriptions are billed monthly and renew automatically. You can cancel your subscription at any time directly from your Dashboard settings. Once cancelled, your service will remain active until the end of your current billing period. Recurring monthly renewals are generally non-refundable once processed.</p>

      <h2 style={{ marginTop: '32px', marginBottom: '12px' }}>4. How to Reach Our Team</h2>
      <p>If you have any questions, feedback, or would like our team to review a refund request, please email us at <a href="mailto:contact@synaptican.com" style={{ color: 'var(--color-text-dark)', fontWeight: '600' }}>contact@synaptican.com</a> with the subject line "Support & Refund Request". Please include:</p>
      <ul style={{ paddingLeft: '20px', margin: '12px 0' }}>
        <li>Your registered account email address.</li>
        <li>A brief explanation of the issue or why you are requesting a refund.</li>
      </ul>
      <p>Our team will get back to you promptly to assist you.</p>
    </div>
  );
}
