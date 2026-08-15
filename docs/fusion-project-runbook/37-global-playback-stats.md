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
| C2 central store/API | DONE | Separate `/var/lib/embyproxy-gsy-sidecar/global-stats.db` stores source/path/status aggregates and safe snapshot rows; old `proxy.db` is untouched | Central store is authoritative for owner-admin | Keep DB permissions and backups |
| C3 collector/ingest | DONE | NOSLA backfill parsed 1016 lines with 0 drops, 2 PlaybackInfo, 733 VideoStream, 30 HTTP 206 and 1,637,611,584 response bytes; restricted sync imported 797 safe rows | Collector errors are isolated from proxy; both node timers are enabled | Owner playback validation |
| C4 admin API/UI | DONE | Deployed release `19039bd-stats`; API returns `central_stats_store`, real NOSLA/BWG rows and recent activity; sessions/duration/client remain unavailable | UI no longer treats BWG local zero as global zero | Owner playback validation |
| D owner playback validation | BLOCKED | Requires owner to play a small video; Codex must not synthesize media traffic | Await owner-triggered 20-second playback | Verify central record after owner signal |
| E failover rehearsal | BLOCKED | Real DNS mutation requires explicit owner confirmation | Prepare plan only | No production switch this phase |

## Deployment record

- Local implementation commit: `19039bd` on `feature/failover-phase2-local`; no force-push.
- BWG deployed release: `/opt/embyproxy-gsy-sidecar/releases/19039bd-stats`; `current` switched atomically from `b9e0ede`.
- BWG sidecar restarted once; `systemd` active, `NRestarts=0`, listener remains `127.0.0.1:18082`.
- BWG collector and NOSLA collector are installed at `/usr/local/sbin/embyproxy-stats-collector`; BWG sync is `/usr/local/sbin/embyproxy-stats-sync`.
- Enabled timers: `embyproxy-stats-collector-bwg.timer`, `embyproxy-stats-sync.timer`, and `embyproxy-stats-collector-nosla.timer`; first runs returned `Result=success`, `ExecMainStatus=0`.
- Existing failover timer remains `enabled/active`; legacy `stream-failover.timer` remains `disabled/inactive`.
- No DNS, Nginx media location, failover policy, ACME, or UHD upstream change was made.
- Backups and verified rollback scripts:
  - `/var/backups/embyproxy-global-stats/20260815T085500Z-bwg/rollback.sh`
  - `/var/backups/embyproxy-global-stats/20260815T085500Z-nosla/rollback.sh`
  - both passed `bash -n` before installation.

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
