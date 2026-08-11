# Backup And Restore Drill

Status: VERIFIED, NON-DESTRUCTIVE

This drill verifies rollback readiness without stopping the healthy service,
deleting files, or restoring over the current deployment.

## Read-only evidence

- Backup root: `/root/backups/embyproxy-gsy-sidecar`.
- Manifest: `/root/backups/embyproxy-gsy-sidecar/20260811T151516Z/pre-deployment-manifest.txt`.
- Release root: `/opt/embyproxy-gsy-sidecar`.
- Current release link: `/opt/embyproxy-gsy-sidecar/current`.
- Config: `/etc/embyproxy-gsy-sidecar/embyproxy.env` (do not print contents).
- Database: `/var/lib/embyproxy-gsy-sidecar/proxy.db`.
- Logs: `/var/log/embyproxy-gsy-sidecar`.

Verify existence, mode, symlink target, and manifest readability with `test`,
`stat`, `readlink`, and bounded metadata output. Do not modify these paths.

## Scoped rollback sequence

1. Stop owner traffic and record the failing health check.
2. Preserve logs and the manifest for diagnosis.
3. Stop only `embyproxy-gsy-sidecar.service` if it is running.
4. Restore the prior release/config/database recorded by a future manifest, or
   disable the independent first-deploy unit when no prior release exists.
5. Start the selected known-good sidecar release.
6. Confirm active state, status zero, loopback listener, Admin UI status, auth
   rejection, and a safe smoke request.
7. Leave Nginx, DNS, rathole, firewall, and unrelated services unchanged.

The current deployment is the first sidecar deployment; its manifest records no
previous release to restore. A future upgrade must add a new timestamped
manifest before switching `current`.

## Restore success criteria

The unit is active, listener is only `127.0.0.1:18082`, health checks pass, logs
contain no panic/error or secret leakage, and the owner tunnel works.
