# Owner Admin Public URL Fix

## Scope and starting state

- Owner Admin Basic-only access is live and isolated on its dedicated hostname.
- The Emby node card incorrectly derived its link from `location.origin`, so
  the `uhd` node displayed an owner-admin URL that correctly returned 404.
- Production media remains on `stream.149077530.xyz`; policy remains auto with
  NOSLA active. The upstream address, DNS, ACME, failover policy, and media
  routing are outside this change.

## Read-only route discovery

- The legacy node is `uhd` and has no node secret.
- The retained managed route `v1` belongs to `v1-node`, not `uhd`; production
  stream `/s/v1/` returns 404, so it must not be inferred as the UHD URL.
- NOSLA's existing production location is the path-based upstream route below
  `/https/<upstream-host>/443/`. Its small `System/Info/Public` and
  `emby/System/Info/Public` endpoints return HTTP 200.
- NOSLA localhost proxy checks also confirm `/uhd` is not an existing route:
  proxy port 18080 returns 400 and Admin/managed-route port 18081 returns 404,
  while the existing path-based small information endpoint returns 200.
- Direct upstream and stream-proxied `/`, `/web/`, `/web/index.html`, and
  `/emby/web/index.html` return the same upstream/Cloudflare HTTP 404. This is
  an upstream Web UI limitation, not an owner-admin or stream Nginx failure.

## Implementation plan

- Add an HTTPS-origin-only `PUBLIC_MEDIA_BASE_URL` and a JSON node-to-path map.
- Reject credentials, path/query/fragment on the base, reject unsafe mapped
  paths, and reject using the owner-admin host as the media base.
- Return a backend-generated `publicUrl` in the Admin node list. Unmapped nodes
  receive no URL; the browser never falls back to the Admin origin.
- Display, copy, and preview only that URL. Never append node secret or query.
- Back up binary, root-only EnvironmentFile, unit, owner-admin Nginx file, and
  release link before restarting only the sidecar. Roll back on any failed
  post-apply gate.

## Verification plan

- Local targeted/full tests, vet, shell syntax, and diff checks.
- Owner Admin outer 401 and Basic-only authenticated UI/API behavior.
- Node-list JSON contains the configured stream URL for `uhd`, without a
  secret, query, or owner-admin hostname.
- Static UI contract confirms display/copy/preview consume only `publicUrl`.
- Small stream information endpoints return 200; no media object is requested.
- owner-admin `/uhd` and `/s/`, canary Admin, and stream Admin remain blocked.
- Sidecar stays loopback-only; failover remains auto/NOSLA and timer waiting.

## Current status

Status: DONE.

- Source/deployed commit: `f4d2d9d`; static Linux amd64 artifact provenance,
  checksum, targeted/full tests, vet, shell syntax, and diff checks passed.
- Isolated BWG dry-run used a temporary database and listener and returned the
  expected safe UHD public URL contract before any live mutation.
- Root-only verified backup:
  `/var/backups/embyproxy-owner-public-url/20260813T054045Z`.
- Rollback script:
  `/var/backups/embyproxy-owner-public-url/20260813T054045Z/rollback.sh`;
  checksums and `bash -n` pass.
- Live owner-admin returned 401 without Basic Auth and 200 with Basic Auth.
  Node-list JSON maps UHD to the configured stream host/path with no userinfo,
  query, fragment, or node secret. Static/runtime HTML checks confirm display,
  copy, and preview consume only `publicUrl`.
- owner-admin `/uhd` and `/s/`, canary Admin, and production stream Admin all
  return 404. Two small public information paths through stream return 200.
- Sidecar is active/enabled with `NRestarts=0` and only
  `127.0.0.1:18082`. Nginx config hash is unchanged and `nginx -t` passes.
- Failover remains mode auto, active NOSLA, `MANUAL_HOLD=none`, reason
  `nosla_healthy_below_threshold`; timer is active/waiting and legacy timer is
  inactive.
- Bounded sidecar/journal/owner-admin access scans contain no severe or
  credential markers and no recent owner-admin 502/503/504.
- No Nginx reload/change, upstream change, DNS action, ACME request, route
  mutation, media fetch, cleanup, or failover-policy mutation occurred.

The displayed address is a real public media/client base. Automated checks
prove its small Emby information endpoints are reachable. The upstream itself
continues to return Cloudflare 404 for common browser Web UI paths, both direct
and proxied; this external behavior was not misreported as a successful login
page and the upstream was not changed.
