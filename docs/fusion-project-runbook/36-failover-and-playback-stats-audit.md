# Failover and Playback Statistics Audit

Date: 2026-08-14 Asia/Shanghai
Status: DONE (NOSLA statistics collector remains a separate follow-up gate)

## Safety boundary

- Keep the currently working UHD playback path and upstream unchanged.
- Do not delete nodes, clean deployment directories, request ACME, force-push,
  change main/master, or perform a real forced failover in this audit.
- Never print or persist tokens, cookies, passwords, Authorization headers,
  complete query strings, UUIDs, private keys, or subscription URLs.
- Use only small health endpoints for probes. Do not synthesize or replay an
  authenticated media URL from logs.
- Any production configuration change requires a dedicated backup, staged
  validation, immediate post-change healthcheck, and an exact rollback path.

## Step status

| Step | Status | Observation | Decision | Next action |
| --- | --- | --- | --- | --- |
| Runbook 0 baseline | DONE | Git, binary, Nginx, systemd, timer, state, DNS and service facts captured | Proceed with root-only backups | Inspect failover API/UI and timer mutation path |
| Runbook 0 rollback point | DONE | BWG/NOSLA snapshots and rollback scripts completed; checksums and `bash -n` passed | No rollback executed | Continue read-only diagnosis and staged repair |
| Runbook 1 API/UI mapping | DONE | Admin API used an empty in-process fixture; external policy state was not projected | Overlay sanitized policy state and bind UI to active target | Deploy BWG binary and verify API/UI |
| Runbook 2 production route wiring | DONE | Policy timer is active/enabled and mutation-capable; DNS and playback evidence point to NOSLA; no forced switch performed | Keep production unchanged and verify post-deploy state | Verify timer, DNS, health, and serving-node consistency |
| Runbook 3 playback statistics | DONE | Real playback is on NOSLA; BWG local SQLite counters are empty by design; NOSLA access-log aggregation shows PlaybackInfo, Sessions and 206 media traffic | Mark local stats unavailable when active target is NOSLA; do not fabricate zero | Deploy API/UI source-status change; plan cross-host aggregation separately |
| Runbook 4 minimal repair | DONE | BWG sidecar and runner state-file permission fix deployed; one shell interpolation issue was corrected immediately | Keep policy, DNS, Nginx and upstream unchanged | Complete post-deploy checks |
| Runbook 5 verification | DONE | Authenticated API/UI contract, public health, isolation, listener, timer and log redaction checks pass | Playback stats remain explicitly unavailable until a NOSLA collector is built | Owner handoff; no cleanup or forced switch |

## Baseline (脱敏)

- Local branch: `feature/failover-phase2-local`.
- Local and origin feature HEAD: `df17eb5`.
- Live BWG binary release: `f4d2d9d`.
- BWG sidecar: active/enabled, `NRestarts=0`, loopback-only
  `127.0.0.1:18082`.
- Failover policy state: `mode=auto`, `active_target=nosla`,
  `previous_target=bwg`, `manual_hold=none`, decision
  `nosla_healthy_below_threshold`.
- Policy timer: active/enabled with a finite next trigger; legacy
  `stream-failover.timer`: inactive/disabled.
- NOSLA Nginx and public Admin include hashes are recorded in the execution
  log; Nginx is active/enabled and `nginx -t` passes.
- `stream.149077530.xyz` currently resolves to the NOSLA public address and
  `/health` returns HTTP 200. Retained canary info returns HTTP 200.
- Current Yamby playback URL is unchanged and owner-reported working.

## Planned rollback snapshot

The snapshot must contain the live BWG binary/release link, sidecar EnvironmentFile
and unit, failover state, owner-admin static/config files, NOSLA Nginx config and
admin include, effective `nginx -T`, service/timer status, checksums, and a
root-only rollback script. No rollback is executed as part of the baseline.

## Evidence rules

- API/UI fields are reported as names and non-sensitive values only.
- Access-log evidence is normalized to route type, path type, status, bytes and
  timing; query strings and identifiers are discarded before output.
- A zero UI value is not interpreted as a true zero until its data source and
  collection path are proven. Unsupported metrics must be shown as unavailable,
  not fabricated.

## Findings and repairs

- The external policy runner is the production state source. Its current
  sanitized decision is `auto`, active target `nosla`, manual hold `none`, and
  reason `nosla_healthy_below_threshold`. DNS resolves the public stream host to
  NOSLA and bounded access-log summaries show successful API and HTTP 206 media
  traffic there. No forced failover was run.
- `/api/admin/failover/status` previously returned only the empty in-process
  fixture controller (`active_node_id` empty, `nodes` empty). The handler now
  overlays allowlisted decision fields from `FAILOVER_STATE_FILE` and exposes
  `active_target`/`activeTarget` plus `state_source=policy_state_file`.
- `stats.get` previously returned BWG-local SQLite rows without indicating that
  BWG was not serving the active stream. It now returns
  `stats_source=local_sidecar_store` and `stats_available=false` with a bounded
  reason when the policy state says active traffic is on NOSLA; the UI renders
  `N/A` instead of misleading zero totals.
- Tests: `go test ./...` and targeted `go vet` passed before live deployment.

## Deployment and verification

- BWG backup:
  `/var/backups/embyproxy-failover-stats-audit/20260814T134500Z-bwg`.
  Rollback script: the `rollback.sh` inside that directory.
- NOSLA backup:
  `/var/backups/embyproxy-failover-stats-audit/20260814T134500Z-nosla`.
  Rollback script: the `rollback.sh` inside that directory.
- Final BWG release: `/opt/embyproxy-gsy-sidecar/releases/804b242`; its binary
  reports source commit `804b242`.
  The initial remote command expanded a local shell variable and temporarily
  pointed `current` at the releases root. The service remained healthy; the
  link was immediately corrected to the explicit release directory and
  reverified. No DNS, Nginx, upstream or failover decision changed.
- The policy runner now writes only its state file as `0640`, inheriting the
  state directory group. Live state is `root:embyproxy-gsy-sidecar 0640` after a
  runner execution. Other JSON backups continue to use `0600`.
- Authenticated status API: PASS. Active target `NOSLA`, mode `auto`, manual
  hold `none`, reason `nosla_healthy_below_threshold`, source
  `policy_state_file`.
- Stats API/UI contract: PASS. Source `local_sidecar_store`, available `false`
  while NOSLA serves production, reason `active_traffic_served_by_nosla`; UI
  displays `未接入` instead of fabricated zero totals.
- Service/timer: sidecar active/enabled with zero restarts and loopback-only
  `127.0.0.1:18082`; new timer active/enabled with a finite next trigger;
  legacy timer inactive/disabled.
- Public checks: stream health HTTP 200; canary info HTTP 200; canary and stream
  admin paths HTTP 404; owner-admin unauthenticated HTTP 401; owner-admin `/s/`
  and `/uhd` HTTP 404.
- Log marker scans after deployment returned zero matches for credential marker
  names, panic/fatal markers, owner-admin query strings, and 502/503/504 in the
  bounded verification window. Raw log lines were not emitted.
- No real failover was forced. Policy unit simulations cover healthy,
  over-threshold, unhealthy, recovery, manual holds and rollback behavior.
- The local source commit is `804b242`. A direct origin feature push timed out
  without advancing the tracked origin ref; publishing remains pending through
  the separately controlled BWG feature-only publish bridge. Main/master and
  force push were not used.

## Remaining statistics gate

Reliable NOSLA-wide playback totals require a query-free cross-host collector.
The current NOSLA log can safely provide aggregate request counts, status,
bytes and duration, but it cannot reliably derive client type, exact active
sessions, or exact viewing duration because those fields are intentionally not
logged. Those unsupported fields remain `未接入`; they are not reported as
zero. A later collector must aggregate only sanitized path classes and never
persist query strings or authentication headers.
