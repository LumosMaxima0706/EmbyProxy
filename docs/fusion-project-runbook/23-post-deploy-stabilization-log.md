# Post-Deploy Stabilization Log

Status: DONE - PUBLISHED

## Scope

- Confirm local, BWG checkout, and remote feature refs.
- Observe only `embyproxy-gsy-sidecar.service` and its dedicated logs.
- Recheck loopback binding, Admin UI, auth rejection, managed-route CRUD, upstream
  smoke, fail-closed, fallback, and secret redaction.
- Verify rollback manifest and target-specific paths.
- Make no DNS, public traffic, existing Nginx server-block, rathole, or NOSLA change.

## Initial state

- Expected branch: `feature/failover-phase2-local`.
- Expected local/BWG/origin ref: `165c91f`.
- Deployed artifact source: `e0f2bb6`.
- Stabilization checks: PENDING.
- Service restart/reload: NOT PLANNED unless an observed fault requires recovery.

## Initial composite check

Result: INCONCLUSIVE. A read-only assertion exited before summary output. No remote
mutation or service action occurred. `POSTDEPLOY-ISSUE-001` records the diagnostic
retry requirement.

## Diagnosis

The service and runtime checks passed. The only failed assertion was rollback
manifest field parsing: the original first-deploy manifest stored all key/value
fields on one line. The manifest contains no credential, but it must be normalized
to newline-delimited fields with mode 0600 before stabilization can close.

## SSH tunnel attempt 1

The tunnel returned Admin UI data, but a `curl | grep -q` pipeline produced a
broken-pipe false failure under `pipefail`. Tunnel cleanup passed and the service
remained active. Retry is tracked as `POSTDEPLOY-ISSUE-002`.

## Completed stabilization result

- Local tunnel to BWG: Admin UI and unauthenticated managed-route rejection PASS.
- BWG localhost smoke: authenticated login, managed-route create/list/update/delete,
  owner-controlled public upstream proxy, disabled-route 404 fail-closed, legacy
  fallback, cleanup, and log redaction PASS.
- Service active/enabled, `NRestarts=0`, loopback listener, Nginx/rathole, and
  `nginx -t`: PASS.
- Rollback manifest normalized and verified; release/config/data/log paths and
  unit-only rollback command are complete.
- No service restart/reload, DNS, public traffic, Nginx block, rathole, or NOSLA action.

## Stabilization publish result

The completed stabilization record was published through BWG at `1aaf193`; no
runtime mutation occurred during publication.

## Day-2 finalization handoff

`DAY2-001` adds recurring owner operations, troubleshooting, backup/restore, and
day-2 checklist documentation. Runtime behavior remains unchanged and public
traffic remains intentionally untouched.
