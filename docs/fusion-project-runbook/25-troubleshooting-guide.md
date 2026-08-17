# Troubleshooting Guide

Status: READY FOR OWNER SELF-USE

Keep diagnosis scoped to `embyproxy-gsy-sidecar.service` and its independent
release, config, database, and log paths. Do not change public ingress.

## Admin UI unavailable

- Confirm the tunnel process and local port (`ss -ltnH | grep 28082`).
- Confirm BWG service state and listener on `127.0.0.1:18082`.
- Recreate the tunnel with `ssh -N -L 28082:127.0.0.1:18082 bwg`.

## SSH tunnel failure

Verify the `bwg` alias, network reachability, and local port availability. Use
another loopback-only local port if needed. Do not change SSH configuration or
use NOSLA.

## Service inactive or failing

```bash
ssh bwg 'systemctl status embyproxy-gsy-sidecar.service --no-pager'
ssh bwg 'journalctl -u embyproxy-gsy-sidecar.service -n 120 --no-pager'
```

Record the symptom in the issue log. Restart only this sidecar to recover an
observed fault, then repeat health checks; never restart the host or unrelated
services.

## Port not listening

Check unit status and `ss -ltnH`. The expected bind is `127.0.0.1:18082`; do not
resolve conflicts by changing existing services or public ports.

## Token or auth failure

Use the owner-held credential through the interactive channel and inspect status
codes only. Never print, copy, rotate, or place credentials in logs, URLs, or
this repository.

## Managed-route CRUD failure

Check API auth, request validation, SQLite availability, and bounded logs. Retry
with a minimal non-sensitive route and remove temporary routes after testing.

## Proxy smoke failure

Confirm an owner-controlled upstream is reachable and target validation is not
rejecting an unsafe/private target. Preserve SSRF protections and feature-flag
semantics.

## Panic, error, or secret-looking logs

Stop smoke tests, capture a bounded redacted excerpt, and record an issue. Do not
paste authorization headers, cookies, tokens, passwords, complete query strings,
or complete UUIDs. Roll back only the isolated sidecar if recovery is required.
