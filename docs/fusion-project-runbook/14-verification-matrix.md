# Verification Matrix

| Verification | When to run | Success standard | Failure handling | Current result |
| --- | --- | --- | --- | --- |
| `git branch --show-current`, `git rev-parse --short HEAD` | Every task start and commit gate | Expected feature branch and recorded base/HEAD | Stop, record mismatch; no reset | Branch verified; HEAD `8d60aa5` |
| `git status --short --untracked-files=all` | Before/after every task | Only declared task paths are dirty | Stop and classify unexpected paths | Source paths clean after `98229bd`/`0b7b590`; current dirty paths are declared runbook evidence updates |
| `git diff --check` | Before every commit and after edits | No whitespace errors | Fix only in task scope, re-run | Passed for current tracked changes |
| `gofmt -w <changed Go files>` | Before source tests/commit | Changed Go files format successfully | Record toolchain or format issue; do not commit | Available via temporary Go 1.26 toolchain; C-001 run pending |
| `go test ./internal/storage ./internal/admin ./internal/proxyadapter` | After C/D source changes | Targeted packages pass | Record failing command/test; do not hard-commit | PASS after D-001 fix |
| `go test ./...` | After implementation batches and before delivery | All Go packages pass | Stop, record issue, do not auto-fix outside scope | PASS |
| `go vet ./...` | Before source delivery/release | No vet findings | Record findings and stop current gate | PASS |
| Admin UI build/test | After E-001 | Existing project UI checks pass, or manual review evidence exists | Record unavailable automation and perform documented manual review | Static contract test PASS; no separate frontend build exists |
| Manual admin review | When UI automation unavailable | Auth, CRUD, validation errors, no secret display, fallback preserved | Record checklist failures | E-001 pending human browser review; UI source does not embed credentials and uses same-origin auth |
| Secret redaction check | Every proxy/admin logging batch | No token, cookie, password, sensitive query, or credential in logs/output | Stop, redact, add regression test | Existing redaction tests identified; current batch not executed |
| No deploy/restart/SSH check | Every task close | No server, process, Nginx/systemd, DNS, real SQLite, or SSH action | Stop and record unauthorized action | Passed; none performed |

## Evidence Requirements

Each completed row requires command, timestamp, result summary, and link to tracker/progress entry. A blocked row must name the issue ID and recovery step. A passing local test does not authorize push or deployment.

## Autonomous B-001 Recovery Evidence

- Recovery status: `IN_PROGRESS`.
- Search scope: current PATH, standard system/user locations, toolchain managers, package managers, and project-provided scripts.
- No installation, network fetch, production access, or secret use is implied by the search.

## B-001 Recovery Result

- `golang-1.26-go` and `golang-src` were downloaded from the configured local apt source and extracted to a temporary directory.
- `go version`: `go1.26.0 linux/amd64`.
- `gofmt` executable: available in the extracted Go toolchain.
- System package database: unchanged.
- B-001: DONE; C-001: IN_PROGRESS.

## C-001 First Test Attempt

- `gofmt` execution: PASS.
- `go test ./internal/storage`: FAIL before compiling project code because the temporary GOROOT lacked the versioned standard-library source package.
- Issue: `ISSUE-TOOLCHAIN-001`, recovery attempt 3.
- No source commit created from this failed test attempt.

## C-001 Completion Evidence

- `gofmt` on the storage files: PASS.
- `go test ./internal/storage`: PASS (`ok embyproxy/internal/storage`).
- `git diff --check`: PASS.
- Source commit: `98229bd`.
- D-001 is now the only active implementation step.

## D-001 Completion Evidence

- `gofmt` on admin/proxyadapter changed Go files: PASS.
- `go test ./internal/admin ./internal/proxyadapter`: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- `git diff --check`: PASS.
- Source commit: `0b7b590`.

## D-001 First Test Attempt

- `go test ./internal/admin ./internal/proxyadapter`: FAIL during admin compilation; proxyadapter package PASS.
- Failure: `validateManagedRouteRequest` returned string constants where `error` values were required.
- Issue: `ISSUE-ADMIN-001`.
- Handling: stop before commit, wrap error codes, rerun targeted tests.

## E-001 Completion Evidence

- UI contract test: `go test ./internal/admin ./internal/proxyadapter` PASS.
- Full Go suite: `go test ./...` PASS.
- Static analysis: `go vet ./...` PASS.
- Formatting/diff hygiene: `gofmt -w internal/admin/managed_routes_ui_test.go` and `git diff --check` PASS.
- Browser automation is not configured in this repository; manual review remains required for visual behavior and live authenticated CRUD.
- Source commit: `8c00f1a`.

## F-001 Completion Evidence

- Runtime wiring audit: `proxyRouteHandler` selects the storage-backed router only when `MediaProxyRoutes` is true and a store is present; otherwise it returns the legacy handler.
- Feature flag parsing: `MEDIAPROXY_ROUTES_ENABLED` defaults false and unknown values fail closed.
- Tests: `go test ./internal/config ./cmd/embyproxy ./internal/proxyadapter` PASS.
- Full suite/vet: `go test ./...` and `go vet ./...` PASS.
- Formatting/diff hygiene: `gofmt -w internal/config/config_test.go cmd/embyproxy/main_test.go` and `git diff --check` PASS.
- Source commit: `29118fb`.

## G-001 Execution Evidence

- Existing `internal/proxyadapter/production_test.go` covers managed HTTP forwarding, Range headers, WebSocket upgrade, Location rewriting, fallback boundaries, unsafe target rejection, and request-secret redaction through the shared mediaproxy executor.
- Current task: rerun this matrix against the recovered local toolchain and add only a focused regression if a contract is missing.

## G-001 Completion Evidence

- Added managed slug assertions for `Location` and `Content-Location` rewrite in `internal/proxyadapter/production_test.go`.
- `go test ./internal/proxyadapter ./internal/mediaproxy`: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- `git diff --check`: PASS.
- Source commit: `4e60097`.

## H-001 Completion Evidence

- Added SQLite close/reopen persistence coverage for managed routes and lines in `internal/storage/managed_routes_test.go`.
- `go test ./internal/storage ./internal/proxyadapter`: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- `git diff --check`: PASS.
- Source commit: `fc00c61`.

## I-001 Execution Evidence

- Targeted redaction tests: PASS (`internal/proxyadapter`).
- Authenticated managed-route API/auth tests: PASS (`internal/admin`).
- Full suite: PASS (`go test ./...`).
- Static analysis: PASS (`go vet ./...`).
- Diff hygiene: PASS (`git diff --check`).
- Local safety audit: no tracked secret-named files or minified/bundled/WASM/source-map/generated artifacts; no deploy/restart/SSH action performed.
- I-001: DONE; J-001 is now the active release/docs hygiene task.

## J-001 Release Hygiene Evidence

- Local evidence matrix, gap checklist, and delivery checklist are current.
- Authoritative license/attribution/SBOM artifacts are not fabricated; `ISSUE-PROV-002` remains release-only BLOCKED pending owner/rightsholder confirmation.
- Implementation remains unblocked; no external network, GitHub, server SSH, deployment, or restart was used.

## K-001 Execution Evidence

- Reconciliation: PASS; source tree is clean and current diff is limited to declared runbook documents.
- `git diff --check`: PASS.
- Tracker status review: A-001 through I-001 DONE, J-001 BLOCKED only for release provenance evidence, K-001 DONE.
- Publish/deploy boundary: PASS; no push, bundle transfer, server SSH, deployment, restart, DNS, Nginx, systemd, or production SQLite action performed.

## Publish Bridge Readiness Result

- Branch/HEAD/worktree precheck: PASS for `feature/failover-phase2-local` at local `53f7437`; worktree clean before this docs-only issue record.
- Remote metadata: target `origin/feature/failover-phase2-local` was present at `c7f475c`; local branch was ahead. Main/master was not selected.
- Path and verification review: PASS; no source changes in the readiness check, and `go test ./...`, `go vet ./...`, and `git diff --check` passed.
- Sensitive scan: no tracked secret-named files; marker count is not treated as proof of absence because code/tests contain redaction terminology.
- Publisher check: `git push --dry-run` failed due unavailable local authentication. Direct origin publishing is also disallowed by the BWG publish bridge rule.
- Result: BLOCKED as `ISSUE-PUBLISH-001`; no actual push, force push, deployment, restart, or SSH occurred.

## BWG Publish Bridge Authorized Attempt

- Authorization: owner explicitly authorized BWG SSH/SCP for the current feature branch only.
- Required checks: local clean branch/HEAD/path scope; bundle verify/list-heads; BWG branch/base/status; BWG bundle verify/list-heads; temporary ref target; path whitelist; `git merge --ff-only`; feature ref push only; remote ref verification; cleanup.
- Forbidden actions remain: NOSLA SSH, deploy/restart, production changes, force push, main/master push, remote/auth changes, and secret output.

## BWG Publish Attempt 1 Result

- Local bundle verification: PASS.
- BWG branch/base/status and bundle verification: PASS.
- Path whitelist command: FAILED due shell `awk` quoting; recorded as `ISSUE-PUBLISH-002`.
- BWG merge/push: NOT RUN.
- Cleanup: PASS; temporary ref and bundle removed, BWG worktree clean.

## BWG Publish Attempt 2 Result

- BWG bundle/path checks: PASS.
- `git merge --ff-only`: PASS; BWG HEAD is `61de764`.
- Pre-push remote assertion: FAILED due retry script logic; expected remote was base `c7f475c`, not target.
- Push: NOT RUN; remote feature ref remains base.
- Cleanup: PASS; temporary ref and bundle removed; BWG worktree clean.

## BWG Publish Success Result

- Final local bundle target: `5cbbe54`; bundle verify/list-heads: PASS.
- BWG branch/base/status: PASS; ff-only update completed without a merge commit.
- Path whitelist: PASS; only approved project source/tests and runbook/docs paths were transferred in the target range.
- Push target: `HEAD:refs/heads/feature/failover-phase2-local` only.
- Remote verification: PASS; feature ref equals `5cbbe54`.
- Force push/main/master/deploy/restart/NOSLA SSH: NOT USED.

## Post-deploy stabilization verification

| Verification | Success standard | Current result |
| --- | --- | --- |
| Local/BWG/origin refs | All feature refs match the expected stabilization base | PENDING |
| Service stability | Active/enabled, main status zero, no unexpected restart | PENDING |
| Listener boundary | Only `127.0.0.1:18082` | PENDING |
| Bounded log review | No panic/ERROR/FATAL or secret leakage | PENDING |
| Self-use smoke | Admin/auth/CRUD/upstream/fail-closed/fallback pass | PENDING |
| Rollback readiness | Manifest, release/config/data/log paths, and unit rollback verified | PENDING |
| Existing service safety | Nginx/rathole active; `nginx -t` pass; no config change | PENDING |

Initial composite stabilization check: INCONCLUSIVE under
`POSTDEPLOY-ISSUE-001`; per-stage diagnosis is required.

Diagnosis result: runtime checks PASS; rollback manifest formatting repair PASS.

SSH tunnel attempt 1 was inconclusive due validation pipeline behavior; retry PASSed
with cleanup and sidecar health under `POSTDEPLOY-ISSUE-002`.

## POSTDEPLOY-001 final verification

| Verification | Current result |
| --- | --- |
| Local/BWG/origin refs | Local stabilization docs were ahead; BWG/origin deployment base verified at `165c91f`; final docs publish pending |
| Service stability | PASS; active/enabled, main status 0, `NRestarts=0` |
| Listener boundary | PASS; only `127.0.0.1:18082` |
| Bounded log review | PASS; no panic/ERROR/FATAL or secret/URI marker |
| Self-use tunnel | PASS; Admin UI and auth rejection |
| Self-use smoke | PASS; auth/CRUD/upstream/fail-closed/fallback/cleanup |
| Rollback readiness | PASS; normalized manifest, current release, config/data/log paths, unit verification |
| Existing service safety | PASS; Nginx/rathole active and `nginx -t` pass |

## Stabilization publish verification

- Local bundle verify/list-heads: PASS.
- BWG branch/base/status/path whitelist: PASS.
- Fast-forward-only feature push and post-push ref: PASS at `1aaf193`.
- Temporary ref/bundle cleanup: PASS.
- Force push/main/master/DNS/traffic/deploy/restart/NOSLA SSH: NOT USED.
- Dedicated BWG temporary ref and bundle: CLEANED.

## Deployment verification rows

| Verification | When | Success standard | Failure handling | Current result |
| --- | --- | --- | --- | --- |
| BWG identity/checkout/status | DEPLOY-001 | Alias is `bwg`; intended checkout and clean state are confirmed | Record DEPLOY issue; do not mutate | PASS: feature branch at `e0f2bb6`, clean |
| Port/service conflict | DEPLOY-001 | New service name is unused and `127.0.0.1:18082` is free | Stop and choose no alternative implicitly | PASS |
| Disk and backup capacity | DEPLOY-001/002 | Independent release, config, log, and DB backup paths have capacity | Do not upload or switch release | PASS; timestamped first-deploy manifest created |
| Artifact checksum | DEPLOY-002 | BWG checksum equals local verified artifact | Remove only new staging artifact and record result | PASS |
| Config validation | DEPLOY-002/003 | New config validates without modifying existing Nginx/server blocks | Stop before service start | PASS: unit verified; runtime start pending |
| Sidecar health/smoke | DEPLOY-003 | Local health, auth, managed route, fallback, WebSocket/Range and redaction checks pass | Execute scoped rollback | PASS for available runtime checks; WebSocket/Range remain covered by automated suite |
| Existing service safety | DEPLOY-003 | Existing services remain active and unchanged | Roll back new sidecar only | PASS |
| No DNS/traffic/NOSLA action | Every deployment gate | No such mutation appears in command/log evidence | Stop and report immediately | PASS so far |

## DEPLOY-001 local result

- Temporary Go 1.26.4 toolchain: available; `gofmt`: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- `git diff --check`: PASS.
- BWG checks: pending; no server mutation has occurred.

## DEPLOY-001 BWG result

- Checkout identity/status: PASS.
- Port/service/path conflict checks: PASS.
- Disk capacity: PASS.
- Existing Nginx and rathole active: PASS.
- `nginx -t`: PASS.
- No remote mutation, DNS, traffic, Nginx, or NOSLA action occurred.

## DEPLOY-002 local artifact result

- Source range check from `e0f2bb6`: PASS; only runbook docs differ.
- Linux amd64 static build: PASS.
- Embedded version/commit: PASS.
- Local SHA-256 recorded in the deployment log.
- Remote checksum and backup manifest: pending.

## DEPLOY-002 remote result

- Backup manifest and independent paths: PASS.
- Uploaded and installed artifact checksum: PASS.
- Unit validation and daemon reload: PASS.
- Config permission 0600 and credential non-disclosure: PASS.
- Pre-start service/port/database baseline: PASS.
- Existing services, Nginx, DNS, traffic, and NOSLA: unchanged.

## DEPLOY-003 attempt 1

- Service start, enabled state, loopback listener, DB init, version, and startup
  markers: PASS.
- Full smoke result: INCONCLUSIVE (`DEPLOY-SMOKE-001`).
- Cleanup and existing service safety after attempt: PASS.

## DEPLOY-003 final verification

- Public upstream proxy smoke: PASS.
- Disabled route fail-closed and temporary route cleanup: PASS.
- Service/error/access log scans: PASS; expected access statuses only.
- Artifact checksum, unit/config permissions, service active/enabled, existing
  services, loopback binding, and `nginx -t`: PASS.

## DEPLOY-004 closeout result

- Local verification: PASS (`go test ./...`, `go vet ./...`, `git diff --check`).
- Remote verification: PASS (service, listener, checksum, Nginx/rathole, staging
  cleanup, and secret/error scans).
- Deployment status: COMPLETE for the isolated BWG localhost sidecar.
- Public DNS/traffic and production failover remain intentionally unchanged.

## Deployment record publish verification

- Local bundle verify/list-heads: PASS.
- BWG base/branch/status and path whitelist: PASS.
- Fast-forward-only merge: PASS.
- Feature-only remote push and post-push ref verification: PASS at `1f60e1c`.
- Temporary ref/bundle cleanup: PASS.
- Force push/main/master/deploy/restart/NOSLA SSH: NOT USED.

## DEPLOY-003 attempt 2

- Fixture readiness, Admin UI, auth rejection/login, and route create/list: PASS.
- Private/loopback target execution: fail closed as designed.
- Successful upstream proxy: still PENDING under `DEPLOY-SMOKE-001`.
- Cleanup, sidecar listener, Nginx, and rathole: PASS.

## Day-2 operations verification

| Verification | When | Success standard | Failure handling | Current result |
| --- | --- | --- | --- | --- |
| Feature refs | DAY2-001 start/finish | local, BWG, and origin feature refs reconciled | Stop publish; record mismatch | PASS at base `31fa87c`; local docs commits intentionally ahead until bridge publish |
| Service state | DAY2-001 | active/enabled, status 0, no unexpected restarts | Record issue; restart only sidecar if unhealthy | PASS; active/enabled, status 0, `NRestarts=0` |
| Listener boundary | DAY2-001 | only `127.0.0.1:18082` listens | Stop and investigate; do not change ports | PASS |
| Bounded logs | DAY2-001 | no panic/error/fatal or secret markers | Record redacted evidence and diagnose | PASS before and after smoke |
| Tunnel/access | DAY2-001 | tunnel reaches UI; unauthenticated API rejected | Recreate tunnel; do not expose public port | PASS; tunnel was closed after check |
| Owner smoke | DAY2-001 | CRUD/upstream/fail-closed/fallback/cleanup pass | Record issue and troubleshoot | PASS; temporary routes removed |
| Rollback readiness | DAY2-001 | manifest and release/config/data/log paths readable | Stop finalization until corrected | PASS; unit verification and modes checked |
| Existing ingress safety | DAY2-001 | Nginx/rathole active; `nginx -t` pass; no changes | Stop/report; do not mutate ingress | PASS; no ingress mutation |

## Day-2 docs publish verification

- Local bundle verification/list-heads: PASS.
- BWG branch/base/worktree and runbook-only path whitelist: PASS.
- Fast-forward-only feature push and remote ref verification: PASS at `3419686`.
- Local and BWG temporary artifacts: CLEANED.
- Force push, main/master, deploy/restart, DNS/traffic, Nginx/rathole, NOSLA: NOT USED.

Publish-result docs retry at `a576863`: bundle verify, dynamic target equality,
runbook-only path check, ff-only merge, feature-only push, remote verification,
and temporary cleanup all PASS.

## Public cutover discovery and planning

| Verification | Current result | Evidence/boundary |
| --- | --- | --- |
| Nginx service/config | PASS read-only | Active/enabled; `nginx -t` successful; live files inventoried |
| Rathole service/config | PASS read-only | Active/enabled; unit/config path inventoried; no change |
| Sidecar boundary | PASS baseline | Active/enabled, `NRestarts=0`, status 0, listener `127.0.0.1:18082` |
| Public topology | RECORDED | Existing stream/admin/staging/failover blocks and upstream ports documented |
| Cutover plan/rollback | COMPLETE AS PLAN | `29-32` added; exact backup path awaits Phase 3 |
| Owner decisions | BLOCKING | Hostname/path, Admin exposure, DNS authorization, failover scope pending |

No public reachability or DNS convergence result exists because cutover has not
started.

- Phase 3 backup: PASS at the recorded timestamped path; checksum verification
  passed for every manifest entry.
- Route separation review: PASS as a plan. A new server block can expose only
  `/s/` while denying all Admin UI/API paths and all other locations.
- DNS automation metadata: existing secure provider configuration found; no
  secret content read or recorded.
- Phase 4 dry-run: BLOCKED pending exact canary hostname.

## BWG-only public canary final verification

- Dedicated staging/live `nginx -t`: PASS.
- DNS readiness/apply/public convergence: PASS for canary only, TTL 60.
- Dedicated certificate and hostname verification: PASS.
- Public `/s/` proxy: PASS with non-gateway upstream status.
- Admin UI/API variants: 404 PASS.
- Unknown/disabled routes: 404 fail-closed PASS.
- Legacy fallback: non-5xx PASS.
- Nginx/rathole/sidecar and listener boundary: PASS.
- Existing production/staging/rathole/app files unchanged: PASS.
- Temporary route cleanup and canary-specific log redaction: PASS.
- Rollback script syntax/checksum: PASS; execution not required.

## Owner-created route verification

- Public `v1` information path: HTTP 200 with valid TLS.
- Public Admin UI/API isolation: 404 for all requested paths.
- Sidecar/Nginx/rathole state, restart count, listener boundary, and Nginx
  syntax: PASS.
- Bounded secret/severe/error marker scans: zero.
- Validation-only scope: no route/config/service/DNS/ingress mutation.
