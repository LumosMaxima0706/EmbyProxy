# VPS Control Plane Runbook

Status: `ISOLATED PHASE 4/5 IMPLEMENTATION IN PROGRESS; PRODUCTION CUTOVER NOT AUTHORIZED`

Last updated: 2026-09-02 (Asia/Shanghai)

This is the operational runbook for the multi-edge EmbyProxy control plane.
It is intentionally separate from the historical fusion runbooks in
`docs/fusion-project-runbook/`; those documents remain the source of truth for
the already deployed playback and failover work. This file records the new
node-management, enrollment, application-login, quota, scheduling and revoke
work planned by the current task.

## Operating Rules

- Phases are internal work units; this session has continuous authorization to
  implement, test and deploy scoped changes with backups and rollback.
- Never stop, replace, reconfigure or rename an existing service to solve a
  conflict. New components use independent paths, names, ports and logs.
- Never print or commit passwords, private keys, Emby tokens, cookies,
  Authorization headers, enrollment tokens or API credentials.
- Nginx changes require a scoped backup, `nginx -t`, and only then a reload.
  Existing server blocks remain outside the change scope unless separately
  approved.
- Existing playback behavior is a release gate. A change is not accepted if
  1111, younoyes, or another published Emby loses redirect discovery, HTTP/80
  support, runtime UserId, normal User-Agent, Range, 206,
  Content-Range, byte growth, or canary compatibility.
- A node is not eligible for proxy traffic merely because its process or
  heartbeat is online. It must pass the existing authenticated playback
  canary and configuration-sync checks.

## Phase Gate

Phases are engineering milestones, not approval gates. Production changes are
performed only with a scoped backup, an exact rollback path and post-change
validation.

| Phase | Scope | State |
| --- | --- | --- |
| 0 | Repository, Git, deployment and production inventory; architecture map | DONE |
| 1 | Schema/API/enrollment/auth-migration design and local implementation | DONE |
| 2 | Application HTML login | DEPLOYED AND VERIFIED |
| 3 | Generic proxy-node model, API and Admin UI | LOCAL FOUNDATION DONE |
| 4 | One-time enrollment/bootstrap and edge-agent identity | ISOLATED INSTALLER IMPLEMENTED; host exercise in progress |
| 5 | Heartbeat, health and playback-canary admission | ISOLATED EDGE AGENT IMPLEMENTED; host exercise in progress |
| 6 | Persistent traffic quota and reset accounting | MODEL FOUNDATION DONE |
| 7 | Manual ordering and explainable smart scheduling | SELECTOR FOUNDATION DONE |
| 8 | Drain, revoke and removal | API FOUNDATION DONE |
| 9 | Local test and acceptance matrix | GO TEST/VET DONE |
| 10-12 | Controlled BWG upgrade, BWG/NOSLA regression, optional extra VPS | BWG AUTH DEPLOYED; EXTRA VPS BLOCKED |

## Repository And Git Inventory

### Canonical source tree

The outer workspace is a documentation/staging directory and is not itself a
Git worktree. The publishable Git repository is:

```text
/mnt/d/codex_work/codex-emby-proxy-project/codex-emby-proxy-project/.research/EmbyProxy
```

Its remotes are:

```text
origin   https://github.com/LumosMaxima0706/EmbyProxy.git
upstream https://github.com/hkfires/EmbyProxy.git (fetch only)
```

The separate `.research/emby-reverse-proxy-go` tree is the upstream reference
implementation, not the production working tree. The separate
`.research/embykeeper-deploy-20260819` tree is a related deployment project,
not the EmbyProxy controller source.

### Snapshot at Phase 0

- Historical Phase 0 branch: `feature/failover-phase2-local`
- Current HEAD: `b42ee844abb3045c14d884cc2dfa7aa9394c46ad`
- HEAD subject: `fix: validate canary through Emby VideoStream`
- `origin/main`: `5e49a3f22f5a58f516cda5d0f4f5392e6a72532b`
- Local `main`: `0629ca3472a14d0fe7b65a36664295d0e5a648bf`
- `HEAD...origin/main`: `1 17` (the branches have diverged)
- `git fetch origin --prune` completed before this snapshot.
- The worktree is dirty. Existing modified and untracked files were
  preserved; none were reset, cleaned, staged or overwritten.

The dirty batch includes admin and statistics changes, publication-agent and
stats unit edits, playback-investigation notes, redirect/playback rollback
helpers and live-capture scripts. Treat every one as pre-existing user work
until it is separately reviewed and committed.

### Current feature worktree (2026-09-02)

- Development worktree: `/mnt/d/codex_work/codex-emby-proxy-project/embyproxy-vps-control-plane`.
- Branch: `feature/vps-control-plane`; clean at `164e01a` before the current
  bootstrap edits, based directly on `origin/main` `5e49a3f`.
- Local `main`: `0629ca3`; it is an older line and is not the deployment source.
- `origin/main`: `5e49a3f` at the last successful fetch; no remote changes were
  found during this run.
- Original user worktree remains dirty on `feature/failover-phase2-local` and
  is not modified, reset, cleaned or staged.
- GitHub push is blocked by missing credentials: HTTPS has no helper token and
  `ssh -T git@github.com` returns `Permission denied (publickey)`. Existing SSH
  keys are host-scoped (`/home/lumos/.ssh/codex_bwg_20260808`,
  `/home/lumos/.ssh/codex_nosla_20260808`, and `aliyun-fns.pem`); no private key
  material is recorded here. Push must use a GitHub credential and never force-push.
- BWG currently runs release
  `/opt/embyproxy-gsy-sidecar/releases/20260902T0100-vps-control-plane-final`,
  build label `vps-control-plane-20260902`, commit label `7bc6512`, SHA-256
  `d9dbad4199460759be69f65d89abc2f28d870de1f6ffe8ee3d6857b7c3de32a8`.
- NOSLA remains on `ghcr.io/gsy-allen/emby-proxy-go:v1.3`; its named-target
  admin artifact SHA-256 is
  `79d279c0d5c95008b8ea90715fe60a3cafb0a9cf85d9c5d40edfeb89f14009ad`.

### Continued implementation record

#### Isolated Phase 4/5 bootstrap implementation (2026-09-02)

- The installer now honors `EMBYPROXY_INSTALL_ROOT` for **all** files it
  writes. In test-root mode it uses only:
  `etc/embyproxy-edge`, `var/lib/embyproxy-edge`, `usr/local/bin`,
  `usr/local/lib`, and `etc/systemd/system` below that root; it does not invoke
  host `systemctl`. This specifically protects production
  `/etc/embyproxy-edge`, `/var/lib/embyproxy-edge`, their credentials, and all
  existing service state during approved isolated testing.
- The bootstrap downloads a node-credential-gated `embyproxy-edge-agent`
  artifact, requires a SHA-256 checksum header, verifies it locally, writes a
  root-only agent configuration, and emits local unit templates. The one-time
  enrollment token remains short-lived, single-use, and is exchanged for a
  per-node credential; it is not placed in the Admin command or server logs.
- The controller exposes `/api/edge/artifact/<node>/edge-agent` and
  `/api/edge/config/<node>` only after validating the per-node credential.
  Edge request capture and access logging remain suppressed to prevent
  credential disclosure.
- `cmd/edge-agent` is a separate data-plane process. It fetches its managed
  route snapshot, serves the established `/s/<slug>` proxy path through the
  shared media proxy implementation, executes bounded Range canaries, and
  reports config-sync/playback health through its node credential.
- `ISOLATED_TEST_MEDIA=true` enables a deterministic Range-capable fixture
  only in the isolated controller. It supports `Range`, `206`,
  `Content-Range`, `Accept-Ranges`, and positive response bytes without
  contacting or modifying an existing Emby service. Private upstream targets
  are admitted only for that explicit fixture mode.
- Local verification after this batch: Go 1.26.4
  `go test ./...`, `go vet ./...`, and `git diff --check` all pass. New API
  coverage verifies missing/wrong node credentials receive no artifact or
  snapshot, and a correct credential receives the expected checksum without
  exposing the Admin secret.

#### Approved host resource boundary

- BWG only: `/opt/embyproxy-vps-control-plane-test`,
  `/var/lib/embyproxy-vps-control-plane-test`,
  `/var/log/embyproxy-vps-control-plane-test`, the four specifically approved
  test units, loopback `18180`, `18181`, `18182`, and `28180`, plus only
  `/etc/nginx/conf.d/embyproxy-vps-control-plane-test-bwg.conf`.
- NOSLA only: the matching approved three test roots, its approved edge test
  unit, and only `/etc/nginx/conf.d/embyproxy-vps-control-plane-test-nosla.conf`.
  No existing Nginx server block, production sidecar, publication agent,
  rathole, 3x-ui, xray, or `stream-erpgo-nosla` container is in scope.
- The prior read-only audit found NOSLA `127.0.0.1:18180` already occupied by
  `stream-erpgo-nosla`. It must not be reused, and the container will not be
  stopped, rebound, or modified. Work may continue with local/BWG-only
  validation, but the NOSLA edge service cannot be started under the listed
  binding without a revised, explicitly approved free NOSLA loopback port.
- Rollback for any approved isolated install is limited to stopping/disabling
  the named test unit, removing only the named isolated include after restoring
  its timestamped backup, `nginx -t`, then `systemctl reload nginx`; test roots
  are retained until the validation evidence is archived. No production path
  is removed or replaced.

#### BWG isolated execution evidence (2026-09-02)

- A no-checkout clone was created at `/opt/embyproxy-vps-control-plane-test/integration`
  and fast-forwarded from the repository bundle. BWG-native Go 1.26.4 with
  CGO enabled built the controller and edge agent into the test-only `bin/`
  directory.
- The controller ran on `127.0.0.1:18180`, the edge on `127.0.0.1:18181`, and
  the approved Nginx include on `127.0.0.1:18182`. The include passed
  `nginx -t` before reload. Controller/edge remained active with zero restarts;
  production sidecar, publication-agent and Nginx also remained active.
- Bootstrap with `EMBYPROXY_INSTALL_ROOT=/opt/embyproxy-vps-control-plane-test`
  created only test-root identity, agent binary, config, state and unit
  templates. The first run found a heartbeat heredoc expansion bug; commit
  `f9b8be5` fixed it and the subsequent bootstrap completed successfully.
- Controller, edge and Nginx paths all returned HTTP `206`, valid
  `Content-Range`, `Accept-Ranges: bytes`, and 1024 positive bytes from the
  deterministic isolated fixture. The enrolled node reported healthy,
  config-synced, and accumulated traffic.
- Manual failover was exercised by marking the first node playback-degraded;
  the next request used a healthy logical node. Default drain of an idle node
  revoked it immediately; a revoked credential received HTTP `403`. Restoring
  the first node returned requests to it. Smart mode selected the low-usage
  node after the first was set to 95% quota, increasing its usage from 1000 to
  2024 bytes.
- Active-connection accounting and reset persistence are covered by storage
  tests. Idle drain now transitions directly to revoked; active drains revoke
  only after the final response. Expired billing cycles reset usage and update
  `next_reset_at`, while schema initialization backfills older rows.

#### Remaining acceptance boundaries after isolated execution

- NOSLA's approved `127.0.0.1:18180` edge binding cannot be used without
  changing a protected service layout; the existing `stream-erpgo-nosla`
  container was not stopped, rebound, or modified. No NOSLA test unit/include
  was installed. A revised free NOSLA loopback port is required for a real
  NOSLA isolated edge.
- No separately authorized third VPS is available for fresh-host E2E. The
  bootstrap and data-plane code is implemented and tested against the
  isolated BWG controller/edge, but unknown-host installation is not claimed.
- Existing 1111/younoyes credentials were not exported or replayed by this
  isolated test. Historical 1111 remains the accepted full canary reference;
  younoyes remains the documented upstream 403 blocker.

#### Production playback regression (2026-09-02)

- Using the existing protected Admin canary endpoint on BWG, a fresh run after
  the isolated control-plane work returned `1111 playback_status=healthy`
  with `samples_passed=2` and `younoyes playback_status=healthy` with
  `samples_passed=3`. The canary used the stored runtime credentials inside
  the production process; no credential, cookie or Authorization value was
  exported. This supersedes the earlier transient younoyes 403 observation;
  no younoyes-specific code path was added.
- The canary endpoint is the existing full chain: authenticated connectivity,
  `PlaybackInfo`, legitimate `VideoStream` entry, redirect/direct media
  endpoint, Range response, `206`, `Content-Range`, and byte-growth checks.
  The request returned HTTP 200 for the Admin operation and marked both
  publications healthy.
- Production was not switched to the new isolated binaries in this approval.
  BWG `current` remains `/opt/embyproxy-gsy-sidecar/releases/20260902T0425-main-a31386a`;
  the new `origin/main` code is built and exercised only under the approved
  test root pending a separately scoped production cutover plan.

#### Final isolated provenance (2026-09-02)

- The feature worktree and BWG integration clone were fast-forwarded from the
  latest fetched `origin/main`, tested, and pushed normally through BWG's
  existing GitHub deploy identity. The resulting canonical commit is
  `7d2ed8ac9301ebf51fda05fb381863252728fcd1`; no force push was used. The
  local feature branch points to the same source commit; the older local
  `origin/main` remote-tracking ref is stale because this environment cannot
  authenticate GitHub directly, while BWG's `git fetch origin` verified the
  remote ref.

- BWG isolated controller build from final `a6f02a4` uses Go 1.26.4 with
  CGO: `90812fe9594323102441986f04eb2288f30020db4495df0715272dff98792d42`.
  The isolated edge-agent build uses:
  `757b298517da809346ab904c551777981d0594c728bfa0361da4d9fec27c741b`.
  Both binaries are under the approved test root only.
- BWG production was intentionally not switched by this approval. Its
  current symlink remains `/opt/embyproxy-gsy-sidecar/releases/20260902T0425-main-a31386a`.
  NOSLA remains on its protected `ghcr.io/gsy-allen/emby-proxy-go:v1.3`
  container. No production database, Nginx server block, DNS, firewall,
  publication-agent, rathole, or media container was changed.
- The isolated N-node evidence is real loopback data-plane evidence, not a
  public cutover claim: controller, edge and Nginx returned `206` with
  `Content-Range`, `Accept-Ranges` and positive bytes; manual failover,
  smart quota pacing, persistent traffic, reset schedule backfill,
  active-connection drain and credential revoke were exercised. NOSLA isolated
  testing remains pending a free approved bind because its protected service
  layout occupies the audited port; fresh bare-VPS enrollment remains pending
  an authorized third machine.

- Final canonical source is `origin/main=a6f02a4d3450...` (full commit is
  recorded by the BWG integration clone and remote fetch). The feature branch
  was pushed through the same normal fast-forward path. The binary-to-source
  mapping is therefore `a6f02a4 -> controller SHA-256 above / edge-agent
  SHA-256 above`; the production release mapping remains intentionally
  unchanged because production cutover was not approved in this scope.

- `0b06d55` added a configurable HTTPS enrollment origin, a guarded one-time
  bootstrap script, persistent monotonic usage updates, explicit IANA-timezone
  monthly reset calculation, and scheduler minimum-dwell/hysteresis policy.
- `695a266` added the additive `schema_migrations` marker
  `proxy_nodes_v1` and redacted admin login success/failure audit events.
- `3a469fa` suppressed edge enrollment and heartbeat requests from traffic
  capture and access logs because those payloads contain node credentials.
- Local verification after these commits: `go test ./...`, `go vet ./...`, and
  `git diff --check` all pass using Go 1.26.4 at `/tmp/embyproxy-go/go/bin`.
- BWG final release for this run:
  `/opt/embyproxy-gsy-sidecar/releases/20260902T0215-vps-control-plane-3a469fa`,
  SHA-256 `8e6b79822ab8903769e600a570f7310544e67254c977fc7f653700b7d591952e`.
  `nginx -t` passed, sidecar is `active`, `NRestarts=0`, `/admin` returns
  HTML 200 without `WWW-Authenticate`, and stream health returns `ok`.
- Two long-running SSH publish sessions closed before returning output. Read-only
  checks proved the first release remained active; the final release was then
  switched and verified explicitly. This is recorded as an SSH transport issue,
  not a production outage.

### Remaining acceptance boundaries

- No authorized third VPS is available for a genuine fresh-host bootstrap and
  data-plane playback acceptance. The generated installer enrolls identity and
  installs an isolated heartbeat timer, but deliberately does not overwrite an
  unknown host's Nginx or media service. A node stays out of the proxy pool
  until the existing authenticated playback canary and publication sync report
  healthy.
- The generic registry/selector path is wired into the shared managed-route
  adapter and isolated edge data plane. Production publication cutover remains
  intentionally out of scope for this approval because public DNS/80/443 and
  existing publication fragments were not changed.
- 1111/younoyes authenticated media playback was not replayed by the isolated
  fixture run. Historical 1111 acceptance remains the regression reference;
  younoyes remains an upstream credential/access blocker.

### Relevant completed history

The current history contains the generic playback fixes that must remain
intact:

- `00d1610` added scoped redirect discovery and playback canary foundations.
- `7a3715c`, `263a526`, `dc8ce23`, `184eb26`, `9c84a34` persisted and
  provisioned protected playback credentials.
- `65e10ed` carried runtime Emby identity through the canary.
- `5bcd42b` aligned canary media headers.
- `9913526` validated canary through Emby `VideoStream`.
- `2261c58` accepted valid ranged media without an optional header.
- `95badef` and `5e49a3f` made playback entrypoint probing generic and recorded
  acceptance.
- `709594d` records the younoyes canary acceptance.

No new control-plane implementation is inferred from these commits. The
existing `internal/failover`, `internal/storage`, `internal/admin`,
`internal/publicationagent`, `internal/proxyadapter`, `internal/mediaproxy`
and `internal/statslog` packages are the extension points.

## Current Architecture

```text
Admin browser
    -> owner-admin Nginx on BWG (currently HTTP Basic Auth)
    -> BWG embyproxy-gsy-sidecar :18082 (loopback)
    -> SQLite /var/lib/embyproxy-gsy-sidecar/proxy.db
    -> publication agent socket / generated edge fragments

Client media request
    -> stream DNS / selected edge
    -> edge Nginx publication fragment
    -> edge-local EmbyProxy proxy (usually :18080)
    -> authorized upstream Emby/CDN endpoint
```

### Roles and live services

**BWG (`bwg`, 144.34.226.187)** is the controller/admin and a fallback data
plane. It currently has Nginx on ports 80/443, the existing rathole and
openlist services, `stream-erpgo-bwg` on loopback `127.0.0.1:18080`, the Go
sidecar on `127.0.0.1:18082`, publication agent, statistics timers, and the
five-minute `embyproxy-failover-policy.timer`. The existing failover web panel
is a separate loopback service on `127.0.0.1:8787`.

**NOSLA (`nosla`, 45.143.130.11)** is the primary data-plane edge. It has
Nginx on ports 80/443, the protected 3x-ui/xray/fail2ban services, Docker and
`stream-erpgo-nosla` on loopback `127.0.0.1:18080`, and a separate named-target
admin sidecar on `127.0.0.1:18081`. Its current publication files are under
`/etc/nginx/conf.d/embyproxy-publications/`.

NOSLA host time is `Asia/Shanghai`; BWG host time is `Etc/UTC`. Project
billing-cycle and scheduler semantics must explicitly use `Asia/Shanghai`
rather than relying on either host default. NOSLA has UFW default-deny incoming
rules. BWG has no `ufw` command and its existing host firewall policy must not
be assumed safe for a new public listener.

### Version and artifact evidence

- BWG sidecar release link resolves to
  `/opt/embyproxy-gsy-sidecar/releases/e4e8159-publish-credential-20260822T034717Z`.
- BWG running sidecar SHA-256:
  `0a0da2facfe0de60f85019f9ffec69a358e4afe084d17034e4e591367daaf25a`.
- The binary reports development build metadata (`dev`, commit `unknown`),
  so the release directory name and hash are the reliable current evidence.
- BWG `stream-erpgo-bwg` image digest:
  `sha256:453bdc2e5e42084cd98cac9187ae71f5e1627393dc289a476493f16013d0dd93`.
- NOSLA runs the same `ghcr.io/gsy-allen/emby-proxy-go:v1.3` image digest.
- NOSLA named-target admin artifact is
  `/opt/emby-reverse-proxy-go-admin/emby-admin-sidecar`; its source/release
  provenance is not encoded in the binary and must be added to the version
  inventory before a future upgrade.

## Authentication Finding

The live `https://owner-admin.149077530.xyz/admin` response is:

```text
HTTP 401
WWW-Authenticate: Basic realm="Owner Admin"
```

The BWG Nginx file `/etc/nginx/conf.d/owner-admin.149077530.xyz.conf` uses
`auth_basic` and `auth_basic_user_file`, then forwards authenticated requests
to `127.0.0.1:18082` with the trusted
`X-Owner-Admin-Authenticated: 1` header. The Go handler deliberately treats
that context as `basic_proxy` and does not present its normal token/session
login. This is why the browser shows a native Basic Auth dialog and why a
password manager cannot reliably use a standard HTML credential form.

The required migration is therefore application-level HTML login, not CSS
customization of the browser dialog. The design must preserve an emergency
recovery path during migration, remove normal `/admin` Basic Auth, use a
standard username/password form (`autocomplete="username"` and
`autocomplete="current-password"`), hash the stored password, issue a Secure,
HttpOnly, SameSite session with expiry, enforce CSRF and login throttling, and
write a redacted audit event. The current Basic Auth file and application
secret must never be copied into Git or this runbook.

## Existing Playback Contract (Regression Gate)

The shared canary and proxy code already covers legal upstream variations:

- runtime Emby `UserId` and protected playback credentials;
- normal Emby client `User-Agent` and client headers;
- redirect discovery, including observed alternate media hosts;
- HTTP/80 upstream media endpoints;
- `Range`, `206`, `Content-Range`, `Accept-Ranges` and byte growth;
- `VideoStream`-compatible entrypoints and multiple legitimate endpoint forms;
- WebSocket upgrade and long streaming timeouts;
- query-free, redacted access/statistics logging.

The canary is implemented in `internal/publicationagent/playback.go` and is
invoked by the authenticated publication/admin flow. It must be reused for
node admission and failover validation. A `/health`, image, `Items`, or
unauthenticated `401` response is not playback proof. The historical runbooks
`docs/fusion-project-runbook/35-yamby-playback-vod1-fix.md` and
`39-emby-playback-throughput-troubleshooting.md` document the accepted
redirect, Range and dual-edge evidence rules.

## Control-Plane Design To Implement

The implementation should extend the existing BWG Go sidecar and publication
agent rather than create a second controller. The intended boundaries are:

1. **Node registry:** a SQLite-backed generic N-node model with stable node ID,
   display name, enabled/state, priority, scheduling mode, public address,
   agent version/build, timestamps, heartbeat and last error.
2. **Enrollment:** Admin creates a node record and a short-lived, single-use,
   revocable enrollment record. The generated command contains no Admin
   password. The new host creates an independent identity key, registers over
   TLS, receives a long-lived node credential/certificate, installs the
   existing edge helper as an independent systemd service, and sends a
   heartbeat. Enrollment tokens are hashed at rest and never logged.
3. **Admission:** states are at least `registered`, `installing`, `online`,
   `healthy`, `degraded`, `draining`, `offline` and `revoked`. Only enabled,
   online, fresh-heartbeat, playback-healthy, synced, non-draining,
   non-exhausted nodes enter the proxy pool.
4. **Traffic:** one persistent source of truth must combine the existing
   edge/statistics mechanism with owner/provider opening balances. Store
   quota bytes, used bytes, remaining bytes, reset rule, timezone and
   `next_reset_at`; do not reset counters on process restart.
5. **Scheduling:** manual mode selects the first eligible node by persisted
   priority. Smart mode uses an explainable quota-pacing score based on health
   hard gates, remaining ratio, time-to-reset ratio, recent failures/latency
   and manual priority. Hysteresis, minimum dwell and cooldown prevent
   oscillation; an active playback failure may fail over immediately.
6. **Removal:** default delete transitions to draining, stops new sessions,
   waits for existing connections/timeout, removes the node from the pool and
   revokes its credential. Force removal is explicit and separate. A revoked
   node cannot re-register or receive configuration.
7. **Versioning:** Admin shows agent version/build and compatibility as
   `current`, `compatible` or `outdated`; controller expected version is
   configuration, not a hard-coded BWG/NOSLA special case.

The exact schema, API names and bootstrap transport are Phase 1 deliverables.
No production database migration or service change was made in Phase 0.

## Phase 0 Evidence And Verification

Read-only commands completed:

- repository `git status`, remotes, branches, logs, `git fetch origin --prune`
  and local/remote ref comparison;
- SSH metadata inspection without private-key contents;
- BWG/NOSLA OS, kernel, uptime, listeners, running services, project units,
  Docker/Compose, Nginx paths/version, firewall summary, disk and timezone;
- artifact symlink/hash and container digest checks;
- `nginx -t` on both hosts;
- loopback project health checks;
- public `/health` and `/https/v1.uhdnow.com/443/System/Info/Public` checks for
  both `stream.149077530.xyz` and `stream-b.149077530.xyz`;
- unauthenticated owner-admin response inspection.

Observed public verification:

```text
stream.149077530.xyz/health                              200
stream.149077530.xyz/.../System/Info/Public              200
stream-b.149077530.xyz/health                            200
stream-b.149077530.xyz/.../System/Info/Public            200
owner-admin.149077530.xyz/admin (no credentials)          401 Basic
```

No production write, package installation, container lifecycle operation,
service restart, Nginx reload, DNS call, firewall change, certificate change,
database write, or credential rotation was performed.

## Risks And Open Decisions

- `origin/main` and the current feature branch have diverged; a future Phase 1
  must choose a merge/rebase strategy without discarding the dirty batch.
- BWG owns 80/443 and many protected services. Any public Admin or new edge
  route needs an independent server block, certificate decision and exact
  port/name conflict review.
- NOSLA already has a protected Nginx/xray surface and no spare public listener
  may be assumed. A new edge agent must use a separately approved port/path.
- The provider reset day for NOSLA remains historically inconsistent in local
  notes and must be confirmed before automated quota decisions. Provider
  billing is ingress plus egress and may differ from host counters.
- Existing owner-admin Basic Auth is a production recovery dependency. The
  migration must stage a rollback before removing it from normal `/admin`.
- Existing BWG/NOSLA production artifacts do not consistently expose source
  commit metadata. Version reporting must be added without claiming a commit
  that cannot be proven.
- No additional VPS enrollment test host was authorized or available in this
  Phase 0 session. The optional `161.114.13.231` host is not required.

## Backup And Rollback Contract For Future Phases

Before any live phase, create a timestamped, root-only backup containing the
affected binary/release link, service unit and state, scoped Nginx files,
database backup/migration marker, firewall/listener snapshot, and checksums.
The phase must provide an operation-specific rollback script. A failed Nginx
candidate is rejected before reload; a failed post-apply health or playback
gate restores the prior route/service state. Rollback never removes unknown
services, images, volumes, certificates or data.

## Next Approved Phase

Phase 1 should produce and locally test the schema/API/enrollment/auth
migration design, including:

- additive SQLite migration and restart persistence tests;
- redacted Admin API contract with CSRF and session boundaries;
- enrollment token hash/TTL/single-use/revoke model and command format;
- agent identity rotation and controller rejection rules;
- application login migration and Basic Auth rollback plan;
- node state machine and playback admission contract;
- exact local files, ports, services and backup/rollback plan.

This original Phase 1 planning note is superseded by the continuous-session
authorization. Production changes remain constrained by the backup,
validation and rollback contract above.

## 2026-09-02 Implementation And Production Record

### Changes

- Added additive `proxy_nodes` and `proxy_node_enrollments` SQLite tables.
  Enrollment tokens are SHA-256 verifiers at rest, have a 15-minute TTL, are
  single-use, and are revoked together with a node credential.
- Added generic node CRUD, reorder, enable/disable, drain/revoke, enrollment,
  and credential-scoped heartbeat APIs. No node name is special-cased.
- Added selector tests for manual priority and smart mode hard exclusions
  (stale heartbeat, draining, exhausted quota, missing playback/config health).
- Added bcrypt application-password authentication with standard HTML form
  fields for password managers. The old `ADMIN_TOKEN` is retained as a
  loopback/recovery API compatibility mechanism, but is not rendered or saved
  in browser storage by the owner-admin page.
- Restored the existing atomic published-backup-line update behavior required
  by the main branch's publication tests; published primary upstream changes
  remain rejected.

### BWG deployment

- Pre-change backup:
  `/var/backups/embyproxy-vps-control-plane/20260902T003519Z`.
- New release:
  `/opt/embyproxy-gsy-sidecar/releases/20260902T0045-vps-control-plane`.
- Deployed binary SHA-256:
  final release `d9dbad4199460759be69f65d89abc2f28d870de1f6ffe8ee3d6857b7c3de32a8`;
  build commit `7bc6512`.
- The temporary root-only helper converted the existing root-only recovery
  password into `ADMIN_PASSWORD_HASH` directly on BWG. It printed neither the
  password nor the hash and was removed after use.
- Only the dedicated `owner-admin.149077530.xyz` Nginx file was changed. Its
  `auth_basic` directives and trusted Basic-success header injection were
  removed. `nginx -t` passed before reload. Only the Go sidecar was restarted.

### Production verification

- `embyproxy-gsy-sidecar.service`: active, `NRestarts=0`.
- Unauthenticated `https://owner-admin.149077530.xyz/admin`: HTTP 200 HTML,
  no `WWW-Authenticate` header, standard `username` and password-manager
  autocomplete attributes present.
- A real login using the existing root-only recovery password returned
  `authenticated:true`; the authenticated proxy-node API returned HTTP 200.
- `stream.149077530.xyz/health` and its small Emby System Info endpoint both
  returned HTTP 200 after deployment.
- No authenticated media object was replayed. The existing runtime
  VideoStream/Range canary has not been re-run in this session, so real
  1111/younoyes regression acceptance remains open rather than claimed.

- Post-main regression on BWG: 1111 completed one authenticated sample with
  `PlaybackInfo=200`, `VideoStream=302`, final media `206`,
  `Content-Range=true`, `Accept-Ranges=true`, 65,536 bytes read and positive
  byte growth; result `healthy`. Younoyes recheck was attempted without
  changing credentials and returned upstream HTTP 403 for System Info/Items,
  followed by canary `PlaybackInfo=500` (`timeout_or_upstream_5xx`). This is an
  upstream credential/access failure, not a code-path success, so younoyes is
  explicitly not marked as passed in this run.

### Current blockers

1. No reachable, authorized third VPS with a public DNS/publication route is
   available in this session. A genuine one-command enrollment, agent install,
   Nginx publication and authenticated VideoStream/206/byte-growth test cannot
   be honestly completed without it.
2. The controller API foundation has no final bootstrap artifact endpoint yet;
   it cannot safely invent an edge data-plane installation because the new
   host's DNS/certificate/publication route must be provisioned without
   disturbing current edges.
3. Existing production artifacts predate the new source commit. The BWG Admin
   release is traceable by release directory and SHA-256; NOSLA has not been
   upgraded in this change.
4. Source commit `1d6f18b` is committed locally on `feature/vps-control-plane`
   but not pushed. The configured `origin` uses HTTPS and this environment has
   no GitHub credential helper/token; `git push -u origin feature/vps-control-plane`
   failed with `could not read Username for 'https://github.com'`. No remote,
   branch history or local user worktree was changed to work around this.

### Rollback

Restore only the snapshot files from the listed backup root: set `current` to
the recorded prior target, restore `embyproxy.env`, the owner-admin Nginx file,
and `proxy.db`; run `nginx -t`, restart only `embyproxy-gsy-sidecar.service`,
then reload Nginx. The snapshot's stored unit and state evidence identify the
exact pre-change version. Do not delete the new release, any certificate,
media container, publication fragment, rathole or unrelated service.

## Continuous-session addendum (2026-09-02)

### Data-plane control-plane wiring (2026-09-02)
### Main release and rollback incident (2026-09-02)

- `origin/main` advanced normally from `7c114b8` to `c3fb409`, then to
  `0d8fbfd` after the base-path test correction. Local
  `feature/vps-control-plane` was fast-forwarded to the same `0d8fbfd`.
- The first stripped `CGO_ENABLED=0` upload crashed on BWG with SIGSEGV during
  startup (systemd status 11/SEGV, restart counter 42). Backup
  `/var/backups/embyproxy-vps-control-plane/20260902T033742Z-routing` restored
  the previous release `20260902T0330-main-d0d6efc`; no Nginx or media
  container was stopped.
- Rebuilding from the BWG integration clone with native `/opt/go1.26.4` and
  CGO produced hash
  `0db2d74aebad40cd9e49451a88ee30a4c579cdb43a3ffb969471471eef63da22`.
  `20260902T0405-main-0d8fbfd` is active with zero restarts, Admin HTTP 200,
  and stream health HTTP 200. The failed artifact is not a production version.

Current provenance is `origin/main 0d8fbfd` -> the BWG native build above ->
`/opt/embyproxy-gsy-sidecar/releases/20260902T0405-main-0d8fbfd`.
The production schema contains `proxy_nodes_v1` and
`proxy_nodes_v2_connections`; no proxy nodes are currently enrolled in BWG,
so selector routing remains a compatibility fallback until an edge reports
playback/config health. NOSLA's existing `ghcr.io/gsy-allen/emby-proxy-go:v1.3`
container remains running and unchanged.

### Final main-aligned release (2026-09-02)

Runbook closure was pushed as `origin/main df0804f` and the local feature branch
was fast-forwarded to that exact commit. BWG rebuilt from the integration clone's
`origin/main` with native `/opt/go1.26.4` and now runs release
`/opt/embyproxy-gsy-sidecar/releases/20260902T0415-main-df0804f`, SHA-256
`f88c242c1a66d4e53830f4e89672b596bc840bc9e545d746ca0f40970a673c00`. The
service is active with zero restarts and `nginx -t` passes. Public Admin is
HTML 200 without `WWW-Authenticate`; the standard username/password form uses
`autocomplete="username"`, `type="password"` and
`autocomplete="current-password"`. Stream health is HTTP 200. NOSLA Nginx and
the existing `ghcr.io/gsy-allen/emby-proxy-go:v1.3` media container remain
running and unchanged.

The browser DOM/session checks are automated; validation of a Bitwarden browser
extension's actual autofill action remains a manual final step because the
extension is not available to this runtime. Younoyes remains externally blocked:
its existing credential produced upstream HTTP 403 for System Info/Items and the
shared canary classified PlaybackInfo as HTTP 500 `timeout_or_upstream_5xx`.
No credential was altered or exposed. No third VPS was available, so a fresh
host's full Nginx/data-plane enrollment and real media canary remain unclaimed.

Final provenance correction: after this closure note was pushed, `origin/main`
and `feature/vps-control-plane` became `a31386a`. BWG was rebuilt from that
exact main commit and runs
`/opt/embyproxy-gsy-sidecar/releases/20260902T0425-main-a31386a` with binary
SHA-256 `acfc7ed6a72edac69b390394b3fd50c9b9405c849741f09984ad15e83d604ddd`.

The managed-route runtime now consults the persisted `proxy_nodes` table before
forwarding `/s/<slug>/...` requests. `StorageResolver` applies the configured
`PROXY_NODE_SCHEDULER_MODE` (`manual` by default, `smart` when explicitly set)
and the shared eligibility gates (enabled, fresh heartbeat, healthy playback,
synced config, available quota). Assignments are retained per admin/slug with
two-minute minimum dwell and smart hysteresis. A node's `public_address` is
parsed as a strict HTTP(S) origin plus optional safe base path; credentials,
query strings, fragments and invalid targets are rejected. The selected edge
marker is consumed by the router and stripped before an Emby upstream request,
preventing routing loops and credential/header leakage. When no eligible node
has a usable address, the original managed-route target remains the compatibility
fallback, preserving legacy `/https/...`, 1111 and younoyes behavior.

Each selected request increments a persistent `proxy_node_connections` counter
and decrements it when the response completes. Draining nodes reject new
connections; the final connection completion transitions the node to `revoked`
and revokes its enrollment/credential. Response bytes are added through the
atomic `AddProxyNodeUsage` store method, so concurrent streams cannot lose
traffic increments and restarts do not reset usage. Migration markers are
`proxy_nodes_v1` and `proxy_nodes_v2_connections`.

Local verification added a two-edge HTTP integration: manual priority selected
edge A, then playback-health failure immediately selected edge B, with no hit
to the origin. Storage tests cover drain waiting for an active connection and
automatic final revoke. Full `go test ./...`, `go vet ./...`, and `git diff
--check` pass after this wiring. Production deployment is intentionally pending
the normal backup/release switch and will retain the existing route fallback.

Phases are internal work units in this session, not approval gates. The
development worktree is clean at `f958ebc`; it is nine commits ahead of
`origin/main` `5e49a3f` and can be merged normally once remote authentication is
available. Local `main` remains `0629ca3` and was not moved. The original dirty
user worktree is untouched.

Commits `0b06d55`, `695a266`, and `3a469fa` add guarded bootstrap, persistent
usage/reset helpers, scheduler dwell/hysteresis, migration marker
`proxy_nodes_v1`, auth audit events, and suppression of credential-bearing edge
requests from logs. Local `go test ./...`, `go vet ./...`, and `git diff --check`
pass.

Final BWG release is
`/opt/embyproxy-gsy-sidecar/releases/20260902T0215-vps-control-plane-3a469fa`
with SHA-256
`8e6b79822ab8903769e600a570f7310544e67254c977fc7f653700b7d591952e`.
`nginx -t` passed; service is active with zero restarts; `/admin` returns HTML
200 without `WWW-Authenticate`; stream health is `ok`. NOSLA's existing media
service and Nginx configuration remain active and unchanged.

Two SSH publish sessions closed before output; read-only checks showed the old
release remained active, then the final release was switched and verified. This
was an SSH transport issue, not a production outage.

GitHub push is the remaining external blocker. HTTPS has no credential helper or
token; all available local keys are host-scoped BWG/NOSLA/aliyun keys and fail
GitHub public-key authentication. Do not print or invent credentials. After a
credential becomes available: `git fetch origin`, verify no new main commit,
merge/fast-forward without force, push `main`, and redeploy that exact commit.

Fresh third-VPS enrollment and authenticated VideoStream/206/byte-growth
acceptance remain unclaimed because no authorized third VPS/publication route is
available. The generated bootstrap intentionally installs only an isolated
heartbeat identity/timer and never overwrites unknown Nginx or media services;
nodes remain out of the proxy pool until the existing authenticated playback
canary and publication sync pass. Publication-agent route selection and active
connection drain accounting are also follow-up work, while force revoke already
invalidates node credentials.

### Main consolidation and final production release (2026-09-02)

- BWG's existing GitHub deploy key authenticated through the
  `github.com-embyproxy-fusion` SSH alias. Its public fingerprint is recorded
  only as `SHA256:4yHJaOzwdnYGQdGZ/B7E6JZKt85j0JS88/kvJdpqkXk`.
- The complete feature history was imported into the independent BWG clone
  `/root/embyproxy-control-plane-integration`, fast-forwarded onto the latest
  `origin/main`, and pushed without force. `origin/main` is now
  `670d0b47196562c127fd43676d9ac9410f9889f5`.
- Final main release:
  `/opt/embyproxy-gsy-sidecar/releases/20260902T0245-main-670d0b4`, SHA-256
  `12988b9c850d3f9fecb5efa3f5c6492a419875221466517cdf5d9514615022af`.
  Backup: `/var/backups/embyproxy-vps-control-plane/20260902T023111Z`.
  Nginx test passed; service active with zero restarts; current points to this
  release.
- Final smoke checks: owner Admin is HTML 200 without `WWW-Authenticate`,
  password-manager autocomplete fields are present, stream health is `ok`, BWG
  publication-agent is active, and NOSLA Nginx/edge health are active.
- Final 1111 canary passed authenticated `PlaybackInfo=200`, `VideoStream=302`,
  media `206`, `Content-Range=true`, `Accept-Ranges=true`, 65,536 bytes and
  positive growth. Younoyes was attempted with its existing credential; the
  upstream returned `403` and canary `PlaybackInfo=500` (`timeout_or_upstream_5xx`).
  No credential was changed or exposed, so younoyes is not claimed as passed.

- Final production provenance check: `origin/main`, local feature and BWG
  integration `main` are all `4dc0dd812633c5d3e83a78cb34f21fa01fd94046`.
  BWG current points to `/opt/embyproxy-gsy-sidecar/releases/20260902T0315-main-4dc0dd8`
  with binary SHA-256
  `0803d151c53ba0dd5ee559bae5dfbde9c2b324e42528911fa44e93f8f8617921`.
  The binary bytes match the tested `ec5d121` source build because the final
  commit only changes this Runbook.

- Final release naming follow-up after the provenance note:
  `/opt/embyproxy-gsy-sidecar/releases/20260902T0330-main-d0d6efc` is active
  on BWG with the same tested binary SHA-256
  `0803d151c53ba0dd5ee559bae5dfbde9c2b324e42528911fa44e93f8f8617921`.

- Final repository check: `origin/main` and `feature/vps-control-plane` are
  both `c40523ca1dfd9a8b20dd54914a61e6d4de55d402`; the worktree is clean.
