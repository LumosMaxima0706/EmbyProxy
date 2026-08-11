# Rollback Plan

Status: VERIFIED FOR CURRENT FIRST DEPLOYMENT

Rollback is limited to newly created sidecar assets. Existing services and
configuration must not be deleted or overwritten.

## Required pre-mutation records

- Previous binary/release path and commit.
- New release path and artifact checksum.
- Config backup path and database backup path, if a database is used.
- New service name and exact stop/start command.
- Local health-check command and expected response.

## Failure rollback

1. Stop smoke tests and do not change Nginx, DNS, or traffic.
2. Stop only the new sidecar service, if it was started.
3. Restore the previous binary/config from the timestamped backup, or disable the
   new independent service without touching existing services.
4. Validate the restored configuration and service status.
5. Re-run localhost health checks and inspect bounded logs.
6. Preserve backups and logs for diagnosis; do not remove unknown data.

Rollback is not considered available until the target-specific commands and paths
are recorded after preflight.

## Target-specific rollback

- Service: `embyproxy-gsy-sidecar.service`.
- Release root: `/opt/embyproxy-gsy-sidecar`.
- Config: `/etc/embyproxy-gsy-sidecar/embyproxy.env`.
- Database: `/var/lib/embyproxy-gsy-sidecar/proxy.db`.
- Logs: `/var/log/embyproxy-gsy-sidecar`.
- Backup root: `/root/backups/embyproxy-gsy-sidecar`.
- First-deploy manifest: `/root/backups/embyproxy-gsy-sidecar/20260811T151516Z/pre-deployment-manifest.txt`.

This is the first deployment and all target paths were absent at preflight. Before
service start, create a timestamped manifest stating that no previous release,
config, database, or service existed. On a failed first start, use
`systemctl disable --now embyproxy-gsy-sidecar.service`, verify port 18082 is free,
and preserve the new release/config/database/log assets for diagnosis. Do not touch
Nginx, rathole, staging data, or any unknown service.

Before first start, the unit is disabled/inactive, port 18082 is free, and no
database exists. This is the verified rollback baseline.

Post-start rollback remains scoped to the new unit and assets. The first-deploy
manifest is retained; no previous release/config/database needed restoration.

Post-deploy stabilization must revalidate the manifest, current release link,
configuration/data/log paths, and exact unit-only rollback command before closing.

Manifest formatting repair is pending; this does not require a service restart or
change to the deployed binary.

## Day-2 drill

The non-destructive drill verifies the backup root, first-deploy manifest,
current release link, independent config/database/log paths, and unit-only
rollback boundary. No restore, delete, stop, or restart is performed while the
service is healthy.
