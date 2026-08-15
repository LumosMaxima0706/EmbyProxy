# Global Playback Statistics

Date: 2026-08-15 Asia/Shanghai
Status: IN PROGRESS

## Safety boundary

- Preserve the working UHD/Yamby playback path and upstream.
- Do not switch production DNS, request ACME, clean releases, force-push, or
  expose credentials.
- Do not add query strings, cookies, Authorization headers, tokens, complete
  UUIDs, or subscription URLs to logs, SQLite, API responses, or runbooks.
- Collector failures must not affect proxy responses. Health checks remain
  small non-media requests.

## Step status

| Step | Status | Observation | Decision | Next action |
| --- | --- | --- | --- | --- |
| A1 static/API version | DONE | BWG serves release `b9e0ede`; `/admin` is `no-store`; API/UI contract passes | No cache-busting change required | Keep Basic Auth and no-store headers |
| A2/A3 failover UI | DONE | `activeTarget=NOSLA`, mode `auto`, hold `none`, one projected event; UI no longer shows `--` | State and events use policy state source | Recheck after stats changes |
| B1 current chain | DONE | NOSLA receives production playback; BWG SQLite is local-only and empty | Do not treat local zero as global zero | Design central collector |
| B2 log/schema design | DONE | Both nodes have query-free logs with URI, status, request/response bytes and timing | These fields are safe and sufficient for aggregate traffic/path metrics | Implement offline parser first |
| C1 parser | TODO | No central parser exists | Build parser with strict redaction and path classes | Add fixture tests |
| C2 central store/API | TODO | Admin reads BWG local store | Add source-node aggregate store only after parser tests | Stage schema and API |
| C3 UI | TODO | UI currently shows `未接入` when active node is NOSLA | Keep unavailable state until central data is trustworthy | Bind central response |
| D owner playback validation | BLOCKED | Requires owner to play a small video; Codex must not synthesize media traffic | Await owner-triggered 20-second playback | Verify central record after owner signal |
| E failover rehearsal | BLOCKED | Real DNS mutation requires explicit owner confirmation | Prepare plan only | No production switch this phase |

## Current chain

`Yamby -> stream Nginx -> NOSLA (current active target) -> old NOSLA sidecar/upstream -> NOSLA query-free access log`.

The owner-admin `stats.get` endpoint reads the BWG sidecar SQLite store. That
store is valid for requests handled by BWG, but it is not a global source and
therefore cannot represent current NOSLA traffic.

## Safe log contract

Both Nginx nodes already emit a query-free contract containing:

- ISO timestamp, host, method, URI path, status
- request length, bytes sent, upstream response time, request time

The collector may derive only bounded path classes (`PlaybackInfo`,
`SessionsPlaying`, `VideoStream`, `HLSManifest`, `HLSSegment`, `Image`,
`Subtitle`, `Health`, `Other`) and aggregate bytes/status/timing/Range hints.
It must never persist raw URI query, headers, cookies, tokens, or identifiers.

Client class, exact active session count, and exact playback duration remain
`未接入` until a separate authenticated event source can provide them without
logging sensitive values.

## Minimal implementation gate

1. Implement and test a parser against sanitized fixtures only.
2. Add a central store/API that records `source_node` and parser freshness.
3. Keep UI unavailable for fields whose source is stale or incomplete.
4. Deploy one node at a time with backup, restart only the affected sidecar,
   and verify public health plus admin isolation.
5. Owner triggers a small playback; verify aggregate record and no secret
   markers before enabling non-zero UI fields.

## Failover rehearsal plan (not executed)

- Record current DNS TTL and serving IP before the gate.
- In a separately approved window, run the existing policy runner in auto mode
  with NOSLA unhealthy simulation, then apply DNS only after explicit approval.
- Confirm DNS convergence, stream health, Emby login, and a small owner-triggered
  playback; verify BWG source-node stats.
- Restore NOSLA after health and fresh-cycle conditions pass; verify DNS,
  playback, and source-node aggregation.
- Any failed health, 5xx, admin isolation, or redaction check means immediate
  rollback via the existing BWG/NOSLA rollback scripts.
- Expected DNS-only impact is bounded by TTL plus resolver cache; exact impact
  must be measured during the approved rehearsal, not guessed now.
