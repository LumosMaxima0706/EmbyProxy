# Post-Deploy Stabilization Log

Status: IN_PROGRESS

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
