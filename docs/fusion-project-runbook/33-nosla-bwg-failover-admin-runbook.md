# NOSLA-primary / BWG-fallback And Public Admin Runbook

Status: PHASE 1 DISCOVERY COMPLETE; BLOCKED BEFORE BACKUP/APPLY

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
