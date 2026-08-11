# Runtime Healthcheck

Status: PASS - BWG SIDECAR HEALTHY

Post-deploy stabilization and day-2 recheck: PASS.

## Checks

- `systemctl is-active <new-sidecar-service>` and bounded service status.
- Listener is bound to `127.0.0.1:18082`, with no public bind.
- Local health endpoint responds with the documented status.
- Unauthenticated Admin API requests are rejected; authenticated behavior is
  checked without printing credentials.
- Managed-route list/create/update/delete smoke checks use redacted output.
- Feature flag off or unknown preserves legacy fallback; enabled mode fails closed
  for unsafe or unavailable managed routes.
- HTTP, Range, WebSocket, Location/Content-Location rewrite, and upstream error
  behavior are checked where a safe local fixture is available.
- Logs are bounded and scanned for secrets, cookies, authorization values, and
  sensitive query parameters.
- Existing BWG services and Nginx remain healthy.

## Current result

- Service `embyproxy-gsy-sidecar.service`: active and enabled.
- Version/commit: `sidecar-e0f2bb6` / `e0f2bb6`.
- Listener: loopback-only `127.0.0.1:18082`.
- Admin UI and unauthenticated rejection: PASS.
- Authenticated managed-route CRUD: PASS; temporary smoke route removed.
- Owner-controlled public upstream proxy request: PASS without response output.
- Disabled managed route: 404 fail-closed.
- Legacy fallback request: no 5xx.
- Error/access log review: expected access statuses only; no ERROR/FATAL/panic or
  sensitive marker found.
- Existing Nginx/rathole: active; `nginx -t`: PASS.
- No DNS, public traffic, existing Nginx block, NOSLA, or host reboot action.

## Failure handling

Any failed check is recorded in `13-issue-resolution-log.md` and
`19-deployment-execution-log.md`. Do not broaden the deployment scope; execute the
scoped rollback before further diagnosis.

## Day-2 recheck scope

- Reconfirm local, BWG, and origin feature refs before documentation publish.
- Recheck service state, restart count, loopback listener, and bounded logs.
- Reuse the SSH tunnel smoke path without printing sensitive data.
- Verify rollback manifest metadata and current release link without mutation.

## Day-2 result

- Local/BWG/origin base ref: `31fa87c`; day-2 docs were then committed locally
  for feature-only bridge publication.
- Service active/enabled, status zero, `NRestarts=0`.
- Listener loopback-only on `127.0.0.1:18082`.
- Bounded log and secret-marker scans: PASS.
- SSH tunnel Admin UI and unauthenticated rejection: PASS; tunnel cleaned up.
- Authenticated CRUD, owner-controlled upstream, fail-closed, fallback, and
  temporary-route cleanup: PASS.
- Rollback metadata and existing Nginx/rathole safety checks: PASS.
- No service restart/reload was required.
