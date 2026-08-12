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

## Phase 4 dry-run result

- Isolated staging `nginx -t`: PASS.
- Loopback-only staging server returned 404 for `/`, all Admin UI/API variants,
  and empty `/s/`: PASS.
- DNS provider read/status and absent-canary readiness checks: PASS.
- Nginx `/s/` allowlist provides the required separation from the mixed app
  listener; no app change was needed.

## Phase 5 controlled cutover result

- Installed only `/etc/nginx/conf.d/embyproxy-gsy-canary.conf`.
- Created only the `canary` A record with TTL 60 through the root-only provider
  module; public DNS verification passed.
- Issued a dedicated canary certificate.
- Every Nginx reload followed a successful `nginx -t`; no other service was
  reloaded or restarted.

## Phase 6 public smoke result

- TLS and HTTP-to-HTTPS redirect: PASS.
- `/`, all Admin UI/API variants, unknown route, and empty `/s/`: 404.
- Authenticated temporary public route creation/read: PASS; target and
  credentials were not emitted.
- Public proxy returned an upstream application status rather than
  502/503/504: PASS.
- Disabled route 404, legacy fallback non-5xx, and temporary route cleanup:
  PASS. Managed route and line counts returned to zero.
- Nginx/rathole/sidecar state, loopback listener, unchanged existing files,
  DNS, certificate, and canary-specific log scans: PASS.

## Phase 7 disposition

Rollback was not triggered because all critical checks passed. Its exact
script was syntax-checked and hashed in the protected backup.

## Runbook publish attempt

The first bundle transfer attempt stopped before SCP because the verified WSL
bundle path was not the Windows path passed to SCP. BWG staging and origin
remained unchanged and clean; no temporary ref/bundle existed and no merge,
push, service, or runtime action occurred. The retry uses a workspace-visible
bundle path and repeats every bridge check.

## Owner managed-route public validation

The owner created public managed route `v1` through the private Admin UI. A
read-only public request to `/s/v1/System/Info/Public` returned HTTP 200 with
valid TLS. Public `/admin`, `/api/admin`, and `/api/admin/status` each returned
404. No response body, upstream target, credential, cookie, query, UUID, or
subscription value was recorded.

Nginx, rathole, and sidecar remained active with `NRestarts=0`; Nginx syntax
passed and the sidecar remained loopback-only. Bounded sidecar and
canary-specific Nginx log scans found zero secret/token/cookie/password/private
key/panic/fatal markers and zero sidecar error markers. No configuration,
route, DNS, service, Nginx, or rathole mutation was performed by this check.
