# Local Delivery Handoff

Status: BWG LOCALHOST SIDECAR DEPLOYED - OWNER USE READY

## Scope Delivered

This local delivery implements the managed-route fusion path:

Admin UI / Admin API -> SQLite managed route storage -> proxyadapter validation/loading -> mediaproxy routing behavior.

## Completed Source Work

- Managed-route SQLite storage with transactional CRUD and line replacement.
- Authenticated Admin API for managed-route create, update, list, and delete operations.
- Embedded Admin UI managed-route editor with route and line controls.
- Feature-flagged fail-closed behavior with legacy fallback.
- Managed-route response header rewrite regression coverage.
- SQLite close/reopen persistence test coverage.

## Key Commits

- `98229bd` managed-route storage
- `0b7b590` authenticated Admin API
- `8c00f1a` embedded Admin UI managed-route editor
- `29118fb` feature flag fail-closed and legacy fallback
- `4e60097` managed-route response header rewrite regression
- `fc00c61` SQLite close/reopen persistence test
- `5af3c6f` full-project delivery gate results

## Verification Completed

- `gofmt`: PASS
- Targeted Go tests: PASS
- `go test ./...`: PASS
- `go vet ./...`: PASS
- Redaction/auth/API regression tests: PASS
- `git diff --check`: PASS
- Working tree before this handoff: clean

## Manual Review Focus

1. Managed-route schema and migration behavior.
2. Admin API authentication, authorization, validation, and error behavior.
3. Admin UI route creation, update, delete, refresh, and line editing flow.
4. Feature flag disabled/unknown legacy fallback and enabled route loading.
5. Runtime route loading and mediaproxy header rewrite behavior.
6. Backward compatibility with existing node/config-based behavior.
7. No secret leakage in logs, docs, API responses, or UI.

Browser automation is not configured in this repository; live authenticated UI behavior remains a human review item.

## Known Remaining Items

### Non-blocking for implementation

`ISSUE-PROV-002` / `GAP-PROV-002` remains open for formal license, attribution, provenance, dependency inventory, SBOM, and notices evidence. It does not block implementation, but may block formal public release/distribution.

### Deployment/runtime boundary

Failover runtime, traffic, DNS, and policy gaps remain open for later phases. The
new sidecar is available only through owner-controlled localhost/SSH access; these
gaps must be reviewed before any production traffic change.

## Publish Readiness

This branch was published to `origin/feature/failover-phase2-local` at `5cbbe54` through the authorized BWG publish bridge. Publishing must continue to use the BWG bridge and must not force push, push `main`/`master`, deploy, restart services, or SSH BWG/NOSLA without explicit authorization.

## Rollback Principle

Before any production traffic change, the target environment must have a recorded
previous working commit, a reviewed configuration backup, a database backup or
migration rollback plan, and a reviewed feature-flag rollback path. The only
service start performed here was the new isolated sidecar; no rollback was needed.

## Deployment result

- BWG sidecar service: `embyproxy-gsy-sidecar.service`, active and enabled.
- Listener: `127.0.0.1:18082` only.
- Release commit: `e0f2bb6`; runtime artifact checksum is recorded in the deployment log.
- Independent release/config/data/log paths and first-deploy rollback manifest are recorded.
- No Nginx test location, DNS, public traffic, existing service, or host reboot was changed.
- Administrator token remains only in the BWG mode-0600 environment file and was not
  printed or committed; owner must use an approved secure retrieval path.

## Next Gate

Next required gate: publish the deployment documentation through the BWG bridge,
then owner operations review. Production traffic changes remain separately gated.
