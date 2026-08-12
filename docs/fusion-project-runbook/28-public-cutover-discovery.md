# Public Cutover Discovery

Status: PHASE 1 COMPLETE - OWNER DECISION REQUIRED

Discovery date: 2026-08-12 (Asia/Shanghai)
Branch/ref: `feature/failover-phase2-local` / `2953dabe`

## Scope and safety boundary

This discovery was read-only. No DNS, Nginx, rathole, systemd, firewall,
database, service, or public-traffic mutation was performed. Secrets, tokens,
cookies, private keys, complete query strings, and complete URLs are omitted.

## BWG runtime topology observed

- `nginx.service`: active and enabled; `nginx -t`: PASS.
- `rathole.service`: active and enabled; server config is
  `/etc/rathole/server.toml`, unit is `/etc/systemd/system/rathole.service`.
- `embyproxy-gsy-sidecar.service`: active/running and enabled, `NRestarts=0`,
  main status `0`, loopback listener `127.0.0.1:18082`.
- Sidecar paths: `/etc/embyproxy-gsy-sidecar/embyproxy.env` (mode 0600),
  `/opt/embyproxy-gsy-sidecar/current`, `/var/lib/embyproxy-gsy-sidecar`, and
  `/var/log/embyproxy-gsy-sidecar`.
- Public listeners are Nginx on ports 80/443. Rathole listens on its configured
  public transport port and maps existing named services; the sidecar is not a
  rathole service and is not publicly reachable directly.

## Existing Nginx entry topology

The existing production stream server blocks are in:

- `/etc/nginx/conf.d/stream-b-proxy.conf`: `stream.149077530.xyz` and
  `stream-b.149077530.xyz`; current upstream is the existing stream service on
  `127.0.0.1:18080`.
- `/etc/nginx/conf.d/stream-proxy-admin-locations.inc`: `/admin/`,
  `/api/admin/`, and `/s/` currently route to `127.0.0.1:18081`.
- `/etc/nginx/conf.d/embyproxy-phase2-staging.conf`: isolated staging host
  `staging-stream.149077530.xyz`, currently routes to `127.0.0.1:19080` and
  explicitly returns 404 for `/api/admin` and `/api/admin/`.
- `/etc/nginx/conf.d/stream-failover-web.conf`: separate failover control
  entry, currently routes to an existing service on `127.0.0.1:8787`.

The architecture runbook describes `admin.149077530.xyz` as a BWG management
entry and `stream.149077530.xyz` as the public data-plane name, but the live
Nginx inventory does not contain an `admin.149077530.xyz` server name. This
must not be resolved by guessing or by silently changing an existing block.

## Required changes (not executed)

The likely minimum change is a new dedicated Nginx location/server block that
routes only an owner-selected public media path to `127.0.0.1:18082`, with
explicit admin/API exposure policy and no direct public binding of port 18082.
Depending on the selected DNS model, a DNS record or provider failover object
may also be required. A rathole change is not currently indicated: the sidecar
already runs locally on BWG and should not be exposed through the existing
rathole service unless the owner explicitly chooses that topology.

## Blocking owner decisions

Phase 1 cannot safely advance to backup or cutover until the owner supplies:

1. The exact public business hostname/path to expose (for example, a new
   dedicated hostname versus an existing `stream` hostname).
2. Whether public clients may reach only media routes, or also the Admin UI;
   the safe default is media-only and deny `/admin/` and `/api/admin/`.
3. The DNS provider/control-plane method and authorization for a dry-run/apply,
   including the intended target/TTL/failover policy, without sending secrets
   to this chat.
4. Whether the intended public target is BWG-only for a staged canary or the
   broader NOSLA-primary/BWG-fallback failover design. NOSLA remains out of
   scope for this run until separately authorized.

Until these are answered, public entry, DNS, Nginx, and rathole remain
unchanged.
