# Plan-policy deployment, 2026-10-21 20:05 IST

Deployed the current backend and website source from `.`.
The desktop source changes were compiled/tested earlier but no desktop installer or
running desktop application was replaced during this server rollout. The inference
gateway code did not change and its deployed binaries were not replaced.

## Live release

- Main API instance: `i-REDACTED`, Linux ARM64.
- Running executable SHA-256, verified through `/proc/<MainPID>/exe`:
  `583fb780070079fbb3590500edb4474277f409ad9b7f1ff5e1d2fd50fe916317`.
- Previous executable preserved at
  `/home/ec2-user/speektome/rollback-plan-policy-20261021T143247Z/speektome-server`.
- Website deployed to the production `master` branch of Cloudflare Pages project
  `speekto-me`: https://7de02d77.speekto-me.pages.dev.
- Browser verification at https://speekto.me/pricing confirmed the new Free
  batch text, Free enhancement exclusion and 20/1,000 active vocabulary limits.
  The observed asset `index-HxSprYhT.js` matches the local build's asset name.
  A direct Python request to the website returned HTTP 403, so website verification
  used the real browser. Public API health separately returned HTTP 200 and
  `{"status":"ok"}` through HTTPS.

## Policy now enforced

Free transcription uses only the configured Whisper Large v3 Turbo candidates,
with no enhancement LLM call. Matching and Jev remain enabled on both plans.
Pro keeps batch or streaming and its enhancement stage. Client `model` and
`enhance` fields cannot override the authenticated plan policy.

Vocabulary additions are limited atomically to 20 on Free and 1,000 on Pro.
Downgrades preserve stored terms; transcription uses the first terms alphabetically
within the plan's active limit. Upgrades restore the larger active snapshot.

## Verification and preservation

The release was built with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`, and the uploaded
artifact's architecture and SHA-256 were verified before replacement. The old API
was stopped, the binary was replaced atomically, and API health passed after start.
The fleet controller remained running. At final verification, the API, controller
and subscription-expiry timer were active, with two ready ACTIVE inference workers.

Read-only before/after digests were identical for all account plan/billing/usage
records, saved vocabulary, account encryption-key records and runtime SaaS
configuration. Existing desktop sign-in remained valid: `/auth/me`,
`/auth/vocabulary` and `/v1/config` returned HTTP 200 using the existing session.
No real account's subscription, plan or vocabulary was edited for verification.

The production configuration has an enabled DeepInfra
`openai/whisper-large-v3-turbo` candidate with credentials available. The private
storage audit passed: zero plaintext usage/meeting/job rows, zero orphan usage
rows, and zero pending audio archives or active meeting jobs.

Pre-deployment validation included the full Go suite, focused race tests for the
new policy and limits, desktop configuration/shortcut tests, and the website
production build. Mocked provider tests cover Free/Pro enhancement flags, inactive
and expired Pro access, vocabulary/Jev on both plans, Free streaming rejection,
Whisper-only fallback and concurrent vocabulary additions. No paid ASR/Jev/LLM
inference request was made as part of the production rollout verification.

Aggregate-only evidence is stored at:
`/home/ssben/.local/state/SpeekToMe/verification/2026-10-21-plan-policy-deployment`.
