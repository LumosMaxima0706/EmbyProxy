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
| P6 feimu dry-run | TODO | No production state has been touched by the new workflow. | Run authenticated dry-run only after candidate deployment; compare DB/config hashes before and after. | Do not call publish. |
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
