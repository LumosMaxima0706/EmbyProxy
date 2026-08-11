# Deployment Execution Plan

Status: IN_PROGRESS

Owner has authorized autonomous self-use deployment. This plan is limited to the
documented BWG sidecar boundary and does not authorize production traffic changes.

## Target and boundary

- Target host: BWG SSH alias `bwg`.
- Deployment checkout/staging: `/root/staging/embyproxy-staging`.
- New sidecar listener: `127.0.0.1:18082` only.
- New assets must use an independent release directory, config, log path, service
  name, and binary backup.
- Existing Nginx server blocks, `/admin/`, `/s/`, rathole, DNS, and production
  traffic remain unchanged.

## Execution gates

1. Mark the deployment step `IN_PROGRESS` and record the attempt.
2. Run local branch, test, build, and artifact checks.
3. Run read-only BWG preflight: identity, checkout, service-name/port conflicts,
   disk space, existing runtime state, and Nginx structure.
4. Resolve or record any conflict before mutation.
5. Create timestamped backups of the current release/config/database as applicable.
6. Upload only the verified artifact to an independent BWG staging/release path.
7. Validate config and artifact checksums; start or reload only the new sidecar.
8. Run localhost health and managed-route smoke checks; inspect bounded logs.
9. On failure, stop and execute the scoped rollback in `20-rollback-plan.md`.
10. Record results in all required runbook files and commit the documentation.

## Stop conditions

Stop without guessing if the target, service name, port, backup path, rollback
command, credentials, or required owner-only decision is unavailable. Never modify
Nginx or DNS as an implicit deployment step.

## Success criteria

- Independent sidecar is running only on `127.0.0.1:18082`.
- Existing services remain running and unchanged.
- Health checks and local smoke tests pass, including auth rejection, managed-route
  behavior, legacy fallback, WebSocket/Range handling, and redacted logging.
- A tested rollback path and backup locations are recorded.
