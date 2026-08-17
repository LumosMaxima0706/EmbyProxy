# Emby server publication workflow

Date: 2026-08-15 Asia/Shanghai

## Safety boundary

- Keep production DNS, failover policy, UHD upstream, ACME and the working media path unchanged.
- Publication accepts one to sixteen already-saved HTTPS upstream lines. Credentials,
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
https://<stream-host>/https/<saved-host>/443
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

## Deployment acceptance record

For each deployment, store the commit/tag, release directory, root-only backup
directory and rollback script in the operator's private change record, not in
Git. The checked-in runbook records only the repeatable gate:

- sidecar listens only on its configured loopback address;
- publication schema and managed-route schema migrate additively;
- dry-run changes no database row, fragment, DNS record or service state;
- owner-admin and stream virtual-host isolation checks pass;
- failover policy, active target and manual hold are unchanged;
- both edges pass `nginx -t` and keep streaming buffering/cache disabled;
- query-free logs contain no credential/header material or full identifiers.

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

Install the empty include hooks only after a root-only backup and syntax check.
The installer creates an operation-specific rollback directory below
`/var/backups/embyproxy-publication-agent/`; retain its path in the private
deployment record. The BWG agent config is root-only and its Unix socket is
group-readable only by the sidecar service account.

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

### Fragment synced but HTTPS returns 403

The stream configuration has separate port 80 and TLS serving blocks. The
publication include must appear in both blocks. A historical hook installer
stopped after finding the directive once and inserted it only in the port 80
redirect/challenge block. In that state the edge helper, fragment write,
`nginx -t`, reload and UI sync state all succeed, but TLS requests never see the
slug fragment and fall through to the stream server's intentional 403 default.

Diagnose this without logging a full public URL:

1. Confirm the slug fragment exists and its fixed upstream path returns 200
   when requested directly from the loopback media proxy.
2. Count the publication include directive in the edge stream config. One is
   incomplete; the expected count is at least two, covering HTTP and HTTPS.
3. Inspect `nginx -T` around each include and verify one is inside the port 80
   stream server and one is inside the port 443 stream server.
4. Test the public small interface with GET. HEAD alone is insufficient because
   upstream and fallback method handling can produce a misleading status.
5. Back up the stream config, add only the missing TLS hook, run `nginx -t`,
   graceful reload, and verify the public GET returns 200. Roll back the stream
   config immediately if syntax or reload fails.

The restricted helper readiness check rejects a stream config containing fewer
than two include directives with `edge_include_hook_missing`. The hook installer
is idempotent: it adds the directive only to matching stream HTTP/HTTPS blocks,
does not edit fragments for another slug, and creates a node-local rollback
script before replacement. Afterward, verify the existing UHD small interface,
Admin isolation, active failover target and no-cache directives unchanged.

## Multi-line upstream publication

`PUBLISH_REQUIRES_ONE_SAVED_UPSTREAM` was a historical planner/agent limitation,
not a UI parsing rule. The current contract splits newline, semicolon, comma and
pipe separated values, trims them, removes exact duplicates and preserves order.
The first line is `main`; later lines are `backup-2`, `backup-3`, and so on.

Publication stages every line in `managed_route_lines`. The privileged agent
re-reads the saved node and staged lines from the central database and rejects
any mismatch; the publish API cannot supply an arbitrary host. Edge fragments
contain only saved hosts plus root-owned, slug-scoped redirect hosts. The public
entry remains based on `main`, so adding or removing a backup does not change
the Yamby address.

For an already-published slug, saving a target list is an atomic single-slug
configuration refresh. The first target must remain unchanged. The workflow
writes the new route lines, replaces only the owned edge fragments, runs both
Nginx tests and reloads, then commits the publication state. Failure restores
the old node, route lines and fragments. A failed or unhealthy backup must not
disable the existing main line. Changing the main line still requires an
explicit migration or unpublish.

The generated main location treats 403, 404, 416, 429 and 5xx as retryable for
configured backups. Internal rewrites contain literal manifest-listed hosts;
there is no arbitrary-host regex. Removing a backup rewrites the same slug
fragment and must leave no orphan route line or host location.

## Published but playback unavailable

Do not use the home page, images or `System/Info` as playback acceptance. A
published entry starts as `playback_status=unverified`. It becomes a playback
success only after an authenticated client request demonstrates a VideoStream
200/206 with growing bytes. The owner UI must show the unverified state instead
of implying that edge synchronization proves playback.

For a 302/307/308 response, record only the status and Location host hash. If a
following stream request reaches the default 403 with no upstream response
time, classify it as `redirect_host_unallowed`; add only the observed,
operator-reviewed host to the slug-scoped root allowlist and refresh that
publication. If direct edge-to-upstream and public proxy throughput are both
slow, classify `upstream_slow` and do not alter proxy policy.

### Multi-hop Redirect Refresh

Some Emby media sources redirect more than once. For one slug, follow the
authenticated Range request internally and record only hop status, scheme,
port and host hash. Add an exact root-owned endpoint for each observed hop,
then refresh that one published slug. A refresh must stage the publication
state, replace only that slug's fragments on BWG and NOSLA, run candidate and
host Nginx tests, reload gracefully, and restore the prior state on failure.

For a CDN that demonstrably rotates only one short label under the same
controlled suffix, use the restricted `redirect_patterns` configuration rather
than opening a dynamic-host route. The pattern fixes scheme, port, suffix and
label length and is unavailable to the owner API. It is not a substitute for
an arbitrary upstream allowlist.

### Playback Acceptance State

`published` means edge synchronization completed; it does not prove a client
can stream. Keep `playback_status=unverified` until an authenticated request
shows `200`/`206` with growing bytes. Mark `range_failed` for no usable ranged
body, `redirect_host_unallowed` for edge fall-through, and `upstream_slow`
only when direct and stream measurements are similarly slow. Do not derive a
successful state from `System/Info`, images, or an unauthenticated `401`.

See `39-emby-playback-throughput-troubleshooting.md` for the full Range/206 and
throughput procedure.

### Redirect Rewriting Requirement

An allowlisted redirect route is necessary but not sufficient. The legacy
loopback media proxy may return the upstream `Location` unchanged. Every
generated publication fragment therefore rewrites only these response forms
back onto the current stream origin:

- a saved primary or backup upstream;
- a root-owned exact redirect endpoint; or
- a root-owned fixed-length CDN-label pattern.

Relative locations are returned beneath the current manifest route. Already
encoded `/https/...` locations are left unchanged. The rewrite preserves a
signed query only while forwarding the response; Nginx's query-free access log
continues to record `$uri`, never the query. No owner API input can create a
rewrite for an arbitrary host.

On 2026-08-15 the BWG publication service was upgraded without its separate
local edge helper, so a refresh reported `synced` while BWG regenerated an old
fragment template. This is a deployment defect, not a browser defect. Both
the agent and local helper must have the same binary hash before a refresh.
The readiness check for this operation is: helper hash equality, candidate
test, host `nginx -t`, graceful reload, and a count of manifest-scoped
`proxy_redirect` rules on both edges.

### Edge Admin Isolation

The stream virtual host is never an owner-admin ingress. Both exact and
slash-suffixed `/admin` and `/api/admin` locations must return `404` on NOSLA
and BWG. A 301 from `/admin` that leads to a proxy response is not acceptable:
it can become reachable after failover. Owner administration remains available
only through the separate owner-admin virtual host, where Basic Auth injects
the trusted loopback header for the sidecar. Keep legacy `/s/` routing separate
from this rule unless it has an independently approved migration plan.

### Multi-line production acceptance prerequisite

A real two-line production test requires two HTTPS entry points belonging to
the same isolated test Emby service. Both lines must be operator-controlled,
saved on the test slug and safe to publish on both edges. Do not use UHD,
feimu, yuchu, another production Emby, or an arbitrary public host as a test
backup. Doing so tests cross-service routing rather than backup behavior and
can expose an unintended upstream.

When the prerequisite exists, record the test slug's public URL, managed-route
line count and both fragment hashes. Publish `main + backup-2`, then add and
remove one backup through the normal owner save workflow. After each change,
verify both edges passed `nginx -t`, only the test slug's rows and fragments
changed, and the public URL stayed byte-for-byte identical. Also verify the
UHD, feimu and yuchu publication rows, fragment hashes and playback state did
not change. If no controlled second line exists, mark only the production
acceptance test blocked; the parser/planner/store/renderer unit tests may pass,
but they are not a substitute for this gate.

### Multi-line staging/mock acceptance

The staging gate uses two controlled TLS mock upstreams and an isolated
`staging-multiline` slug. The line checker must request both mocks independently
and retain ordered results when main is reachable and backup returns an
unhealthy status. This verifies that one bad backup does not turn a healthy
main into an unreachable server.

The workflow test then saves two HTTPS lines, runs publication dry-run,
publishes two managed route lines, adds a third backup and removes it again.
The publication public URL must remain unchanged, the final lines must remain
`main` plus `backup-2`, and a pre-existing unrelated route must remain
byte-for-byte unchanged. The generated multi-line edge candidate must pass the
host's real `/usr/sbin/nginx -t`. This mock gate is allowed without DNS,
production reload or a public test certificate; it does not replace a later
controlled production failover exercise.

### Production-safe multi-line owner gate

Run `deploy/publication/production-multiline-safe-test.sh` only with an isolated
slug, an explicit owner-admin HTTPS origin, a root-readable password file and
`CONFIRM_SINGLE_SLUG_TEST=yes`. The script uses IANA-reserved `.invalid` HTTPS
names, never accepts an arbitrary publish host from the API, and cleans only
the named test slug. It must prove two-line save, per-line detection, dry-run,
publish, add/remove backup, public-URL hash equality, unpublish and absence of
an orphan managed route. The legacy `PUBLISH_REQUIRES_ONE_SAVED_UPSTREAM` error
must not occur. Record unrelated publication/fragment hashes before and after.

New publications use the same `renderNginxFragment` implementation as existing
managed nodes: no buffering/cache, Range/If-Range forwarding, Content-Range
pass-through, and manifest-scoped 302/307/308 Location rewriting. An unknown
redirect host is still fail-closed and must never be auto-allowed. Such a node
remains playback `unverified` until a real authenticated playback canary
succeeds; edge synchronization alone must not claim playback success.

Use the operation-specific rollback script created during deployment. Restore
the prior release without restoring the database when possible because the
new columns are backward-compatible. Restore a database snapshot only for a
confirmed migration/data failure and only after stopping the writer.

## Deploying this release on a new server

1. Clone the repository or fetch tags, then check out the signed-off release
   tag in detached-head state. Verify `git status --short` is empty.
2. Run `go test ./...`, `go vet ./...`, the historical publication migration
   test and the Nginx candidate-fragment test before building.
3. Build `./cmd/embyproxy`, `./cmd/publication-agent`,
   `./cmd/stats-collector`, and `./cmd/stats-sync` with `-trimpath`. Inject the
   release tag, commit and UTC build time into `embyproxy/internal/buildinfo`
   with `-ldflags`; never place credentials in build flags.
4. Create service users, state directories and a root-only backup directory.
   Copy `.env.example` files to paths outside Git and fill them locally. Never
   commit these deployed environment files.
5. Stop the sidecar writer, back up its SQLite file including WAL/SHM state,
   install the new binary, and restart it. `storage.New` performs the additive
   `emby_publications` playback columns and `managed_route_lines` migration on
   startup. Verify the historical migration test first; do not run ad-hoc SQL.
6. On BWG, render `agent-config.example.json` and `edge-bwg.example.json` into
   root-owned configuration, install the same publication-agent build as both
   agent and local helper, and enable the service. On NOSLA, install that exact
   build as the forced-command edge helper and render `edge-nosla.example.json`.
   Pin the NOSLA host key and keep the private key root-only.
7. Run `install-edge-hook.sh NODE STREAM_CONFIG PUBLIC_MEDIA_HOST` as a dry-run
   on each edge. After reviewing its scoped plan, rerun with `--apply`. Render
   publication fragments only through the restricted agent; do not copy live
   fragments between servers.
8. Run `nginx -t` on BWG and NOSLA, then graceful reload. Set
   `PUBLICATION_AGENT_SOCKET` in the sidecar's private environment, restart the
   sidecar, and verify readiness/dry-run before publishing any slug.
9. Verify owner-admin authentication/isolation, stream `/admin` denial,
   loopback-only sidecar binding, unchanged failover state, and a real
   authenticated Range/206 playback canary before marking playback verified.

## Release rollback

1. Stop new publication operations and record the affected slug/state.
2. Run only the operation-specific BWG/NOSLA rollback scripts created by the
   restricted helper, then run `nginx -t` and graceful reload on each edge.
3. Repoint the sidecar release symlink to the previous tested tag and restart
   the publication agent and sidecar. Keep the additive schema unless database
   validation proves restoration is required.
4. For a single failed publication, use its reconcile/cleanup API; never remove
   global routes or another slug's fragment.
5. Recheck UHD/existing route hashes, public URL, playback, isolation and
   failover state. Store concrete backup paths in the private deployment record.
