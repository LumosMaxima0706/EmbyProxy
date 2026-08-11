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
- Dedicated BWG temporary ref and bundle: CLEANED.

## Deployment verification rows

| Verification | When | Success standard | Failure handling | Current result |
| --- | --- | --- | --- | --- |
| BWG identity/checkout/status | DEPLOY-001 | Alias is `bwg`; intended checkout and clean state are confirmed | Record DEPLOY issue; do not mutate | PASS: feature branch at `e0f2bb6`, clean |
| Port/service conflict | DEPLOY-001 | New service name is unused and `127.0.0.1:18082` is free | Stop and choose no alternative implicitly | PASS |
| Disk and backup capacity | DEPLOY-001/002 | Independent release, config, log, and DB backup paths have capacity | Do not upload or switch release | PASS for preflight; backup creation pending |
| Artifact checksum | DEPLOY-002 | BWG checksum equals local verified artifact | Remove only new staging artifact and record result | PENDING |
| Config validation | DEPLOY-002/003 | New config validates without modifying existing Nginx/server blocks | Stop before service start | PENDING |
| Sidecar health/smoke | DEPLOY-003 | Local health, auth, managed route, fallback, WebSocket/Range and redaction checks pass | Execute scoped rollback | PENDING |
| Existing service safety | DEPLOY-003 | Existing services remain active and unchanged | Roll back new sidecar only | PENDING |
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
