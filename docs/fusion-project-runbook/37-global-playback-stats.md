# Global Playback Statistics

Date: 2026-08-15 Asia/Shanghai
Status: DEPLOYED - OWNER PLAYBACK VALIDATION PENDING

## Safety boundary

- Preserve the working UHD/Yamby playback path and upstream.
- Do not switch production DNS, request ACME, clean releases, force-push, or expose credentials.
- Do not add query strings, cookies, Authorization headers, tokens, complete UUIDs, or subscription URLs to logs, SQLite, API responses, or runbooks.
- Collector failures must not affect proxy responses. Health checks remain small non-media requests.

## Step status

| Step | Status | Observation | Decision | Next action |
| --- | --- | --- | --- | --- |
| A1 static/API version | DONE | BWG serves release `b9e0ede`; `/admin` is `no-store`; API/UI contract passes | No cache-busting change required | Keep Basic Auth and no-store headers |
| A2/A3 failover UI | DONE | `activeTarget=NOSLA`, mode `auto`, hold `none`; UI no longer shows `--` | State and events use policy state source | Recheck after stats changes |
| B1 current chain | DONE | NOSLA receives production playback; BWG SQLite was local-only and empty | Do not treat local zero as global zero | Design central collector |
| B2 log/schema design | DONE | Both nodes have query-free logs with URI, status, request/response bytes and timing | Safe bounded path classes are sufficient for aggregate metrics | Implement offline parser |
| C1 parser | DONE | Strict parser accepts the existing bare-timestamp query-free Nginx format and rejects query/control data | Parser returns safe path class, status, bytes, timing and 206 hint | Cross-node summary transport |
| C2 central store/API | DONE | Separate `/var/lib/embyproxy-gsy-sidecar/global-stats.db` stores timestamp/source/path/status aggregates plus empty hash contract columns; old `proxy.db` is untouched | Central store is authoritative for owner-admin | Keep DB permissions and backups |
| C3 collector/ingest | DONE | NOSLA backfill parsed 1016 lines with 0 drops, 2 PlaybackInfo, 733 VideoStream, 30 HTTP 206 and 1,637,611,584 response bytes; restricted sync imported 797 safe rows | Collector errors are isolated from proxy; both node timers are enabled | Owner playback validation |
| C4 admin API/UI | DONE | Deployed release `19039bd-stats`; API returns `central_stats_store`, real NOSLA/BWG rows and recent activity; sessions/duration/client remain unavailable | UI no longer treats BWG local zero as global zero | Owner playback validation |
| D owner playback validation | BLOCKED | Requires owner to play a small video; Codex must not synthesize media traffic | Await owner-triggered 20-second playback | Verify central record after owner signal |
| E failover rehearsal | BLOCKED | Real DNS mutation requires explicit owner confirmation | Prepare plan only | No production switch this phase |

## Deployment record

- Initial central stats implementation: `19039bd`; schema-contract follow-up: `bbd971f`; no force-push.
- BWG deployed release: `/opt/embyproxy-gsy-sidecar/releases/19039bd-schema`; `current` switched atomically from `19039bd-stats` (schema migration release).
- BWG sidecar restarted once; `systemd` active, `NRestarts=0`, listener remains `127.0.0.1:18082`.
- BWG collector and NOSLA collector are installed at `/usr/local/sbin/embyproxy-stats-collector`; BWG sync is `/usr/local/sbin/embyproxy-stats-sync`.
- Enabled timers: `embyproxy-stats-collector-bwg.timer`, `embyproxy-stats-sync.timer`, and `embyproxy-stats-collector-nosla.timer`; first runs returned `Result=success`, `ExecMainStatus=0`.
- Existing failover timer remains `enabled/active`; legacy `stream-failover.timer` remains `disabled/inactive`.
- No DNS, Nginx media location, failover policy, ACME, or UHD upstream change was made.
- Backups and verified rollback scripts:
  - `/var/backups/embyproxy-global-stats/20260815T085500Z-bwg/rollback.sh`
  - `/var/backups/embyproxy-global-stats/20260815T085500Z-nosla/rollback.sh`
  - schema migration backups: `/var/backups/embyproxy-global-stats/20260815T093000Z-bwg/rollback.sh` and `/var/backups/embyproxy-global-stats/20260815T093000Z-nosla/rollback.sh`.
  - all rollback scripts passed `bash -n` before installation.
- Rollback audit found and fixed missing `current` symlink/central-DB restoration in the original generator. The latest schema rollback scripts are mode 0700, restore the pre-schema collector/sync binaries, restore the BWG DB and release target, and passed `bash -n`; they were not executed.
- Live schema inspection confirms `timestamp`, `session_hash`, `item_hash`, `user_hash`, and `device_hash` columns; hash values remain empty because query-free logs do not provide safe identity data.

## Post-deploy verification

- Authenticated owner-admin `stats.get`: `stats_source=central_stats_store`, `stats_available=true`, two aggregate rows (NOSLA and BWG), and 20 safe recent activities.
- Aggregate sample: NOSLA `plays=2`, `outbound_bytes=1,637,620,409`; BWG `plays=0`, `outbound_bytes=171,475`. These are access-log aggregates, not a claim of a live owner playback session.
- Unsupported dimensions are explicit: `sessions=false`, `duration=false`, `client_class=false`; UI renders `unavailable` for these fields.
- Isolation matrix: canary `/admin=404`, owner-admin `/uhd=404`, owner-admin `/s/=404`, stream `/admin=404`, canary `/s/v1/System/Info/Public=200`.
- Owner-admin authenticated `/admin=200`; sidecar remains loopback-only.
- Public stream root probe returned `403` because the root is not an authenticated Emby health endpoint; the approved canary small endpoint remains `200`. No media-path change was made.
- No query strings, tokens, cookies, Authorization values, complete UUIDs, or full playback URLs were printed or persisted by the collector/store.

## Current chain

`Yamby -> stream Nginx -> NOSLA (current active target) -> NOSLA query-free access log -> local safe snapshot -> restricted meter -> BWG central stats.db -> owner-admin API/UI`.

BWG requests are parsed directly from its query-free access log into the same central store. The former BWG-only `proxy.db` is not used for global playback statistics.

## Safe log contract

Both Nginx nodes emit a query-free contract containing ISO timestamp, host, method, URI path, status, request length, bytes sent, upstream response time, and request time.

The collector derives only bounded path classes (`PlaybackInfo`, `SessionsPlaying`, `VideoStream`, `HLSManifest`, `HLSSegment`, `Image`, `Subtitle`, `Health`, `Other`) and aggregate bytes/status/timing/Range hints. It never persists raw URI query, headers, cookies, tokens, or identifiers.

Client class, exact active session count, and exact playback duration remain unavailable until a separate authenticated event source can provide them without sensitive logging.

## Owner validation gate

1. Owner plays a small video for 20 seconds through the existing stream URL.
2. Wait for the next NOSLA collector and BWG sync timer; do not force a media request.
3. Query owner-admin stats and verify a new NOSLA aggregate, outbound bytes, and a recent safe activity.
4. Confirm playback remains healthy and no sensitive marker appears in logs, API, SQLite, or UI.

## Failover rehearsal plan (not executed)

- Record current DNS TTL and serving IP before the gate.
- In a separately approved window, simulate NOSLA unhealthy in dry-run, then apply DNS only after explicit approval.
- Confirm DNS convergence, stream health, Emby login, and a small owner-triggered playback; verify BWG source-node stats.
- Restore NOSLA after health and fresh-cycle conditions pass; verify DNS, playback, and source-node aggregation.
- Any failed health, 5xx, admin isolation, or redaction check means immediate rollback via the existing rollback scripts.

## 2026-08-15 traffic-bytes reconciliation and publication-state gate

### Plan status

| Step | Status | Observation | Decision | Next action |
| --- | --- | --- | --- | --- |
| R1 query-free log | DONE | NOSLA log has `request_length`, `bytes_sent`, request/upstream timing, HTTP 200/206 and classified playback paths | Use request length for inbound and bytes sent for outbound; do not add query/header logging | Keep query-free format |
| R2 parser | DONE | Backfill parsed 2,416 safe lines with zero drops and produced 11,077,932,286 outbound bytes; a later cursor dry-run also produced nonzero outbound bytes | Parser is not the zero-byte point | Keep 200, 206 and Range handling |
| R3 central store | DONE | Direct SQLite totals and per-node rows contain nonzero inbound/outbound values and 206 rows | Central ingest is not the zero-byte point | Keep central store authoritative |
| R4 Admin API | DONE | API snake_case totals exactly matched SQLite totals | API/store transport is correct | Preserve API compatibility |
| R5 Admin UI | DONE | UI read camelCase only while the central API returns snake_case | Add camelCase/snake_case compatibility at the formatting boundary | Owner browser refresh only if an old tab remains open |
| P1 publication model | DONE | UHD is fully published; feimu is only a saved upstream and has no public mapping or edge route | Expose saved/published state explicitly; do not auto-publish | Await owner approval before feimu publication |

### Before-fix byte accounting

- NOSLA safe log sample: 2,421 lines, 480,367 request bytes and 11,142,192,601 response bytes; 99 HTTP 206 responses, 2,089 VideoStream classifications and 14 PlaybackInfo requests.
- Parser dry-run: 2,416 parsed, zero dropped, 14 PlaybackInfo, 1,654 VideoStream, 98 partial responses and 11,077,932,286 outbound bytes.
- Central SQLite: 533,996 inbound bytes, 11,142,870,915 outbound bytes and 1,704 rows. NOSLA contributed 480,212 inbound and 11,142,191,582 outbound bytes.
- Admin API returned the same nonzero snake_case fields.
- UI rendered `0B` because it read `inboundBytes`/`outboundBytes`, not `inbound_bytes`/`outbound_bytes`.

### Minimal fix and deployment

- Commit `e80a26c` adds UI compatibility for snake_case/camelCase stats fields and explicit node publication-state fields.
- Admin nodes now return `publicUrlStatus` and `publicUrlReason`:
  - configured public mapping: `published / public_entry_configured`;
  - saved upstream without public mapping: `saved_unpublished / no_edge_route_configured`.
- The Admin UI says that a saved-only node has not been published to stream. It does not synthesize an owner-admin media URL and does not offer a false usable address.
- BWG release: `/opt/embyproxy-gsy-sidecar/releases/e80a26c-admin-publish`.
- Backup: `/var/backups/embyproxy-global-stats/20260815T144500Z-bwg`.
- Rollback: `/var/backups/embyproxy-global-stats/20260815T144500Z-bwg/rollback.sh`; syntax check passed and rollback was not executed.
- Deployment restarted only the loopback sidecar. No DNS, Nginx, media route, collector, failover policy, ACME, or upstream change occurred.

### Post-fix reconciliation

- Direct central store snapshot: 558,557 inbound bytes, 11,966,492,331 outbound bytes, 1,776 aggregate rows and 60 aggregate rows marked HTTP 206.
- Per-node store snapshot: NOSLA 498,721 inbound / 11,965,764,058 outbound; BWG 59,836 inbound / 728,273 outbound.
- Admin API totals and both node totals exactly matched SQLite at verification time.
- The deployed Admin HTML contains the snake_case byte compatibility code; the API reports nonzero totals, so both top totals and per-node rows now use the same central values.
- A later NOSLA collector dry-run parsed one new VideoStream event with nonzero outbound bytes without mutating the cursor or store.
- This verifies live byte flow through log, parser, store, API and UI mapping. It does not attribute the observed increase to a specific owner playback session unless the owner supplies the playback time window.

### Runtime and isolation verification

- Deployed build reports commit `e80a26c`; sidecar is active with zero restarts and listens only on `127.0.0.1:18082`.
- Owner Admin unauthenticated `/admin` returned 401; Basic Auth returned 200.
- Owner Admin media paths returned 404; canary and stream Admin paths returned 404.
- Canary and production small `System/Info/Public` checks returned 200 with a compatible non-media client user agent.
- Failover remains mode `auto`, active target `NOSLA`, manual hold `none`; the new timer remains active/enabled and the legacy timer inactive/disabled.
- Owner Admin access-log and sidecar journal redaction scans passed. No credential/header/query data was persisted or printed.

## Current production model

```text
Yamby public media address
  -> stream DNS
  -> active edge selected by failover (NOSLA now, BWG fallback)
  -> explicit edge Nginx/sidecar route and upstream allowlist
  -> saved Emby upstream server
  -> allowlisted playback redirect hosts and response rewriting
  -> client
```

- NOSLA/BWG are edge/data-plane nodes. Failover changes which edge the stream hostname resolves to; it does not create or delete an Emby upstream.
- UHD and feimu are Emby upstream servers saved in the Admin node store.
- A managed/public route is a separate publication object that makes an allowlisted upstream reachable through the stream hostname.
- Saving an Emby server currently does not publish it automatically.
- UHD works because it has all publication layers: saved node, public node-path configuration, managed route, explicit NOSLA/BWG edge locations, and required redirect-host handling.
- Feimu currently has only the saved node. It has no public node-path mapping, managed route, or NOSLA/BWG edge preparation, so its correct UI state is saved but unpublished.

### Feimu dry-run publication plan (not applied)

- Proposed slug: `feimu`.
- Scheme and effective port: HTTPS/443.
- Public path shape: `/https/<saved-feimu-host>/443`, without a trailing slash in the recommended client address.
- Required gates: create the managed route, add the explicit public node-path mapping, prepare allowlisted routes on both NOSLA and BWG, discover and allow only required playback redirect hosts, back up both edges, and validate rollback plus small non-media health checks.
- Do not open arbitrary dynamic upstream hosts. Do not publish on the owner-admin hostname. Do not apply any part of this plan until the owner explicitly approves production publication.

## Product answer

The current version is not "save an Emby server and it is automatically public through failover." The missing step is explicit publication to both edge nodes. The target product should keep save and publish as separate visible states, then provide a guarded publish workflow that prepares both edges before returning a stream address. UHD has that previously configured publication stack; feimu does not.
