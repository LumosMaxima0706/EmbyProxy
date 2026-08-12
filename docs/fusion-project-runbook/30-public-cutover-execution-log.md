# Public Cutover Execution Log

Status: NOT STARTED - PHASE 1/2 DISCOVERY AND PLAN ONLY

2026-08-12: Read-only discovery completed on BWG. Nginx and rathole are
active/enabled; Nginx syntax passes. Existing production stream blocks route to
existing services (`18080` and `18081`), while the sidecar remains isolated on
`127.0.0.1:18082`. No live file, DNS record, service, or traffic state changed.

The execution gate is blocked pending the decisions recorded in
`28-public-cutover-discovery.md`. Phase 3 backups and Phase 4 staging dry-run
are intentionally not started because the exact public entry and scope are not
defined.

## Phase 3 backup

Owner scope was accepted for a new BWG-only media canary. A mode-preserving,
timestamped backup was created at:

`/root/backups/embyproxy-public-cutover/20260812T014601Z`

It contains the Nginx baseline files, rathole unit/config, sidecar unit/env and
release pointer, service/listener baseline, path manifest, and checksums. All 12
checksum entries passed; 14 files are present. No configuration content or
credential was emitted.

Phase 4 is blocked before rendering the exact server block because the owner has
not yet supplied the dedicated canary hostname. Nginx, DNS, rathole, services,
and traffic remain unchanged.
