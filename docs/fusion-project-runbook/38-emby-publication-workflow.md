# Emby server publication workflow

Date: 2026-08-15 Asia/Shanghai

## Safety boundary

- Keep production DNS, failover policy, UHD upstream, ACME and the working media path unchanged.
- Do not publish feimu during this phase.
- Publication accepts only an already-saved single HTTPS upstream. Credentials,
  query strings, fragments, private targets, owner-admin and the public entry
  host are rejected.
- Dry-run returns only redacted host/base-path shapes. It never returns the
  saved target or a complete playback URL.
- No arbitrary dynamic upstream route is permitted. Both edge results must be
  `synced` before the database route becomes enabled/public.
- Edge or database failure rolls back. Incomplete rollback becomes
  `needs_sync`; it is never reported as published and deletion stays blocked.

## Plan status

| Step | Status | Observation | Decision | Next action |
| --- | --- | --- | --- | --- |
| P1 baseline and architecture | DONE | UHD has a legacy public mapping; feimu is saved-only. Both edges use explicit host-path Nginx locations. The BWG sidecar is unprivileged and has no production edge mutation capability. | Keep legacy UHD read-only and add a publication state model. | Preserve current playback and isolation. |
| P2 storage/API orchestration | DONE | `emby_publications` stores state, failure step and per-edge sync state. Authenticated status, dry-run, publish, unpublish and verify APIs are implemented. | Stage the managed route disabled/non-public, require both edges, then commit. | Runtime edge adapter remains fail-closed until separately installed. |
| P3 owner UI | DONE | Node cards show upstream, publication and NOSLA/BWG states, and expose publish/dry-run/unpublish/copy/verify actions according to state. | Emby server management is the normal owner workflow; managed routes remain advanced. | Deploy only after backup and production dry-run gates. |
| P4 orphan prevention | DONE | Published or uncertain nodes cannot be renamed, retargeted, imported-over or deleted. Partial publish invokes edge cleanup and removes the staged route; failed cleanup is `needs_sync`. | Require unpublish/reconciliation before delete. | Verify against an isolated production DB copy. |
| P5 local verification | DONE | Targeted tests, `go test ./...`, `go vet ./...` and `git diff --check` pass on the isolated BWG test tree. | Code gate passes. | Build a commit-tagged candidate. |
| P6 feimu dry-run | DONE | Authenticated production dry-run returned the redacted HTTPS/443 plan. Publication rows and feimu route rows remained zero; DB logical state, sidecar env and BWG Nginx hashes were identical before/after. | Dry-run is non-mutating. | Do not call publish. |
| P7 restricted edge adapter | DONE | The original `edge_sync_unavailable` was caused by a nil runtime `PublicationSyncer`: the socket/config/agent were absent. A root-owned agent, BWG helper and NOSLA forced-command helper are now installed and readiness passes. | Keep the public API fail-closed and require a separate owner confirmation before calling `publish` for feimu. | Run only the owner-approved feimu publication gate. |

## Implemented contract

- `GET /api/admin/emby-servers/{slug}/publish-status`
- `POST /api/admin/emby-servers/{slug}/publish/dry-run`
- `POST /api/admin/emby-servers/{slug}/publish`
- `POST /api/admin/emby-servers/{slug}/unpublish`
- `POST /api/admin/emby-servers/{slug}/verify-proxy`

Publication states:

- `saved_unpublished`
- `publishing`
- `published`
- `unpublishing`
- `publish_failed`
- `needs_sync`

The public client address has no trailing slash. Its redacted shape is:

```text
https://stream.149077530.xyz/https/<saved-host>/443
```

If the saved upstream has a base path, it is retained but redacted in dry-run
as `<saved-base-path>`.

## Owner workflow

1. Save one HTTPS upstream in Emby server management and run `检测上游`.
2. Run `查看 dry-run`; confirm the redacted path shape and both edge plans.
3. Click `发布反代`. The API stages the DB route, asks the restricted adapter
   to sync both edges, then enables the route only after both report success.
4. Copy the generated stream address to Yamby and use `检测反代` for a small
   `/System/Info/Public` check.
5. Click `取消发布` to remove only the public mapping/edge fragment while
   retaining the saved upstream. A node with `needs_sync` must be reconciled
   before deletion.

The managed-route page remains an advanced/debug view. Owners do not need to
manually edit managed routes, node-path JSON, Nginx locations or allowlists.

## Adapter troubleshooting

Reproduce with the authenticated `publish/dry-run` endpoint first. Inspect the
publication status row (`status`, `reason`, `failed_step`, and per-edge states),
the sidecar journal, the `embyproxy-publication-agent.service` journal, and the
two edge helper results. A dry-run can pass its route plan while readiness is
false; that means apply is still correctly fail-closed.

Common codes and the owning step:

| Code | Meaning |
| --- | --- |
| `edge_sync_unavailable` | No registered socket syncer in the sidecar release |
| `edge_adapter_unreachable` | Socket path or agent service unavailable |
| `edge_sync_denied` | Unix peer UID or forced-command trust check failed |
| `edge_sync_partial` | One edge applied and the other failed; rollback/cleanup state is reported |
| `edge_adapter_response_invalid` | Helper returned malformed/unsupported JSON |
| `route_conflict` | Existing slug fragment does not match the saved node |
| `nginx_test_failed` | Candidate or host Nginx syntax check failed |
| `reload_failed` | Graceful reload failed; helper attempts rollback |
| `rollback_failed` | Manual reconciliation is required; state is never reported published |
| `upstream_not_saved` / `upstream_invalid` | The agent re-read the DB and rejected the saved node |

Readiness checks are read-only: adapter registration, socket permissions,
saved-node validation, both edge reachability, include-hook presence and
current Nginx syntax. They never write a route or request a media resource.

## Security and rollback boundary

- Only an existing saved node can be published; the API never accepts a host
  override. Private, owner-admin and public-entry targets are rejected.
- The root agent has no public HTTP listener. The sidecar socket is loopback
  filesystem-only and restricted to the sidecar UID. NOSLA accepts only the
  pinned forced-command key.
- Each edge writes only its slug fragment below the publication include
  directory. Query strings, tokens, cookies, Authorization headers, complete
  UUIDs and complete playback URLs are not logged or returned.
- `stream /admin` and owner-admin media paths remain blocked; UHD's legacy
  fragment is never rewritten by a feimu operation.
- Roll back in this order: stop the publication agent, run the edge hook
  rollback scripts if the include hook itself must be removed, restore the
  root-only agent/service/env backup, run `nginx -t`, reload Nginx, then
  restart the sidecar. Restore the DB only with the explicit existing
  `--restore-db` disaster-recovery option.

## Verification evidence

- Dry-run creates no publication row and no managed route.
- Publish without an edge adapter returns `publish_failed / edge_sync` and
  creates no managed route.
- Partial edge success is rolled back; successful cleanup leaves both edge
  states removed and no route.
- Failed cleanup returns `needs_sync` and blocks node deletion.
- A pre-existing managed route is not overwritten.
- A published node cannot change name or upstream until it is unpublished.
- Legacy UHD cannot be unpublished or deleted through this workflow until it
  has been explicitly migrated.
- The owner-admin origin is never used to synthesize a media address.

## Deployment and rollback gate

Before deployment, capture the current binary/release link, central proxy DB,
sidecar environment and unit, BWG stream Nginx configuration, and publication
state. Generate a root-only rollback script that restores the old DB and
release, validates Nginx, and restarts only the sidecar if necessary.

After deployment, run the feimu dry-run and prove zero changes to the production
database, managed routes, sidecar environment, Nginx hashes and DNS. UHD must
remain published and its address unchanged. Failover must remain auto/NOSLA
with manual hold none. No cleanup is part of this phase.

## 2026-08-15 production UI/API deployment

- Deployed commit: `0c52667`.
- Release: `/opt/embyproxy-gsy-sidecar/releases/0c52667-publication`.
- Backup root: `/var/backups/embyproxy-publication/20260815T091800Z-bwg`.
- Rollback script:
  `/var/backups/embyproxy-publication/20260815T091800Z-bwg/rollback.sh`.
- The backup checksum verification and rollback `bash -n` passed. The default
  rollback preserves the live database; `--restore-db` is an explicit disaster
  recovery option.
- Sidecar is active/enabled with zero restarts after deployment and still has
  exactly one `127.0.0.1:18082` listener and no non-loopback listener.
- `emby_publications` exists and has zero rows. The existing managed route count
  remains one; feimu has zero managed-route rows.
- The live UI contains the publication/status/dry-run/unpublish workflow. UHD
  is `published` with its prior public address. Feimu is
  `saved_unpublished`, has no public address, and both edges are
  `not_configured`.
- The feimu dry-run shape is HTTPS on the stream hostname with a redacted saved
  host and effective port 443. It declares managed route/line, public mapping,
  BWG edge, NOSLA edge and redirect-host allowlist steps. It did not disclose
  the saved host or upstream URL.
- Pre/post dry-run fingerprints matched: managed/publication DB logical state,
  sidecar environment and BWG stream Nginx configuration. No sidecar restart,
  Nginx reload, DNS update or edge route mutation occurred for the dry-run.
- Isolation checks: unauthenticated owner-admin `/admin` returned 401;
  authenticated `/admin` returned 200; owner-admin `/uhd` and `/s/`, stream
  `/admin`, and canary `/admin` returned 404. Stream health and the retained
  canary public-info small endpoint returned 200.
- Failover remains `auto`, active target `nosla`, manual hold `none`; the new
  timer remains active/enabled and the legacy timer inactive/disabled.
- Central playback statistics remain available with non-empty rows.
- Bounded Admin access-log scans found no query marker, credential/header
  marker or complete UUID. Sidecar journal scans found no panic/fatal or
  502/503/504 marker.
- Both edges still have no `proxy_cache_path`, enabled `proxy_cache`, background
  update, slice, prefetch, preload or warmup directive. Streaming locations
  retain buffering-off, request-buffering-off, Range and If-Range behavior.
- No ACME request, cleanup, force push, DNS switch, failover change, UHD target
  change or feimu publication was performed.

## 2026-08-15 restricted adapter remediation

The owner-triggered publish was reproduced through the authenticated API. The
request reached the sidecar, entered `publishing`, and failed before any route
row or edge mutation with `edge_sync_unavailable`. The cause was confirmed in
source and runtime: `PublicationSyncer` existed as an interface, but
`cmd/embyproxy` had no runtime registration and the publication-agent socket
was not configured in the service environment. This was not a browser error.

The adapter now has two privilege domains:

1. The sidecar sends only action, slug and operation id over a loopback Unix
   socket. The root agent re-reads the saved node and staged DB route and
   rejects all caller-supplied hosts.
2. The root agent invokes a fixed BWG helper and a fixed NOSLA SSH
   forced-command helper. Each helper accepts only a JSON manifest on stdin,
   writes one slug-specific Nginx fragment, runs a candidate syntax test,
   atomically applies it, runs the host `nginx -t`, reloads gracefully, and
   rolls back on failure. No generic shell is exposed.

The one-time empty include hooks were installed after backups and syntax
checks. They do not contain a feimu fragment. Current backup/rollback paths:

- BWG infrastructure: `/var/backups/embyproxy-publication/20260815T115500Z-bwg-agent`
- NOSLA infrastructure: `/var/backups/embyproxy-publication/20260815T115500Z-nosla-agent`
- BWG hook rollback: `/var/backups/embyproxy-publication-agent/20260815T114625Z-bwg-hook/rollback.sh`
- NOSLA hook rollback: `/var/backups/embyproxy-publication-agent/20260815T114627Z-nosla-hook/rollback.sh`
- BWG agent config: `/etc/embyproxy-publication-agent/config.json` (0600)
- BWG socket: `/run/embyproxy-publication-agent/agent.sock` (0660,
  root:embyproxy-gsy-sidecar)

The dedicated NOSLA key is root-only on BWG and is authorized on NOSLA only
with the fixed edge-helper command. Its known-host entry is pinned. No key
material is recorded here.

Readiness was verified through the real dry-run API without changing the
database: `status=dry_run_ok`, `adapter_ready=true`, and both edge results were
`ready`. The dry-run returned only the redacted path shape. `emby_publications`
still has no feimu row, the managed-route count is unchanged, and both
publication include directories contain no `.conf` route. The actual publish
endpoint was not called; owner confirmation is still required for that gate.

During readiness debugging two implementation issues were fixed and recorded:

- the hook installer initially counted the `server_name` line as a closed
  block; the script was corrected and both Nginx files were re-tested;
- the helper initially passed `-p /` to Nginx, hiding the configured module
  prefix and causing a false GeoIP module failure. The helper now preserves
  the host prefix, and the real service `nginx -t` passes on both edges.

Failure codes are surfaced in the owner UI with the failed step, including
adapter unreachable/denied, NOSLA or BWG edge failure, route conflict,
`nginx_test_failed`, `reload_failed`, `rollback_failed`, DB-stage errors and
partial sync. A `needs_sync` record is never shown as published and blocks
unsafe deletion until reconciliation.
- Git delivery note: the local feature history is a fast-forward of the origin
  feature branch. A normal push first failed during the GnuTLS handshake; a
  normal retry and a one-command HTTP/1.1 retry then received no remote response
  and were terminated. No force push, remote/auth edit or alternate publisher
  was used. The deployed commit remains present locally and on BWG; origin is
  not claimed as updated.

## Recovering failed publication state

`PUBLICATION_REQUIRES_RECONCILIATION` means the saved workflow state and the
slug-scoped production artifacts must be compared before another publish. It
does not by itself mean an edge route exists.

The API classifies the result as one of two conditions:

- `stale_failed_state_only`: publication state is `publish_failed` or
  `needs_sync`, while the slug has no managed route, no managed route lines, no
  public URL, and both edge states are `not_configured` or `removed`. This is a
  state-only residue and may be normalized safely to `saved_unpublished`.
- `partial_artifacts_exist`: at least one slug-scoped DB route/line, public URL,
  or edge artifact state remains. Publishing is blocked until the residue is
  removed and verified.

Owner-facing recovery endpoints are authenticated Admin APIs and accept no
upstream host in the request:

```text
POST /api/admin/emby-servers/{slug}/publish/reconcile
POST /api/admin/emby-servers/{slug}/publish/cleanup
POST /api/admin/emby-servers/{slug}/unpublish
```

`publish/reconcile` is read/normalize-only. It clears a stale failed row when
there are no artifacts; if partial artifacts exist it returns the inventory and
does not mutate an edge. `publish/cleanup` is scoped to exactly one saved slug.
It removes only that slug's edge fragments and managed route, running the
restricted helper's Nginx test, atomic apply/revert and graceful reload flow.
There is no global cleanup endpoint.

Unpublish is idempotent. Calling it for an unpublished server with no artifacts
returns success with `no_publication_to_unpublish`; it must not call the edge
helper and must not return `edge_unpublish_failed`. An empty helper reason or
step is mapped to `helper_failed` and `edge_cleanup` rather than `unknown`.

### Owner UI recovery

For state-only residue, use **Clear failed state**. For an artifact inventory,
use **Clean publication residue**. After success the row returns to
`saved_unpublished`, both edges show not configured, the public URL remains
empty, and **Publish proxy** becomes available. The upstream server itself is
retained.

The owner may self-service stale-state reconcile, single-slug cleanup,
idempotent unpublish and dry-run. Stop for operator review when artifacts from
another slug would be changed, the restricted helper reports rollback failure,
Nginx cannot be restored, or the inventory cannot determine whether an edge
fragment exists.

### `edge_unpublish_failed` with an empty step

This legacy symptom means unpublish called the edge adapter without first
checking whether a publication existed, then lost the helper's empty reason and
step during response mapping. Check the publication row, managed route count,
route-line count, public URL presence, and both slug-specific edge fragment
directories. If all are absent, reconcile the row; do not execute edge cleanup.
If any are present, use the cleanup endpoint and retain its exact failed step.

The canonical state-only recovery case is:

```text
publish_failed or needs_sync
+ managed_routes=0
+ managed_route_lines=0
+ edge fragments=none
+ public_url empty
=> reconcile to saved_unpublished
```

## `nosla_edge_sync_failed` troubleshooting

A readiness dry-run proves that the sidecar can reach the restricted agent,
both helpers can validate their include hooks, and the current host Nginx
configuration passes syntax validation. It does not write a fragment, perform
an atomic replace, reload Nginx, or prove that the publish rollback path works.
Only an authenticated production publish exercises those stages.

Start with the request ID from the sidecar access/publication log. Correlate it
with the publication-agent journal, which records only action, slug, operation
ID, edge status, error code and failed step. It must never log a host, URL,
query, cookie or authorization header. On NOSLA, locate the request-ID-specific
directory below `/var/backups/embyproxy-publication-agent`; the presence of
`candidate.conf` and `nginx-candidate.conf` shows that the helper reached the
candidate-test stage. An empty directory means the operation stopped before
candidate creation or was an idempotent unpublish.

Check the following in order:

1. The BWG agent socket and service are active, and the sidecar peer UID matches
   the root-only agent configuration.
2. The dedicated SSH key is 0600, its known-host entry is pinned, and the NOSLA
   authorized key still has the exact forced command. Do not grant a shell.
3. BWG and NOSLA helper binary hashes match. Edge config is root-only 0600, the
   include directory exists, and the stream config contains the expected fixed
   include hook.
4. Inspect the safe agent fields `nosla_error` and `nosla_step`. Run the saved
   candidate with `nginx -t -c` only after redacting host/path details from any
   operator output. Then run the normal host `nginx -t`.
5. Confirm whether the target fragment exists and whether Nginx reload completed
   in systemd. A successful helper uses candidate test, atomic replace, host
   test, and graceful reload in that order.

Publish, automatic rollback, explicit cleanup and retry may share one request
ID. Their backup directories must nevertheless be unique by action and random
suffix. Reusing a seconds-resolution directory causes `backup_failed`, can
prevent rollback, and can overwrite the first edge error with a misleading
partial-sync status.

The BWG and NOSLA stream configurations use different pre-existing WebSocket
connection variables: BWG uses `$stream_bwg_connection_upgrade`, while NOSLA
uses `$stream_connection_upgrade`. The generated fragment must select the
variable for its edge. A standalone candidate test must define both variables;
otherwise it can pass while the production host test fails with an unknown
variable. If BWG fails before NOSLA is attempted, the response must report the
BWG error and stage, not `nosla_edge_sync_failed`.

Partial publication exists if any one of the slug's managed route, route lines,
public URL, BWG fragment or NOSLA fragment remains. Use only
`POST /api/admin/emby-servers/{slug}/publish/cleanup`; never remove the global
publication directory. After cleanup, verify both fragments are absent, the
slug has zero route rows, and both host Nginx tests pass.

To protect existing servers, fingerprint the sidecar environment and total
managed-route counts before the operation, run the existing published server's
small `/System/Info/Public` check before and after, and confirm its public URL
mapping was unchanged. Do not perform a media smoke test automatically.
