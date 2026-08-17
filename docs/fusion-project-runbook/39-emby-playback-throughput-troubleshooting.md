# Emby playback throughput troubleshooting

Date: 2026-08-15 Asia/Shanghai

## Scope and safety

This runbook diagnoses a published server whose library and images work but
whose VideoStream has no speed. Do not change DNS, ACME, failover policy or an
unrelated slug. Never print or persist a query, token, cookie, authorization
header, complete UUID or complete playback URL. Use path classes and host
hashes in operator output.

## Evidence chain

Compare the working UHD sample and the affected slug at every layer:

```text
Yamby -> stream DNS -> active edge -> slug Nginx fragment -> loopback media
proxy -> saved Emby host -> controlled redirect host -> 200/206 bytes
```

For PlaybackInfo, VideoStream, HLS manifest and segment requests record only:
edge, slug, line ID, status, Range present, Content-Range present, bytes sent,
TTFB, request time, upstream response time and redirect host hash.

## Range and throughput gate

An authenticated canary must test `bytes=0-1048575` and
`bytes=0-8388607`. Accept 200 only when bytes continuously grow; normally a
range-capable direct stream returns 206 with Content-Range and Accept-Ranges.
Read for ten seconds and calculate Mbps from received bytes. The default
minimum is 1 Mbps and must remain configurable.

Classify the result as one of:

- `playback_ok`: 200/206, sustained bytes and throughput above threshold.
- `upstream_slow`: both edge-direct upstream and public proxy are similarly slow.
- `range_failed`: Range was sent but no usable 200/206 byte stream resulted.
- `redirect_direct`: the client was intentionally sent to a controlled direct host.
- `redirect_host_unallowed`: redirect target fell through to edge 403/404.
- `proxy_failed`: upstream is healthy but the public path fails or stalls.
- `unverified`: no authenticated playback request was available.

Do not report an unauthenticated 401/403 probe as a throughput failure.

## Redirect diagnosis

If the primary returns 302/307/308, correlate the next query-free request by
time and host hash. A zero upstream-response-time 403 identifies the edge
allowlist, not the Emby server, as the failing layer. A redirect followed by a
slow upstream response identifies the upstream/CDN path instead.

Only a saved target or a root-owned slug-specific redirect-host entry may be
rendered. Refreshing an existing publication atomically replaces that slug's
fragment on BWG and NOSLA. It must not add a generic `/https/<host>` location.

## Streaming fragment baseline

Every generated primary, backup and redirect location must include HTTP/1.1,
Range and If-Range forwarding, `proxy_buffering off`,
`proxy_request_buffering off`, `proxy_cache off`, zero proxy temp-file size,
gzip off, SNI-correct loopback proxy behavior, long read/send timeouts and the
existing authentication/User-Agent header policy. Candidate and host
`nginx -t` must pass before graceful reload.

## Multi-line partial failure

The first saved line is the stable public primary. Later healthy lines are
backups. A retryable response on main may internally rewrite to a literal
backup target. A failed backup must not change the public address or disable
main. Saving an already-published node performs a route diff for that slug,
backs up both fragments, tests, reloads and commits. On any failure, restore
the old node, route lines and edge fragments.

## Single-slug rollback

1. Stop further updates for the affected slug only.
2. Restore the central database backup or the saved node, publication and
   managed-route rows for that slug.
3. Run the BWG and NOSLA operation-specific rollback scripts recorded by the
   restricted helper.
4. Run `nginx -t` on each edge and graceful reload.
5. Verify UHD's fragment hash and public entry are unchanged.
6. Confirm stream Admin isolation, active target, policy and manual hold.

Never remove the global publication include directory or global allowlist.

## Redirect endpoint forms

An observed media redirect is not always `https` on port `443`. The
root-owned publication-agent configuration supports two constrained forms:

- `redirect_endpoints`: one exact `scheme`, `host` and `port`. A literal IP is
  allowed only here, only when it is globally routable. Saved upstream targets
  remain DNS-only; loopback, private, multicast, link-local and unspecified
  addresses are rejected.
- `redirect_patterns`: a fixed scheme, port, DNS suffix and fixed left-most
  `[a-z0-9]` label length. This is only for an observed CDN that rotates that
  one label. It is rendered as one slug-scoped Nginx regex location; no generic
  host or arbitrary port route is created.

Both are root-owned configuration. The owner UI and publish API can never pass
an endpoint, suffix or regex. Refreshing the affected published slug runs the
usual candidate test, atomic fragment replacement, host `nginx -t` and
graceful reload on both edges.

## 2026-08-16 Production Evidence

Yuchu demonstrated a two-hop chain: the saved HTTPS upstream returned `307`
to an exact HTTP non-standard-port endpoint, which returned `302` to a second
exact HTTPS endpoint. Before both endpoints were root-allowlisted, the public
rewritten request fell through to stream `403`. After the single-slug refresh,
both direct and stream requests returned `206` with `Content-Range` and
`Accept-Ranges`; 1 MiB and 8 MiB reads sustained above the 1 Mbps threshold.

Feimu's historical VideoStream evidence shows the primary returning `302` to
three observed, rotating four-character CDN labels under one controlled suffix.
The publication fragment now includes the narrow pattern described above and
response-Location rewrites on NOSLA and BWG. The rewrite returns only a
pattern-matched CDN redirect to an allowlisted stream path, so the client is
not sent to an unreviewed CDN hostname. It passed candidate and host Nginx
tests. A fresh authenticated Feimu VideoStream request is still required
before classifying it `playback_ok`; a home page, image, or unauthenticated
`401` is not evidence.

An earlier generated pattern used an Nginx `{n}` quantifier directly in an
unquoted `location` expression, which Nginx tokenized incorrectly. The
renderer now emits repeated literal character classes and candidate tests the
result before a fragment can be replaced.

## Logging Boundary Follow-up

Nginx stream access logs and central stats remain query-free. The legacy
`stream-erpgo` container, however, can include a full upstream request URL in
its Docker error output when a client cancels an upstream request. Do not copy
or print those lines. The container has no exposed runtime logging-redaction
setting, so this is an open security follow-up: rebuild or replace that
application with a redacting logger, or recreate only that container with an
approved logging sink after a dedicated UHD playback-impact gate. Do not claim
full raw-log redaction until that change is verified.

Record operation-specific rollback paths in the private deployment record, not
in Git. Pass only the reviewed operation directory to the matching node-local
rollback script. A rollback may restore only the saved binary, root-owned
config, one slug fragment or query-free logging include named by that operation.

## 2026-08-15 Location-Rewrite Deployment

The missing playback behavior was diagnosed at the media response boundary:
Feimu produced real `302` VideoStream responses while UHD produced sustained
`206` media responses. The publication template previously staged the allowed
redirect destination but did not rewrite `Location`, so a client could follow
the upstream/CDN address directly rather than return through stream.

The restricted helper now renders `proxy_redirect` directives only for manifest
routes. It supports saved upstreams, exact root-owned redirect endpoints, and
the fixed four-character Feimu CDN pattern. It has no catch-all redirect rule.
Both BWG and NOSLA helpers were upgraded and each affected slug was refreshed
through the authenticated publication API. The refresh performed candidate
test, host `nginx -t`, and graceful reload on both edges.

The test gate remains strict: observe a new authenticated VideoStream request
and report only status, `Range` present, `Content-Range` present, byte count,
TTFB and throughput. `302` followed by a rewritten stream path and a sustained
`200`/`206` is `playback_ok`; a missing authenticated request remains
`unverified`, not a pass or an upstream-slow diagnosis.

## Post-rewrite owner playback evidence gate

Record the query-free log inode, byte offset, UTC timestamp and central-store
totals immediately before the owner starts playback. Attribute follow-up media
requests using every exact and fixed-pattern location in that slug's deployed
publication fragment, not only the saved upstream prefix. Redirected CDN paths
otherwise appear unrelated and produce a false zero-byte result.

For each slug, report only path class, status, request count, request bytes,
response bytes, request time, upstream response time and derived throughput.
Never print the matched location, URI, item identifier or redirect target.
Require a `VideoStream`/HLS request with sustained `200` or `206` bytes before
marking playback successful. A fragment containing `proxy_redirect` rules is
configuration evidence, not client playback evidence.

Central snapshot imports can lag the edge log. A central-store increase after
the baseline may therefore contain older NOSLA buckets. Do not attribute that
increase to the current playback until the query-free edge log contains the
corresponding post-baseline request. If feimu, yuchu or UHD has no matching
request after the baseline, report `client_playback_not_observed` and keep the
gate open instead of reporting zero throughput as a proxy failure.

## Dual-edge live capture evidence

The previous post-playback check used a baseline created after the owner had
already played all three servers and assumed access records were written while
the stream was active. Both assumptions were wrong. Nginx writes the access
record when a long response closes. During the corrected capture, one Yuchu
`206` response ran for about 88.7 seconds and completed after the capture
started even though it had begun before the start offset. This directly proves
that an EOF-only instantaneous check can miss an in-flight playback.

The corrected gate ran a bounded, root-only capture on NOSLA and BWG. It began
at each query-free access/error-log EOF, sampled established HTTPS connections
and interface counters once per second, attributed all primary, exact redirect
and fixed-pattern aliases from the deployed fragment, and stored only slug,
edge, stage, status, byte counts, timings and a short route-host hash. Raw URI,
query, headers, identifiers and Location values were never stored.

An accepted sequence contains a primary request, any allowlisted 302/307/308
alias hop, and a final sustained `200` or `206` response above the configured
throughput threshold. Attribute the response to the effective edge and compare
the inactive edge to rule out split serving. Store only aggregate counts,
bytes, timings and short host hashes; keep raw logs outside the repository.

The current query-free Nginx format does not include safe `range_seen`,
`content_range_seen`, `upstream_status` or Location-class fields, so those
values remain `unavailable` rather than being invented. A `206`, the deployed
Range/If-Range forwarding directives, Content-Range pass-through directives,
the allowlisted stream alias follow-up and successful client playback provide
the acceptance evidence. The capture reader already accepts future boolean or
classified fields if a separately reviewed query-free log-format migration is
performed.

## Persisting an accepted playback result

Publication state now stores `playback_status` and
`playback_verified_at`. New publications default to `unverified`. The
authenticated, serialized `POST
/api/admin/emby-servers/{slug}/playback-verify` operation may set `ok` only
when the database row is published, both edges are synced and a public URL is
present. Use it only after retaining a redacted live-capture result; it is not
a replacement for the playback canary.

After an accepted dual-edge capture, mark only the tested published slug `ok`.
The owner API/UI should then show published, both edges synced and playback
speed normal. Legacy mappings without an `emby_publications` row remain
`unverified`; never infer verification from a home-page or image request.

The schema upgrade is additive and was tested from the historical table shape.
An old published row migrates to `unverified`; no existing node is silently
marked successful. The UI already maps `ok` to the green playback-speed-normal
label and keeps all other published nodes unverified until accepted evidence is
recorded.
