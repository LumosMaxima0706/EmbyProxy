# NOSLA-primary / BWG-fallback And Public Admin Runbook

Status: PHASE A/B IMPLEMENTED LOCALLY; REMOTE BACKUP/DRY-RUN PENDING

## Owner-confirmed billing source (2026-08-12)

- Observation time: `2026-08-12T16:21:00+08:00`.
- Source: owner provider panel; units remain provider-panel GB, not GiB.
- Billing direction: provider-billed RX+TX (ingress+egress).
- NOSLA: 1100 GB quota, 484 GB opening usage, 44.00%, 85% switch
  threshold (935 GB), 451 GB remaining before threshold, cycle
  `2026-07-21` through `2026-08-21`.
- BWG: 2000 GB quota, 217 GB opening usage, 10.85%, cycle `2026-08-07`
  through `2026-09-07`.
- Until a provider API is authorized, the traffic source is explicitly
  `owner_provider_seed_plus_host_rx_tx_estimate`. The provider values are the
  opening balances and restricted host counters add subsequent RX+TX bytes.
  This estimate is not represented as provider billing. It must be calibrated
  against the next provider-panel cycle.
- A reset-cycle baseline is established only after the six-hour provider grace
  window. Missing counter input prevents an automatic return to NOSLA. It does
  not force a healthy active NOSLA away merely because accounting is unknown;
  health failure can still switch to BWG.

## Confirmed historical NOSLA incident

The 2026-08-07 failover was not caused by DNS selection, IPv6, SNI, status-code
logic, or debounce defects. Both fixed-IPv4 HTTPS probes used the production
hostname for TLS SNI and timed out at approximately 12 seconds from 13:30 UTC.
Three consecutive failed evaluations triggered the DNS switch to BWG at 13:40
UTC. Failures continued for roughly 50 minutes; a later connection refusal was
also recorded before recovery. The current bounded history has ten consecutive
successful HTTP 200 results. The legacy checker was not invoked during the
latest read-only audit because it mutates health history.

## Selected controller and switching architecture

- Production switching remains the existing exact `stream` A-record adapter,
  TTL 60, with NOSLA/BWG IPv4 allowlisting. Managed route `v1`, canary,
  `stream-b`, and `staging-stream` are outside the mutation scope.
- The existing five-minute cadence is retained. At final handoff there will be
  exactly one controller: the old timer is disabled before the new timer is
  enabled. No overlapping controller window is allowed.
- New runner files are under `deploy/failover/`. It is lock protected,
  idempotent, defaults to dry-run, reads root-only configuration, performs only
  `/health` and the small system-information request, writes an atomic state
  file, backs up the prior DNS value before a switch, verifies the new target,
  and restores the prior value on verification failure.
- A restricted NOSLA meter account will use an SSH forced command that returns
  only interface RX/TX counters. It has no interactive shell and the BWG
  identity/known-hosts files remain root-only.
- Five minutes is retained instead of fifteen because it preserves the proven
  three-sample health debounce response while adding negligible small-endpoint
  traffic. No media URL is checked.

## Policy configuration

- `FAILOVER_MODE=dry-run|auto`; initial deployed mode is `dry-run`.
- `MANUAL_HOLD=none|nosla|bwg`; initial value is `none`.
- Preferred primary `nosla`; fallback `bwg`.
- NOSLA switch threshold 85%; return hysteresis 80%; new reset-cycle return
  threshold 15% after a six-hour grace window.
- Health switch requires three consecutive failures; return requires three
  consecutive successes; switch cooldown is one hour.
- Timezone is explicitly `Asia/Shanghai`; reset days are NOSLA 21 and BWG 7.

## Local scenario and safety validation

- Healthy and 44% usage selects NOSLA.
- 85% usage selects BWG.
- Three NOSLA health failures select BWG.
- Healthy new cycle below return threshold selects NOSLA after grace.
- Manual holds select their requested target and record the risk boundary.
- Unknown usage holds an already-active NOSLA but blocks BWG-to-NOSLA return.
- Dry-run calls no DNS mutation adapter.
- Simulated post-switch health failure restores the previous target.
- Nine Python policy/rollback tests, Python compile, shell syntax,
  `git diff --check`, full Go tests, and Go vet pass locally.

## Runtime no-cache evidence

- BWG and NOSLA streaming Nginx locations explicitly set
  `proxy_buffering off`, `proxy_request_buffering off`, and `proxy_cache off`,
  and preserve Range and If-Range headers.
- Scoped configurations contain no `proxy_cache_path`, slice preload, or
  background update directive.
- BWG live SQLite read-only inspection shows `imageCacheEnabled=false` through
  the safe default because `system:config` is absent. No config value was
  changed.
- NOSLA uses the v1.3 container on loopback port 18080 with no persistent
  mounts or cache-related environment keys; no prefetch/warmup marker was found
  in the bounded container audit. No media request was made.

## Secure public Admin design

- Dedicated hostname: `owner-admin.149077530.xyz`; dedicated exact-allowlist A
  record to BWG with TTL 60.
- Nginx Basic Auth is the outer layer. The Basic Authorization header is
  consumed and cleared before proxying; the application then requires its
  existing ADMIN_TOKEN login and issues its path-scoped secure session cookie.
- Only `/admin`, `/admin/`, `/api/admin`, and `/api/admin/*` proxy to
  `127.0.0.1:18082`; every other path, including `/s/`, returns 404.
- The dedicated access format records method plus `$uri`, never request args,
  Authorization, Cookie, or Referer. Rate limiting and no-store/fail-closed
  headers are enabled. The htpasswd/password files will be mode 0600/root-only.
- The canary Admin deny remains unchanged. The sidecar listener remains
  loopback-only.

## Remaining apply gates

1. Create and verify timestamped BWG/NOSLA backups and an exact rollback script.
2. Install the restricted meter with a pinned host key; verify it has no shell.
3. Stage the runner/config/units and complete dry-run plus scenario fixtures.
4. Validate Nginx Admin staging config with temporary certificate material,
   then perform DNS/certificate/readiness checks.
5. Hand off from legacy to new timer with no overlap, initially dry-run.
6. Verify a real dry-run decision and state file, then enable auto only after
   all gates pass. The first policy-consistent production decision is NOSLA if
   the live health and traffic samples remain fresh.
7. Apply the independent Admin entry and verify outer 401, inner application
   authentication, `/s/` 404, canary Admin 404, loopback listener, and logs.

## Verified starting point

- The dedicated BWG-only canary is live and accepted for owner self-use.
- Retained managed route slug: `v1`.
- Public media entry: `https://canary.149077530.xyz/s/v1/`.
- The small public information endpoint returned HTTP 200 with valid TLS.
- Public `/admin`, `/api/admin`, and `/api/admin/status` returned 404.
- `embyproxy-gsy-sidecar.service` listens only on `127.0.0.1:18082`.
- Existing production and staging entries were unchanged by the canary cutover.

## Authorized scope

This stage may change only the NOSLA/BWG failover policy, traffic accounting,
the dedicated scheduler/timer, the selected managed-route line or other
topology-supported switching mechanism, a separately secured public Admin
entry, and this runbook. NOSLA access is authorized, beginning with read-only
discovery. Every live change requires a verified backup, dry-run, exact
rollback, and bounded post-change verification first.

## Prohibited actions

- Never expose Admin without both an ingress authentication layer and the
  existing application authentication boundary.
- Never print or record credentials, cookies, private keys, complete query
  strings, complete UUIDs, or subscription links.
- Never pre-cache, prefetch, warm up, background-fetch, or probe media.
- Never use a media object for an automated healthcheck.
- Never modify a production entry without a verified backup and rollback.
- Never force push or push `main`/`master`.
- Never perform a large-traffic smoke test or reboot either host.

## Phase 1 read-only discovery checklist

- BWG and NOSLA: Nginx, rathole, sidecar, listener, managed-route metadata,
  timezone, timers, traffic counters, and no-cache configuration.
- Public topology: production `stream`, `stream-b`, `staging-stream`, and the
  dedicated canary, without emitting configuration secrets.
- DNS automation capability and protected credential-file metadata only.
- Managed-route support for multiple lines, `default_line`, enabled/public
  flags, and any health state.
- Existing failover, health, scheduler, traffic accounting, provider API,
  `vnstat`, nftables/iptables, and reset-cycle support.
- Application and ingress searches for cache, prefetch, warmup, background
  update, media probes, and request transformations that could add traffic.

## Phase 1 discovery result (2026-08-12)

- BWG has active Nginx, rathole, the loopback-only sidecar, and a five-minute failover timer. NOSLA has active Nginx and its separate admin/proxy service; it does not run the BWG sidecar or rathole service in this topology.
- Existing production/staging/canary ingress blocks remain separate. Production failover is DNS-based, not a managed-route default-line switch.
- Small `/health` and system-information checks returned HTTP 200 on NOSLA, BWG, and `stream-b`; no media object was requested. The legacy check appended bounded health history, recorded as a discovery deviation.
- Live state is active target `bwg`, automatic evaluation armed. Existing policy uses Asia/Shanghai, reset days 21/7, a 95% threshold, and a return window; it does not implement the requested 85% reset-cycle rule.
- Traffic accounting is unsuitable for formal auto mode: config contains a manual NOSLA quota/usage baseline and no BWG usage; no `vnstat` or verified provider billing API/CLI was found. Generic nftables/iptables counters do not prove provider billing usage.
- Scoped Nginx streaming locations disable buffering and cache. No scoped cache path, background update, slice preload, or prefetch directive was found. Application image-cache runtime state remains to be confirmed.
- NOSLA reports Asia/Shanghai and BWG UTC, so policy time must explicitly use Asia/Shanghai.
- No public Admin hostname/server block was created; canary Admin paths remain denied.

## Owner input required

Provide or authorize a safe BWG-local source for NOSLA monthly quota/current-cycle usage, BWG monthly quota/current-cycle usage, and whether accounting is provider billing RX+TX or an explicitly approved alternative. No secret belongs in chat. Until then no new auto runner, timer, DNS switch, or public Admin entry is applied.

## Phase 2 local policy result

- Preferred primary is NOSLA; fallback is BWG.
- Default NOSLA switch threshold is configurable and set to 85%.
- NOSLA/BWG reset days are configurable and default to 21/7.
- Policy time is explicitly Asia/Shanghai, independent of host timezone.
- Return from BWG requires three healthy NOSLA checks, a newly confirmed
  billing cycle, a six-hour reset grace, and known usage below the configurable
  15% return threshold. Unknown/stale usage is fail-closed by default.
- `MANUAL_HOLD=none|nosla|bwg` maps to auto/force-NOSLA/force-BWG decisions.
- Cooldown and switch-window rate limiting remain in the shared controller.

## Dry-run runner

`cmd/failover-policy` accepts a bounded, non-secret JSON state snapshot from
stdin, takes an exclusive lock, evaluates the shared policy, and emits a
redacted JSON decision. It defaults to `FAILOVER_MODE=dry-run` and always
reports `mutation_applied=false`. `FAILOVER_MODE=auto` fails closed because no
production apply adapter has been configured yet. It does not call DNS, the
Admin API, or a provider.

Local tests cover healthy/below threshold, threshold fallback, health fallback,
reset/grace return, both manual holds, dry-run non-mutation, and auto refusal.
Targeted tests, `go test ./...`, `go vet ./...`, and `git diff --check` passed
with the temporary non-installed Go 1.26 toolchain.

## Phase status at owner-input gate

- Phase 0: DONE.
- Phase 1: DONE, with one documented state-history write deviation.
- Phase 2: DONE locally; not deployed.
- Phase 3: BLOCKED on reliable provider billing/quota input.
- Phase 4: scoped Nginx scan PASS; app runtime image-cache check pending.
- Phase 5/6: runner decision layer exists locally; no apply adapter selected or
  installed. Existing production still uses the legacy DNS mechanism.
- Phase 7: local deterministic scenarios PASS; live canary mutation/rollback
  simulation not run.
- Phase 8: NOT RUN.
- Phase 9: NOT RUN; SSH tunnel remains the only Admin access method.
