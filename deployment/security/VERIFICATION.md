# Main-server security release verification, 20 October 2026

Application source commit: `6f4189b` (merged into local master).
Live ARM64 binary SHA-256:
`2296a369b2ea90cdade19350ea265aa54ee022c07a32b42fb1af5c5b1b103a80`.

- Full server `go test ./...` passed, including regression tests for the reviewed
  defects, OAuth state/code replay, session hashing/migration, config preservation,
  encryption round trips, cross-account ownership and protected/admin route gates.
- Desktop `cargo check --locked` passed. This verifies compilation, not a full
  Windows/macOS runtime exercise. Installed desktop login remains supported.
- Website production build and lint completed (existing lint warnings remain).
  Production Cloudflare Pages uses branch **master**, not main. Production HTML
  referenced the exact generated JS asset after deployment.
- Main server and fleet were active; public API health returned 200 through
  Cloudflare. Direct-origin HTTPS timed out after the broad web-ingress group was
  replaced on this instance. Other instances' groups were not modified.
- Existing desktop token still authenticated after migration, including a request
  without a User-Agent header, as used by desktop HTTP clients. Authenticated
  meetings/history/storage requests returned 200: 8 meetings, 17 final history
  entries. Responses were processed without printing private contents/tokens.
- Private-data audit: plaintext meeting rows **0**, plaintext job-result rows **0**,
  plaintext usage rows **0**, orphan usage rows **0**; ready encrypted audio
  objects **8**, pending objects **0**. Existing 1261 encrypted legacy results
  remained present.
- Raw session rows **0**, legacy bearer cache keys **0**, retired config rows **0**.
  Existing normal sessions were hashed in place, preserving installed clients.
- Model JSON, meeting endpoint and account billing-state fingerprints matched
  before/after. The account's paid period and existing Razorpay test-mode
  configuration were preserved. No live subscription was created/cancelled.
- Expiry service last result remained success/exit 0. Its persistent daily timer
  and request/startup 48-hour grace enforcement remain in place.
- Nginx query/referrer logging was removed; 482 credential-bearing fields in
  retained logs were scrubbed. No raw tokens were saved in verification output.

Deployment incident: the first artifact was incorrectly built for x86 and was
rejected by the ARM host before execution. Database fingerprints/raw-session
counts confirmed that no migration ran. The prior binary was restored and health
verified, then the correct static ARM64 build was deployed with an architecture
check. The final service started normally with zero restart failures. Deployment
instructions now explicitly require ARM64.

SSH now uses a dedicated administration security group restricted to the verified
operator address; web traffic uses the Cloudflare-only group. Update the SSH /32
through AWS when the operator's public address changes.

Primary checkout's pre-existing generated Tauri schema changes were preserved.
