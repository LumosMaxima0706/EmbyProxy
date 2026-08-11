# Runtime Healthcheck

Status: PENDING DEPLOYMENT

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

## Failure handling

Any failed check is recorded in `13-issue-resolution-log.md` and
`19-deployment-execution-log.md`. Do not broaden the deployment scope; execute the
scoped rollback before further diagnosis.
