# Account limits

Command Mode, recording admission, and durable meeting processing are scoped by authenticated **user ID**, never by session token, IP address, or device.

## Command Mode

- One active transform per account, shared by `/transform` and `/v1/transform`, including SSE and JSON responses.
- 10 submissions per UTC minute. An existing lower per-user `rate_limit_rpm` override is respected.
- 5,000,000 input tokens and 1,000,000 output tokens per billing period. Either exhausted allowance blocks further work.
- The current subscription's `razorpay_current_start/end` define the period when available. Expiry alone does not refill a known period while a renewal webhook is delayed; the confirmed payment period must change. Otherwise, use the subscription anniversary with month-end dates clamped; accounts without an anchor use UTC calendar months.
- Command Mode requires an active Pro subscription, matching the existing desktop UI restriction.
- Keep the existing 10,000-word combined selection/instruction validation. Additionally bound HTTP JSON to 2 MiB, request reads to 15 seconds, all provider work to 90 seconds, and output to the smaller of the remaining monthly budget and 32,768 tokens.

Dynamic SaaS configuration keys (existing values are preserved):

| Key | Default |
| --- | ---: |
| `ratelimit_command_rpm` | 10 |
| `quota_command_input_monthly_tokens` | 5000000 |
| `quota_command_output_monthly_tokens` | 1000000 |
| `command_max_output_tokens` | 32768 |

Each model attempt reserves input and output before making an upstream call. Input reservation uses UTF-8 prompt bytes plus framing headroom, rather than an English words-to-tokens guess. Provider-reported prompt/completion usage reconciles the reservation, including system prompts, cached input, reasoning output reported by the provider, and fallback attempts. SSE requests ask for final usage. Unambiguous upstream client rejection (such as invalid input or HTTP 429) refunds the attempt. Ambiguous HTTP 5xx failures retain the reservation. Missing usage after a timeout, disconnect, malformed response, or crash keeps the conservative reservation; it does not create free retries. Provider/model and per-attempt token counts remain in `command_attempts`.

The server constrains generation using `max_tokens`; an output-length stop is reported explicitly instead of silently treating a truncated transform as complete. Quota refusal stops fallback. If no data has been streamed, refusal remains HTTP 429 with a structured error and `Retry-After`.

## Meetings

- One active recording permit per account, across devices and the legacy live-meeting endpoint.
- One admitted durable processing job per account, covering ASR, CPU diarization waits, and summary generation. Three other jobs may wait. Incomplete uploads count toward these three pending slots.
- A valid existing manifest can resume without consuming another new-job allowance. Manifest submissions are limited to 30/minute, with new jobs additionally limited to 2/minute; job status to 120/minute; chunk uploads to 600/minute; commit/retry to 10/minute. Meeting reads and speaker edits have separate generous rate limits.
- The existing per-channel 512 MiB, per-chunk 4 MiB, four-hour audio duration, and atomic monthly audio quota reservation remain. Abandoned uncommitted uploads expire after 24 hours; local recordings remain available for re-upload.
- Upload-body reads have a 30-second deadline and happen outside the per-job striped lock. A slow upload cannot monopolize a single global meeting lock.

The desktop obtains a recording permit **before** starting audio capture. Internet is required for that step. A permit lasts four hours plus five minutes of safety margin. Losing connectivity afterward does not stop local capture or lose audio. The desktop stops and saves at four hours; it releases the permit when recording ends, or retries release on reconnect. A crash leaves a local release marker that the next start can reconcile, with server expiry as a fallback. Another device remains blocked during the outage rather than inheriting the account's recording slot early.

Audio is persisted locally and the existing 30-second recovery loop resumes uploads and processing status checks after reconnect. Automatic retries respect server `Retry-After`; manual retry remains available. Quota and rate failures surface as a desktop notification and Settings toast, deduplicated for five minutes. A completed offline recording is shown as saved with upload pending, not as lost audio.

## Cache, persistence, and failure behavior

Valkey keys `ratelimit:account:<user>:<operation>:<UTC-minute>` accelerate rate rejection. SQL `account_rate_windows` also records admitted attempts so cache flush/restart/outage cannot refill the minute's allowance. `quota:command:<user>:<period>` caches monthly usage snapshots; SQL `command_usage` and `command_attempts` are authoritative. SQL `account_leases` provides account-wide ownership with owner-checked release, independent of cache eviction.

When SQL admission is unavailable, costly work fails closed with HTTP 503 and an actionable message. The existing admin rate-limit reset clears the new minute counters too, while preserving monthly token usage and active work. Existing transcription rate and audio quota settings remain separate.

## Rollout and validation

Deploy the backend first: additive database schema, configuration defaults, and `/v1/meetings/session` permit routes. Then distribute the paired desktop client. The new client intentionally refuses to start recording against a backend that cannot authorize a permit; do not install it ahead of the backend.

Tests use local SQLite, an isolated Valkey-compatible test server, and mocked inference endpoints. They cover account/session isolation, cache outage/flush, concurrent admission, billing boundaries, durable token reservations and reconciliation, quota refusal before SSE, fallback usage, pending queue capacity, resumable uploads, and recording permit ownership. No production abuse or paid inference probe is needed.

## Account usage display and renewal

`GET /auth/me` returns `command_usage` from durable per-account counters: input/output used, limits and remaining tokens, remaining percentage, period start, reset time, entitlement, and renewal-pending state. The percentage is the smaller of the two remaining fractions. In-flight reservations are included; unavailable accounting returns null rather than an invented zero. Desktop Account and dashboard Analytics show this same snapshot with expandable token details. They refresh on account/tab access; the web dashboard also refreshes when its window regains focus, without interval polling. Public `/config` exposes configured monthly token limits for the pricing-page disclosure.

Paid transcription usage is reset by `ApplySuccessfulSubscriptionPayment`; free transcription resets on calendar-month maintenance. Successful payments also initialize a fresh Command Mode billing-period row in the same transaction. If a new invoice/payment webhook omits subscription dates, it fetches the subscription from Razorpay before recording the payment; reconciliation failures leave the event retryable. Duplicates do not require another provider fetch. Old rows and attempt records remain, so late settlements affect only their original period. An existing period is never cleared by a repeated notification. Non-payment subscription events preserve confirmed billing boundaries; expired confirmed periods remain in place until payment confirms renewal. The same period resolver drives enforcement and both displays. Without known provider billing dates, the existing activation-anniversary/month fallback applies.
