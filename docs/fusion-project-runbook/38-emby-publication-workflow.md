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
| P7 real feimu publication | BLOCKED | The production sidecar has no configured privileged `PublicationSyncer`; a no-op adapter would create false success. | Keep publication fail-closed. A restricted root-owned BWG/NOSLA edge adapter must be backed up, dry-run and verified before owner approval can be applied. | Await a separate production-publication gate after the adapter is ready. |

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
- Git delivery note: the local feature history is a fast-forward of the origin
  feature branch. A normal push first failed during the GnuTLS handshake; a
  normal retry and a one-command HTTP/1.1 retry then received no remote response
  and were terminated. No force push, remote/auth edit or alternate publisher
  was used. The deployed commit remains present locally and on BWG; origin is
  not claimed as updated.
