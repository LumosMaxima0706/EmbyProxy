# Rollback Plan

Status: REQUIRED BEFORE DEPLOYMENT MUTATION

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
