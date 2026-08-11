# Day-2 Operations Checklist

Status: DONE - OWNER SELF-USE READY

## Before each use

- [ ] Start a loopback-only SSH tunnel to BWG.
- [ ] Confirm sidecar active/enabled and listener loopback-only.
- [ ] Open Admin UI locally and authenticate without exposing the credential.
- [ ] Confirm no public ingress, DNS, Nginx, or rathole change is planned.

## After managed-route changes

- [ ] List the route and verify saved fields.
- [ ] Run one safe owner-controlled smoke request.
- [ ] Check fail-closed or legacy fallback for the selected flag state.
- [ ] Remove temporary test routes and review bounded logs.

## When behavior is abnormal

- [ ] Capture service status and a bounded redacted log excerpt.
- [ ] Check listener, database path, and upstream reachability.
- [ ] Record an issue before recovery restart.
- [ ] Restart only the isolated sidecar if necessary, then repeat health checks.

## Before rollback

- [ ] Preserve logs and identify the failing check.
- [ ] Verify timestamped manifest and release/config/data paths.
- [ ] Confirm rollback affects only the sidecar unit.
- [ ] Do not touch Nginx, DNS, rathole, public traffic, or unrelated services.

## Before any future public cutover

- [ ] Obtain explicit approval for traffic, DNS, Nginx, rathole, and firewall changes.
- [ ] Complete provenance/license/SBOM/notices release hygiene.
- [ ] Test rollback from a pre-cutover manifest.
- [ ] Define monitoring, owner access, and an abort threshold.
