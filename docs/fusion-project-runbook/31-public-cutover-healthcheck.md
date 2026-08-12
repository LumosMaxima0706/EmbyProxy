# Public Cutover Healthcheck

Status: PLAN ONLY - NO PUBLIC CUTOVER

The pre-cutover baseline is healthy: sidecar active/enabled with zero restarts,
loopback-only listener, Nginx and rathole active, and Nginx syntax valid.

After an owner-approved cutover, run the matrix in `29-public-cutover-plan.md`
in this order: service/listener, Nginx syntax, TLS/HTTP reachability, admin/API
isolation, authenticated owner CRUD, bounded owner upstream smoke, fail-closed,
fallback, DNS convergence, and redacted log scans. A failed critical check
aborts the canary and invokes `32-public-cutover-rollback.md`.
