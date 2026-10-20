# Main-server security deployment

The server migrates existing session tokens to SHA-256 digests without logging
out installed clients. Existing raw Redis session keys must be removed **after
stopping the previous server**, so it cannot recreate them. Clear the SaaS config
cache as well. Do not remove the account encryption keys or change the master key.

Startup encrypts retained meeting text and job intermediates using the existing
account keys. Completed intermediates are then removed. This migration is
restart-safe. Older binaries cannot read these ciphertexts or hashed sessions:
rollback builds must retain the new codecs and session hashing.

Deploy the website (Cloudflare Pages project `speekto-me`, production branch
`master`) before the server. It supports both the old token callback and the new
single-use code exchange. New desktop builds opt into code exchange; installed
versions retain the nonce-bound loopback callback. No bearer is included in the
payment URL anymore.

`harden_nginx.py` removes query strings and referrers from access logs, suppresses
ordinary error logs which can include secret request URLs, and scrubs retained
credential query fields. Review actual Nginx configuration before applying it.
The proxy must replace `X-Real-IP` with its trusted `$remote_addr`, using only
Cloudflare's configured real-IP networks. The application trusts that header
only from a loopback peer.

The main instance must use the Cloudflare-only web security group plus a dedicated
SSH security group. Do not attach a second group permitting public TCP 80/443.
Administrative SSH is currently restricted to the operator's verified public /32;
update this rule through AWS when the administration address changes. Other
instances' security groups must remain untouched.

After restart, verify API health through Cloudflare, direct-origin rejection,
private-data audit counts, ordinary account authentication, paid period and quota
preservation, model config fingerprint, the expiry timer, and session-cache key
hygiene. Payment testing must use mock providers or the configured sandbox;
do not create or cancel the real account's subscription during deployment.
