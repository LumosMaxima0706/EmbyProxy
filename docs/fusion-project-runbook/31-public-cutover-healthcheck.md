# Public Cutover Healthcheck

Status: PASS - BWG-ONLY PUBLIC CANARY HEALTHY

The pre-cutover baseline is healthy: sidecar active/enabled with zero restarts,
loopback-only listener, Nginx and rathole active, and Nginx syntax valid.

After an owner-approved cutover, run the matrix in `29-public-cutover-plan.md`
in this order: service/listener, Nginx syntax, TLS/HTTP reachability, admin/API
isolation, authenticated owner CRUD, bounded owner upstream smoke, fail-closed,
fallback, DNS convergence, and redacted log scans. A failed critical check
aborts the canary and invokes `32-public-cutover-rollback.md`.

The approved public surface is only `/s/`. The public checker must assert 404
for `/`, `/admin`, `/admin/`, `/api/admin`, and `/api/admin/` before any proxy
smoke can pass the gate.

## Final results

| Check | Result |
| --- | --- |
| Public DNS/TLS/redirect | PASS; TTL 60, valid certificate, HTTP to HTTPS |
| Admin isolation | PASS; `/admin` and every `/api/admin` form return 404 |
| Required `/s/` proxy | PASS; temporary owner route reached an upstream application status, not a gateway failure |
| Unknown/disabled route | PASS; 404 fail-closed |
| Legacy fallback | PASS; non-5xx localhost result |
| Cleanup | PASS; temporary route and line counts returned to zero |
| Services/listener | PASS; Nginx/rathole/sidecar active/enabled, status zero, `NRestarts=0`; sidecar loopback-only |
| Existing ingress/config | PASS; all backed-up production/staging/rathole/app files unchanged |
| Canary-specific logs | PASS; no panic/error/secret marker |

The shared Nginx error log contains unrelated public TLS handshake noise and
other upstream-close entries. Canary-specific filtering found no canary error;
sidecar and canary access scans contained no sensitive marker.
