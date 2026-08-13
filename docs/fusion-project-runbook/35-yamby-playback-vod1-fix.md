# Yamby Playback vod1 Fix

## Scope and safety boundary

- Keep the UHD upstream unchanged.
- Keep failover policy, DNS, ACME, managed nodes, and cleanup outside scope.
- Change the owner-admin display/copy/preview base only by configuring the
  existing UHD public path without a trailing slash.
- Add only the explicitly observed `v1-vod1` media-host path to NOSLA's
  production stream allowlist. Do not permit arbitrary upstream hosts.
- Do not replay authenticated playback URLs from logs. Verification uses only
  small public information endpoints until the owner initiates a playback.
- Preserve query-free access logging, loopback-only sidecar binding, Admin
  hostname isolation, no-cache streaming, and feature-branch-only publishing.

## Read-only diagnosis

- The configured owner-admin node mapping currently ends in `/443/`; Yamby
  accepts the same client base only when configured as `/443`.
- NOSLA has exact and prefix locations for the primary upstream and `v1-vod2`,
  but no location for `v1-vod1`; the server's default location returns 403.
- Bounded, query-free production access evidence shows the failing sequence:
  `PlaybackInfo` returned 200, the primary media request returned 302, and its
  redirected `v1-vod1` request returned 403 with no upstream response time and
  zero request time. Repeated instances match the client's playback spinner.
- A small localhost sidecar request to the explicit `v1-vod1` public-info path
  returned 200. This isolates the current fault to the NOSLA Nginx allowlist,
  before the sidecar and upstream.
- Existing primary and `v1-vod2` locations already forward Range, If-Range,
  authorization-related headers, Cookie, and User-Agent; pass Content-Range
  and Accept-Ranges; disable request/response buffering and cache; and use
  streaming timeouts. Those settings will be copied without alteration.

## Minimal change and rollback plan

1. Change `PUBLIC_MEDIA_NODE_PATHS_JSON` for UHD from `/443/` to `/443`.
   Existing NOSLA exact-location behavior keeps both incoming base forms
   compatible by redirecting the no-slash base to the slash prefix.
2. Add exact and prefix `v1-vod1` locations immediately before the existing
   stream Admin/public include, matching the current `v1-vod2` stanza.
3. Before either live mutation, back up the affected live file, service state,
   effective configuration, and checksums; generate and syntax-check an exact
   rollback script.
4. Run an isolated sidecar dry-run for the no-slash URL contract and a staged
   NOSLA Nginx syntax test. Apply only after both pass.
5. Restart only the BWG sidecar for its EnvironmentFile change and reload only
   NOSLA Nginx for the location change. Each apply script invokes rollback on
   an error.

## Verification plan

- Owner Admin returns 401 without Basic Auth and 200 with Basic Auth; its node
  API returns the stream-host UHD URL with no trailing slash, credentials,
  query, or fragment. Display/copy/preview continue to consume `publicUrl`.
- The `v1-vod1` small public-info endpoint returns 200 through production
  stream after apply. No media object is fetched automatically.
- Owner-admin `/uhd` and `/s/`, canary Admin, and stream Admin stay blocked;
  sidecar stays on `127.0.0.1:18082`; failover remains auto/NOSLA and its timer
  remains active/waiting.
- After owner-triggered playback, query-free logs must show the redirected
  `v1-vod1` request reaching an upstream and returning a successful streaming
  status with nonzero bytes and duration. A 1 MiB Range test is allowed only
  if a current authenticated URL can be supplied without exposing it in
  command arguments, output, or history.

## Execution status

Status: APPLIED; OWNER PLAYBACK CONFIRMATION PENDING.

- Local targeted/full Go tests, vet, shell syntax, and diff checks passed. A
  temporary extracted Go toolchain was used without installing system packages.
- Effective root-only backups and verified rollback scripts:
  - BWG: `/var/backups/embyproxy-owner-public-url/20260813T063300Z-no-trailing-slash`
  - NOSLA: `/var/backups/embyproxy-playback-vod1/20260813T063300Z`
- An earlier backup wrapper received a CRLF control character in its generated
  directory name. It never changed live state and is not an accepted rollback
  point; explicit fixed paths were then created and independently verified.
- Isolated sidecar URL-contract dry-run and staged NOSLA `nginx -t` passed.
  The BWG sidecar mapping-only apply and NOSLA Nginx-only apply both passed;
  Nginx syntax passed before and after its reload.
- Owner Admin returns 401 without Basic Auth and 200 after Basic Auth. Its UHD
  public URL contract, display, copy, and preview checks pass with the stream
  hostname and no trailing slash. The no-slash production base remains
  compatible via the existing same-host 301 to its slash prefix.
- The newly allowed `v1-vod1` small public-info endpoint returns 200 with
  nonzero bytes. Primary and canary information checks also return 200.
- Owner-admin `/uhd` and `/s/`, canary Admin, and stream Admin return 404;
  owner-admin remains Basic-protected. The sidecar is active/enabled with
  `NRestarts=0` and listens only on `127.0.0.1:18082`.
- Failover remains auto, active NOSLA, `MANUAL_HOLD=none`, reason
  `nosla_healthy_below_threshold`; the new timer is active/enabled and the
  legacy timer remains inactive/disabled.
- Production stream config retains three explicit no-cache/no-buffer location
  sets, Range/If-Range forwarding, and no cache-path/background-update/slice
  directives. Bounded post-apply access/error logs have no gateway, severe,
  credential, or query markers.
- No post-apply authenticated playback request has reached the stream log yet.
  Actual playback start, 200/206 transfer, sustained bytes, and statistics
  change remain pending an owner-triggered small-video playback. No credential
  or authenticated media URL was reconstructed or replayed from logs.
