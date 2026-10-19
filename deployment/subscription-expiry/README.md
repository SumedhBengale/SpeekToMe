# Subscription expiry

The main server runs an expiry sweep before accepting traffic. The same binary's `--expire-subscriptions` mode provides the daily maintenance job without starting HTTP, workers, or orchestration.

Install the service and timer under `/etc/systemd/system/`, then run:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now speektome-subscription-expiry.timer
```

The persistent systemd timer is the daily cron equivalent: 00:15 UTC (05:45 IST), plus two minutes after boot. Missed scheduled runs execute when the timer starts again. API service restarts independently run the sweep, covering occasions when only the application was stopped. Failed jobs retry after five minutes.

The sweep atomically downgrades `pro` accounts with a positive `razorpay_current_end` at least 48 hours in the past. It sets `plan_tier=free`, `plan_active=0`, and `monthly_usage_seconds=0`, and invalidates affected Valkey sessions. Repeated runs cannot reset an already-Free account. The existing calendar-month Free quota reset is unchanged. Payment history, subscription identifiers, confirmed billing boundaries, Command Mode usage, and attempts remain intact. Accounts without a known end date are not assigned a guessed expiry by this job.

Session authentication independently checks this deadline (including on Valkey cache hits) so users cannot retain paid access until the next scheduled sweep. Older cached sessions without billing dates are reloaded. A later confirmed payment restores the subscription through the existing payment handler.

Inspect `systemctl list-timers speektome-subscription-expiry.timer` and `journalctl -u speektome-subscription-expiry.service`. Never flush the complete Valkey database; quota and lease data is unrelated to expired session snapshots.
