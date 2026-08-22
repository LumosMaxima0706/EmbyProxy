# Emby Reverse Proxy Playback Investigation

## Scope

- Affected public route: `/https/<redacted-upstream>/443` (the reported `pro.emby.moe` route is intentionally not repeated in full).
- Control comparison: the existing UHD publication, which has verified playback.
- Safety boundary: local repository and mock analysis only unless a later owner-approved, redacted production capture is supplied. No credentials, query tokens, cookies, complete UUIDs, or complete media URLs belong in this document.

## Timeline and Baseline

| UTC | Phase | Result |
|---|---|---|
| 2026-08-21 | Initial inventory | Existing publication and playback runbooks, proxy adapter, publication agent, edge templates, and tests located. |
| 2026-08-21 | Local evidence | Core adapter tests cover Range/206/Content-Range, Location rewriting, base paths, cookies, Authorization, and HTTPS upstreams. |
| 2026-08-21 | Initial hypothesis | New route may be saved/API-reachable but missing an applied edge fragment or redirect route; this is not proven until route artifacts and live request stages are captured. |

## Current Hypotheses

1. **H1: publication artifact gap** - managed route exists in SQLite/UI, but one or both edge fragments were not generated, loaded, or reloaded for the new slug.
2. **H2: redirect-host gap** - upstream VideoStream returns 302/307/308 to a media host not represented in the manifest allowlist, causing an edge fail-closed 403 or an unusable redirect.
3. **H3: path/base-path mismatch** - saved upstream has a path or port shape that works for API paths but produces an invalid media path.
4. **H4: header/range regression** - Range, If-Range, Authorization, Cookie, Host, or SNI differs between the new route and the verified UHD route.
5. **H5: upstream media policy** - upstream accepts API/image requests but denies or throttles the selected media; this must be distinguished from a proxy defect.

## Files and Evidence Reviewed

- `docs/fusion-project-runbook/38-emby-publication-workflow.md`: publication state machine, multi-line routes, redirect allowlist, redirect rewriting, Range/206 canary, rollback.
- `docs/fusion-project-runbook/39-emby-playback-throughput-troubleshooting.md`: prior UHD/Yamby incident and the diagnostic signature for edge redirect-route 403.
- `internal/proxyadapter/*`: slug/node routing and mediaproxy delegation.
- `internal/mediaproxy/*`: target parsing, header forwarding, Range handling, response and Location rewriting.
- `internal/admin/publications.go`: saved-upstream validation, managed route planning, line ordering, publication state.
- `internal/publicationagent/edge.go`: edge manifest validation, fragment rendering, candidate `nginx -t`, atomic install, reload, redirect rewriting, fail-closed fallback.
- `internal/publicationprotocol/protocol.go`: manifest/route contract.
- `deploy/failover/*vod1*locations.inc`: verified legacy UHD route templates and edge parity.
- `deploy/publication/test_live_playback_capture.py`: query-free log attribution and dynamic/legacy route matching.
- `internal/proxyadapter/*_test.go`, `internal/publicationagent/edge_test.go`: local contract coverage.

## Commands and Results

The initial local inventory used `rg` over runbooks, source, templates, and tests. The repository contains no production access logs or live SQLite state for the reported new slug, so no conclusion about the live route is yet possible from this checkout alone.

Observed source contracts:

- `buildPublicationPlan` accepts one to sixteen saved HTTPS targets, preserves a saved base path, assigns the first target as `main`, and records edge route/allowlist changes.
- `renderNginxFragment` emits exact route locations for every manifest route, forwards Range/If-Range and Emby auth headers, disables request/response buffering, passes Content-Range/Accept-Ranges, and rewrites only manifest-listed absolute or relative redirects.
- `verifyIncludeHook` requires the publication include in both HTTP and HTTPS Nginx blocks; a hook present only in HTTP is rejected.
- Adapter tests prove that a mock VideoStream can return 206 plus Content-Range and a Location that is rewritten to the public route.

## Local Reproduction Plan

1. Build a mock upstream with API/image endpoints returning 200 and VideoStream returning 206 or 302/307.
2. Render a manifest for an UHD-like route and a new-slug route with identical upstream behavior.
3. Assert both fragments contain exact slash and non-slash locations, Range/If-Range, no buffering, auth headers, and redirect rewrites.
4. Remove the redirect route from the new-slug manifest and assert the edge falls closed; this models the reported symptom without using real credentials.
5. Run `go test ./...`, `go vet ./...`, targeted proxy/publication tests, and the live-capture parser tests.

## Live Evidence Required (redacted)

If local reproduction passes, request one owner-side capture after a fresh playback window. Record only:

- slug/edge/stage; status; upstream status; request time; upstream response time; bytes out; Content-Range/Accept-Ranges presence; Range/If-Range presence; redirect host hash; route alias.
- Separate rows for `PlaybackInfo`, `VideoStream`, manifest/playlist, and one Range request.
- Nginx loaded config path and fragment mtime/hash, plus publication status and managed route line count.

Never collect or print query strings, Authorization, cookies, tokens, complete UUIDs, or complete URLs.

## Decision Rules

- `VideoStream 206` with growing bytes: playback path is working; investigate client/UI attribution only.
- Upstream 302/307/308 followed by local 403 with no upstream timing: classify `redirect_host_unallowed` / missing edge route.
- Upstream 403/429 or timeout on both direct and proxied media requests: classify upstream media policy/rate-limit/slow source, not a proxy rewrite defect.
- Proxy 200 without Content-Range for a ranged request: classify Range/header regression.
- API/image 200 with no VideoStream row: classify client playback-path or URL-generation issue until a client trace proves otherwise.

## Fix/Verification Gate

No production edit is justified until the new slug's publication status, managed route lines, both edge fragments, loaded Nginx config, and a redacted VideoStream/Range trace are compared with UHD. Any fix must add a regression test and pass the full Go/vet/template suite. Rollback is the operation-local edge backup plus the prior published fragment; never delete unrelated slugs.

## New Server Standard Checklist

- Save a canonical HTTPS upstream URL without credentials/query/fragment; verify scheme, host, port, and base path.
- Run `检测上游` and confirm API plus an authenticated PlaybackInfo call.
- Publish and confirm `managed_route_lines` count, both edge statuses `synced`, loaded fragment presence, and `nginx -t`.
- Discover media redirect hosts from a bounded authenticated Range/VideoStream probe; add only controlled allowlist routes/patterns.
- Verify exact and slash-suffixed public paths, Range/If-Range forwarding, Content-Range/Accept-Ranges, 302/307/308 rewrite, and at least 30 seconds of growing bytes.
- Mark playback `verified` only after the active edge (NOSLA or BWG) passes the canary; compare the inactive edge before failover use.
- Keep a per-slug backup and rollback record; do not alter UHD or unrelated fragments.

## Status

## Credential lifecycle hardening (2026-08-22)

The multi-sample canary must not depend on an operator copying a runtime Emby
token for every verification. Investigation found no safe reusable source in
the ordinary node record: the existing `Node.Secret` value is SQLite data and
is not suitable for playback credentials. The new lifecycle stores one token
per route slug outside SQLite, under the configured
`PLAYBACK_CREDENTIAL_DIR` (default `/var/lib/embyproxy-gsy-sidecar/playback-credentials`).

The sidecar creates the directory as `0700` and writes `<slug>.token` atomically
with mode `0600`. The admin API accepts a token only for configure/rotation and
returns only `credential_configured`; it never returns the token, writes it to
Nginx, logs, Git, runbook, or publication state. Read/delete operations are
runtime-only and support rotation without exposing the previous value. A
missing or unreadable credential forces `playback_status=unverified` with
`reason=credential_missing`; API connectivity alone cannot mark a route healthy.

The authenticated Admin UI now has a one-time “configure playback credential”
action and a stored-credential multi-sample canary action. Operators enter only
bounded item IDs for subsequent checks. The credential is read by the backend
and passed through the existing protected publication bridge in memory.

Implementation was built and tested in the isolated BWG Docker Go 1.26
environment (`go test ./...` and `go vet ./...` passed). Commit
`7a3715cc30fcd63d6c7b097c974c4d68548df05a` was pushed to
`origin/feature/failover-phase2-local`. The BWG sidecar is now running that
binary from a versioned release directory. Its credential directory was
created with owner-only permissions (`0700`) for the sidecar account; no
credential has been provisioned or inspected. The previous release path was
retained as the rollback target. NOSLA needs no binary change for this stage:
the credential remains only on the BWG sidecar and reaches the existing
publication-agent over the local protected socket at canary time.

## Credential source audit (2026-08-22)

Before requesting provisioning, the normal configuration sources were checked
without reading or exporting secret values. The saved `1111` node record has
only compact upstream/display/keepalive fields and no API key, access token, or
playback-credential field. BWG's protected environment exposes only the
administrator authentication setting; the publication-agent and edge configs
contain no Emby credential source. The per-slug credential directory exists but
contains no token file for `1111` (or any other slug).

The ordinary node `Secret` field is not an Emby playback credential and is not
reused. No client, access-log, capture, process-memory, or browser/session data
was inspected. Therefore `1111` has no system-legitimate credential available
for PlaybackInfo/VideoStream validation and requires one-time administrator
provisioning through the authenticated Admin UI.

Architecture check: BWG is the control plane. The BWG sidecar reads the local
credential and invokes the publication-agent over its protected Unix socket;
the agent performs the bounded canary and uses the existing edge helper to
publish scoped routes to both edges. NOSLA is an edge-only Nginx/helper host and
has no active publication-agent service or canary executor. It therefore does
not need a copy of the Emby credential. Failover validation still runs from
the BWG control plane against the public route, so the credential remains only
on BWG and is never synchronized to NOSLA, Nginx fragments, Git, or logs.

Validation gates for the outstanding 1111 regression remain unchanged:
Item 625260 is the known successful sample and Item 601953 is the known 403
sample. The route remains `partially fixed / failed-unverified` until the
stored-credential canary discovers both exact media endpoints and demonstrates
206, valid Range/Content-Range, and sustained byte growth for both samples.

## BWG Read-Only Evidence (2026-08-21)

The owner-approved BWG read-only inspection produced the following redacted evidence:

- `/etc/nginx/conf.d/embyproxy-publications/1111.conf` exists, is root-owned (`0640`), and is loaded by `nginx -T`.
- Publication-agent journal records `slug=1111`, `action=publish`, and `nosla_status=synced`, `bwg_status=synced` at the latest publication operation.
- SQLite publication state for `1111` is `published`, reason `public_entry_configured`, both edges `synced`, and playback status `unverified`.
- `managed_route_lines` contains one enabled `main` line for `1111`; there is no saved backup line.
- The `1111` fragment contains exact and slash-suffixed locations, `Range`/`If-Range`, all Emby auth/cookie headers, `proxy_buffering off`, `proxy_request_buffering off`, `proxy_cache off`, `Content-Range`/`Accept-Ranges` pass-through, and a bounded redirect rewrite for the saved upstream host.
- Unlike the verified `feimu` and `yuchu` fragments, `1111.conf` contains no additional redirect host or redirect-pattern locations. This is a concrete publication artifact difference, not a theoretical one.
- BWG's recent stream access log contains no requests for the new slug and no `VideoStream`/`206` row for it. It does contain small `200` `System/Info/Public` rows for the UHD route. This means BWG is not the active edge for the observed new-slug attempt, or the client did not reach the media path.
- Public unauthenticated probes reach the new route's web shell (`/web/index.html`) but receive `401` for API paths. This is consistent with an authenticated client requirement and does not prove playback.
- A like-for-like public check of `/System/Info/Public` returns `200` for both the new route and UHD. This proves public routing to the saved upstream is healthy for a query-free API endpoint.
- A query-free dummy `/Videos/1/stream` request receives an upstream-relative `302 /404`; through the public route it becomes `302 https://<stream-host>/https/<saved-host>/443/404`. Relative `Location` rewriting is therefore working for the new fragment.
- The direct upstream and public route differ only where authentication is required; no credential was read or constructed during these probes.
- The central stats store has NOSLA/BWG `PlaybackInfo`, `VideoStream`, 302 and 206 activity, but its schema intentionally lacks slug/route attribution. It cannot prove which server generated a row without a bounded single-route playback window.

An independent security finding was recorded: the generic BWG access log returned `200` for probes to `/web/.env` and `/web/.git/config`. No change was made during this playback investigation; those paths require a separate urgent hardening gate.

## Updated Decision

H1 (route not published at all) is **rejected** for `1111`: both edge sync and the loaded BWG fragment are present.

H4 (missing Range/buffering directives) is **rejected** for the generated fragment: all required directives are present and match the legacy contract.

H3 (broken saved scheme/host/port/base path) is **rejected** for the public API path: direct and proxied `System/Info/Public` both return `200`, and the published path is canonical HTTPS/443 with no base-path discrepancy.

Relative redirect rewriting is **verified**. Only an authenticated VideoStream can determine whether this upstream uses a cross-host absolute CDN redirect.

H2 (missing media redirect route) is now the leading cause. The evidence is the exact difference between `1111.conf` and working `feimu.conf`/`yuchu.conf`: the new route has only its upstream host, while working routes have observed CDN/origin redirect endpoints/patterns. This remains a *probable* root cause until one authenticated VideoStream response records a redirect status and host hash.

H5 (upstream media denial/slow source) and active-edge attribution remain open because no authenticated VideoStream or Range request for `1111` is present in BWG logs, and NOSLA is not reachable from this local session.

## Required Minimal Live Capture

Do not add an arbitrary host to the allowlist. One owner playback window is required. Capture on the active edge only, with query/token/cookie/Authorization values removed:

1. `PlaybackInfo` response status and whether its media URL points to the public route.
2. First `VideoStream`/playlist request status, request `Range` presence, response `Content-Range`/`Accept-Ranges`, bytes and timings.
3. If status is 302/307/308, record only the redirect host hash and whether the follow-up request returns edge 403, upstream 403, 206, or bytes.
4. Record active edge (`NOSLA` or `BWG`) and the loaded `1111` fragment mtime/hash.

If the redirect host is observed, add only that root-owned controlled endpoint/pattern to the `1111` manifest, render both edge fragments, run candidate and full `nginx -t`, reload only after backup, and re-run a 30-second 206/bytes canary. If the redirect host is not observed and direct upstream media is also slow/403, classify the issue as upstream media policy or rate limiting instead of changing proxy code.

Access note (corrected 2026-08-21): the earlier direct `root@<address>` attempt used the wrong connection form. The existing Windows SSH configuration contains the `nosla` alias with its dedicated identity, user and port. `ssh -o BatchMode=yes nosla` succeeds and identifies the host as `silver-charge`, user `root`. No key material was read or printed. The active-edge trace can therefore be collected directly from this session.

The existing safe collector is `deploy/publication/live-playback-capture.py`. On NOSLA it should be run for slug `1111` against the query-free stream access log while the owner performs one 30-second playback. The JSONL output contains only slug, edge, stage, status, bytes/timing flags and route-host hashes; it rejects lines containing Authorization/cookie/token markers.

## Verification Performed

- Public route `/System/Info/Public`: `200`, matching the direct upstream and UHD control endpoint.
- Public route relative redirect: upstream-relative `/404` becomes the same stream-host route prefix, confirming `proxy_redirect` for relative/same-host Location.
- BWG loaded fragment: exact + prefix locations, headers, Range, timeouts and buffering directives present.
- Publication database: `published`, both edges `synced`, playback `unverified`, one main route line.
- Source inspection: no unrestricted redirect forwarding; external media destinations require root-owned manifest entries by design.
- Local Go/Python execution: skipped because this Windows environment lacks both toolchains; no source/config change was made, so no unverified patch is being proposed.

## Current Conclusion

The repository's core `emby-reverse-proxy-go` integration correctly proxies media and preserves Range/auth headers. The new server was published with a valid primary route, but unlike the verified servers it has no observed redirect-host routes and has no playback verification. The most likely failure is therefore an incomplete media redirect allowlist/edge manifest, not a missing VideoStream proxy implementation. Final confirmation is blocked only by one redacted authenticated VideoStream capture on the active edge.

Local execution note: this Windows host currently has neither the Go nor Python toolchain on `PATH`; therefore the existing Go/Python suites were inspected but could not be rerun here. No source change is being proposed without a reproducible failing test. BWG has the deployed binaries and live artifacts but no Go toolchain. Existing repository tests already cover the relevant contracts.

## NOSLA SSH Recovery and Active-Edge Evidence (2026-08-21)

- Searched the project, parent runbooks/deployment records, Windows OpenSSH configuration and PowerShell history for `NOSLA`, SSH aliases and prior connection commands.
- `ssh -G nosla` resolves the expected host, root user, port 22, dedicated identity file and `IdentitiesOnly yes`; the private key was not opened.
- The Windows `ssh-agent` is not running, but it is not required because the alias selects the dedicated identity directly.
- Non-interactive login to `nosla` succeeds. This rejects the earlier hypothesis that new external SSH authorization was required.
- Nginx is active on NOSLA and the publication include is loaded. The admin sidecar remains on loopback.
- `/etc/nginx/conf.d/embyproxy-publications/1111.conf` is present and loaded. It has the primary exact/prefix locations, Range/If-Range forwarding, Emby auth header forwarding, disabled buffering/cache, and same-origin/relative Location rewriting. It has no additional CDN redirect routes.
- Working `feimu.conf` and `yuchu.conf` contain multiple additional redirect hosts or patterns. This is the principal artifact difference from `1111.conf`.
- Redacted historical NOSLA access-log samples for `1111` include API/image success plus VideoStream-class 302/401/404 responses, but no attributable successful 206 with growing bytes. In the same log, UHD has PlaybackInfo 200, VideoStream 302 followed by 206, and large byte counts. Aggregate stats also prove NOSLA can serve 206 generally, but their schema has no slug attribution.
- Self-probe requests originating from the NOSLA address are excluded from client-playback conclusions.

Next action is a bounded, query-free live capture for slug `1111`. It will record only stage, status, bytes/timing flags, Range/Content-Range presence and hashed redirect-route ownership. A production fragment will be changed only if the trace proves a specific missing redirect route or another scoped configuration defect.

## Historical Authenticated Playback Classification (2026-08-21)

The NOSLA query-free access log and the proxy container's status-only log retain a complete owner playback window around 12:46-12:49 local time. A local redaction parser classified the path in memory and emitted only stage, status, byte count and timing; raw query strings, account identifiers and media identifiers were not copied into this runbook.

Observed sequence for `1111`:

- At least three different selected media items completed `PlaybackInfo` with HTTP 200.
- Each then requested the original VideoStream path and received HTTP 302 from the saved upstream.
- The client retried those VideoStream requests repeatedly over increasing intervals.
- No primary or redirect-alias request for `1111` returned HTTP 206 anywhere in the retained log.
- No request with growing media-sized bytes followed any of the `1111` 302 responses.
- During the same minute, the UHD control route completed `PlaybackInfo 200`, primary media `302`, allowlisted media-host `206`, and transferred tens of megabytes.

This evidence rejects the following causes:

- A NOSLA-wide Range/206 failure: UHD proves the same edge, Nginx and proxy container can stream 206.
- A single corrupt or non-playable selected item: multiple `1111` items show the same 302-only outcome.
- Failure before PlaybackInfo: PlaybackInfo succeeds repeatedly.
- Missing primary publication path: all API, image and VideoStream requests reach the saved upstream with normal sub-second response times.

The remaining fault boundary is the external redirect hop. `1111.conf` rewrites only relative and saved-host Locations and contains no redirect endpoint/pattern route. The working UHD/feimu/yuchu publications have the extra redirect route required for their 302 target. The specific `1111` redirect endpoint must still be observed before it can be allowlisted safely.

## Bounded Live Probe Attempt (2026-08-21)

- Uploaded the repository's query-free `live-playback-capture.py` to `/tmp` and ran a bounded NOSLA capture for slug `1111`.
- The window produced no `1111` client request; only background interface samples were recorded. It therefore cannot be used as a post-fix or current-client result.
- Started a second, memory-only loopback parser on port 18080. It ignores request headers and bodies, never writes raw packets, and emits only redirect scheme/port, a short host hash and same-host classification. A root-only endpoint file is written only after a matching `1111` VideoStream response is observed.
- No matching request arrived during the initial observation period. This is a missing canary action, not an SSH or NOSLA authorization problem.

Production remains unchanged: no fragment edit, Nginx reload, DNS change or failover change has occurred. When one owner playback reaches the active probe, the safe change gate is: validate the observed public endpoint against the controlled upstream, back up only `1111.conf` on NOSLA and BWG, render both candidates, run `nginx -t`, install the scoped fragments, reload each Nginx only after a passing test, and verify `302 -> redirect alias -> 206` with growing bytes.

## Yamby Reproduction at 18:17-18:27 (2026-08-21)

The owner added the new public entry in Yamby and reproduced the failure. NOSLA access logs show two fresh client windows, including a final retry sequence at 18:27:

- Normal API/library/image requests returned 200.
- PlaybackInfo returned 200.
- The original VideoStream returned 302 repeatedly, with increasing retry delay and normal upstream response times around 0.1-0.2 seconds.
- No `1111` request returned 206 and no media-sized byte stream followed.
- A same-client correlation scan found no follow-up `/https/<redirect-host>/<port>` request on NOSLA after the 302. This means Yamby did not reach an unallowlisted stream-host alias; it rejected the received Location or attempted the target outside this edge.

The first bounded probe had already expired before this playback window. A new loopback-only probe is now active. It parses matching response headers in memory, never writes raw traffic and records only redirect host hash plus a root-only endpoint tuple.

A second owner retry at 18:54 reproduced the same sequence: PlaybackInfo 200 followed by VideoStream 302 retries through 18:56:58 and no 206. That retry occurred while the first response parser was being replaced after its public-host/encoded-path attribution bug was found; the corrected parser became active shortly afterwards and therefore did not retroactively recover the Location. The access log intentionally does not store response Location, so the endpoint cannot be reconstructed after the request without another first-302 observation.

An 18:41 window in the same access log did contain `302 -> 206` and large bytes, but its redirect-route host hash differs from the `1111` saved-host hash. It belongs to another publication and is explicitly excluded from `1111` success evidence. This distinction is recorded to prevent a false green result from aggregate edge traffic.

## Deployed Gsy Image Redirect Contract

The deployed media proxy is `ghcr.io/gsy-allen/emby-proxy-go:v1.3` on loopback port 18080. Source and tests for the integrated version state that an absolute third-party 302 Location is rewritten to the public proxy form `/https/<redirect-host>/443/...`.

This was verified against the exact deployed image using an isolated localhost mock on temporary ports, with no production request or credential:

- Mock upstream returned an absolute HTTPS third-party Location.
- With production-equivalent `Host`, `X-Forwarded-Proto`, `X-Forwarded-Host` and `X-Forwarded-Port`, v1.3 returned an HTTPS Location on the public host with the encoded third-party route.
- The temporary container and mock were stopped immediately after the check.
- Production Nginx and the running port-18080 container were not changed or restarted.

One edge case remains important: the v1.3 implementation leaves protocol-relative Locations beginning with `//` unchanged. If the real media upstream uses that form, Yamby would attempt a direct external CDN connection instead of the stream-host route, exactly matching the absence of a NOSLA alias follow-up. The active response-header probe will distinguish this from an absolute Location without exposing its path or query.

## Publication-Flow Defect

Independent of the exact Location syntax, the permanent workflow defect is confirmed: publication marks the route published after API health and edge sync, while playback remains `unverified`. It neither performs an authenticated VideoStream canary nor discovers/validates the redirect hop. Therefore a newly published server can pass every current publication check while images work and playback fails. Future completion criteria must require a bounded PlaybackInfo + VideoStream test, redirect classification, and `206`/growing-byte proof before presenting a route as playback-ready.

## Capture Tool Hardening (2026-08-21)

The query-free capture utility had two attribution gaps exposed by this incident:

- An encoded but not-yet-manifested `/https/<host>/<port>/...` route was labeled `unknown` with no route prefix, so its host hash was unavailable.
- `route_prefix()` included the first media path segment in the prefix, which made route comparisons less exact.

The utility now labels such rows `unmanaged_dynamic`, returns exactly `/https/<scheme>/<host>/<port>`, and still emits only the short route-host hash. A regression test covers unknown dynamic routes, sensitive-field rejection and existing route attribution. The test suite passes on NOSLA with Python 3.10. This change does not open a dynamic proxy route; it only improves safe diagnosis.

The live response-header probe was also corrected to understand the deployed Gsy contract: an external absolute Location is returned by the container as the public stream host plus an encoded `/https/<redirect-host>/<port>` path. Protocol-relative Locations are classified separately. Raw Location values are never persisted.

**Status: OPEN - Yamby failure reproduced on NOSLA; API and PlaybackInfo pass, VideoStream is trapped at repeated 302 with no follow-up and no 206. The exact redirect target was not captured because the probe was started after the last client retry; no safe allowlist change has been made. The query-free capture tool was hardened and its tests pass. No production configuration changed.**

## Active Capture Retry (2026-08-21 19:30 +08:00)

- Reconnected to NOSLA through the existing `nosla` SSH alias; no new authorization was required.
- The running proxy logs show fresh 1111 VideoStream 302 retries with ~2.5 KB responses and no 206; the owner playback client has since stopped retrying.
- A port-corrected, memory-only response probe is active on the Docker proxy listener. It records only redirect host hash, port, form and value length; it never stores raw Location, query, headers or credentials.
- The first probes observed 302 response headers but did not safely correlate the request to the saved upstream because Docker-side host/request framing differs from the initial parser assumptions. No redirect host was added and no production config was changed.
- Next gate: one fresh owner playback retry while the probe is active. If a redirect target is observed, validate and add only that exact root-owned route to the 1111 candidate; otherwise classify the failure as upstream/client redirect behavior and leave production unchanged.


## Latest NOSLA Correlation (2026-08-21 19:34 +08:00)

- Reconnected with the existing `nosla` alias and verified Nginx/container health; no authorization or service restart was needed.
- Fresh sanitized access-log rows continue to show `1111` VideoStream status 302 with approximately 2.5 KB response bodies and no 206/follow-up alias. Control/API rows remain 200.
- A packet-level, root-only probe was syntax-checked on NOSLA. It is bounded, memory-only for redirect contents, and emits at most a host hash/port/form/length. It produced no tuple because the client stopped retrying before the active window; no raw packet or Location was retained.
- A separate 35-second raw-header inspection observed Location headers (length classes only); raw values were not saved. Because the request stream could not be safely correlated to `1111` without an owner playback window, this is not treated as redirect-host evidence.
- No fragment, proxy image, container, DNS, failover, helper or Nginx configuration was changed. No reload occurred.

### Evidence-based conclusion

The directly observed failure boundary is stable: `PlaybackInfo`/metadata succeed, the saved upstream returns VideoStream 302, and the client receives no media bytes. The scoped `1111` fragment lacks the redirect endpoint/pattern locations present in verified UHD/feimu/yuchu fragments. This makes an incomplete redirect-hop publication the leading cause, with a protocol-relative Location from the upstream remaining a second code-level possibility. Neither can be safely repaired by guessing a hostname.

The safe repair gate remains one fresh Yamby playback retry while the probe is active. Once a correlated redirect form/host hash is observed, only that exact root-owned route (or a narrowly validated protocol-relative rewrite fix) may be rendered and tested. Until then `1111` remains `playback=unverified`; claiming a fix would be unsafe.

## Redirect Form Confirmed (2026-08-21 19:36 +08:00)

- The bounded probe captured one fresh redirect header without retaining its value. Its outer host hash matched the public stream host (not the saved upstream), scheme/port were HTTPS/443, and the value length class was 141. This is the Gsy proxy's encoded public redirect form, not a direct client-to-CDN redirect.
- Query-free access-log correlation for the same client shows no subsequent request for the encoded redirect host and no 206. The only 206 hashes in the window belong to the already verified UHD media route; they are not `1111`.
- Therefore the concrete failure is now localized to the second hop: Gsy emits a public encoded redirect for an external media endpoint, but the `1111` Nginx fragment has no corresponding exact/pattern location. Yamby receives the 302 and never obtains media bytes.
- A generic `/https/*` location is explicitly rejected because it would bypass the fail-closed redirect allowlist. The exact inner host still must be captured from the encoded path in one owner playback window before a scoped route can be added.

This closes the protocol-relative hypothesis for this observed window: the outer Location was absolute/public-encoded. Production remains unchanged; no reload or helper/DNS/failover operation was performed.

## Scoped Redirect Repair Applied (2026-08-21 23:40 +08:00)

- The latest bounded probe captured three inner redirect classes for the owner window. The media redirect used the exact `http` scheme, port `80`, and host hash `8bdb4524b5ef`; the other two rows were longer signed/secondary forms with host hash `156fca61f27c` on port `80`. The public access log immediately correlated the first media hop to `/http/<redacted-host>/80/stream` returning 403 before the fix.
- The primary inner media host was validated against the captured public encoded path and existing route policy. No wildcard route was used. The exact endpoint was added only to slug `1111` on both NOSLA and BWG, with exact and prefix locations and HTTP/80 redirect rewriting.
- Backups were created before each edge edit under the existing publication backup area. The original NOSLA fragment checksum was recorded; both candidate configurations passed `nginx -t`.
- Nginx was reloaded only on NOSLA and BWG after successful tests. Both services remain active. No EmbyProxy container, DNS, failover target or helper was changed.
- Post-reload probe to the new endpoint no longer hit the previous fail-closed 403; it reached the proxy/upstream and returned an upstream failure status (520 from the synthetic unauthenticated probe), proving the route is now matched. A real authenticated playback canary is still required to prove 302 -> redirect endpoint -> 206 and to distinguish upstream media policy from route reachability.

## Why Publication Missed This (Permanent Workflow Finding)

`internal/admin/publications.go` publishes after managed-route persistence and both edge sync results. Its `verify-proxy` action checks only query-free `System/Info/Public`; the stored publication intentionally remains `playback_status=unverified` until a separate authenticated client calls `playback-verify`. The publication agent accepts root-owned `redirect_endpoints`/`redirect_patterns`, but publication does not discover them from an authenticated VideoStream response. This is why API/images passed while the media redirect was absent.

The generic fix is procedural and schema-compatible, not a host hardcode:

1. Keep every new publication `playback_status=unverified` after API/edge sync.
2. Require a bounded authenticated PlaybackInfo + VideoStream canary on the active edge.
3. Capture only redirect scheme/port and a redacted host hash; do not persist query, token, cookie or full URL.
4. Validate the observed endpoint against the root-owned redirect policy, add it to the slug-scoped `redirect_endpoints` or narrow `redirect_patterns`, render both edges and run `nginx -t`.
5. Re-run the same Range request and require 200/206 plus growing bytes before `playback-verify` is allowed.
6. Mark `playback_status=ok` only after that evidence; otherwise leave the route published but visibly `unverified`/failed and block failover readiness.

This workflow must be implemented in the publication operator/agent as a canary gate; the current code already exposes the safe state and endpoint policy but does not have client credentials to perform the authenticated media step automatically.

**Current status:** route-specific redirect allowlist repair is installed and validated syntactically on both edges. Final success remains pending one fresh authenticated Yamby playback window showing a 206 response through the new `/http/<redirect-host>/80` alias and nonzero media bytes.

## Permanent-Fix Workstream Started (2026-08-22)

### Current status

- `1111` is playback-recovered in the real Yamby client after the scoped HTTP/80 redirect route was added on both edges.
- The incident is now treated as a regression case, not a one-off operational note.
- Production route configuration is currently healthy; no unrelated publication is being changed.

### Regression case captured

`PlaybackInfo=200` + API/image success + `VideoStream=302` + missing scoped media redirect route + edge fail-closed 403 + no 206/byte growth. The redirect endpoint used HTTP/80 even though the saved Emby upstream used HTTPS/443. Adding only the exact slug-scoped endpoint restored playback. UHD/feimu/yuchu already had additional media routes, explaining their different outcome.

### Permanent-fix investigation

The next code gate is to trace server create -> publication plan -> edge manifest -> candidate Nginx -> sync -> playback verification. The implementation must separate connectivity from playback readiness, discover redirect endpoints only from an authenticated bounded canary, keep unknown targets fail-closed, and add regression coverage for HTTP/80 plus unknown redirects.

## Permanent Code Fix Implemented (2026-08-22)

### Current status

- The code-level fix is implemented and tested in an isolated source tree. Production binaries and edge services have not yet been replaced by this worktree.
- The real `1111` route repair remains the known-good baseline. It was confirmed by Yamby before this code change.

### Regression case promoted to code

The incident is now a first-class regression case: `System/Info/Public`, images and `PlaybackInfo` may all return 200 while `VideoStream` returns 302. The redirect target can be a different host and can use HTTP/80. If the slug-scoped route is absent, the edge fail-closed rule returns 403 and the client never reaches a 206 response or sustained media byte growth. UHD/feimu/yuchu were green because their fragments already contained additional media routes. The exact scoped route repair restored `1111` in Yamby.

### Code changes

- `internal/publicationagent/playback.go` adds a pure canary validator and a root-owned runtime canary path. It accepts 301/302/303/307/308, extracts only scheme/host/port/path-prefix, recognizes HTTP/80, rejects private/unknown destinations, caps redirect hops and route count, and requires 200/206 plus Range headers and byte growth.
- `internal/publicationprotocol/protocol.go` carries a one-shot canary request over the peer-credential-protected Unix socket. The token and item ID are runtime-only and never logged or persisted.
- `internal/admin/publication_socket.go` exposes the canary call; `internal/admin/publications.go` adds `playback-canary` and makes the old `playback-verify` endpoint return `PLAYBACK_CANARY_REQUIRED`. A failed canary records a classified `playback_status=failed`; a passing canary records `playback_status=healthy`.
- `internal/storage/publications.go` adds `playback_failure_class` and makes the healthy state explicit. Re-publishing resets playback to `unverified`.
- `internal/publicationagent/config.go` adds a root-owned, mode-0600 discovered endpoint store under `/var/lib/embyproxy-publication-agent`; it is not a Git artifact. The agent merges only slug-scoped exact endpoints from that store into the manifest.
- The admin page now exposes a playback-canary action. It requests an item ID and runtime token without returning either value in the result. The UI displays `unverified`, `failed`, and `healthy` separately from edge `published`.

### Tests executed

In `/tmp/embyproxy-permanent-fix-test` on BWG, using an isolated `golang:latest` container:

- `gofmt` on all changed Go files: passed.
- `go test ./...`: passed, including `internal/publicationagent` HTTP/80/different-host/unknown-host/API-only/Range cases and admin state-gate tests.
- `go vet ./...`: passed.
- `git diff --check`: passed locally after synchronizing formatted sources.

### Safety gates and rejected approaches

- No unrestricted `/http/*` or `/https/*` wildcard was added. Unknown redirect hosts remain fail-closed.
- No endpoint is accepted merely because it appeared in an API response; it must be observed by the bounded authenticated canary, resolve to a public address, pass candidate edge sync, and then pass the media 200/206/byte-growth check.
- No token, cookie, Authorization value, complete media URL, or redirect query is persisted in the discovered endpoint store or response JSON.
- The publication operation may show `published` while `playback_status=unverified`; it must not show playback healthy until the canary succeeds. This preserves rollback and makes the remaining verification visible.

### Deployment gate

Before NOSLA/BWG rollout, build the exact commit in an isolated container, record the binary SHA-256, back up the current binaries/configuration on each edge, render candidate fragments, run `nginx -t`, and use the existing scoped helper reload path. Recheck `1111`, UHD, feimu and yuchu. If any edge or canary check fails, restore the operation backup and the prior binaries; do not widen the allowlist.

The publication-agent systemd sandbox must grant write access only to
`/var/lib/embyproxy-publication-agent` for the root-owned mode-0600 discovery
store. No other filesystem path is required by the canary.

### Reusable SOP

`Server create -> API connectivity -> authenticated PlaybackInfo -> VideoStream -> bounded redirect discovery -> slug-scoped exact route generation -> candidate fragment -> nginx -t -> NOSLA/BWG sync -> Range 200/206 and byte-growth canary -> playback_status=healthy -> publish complete.`

An API-only success is `connectivity healthy`, never `playback healthy`. Any failed canary is classified (`upstream_401`, `upstream_403`, `upstream_404`, `upstream_429`, `timeout`, `unknown_media_host`, `redirect_loop`, `invalid_range`, `no_byte_growth`, or `edge_sync_failed`) and leaves the route visibly unverified/failed.

## Final Code Review Adjustments (2026-08-22)

### Current status

- The permanent-fix implementation remains isolated until the rebuilt binaries pass all gates. Existing NOSLA/BWG binaries were not replaced by this worktree.

### Evidence and changes

- `validateStagedRoute` originally accepted only `publishing`. A playback canary that discovers a new scoped endpoint runs after the initial publish is already `published`, so the discovery update would be rejected. The check now permits only `publishing` or `published`, while retaining the managed-route line, enabled/public, and default-line checks.
- `canaryPublicURL` now recognizes an already encoded public route and avoids adding the slug twice. It also preserves runtime-only signed query parameters for the outbound canary request; they remain absent from logs, state, endpoint discovery and API responses.
- Added regression coverage for the encoded public path. Existing tests cover different media host, HTTP/80, private/unknown redirect rejection, API-only failure, Range headers, byte growth, and fail-closed rendering.

### Rejected hypotheses / safety decisions

- No unrestricted `/http/*` or `/https/*` location is acceptable.
- No redirect query, token, cookie or complete media URL is persisted.
- A `published` edge state is not playback healthy; only a passing bounded canary may set `playback_status=healthy`.

### Next step

Re-run the isolated full Go test/vet/build gate, then deploy only the resulting binaries after per-edge backup, candidate rendering, `nginx -t`, scoped reload, and regression checks for `1111`, UHD, feimu and yuchu.

## Isolated Gate and Edge Rollout (2026-08-22)

### Commands/tests and results

- `go test ./...` in `/tmp/embyproxy-permanent-fix-test` passed with `GOMAXPROCS=2` and `GOFLAGS=-p=1`; the first unrestricted build was killed by BWG memory pressure and was not treated as a code failure.
- `go vet ./...` passed with the same bounded compiler settings.
- Candidate builds passed. SHA-256: `embyproxy` `e06b5a8dec7a80bf2765b9deefe6d1d1cf3ce45cd199c552160ee65b514666d2`; publication-agent/edge `182970c41c77258da539f534fe8fbf32746ee6cfde3cd533901f6c0912739d6e`.
- NOSLA and BWG `nginx -t` both passed before and after the rollout. Existing `1111.conf` content was not changed by the binary rollout.

### Deployment and rollback evidence

- BWG publication-agent and sidecar binaries were backed up with `.bak-20260822` suffixes. NOSLA edge helper was backed up with the same suffix. The deployed candidate hash matches on BWG agent, BWG edge helper and NOSLA edge helper.
- Only the publication-agent service and the managed-route sidecar were restarted; Nginx was not reloaded because no fragment changed. Nginx remained active on both edges.
- A query-free `1111` public System/Info probe returned HTTP 200 after rollout. Existing edge fragments and the manually validated HTTP/80 route remain present.
- Rollback is `install` of the corresponding `.bak-20260822` binary, then restart only the affected publication-agent/sidecar unit. If a future fragment is changed, restore the operation backup, run `nginx -t`, and reload only after the candidate passes.

### Current status / remaining live gate

- The code and restricted edge adapter are deployed to NOSLA/BWG and healthy. The real Yamby playback result for `1111` remains the known-good baseline.
- A new authenticated playback canary still requires a runtime item ID and token at the admin endpoint; those values are intentionally not present in Git, logs, or this runbook. No credential was used or exposed during this rollout.
- No DNS, failover target, helper policy, unrelated Nginx server block, or wildcard redirect route was changed.

## Deployment Verification and Commit Gate (2026-08-22)

### Current status

- The permanent publication binaries are installed on both edges and the known-good
  `1111` scoped HTTP/80 route remains in place. The owner has independently
  re-confirmed Yamby playback after the original route repair.
- The deployment uses no broad dynamic route. The discovery implementation is now
  the only supported way to add a media endpoint for a new publication.

### Evidence

- BWG reports `embyproxy-publication-agent`, `embyproxy-gsy-sidecar`, and `nginx`
  as active. Its deployed agent/edge SHA-256 is
  `182970c41c77258da539f534fe8fbf32746ee6cfde3cd533901f6c0912739d6e`; the sidecar
  SHA-256 is `e06b5a8dec7a80bf2765b9deefe6d1d1cf3ce45cd199c552160ee65b514666d2`.
- NOSLA reports `nginx` active and its deployed edge SHA-256 is
  `182970c41c77258da539f534fe8fbf32746ee6cfde3cd533901f6c0912739d6e`.
- `nginx -t` succeeded on both edges. The `1111` fragments and their pre-repair
  backups remain present on both hosts. No fragment changed in this binary-only
  rollout, so no Nginx reload was required for the permanent code deployment.

### Final gate and residual risk

- The new authenticated canary intentionally requires a runtime token and an item
  identifier. They are not recorded in this repository, logs, or this runbook.
  The existing real Yamby recovery validates the repaired route; a future operator
  can use the admin `playback-canary` action to mark this publication `healthy`
  using a bounded authenticated request.
- The bounded isolated `go test ./...`, `go vet ./...`, formatting, diff, and
  regression gates passed before rollout. The permanent change was committed as
  `00d1610b33ff84aeadb09031482e34e31be13382` and pushed to
  `origin/feature/failover-phase2-local`; the remote ref was re-read after push.
- The bundle `embyproxy-playback-fix-00d1610.bundle` is retained locally with
  SHA-256 `F7BC4F03A661F1B1F07C438E3018615CFCF1B14011B1FF3504C93DD1BFE83A24`.
  The worktree still contains unrelated pre-existing changes; they were not
  staged or included in the permanent-fix commits.
- Residual live risk is limited to per-publication runtime credentials and media
  item selection for an authenticated canary. Those values remain runtime-only
  and are never persisted in Git, logs, state, or this runbook.

## Partial Playback Regression: Multiple Media Origins (2026-08-22)

### Current status

- `1111` playback is **partially fixed, not completed, and must not be marked
  healthy**. The owner confirmed that some Yamby selections play normally while
  another selection fails with `playback failed / source error / response 403`.
- The earlier HTTP/80 endpoint is therefore one valid media route, not proof that
  the publication's complete media-origin set has been discovered.

### Evidence and revised hypothesis

- A successful item follows `PlaybackInfo -> VideoStream -> redirect endpoint X
  -> 206 -> growing bytes`.
- A failing item follows the same API path but ends in 403 after its media
  redirect. The exact endpoint for the failing selection remains runtime
  evidence and is not copied into this repository.
- Source review shows the deployed canary accepts exactly one `item_id` and marks
  the publication healthy after that one item succeeds. This is the code-level
  reason a heterogeneous server can be falsely green even though another item
  uses a different CDN/origin.

### Rejected hypotheses / safety decisions

- Do not assume one host, scheme, port, or path prefix covers all media on a
  server.
- Do not manually widen `1111.conf`, add suffix wildcards, or infer a CDN from a
  403 alone.
- Discovery remains bounded, slug-scoped, public-address-only, and fail-closed.

### Next step

- Extend the runtime-only canary request to accept a bounded set of media item
  IDs, execute every item independently, merge only endpoints actually observed
  in their redirect chains, and mark playback healthy only when every requested
  sample reaches 200/206 with byte growth. Add regression coverage for item A on
  endpoint X and item B on endpoint Y, including the API-healthy/media-403 state
  gate. Then rebuild and roll out the publication components on NOSLA and BWG.

### Implementation and test update

- Implemented bounded `item_ids` (one to eight, distinct, runtime-only) while
  retaining the legacy single `item_id` request for compatibility. The admin UI
  now treats a comma-separated entry as a sample set.
- Each sample refreshes the manifest after prior scoped discovery. A failure
  returns `playback_status=failed` with its classified reason and a count of
  samples passed; no successful first sample can mark the set healthy.
- Safely observed endpoints are retained only after edge synchronization, even
  if a later sample receives upstream 403. They remain exact slug-scoped routes,
  and the status remains failed until every sample succeeds.
- The isolated BWG test clone passed `TestHeterogeneousMediaSamplesRequireEveryItemToPass`,
  `TestNormalizedCanaryItemsSupportsBoundedDistinctSampleSet`, the existing
  `TestPlaybackCanary*` suite, and `go vet` for publication-agent/protocol.
  A full admin-package test gate is separately blocked by pre-existing local
  admin test/source drift, not by the staged multi-sample code.
- The permanent change is committed and pushed as `0a5faa6a32644e731982fd2c900b75a809b31c92`
  on `origin/feature/failover-phase2-local`. Deployment remains pending fresh
  sidecar and publication-agent builds; no fragment has been edited manually.

### Live partial-failure evidence and rollout (2026-08-22)

- A redacted NOSLA correlation of the owner's Yamby window proves two distinct
  HTTP/80 media destinations for `1111`. The existing scoped endpoint (host
  fingerprint `156fca61f27c`) returned 206 with multi-megabyte byte growth for
  one title. A different endpoint (host fingerprint `94b771d3f2dd`) followed a
  primary `VideoStream=302` for failing titles and immediately returned the
  local small-body 403 response.
- The `1111` fragment contains only the saved upstream HTTPS/443 endpoint and
  the former HTTP/80 endpoint; it does not contain fingerprint
  `94b771d3f2dd`. This classifies the observed 403 as an edge fail-closed
  missing scoped route, not an upstream permission denial. The actual host,
  item identifiers, query strings and credentials were not retained.
- This validates the heterogeneous-origin regression: the original manual
  repair fixed endpoint X, while item B uses endpoint Y. `1111` remains
  **playback partially fixed / failed-unverified**, never healthy.
- Candidate binaries built from `0a5faa6` were installed with backups on both
  edges. Publication-agent/edge SHA-256 is
  `a25ca3bc368d852afc31fd4d4718be8b716ac9f6bbcab4b6af7188d067dcf411`;
  BWG sidecar SHA-256 is
  `70994936c14264262a9900386d8529ead7eac13ac566f07f990bc11ba9e8fabc`.
  BWG publication-agent and sidecar, NOSLA Nginx, and BWG Nginx are active;
  `nginx -t` passed before and after. No fragment changed and no Nginx reload,
  DNS, failover, helper policy, or wildcard route change occurred.
- NOSLA/BWG `1111` fragment byte hashes differ because edge rendering includes
  node-local values, but their security/media structure is identical: 152 lines,
  four exact locations, four Range and If-Range forwarders, four disabled
  request/response buffering directives, and eight bounded redirect rewrites.
  Neither fragment includes the failing endpoint fingerprint.

### Current gate

- To discover endpoint Y through the permanent path, run the deployed admin
  playback canary with a bounded sample set containing both a known-working
  title and a title that reproduces the 403. The runtime token and item IDs
  must be entered only into the authenticated admin UI; they must not be sent
  through chat, stored in Git, logged, or copied into this runbook. The canary
  will render the exact Y route on both edges and leave the publication failed
  unless every sample proves 200/206 plus byte growth.

### Runtime sample correlation (2026-08-22)

- NOSLA access-log correlation found a concrete successful sample and a concrete
  failing sample without copying URLs, query strings, headers, or credentials:
  the successful item identifier is represented only by SHA-256 prefix
  `3c59ce0b8e00`; the failing item identifier only by SHA-256 prefix
  `87cce547bec0`.
- The successful sample reached the existing HTTP/80 endpoint with 206 and
  multi-megabyte byte growth. The failing sample repeatedly reached the primary
  302 and then the missing HTTP/80 endpoint with the local 403 body. This is the
  live X/Y regression required by the second-stage acceptance.
- No safe runtime Emby token is persisted or derivable from the admin token,
  publication-agent SSH identity, or edge environment. The final canary must be
  initiated from the authenticated Admin publication page, where the owner
  enters the two item IDs and token in memory only. The token will not be sent
  in chat, written to Git, logs, state, or this runbook.

## Integrated Credential Provisioning (2026-08-22)

### Current status

- Playback credential provisioning is integrated into the Admin publish/refresh
  action. A separate raw-token prompt is no longer part of the publication UI.
- The publish sheet accepts optional Emby username/password. Empty fields reuse
  the existing protected credential; if none exists, publication remains
  `playback_status=unverified` with `credential_missing`.

### Evidence and implementation

- The backend calls official `POST /emby/Users/AuthenticateByName` over HTTPS,
  receives `AccessToken`, and writes only that token to the existing protected
  file store. Username/password are request-scoped and are cleared after the
  authentication exchange; they are not persisted, returned, logged, written to
  SQLite, Nginx, Git, or edge manifests.
- Successful provisioning reuses the protected token to discover a bounded set
  of up to eight item IDs through `Users/Me` and `Users/{id}/Items`, then invokes
  the existing multi-sample canary. Authentication failures are classified as
  `credential_invalid` without exposing credential material.
- The UI exposes `配置凭据并验证` / `重新授权并验证` through the same publish
  dialog. The stored credential status remains boolean-only.

### Tests

- Added tests for official authentication request shape, auth-failure redaction,
  bounded item discovery, and the publication UI marker. These pass in the
  isolated BWG Go 1.26.4 build clone.

### Remaining live gate

- The `1111` 625260/601953 real canary cannot run until an owner submits valid
  Emby username/password once through the new Admin publish dialog. No valid
  credential is currently available in BWG's protected store. After that one-time
  provisioning, route discovery and re-canary are automatic. `1111` remains
  `failed-unverified` until both real samples reach 200/206 with Range headers
  and sustained byte growth.
