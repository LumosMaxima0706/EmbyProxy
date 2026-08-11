# Issue Resolution Log

Every issue is recorded when detected. Resolution work updates the same row with evidence; unresolved issues remain visible and linked from the tracker.

| Issue ID | Detected during step | Symptom | Root cause | Impact | Blocking? | Resolution plan | Resolution result | Status | Related commit |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ISSUE-TOOLCHAIN-001 | B-001 preflight / C-001-D-001 verification | `go` and `gofmt` commands were unavailable; formatting and tests could not run | System Go is not installed and no manager-provided binary exists | Source batch could not initially be safely formatted, tested, or committed | Yes for source commit; no for docs-only planning | Search local paths/managers, then use a safe user-local package extraction if a configured package source is available | Resolved by downloading `golang-1.26-go` and `golang-src` from the configured apt source into `/tmp/embyproxy-go-recovery.gpw7iY`, extracting with `dpkg-deb -x`, and verifying `go version go1.26.0 linux/amd64` plus gofmt help output; system package database unchanged | DONE | None |
| ISSUE-SOURCE-001 | C-001/D-001 carry-over | Managed-route storage/admin API source changes were dirty and uncommitted | Implementation began before toolchain recovery was available | Remaining admin/proxyadapter batch still needs validation; storage subset can be delivered independently | Yes for remaining batch | Keep exact dirty paths, run targeted/full tests, then commit only reviewed source/tests | Storage subset resolved in `98229bd`; admin/proxyadapter batch resolved in `0b7b590`; no source paths remain dirty | DONE | `0b7b590` |
| ISSUE-WORKTREE-001 | A-001 precheck | Worktree contained six managed-route source/test paths, including two untracked files | Prior Phase 3 implementation attempt intentionally stopped before commit | Cleaning or reset would risk losing owner-authorized work | Yes for clean source verification; no for runbook documentation | Do not reset/clean; classify paths in tracker and commit only after verification | Source paths were committed as `98229bd` and `0b7b590`; remaining dirty paths are declared runbook evidence updates | DONE | `0b7b590` |
| ISSUE-ADMIN-001 | D-001 targeted test | `go test ./internal/admin ./internal/proxyadapter` initially failed because validation returned strings as `error` | `validateManagedRouteRequest` had an `error` return type but did not wrap error codes | Admin API could not compile | Yes for D-001 | Wrap stable error codes with `errors.New`, rerun targeted tests, then full tests before commit | Fixed with `errors.New`; targeted admin/proxyadapter tests, `go test ./...`, `go vet ./...`, and `git diff --check` all passed | DONE | `0b7b590` |

## Resolution Recording Rules

- A resolved issue must include the exact command/evidence, changed files, resulting status, and related commit.
- Toolchain recovery does not authorize deployment, SSH, push, or dependency installation.
- A test failure becomes a separate issue if it has a distinct cause or scope.

## ISSUE-TOOLCHAIN-001 Autonomous Recovery Attempt 1

- **Status**：IN_PROGRESS
- **Owner authorization**：本地自主恢复已获授权；不需要 owner-only access、secret 或生产操作。
- **计划**：搜索 PATH、常见安装目录、Go 版本管理器、系统包管理器和项目脚本；发现可用工具链后临时更新 PATH，随后运行 gofmt/targeted/full tests/vet。
- **已尝试**：`command -v go`、`command -v gofmt`、`go version`；此前已检查 `/usr/local/go/bin`、`/usr/bin`、`/usr/local/bin`、用户本地常见目录。
- **当前结果**：待完成本轮更广泛的本机搜索与包源检查。

### Recovery attempt 2: local package source

- **尝试**：确认 `apt`/`apt-get` 可用、`golang-go` 有本地配置的软件源候选；当前用户无免密 sudo，因此优先使用用户可写临时目录下载并解包，不修改系统包数据库。
- **安全边界**：仅本地开发环境；不接触生产服务器、凭据或项目外部服务配置。
- **结果**：待执行临时包下载/解包并验证 `go`/`gofmt`。

### Recovery attempt 2 result

- Temporary package extraction succeeded; Go 1.26.0 and gofmt are available via `/tmp/embyproxy-go-recovery.gpw7iY/extracted/usr/lib/go-1.26/bin`.
- No system install, service action, deployment, SSH, or secret access occurred.

### Recovery attempt 3: complete standard library source

- **Symptom**：Go binary starts, but `go test ./internal/storage` reports standard packages such as `context`, `bytes`, and `database/sql` missing from the temporary GOROOT.
- **Cause**：The downloaded `golang-src` package is only a small transitional package; the versioned `golang-1.26-src` payload was not yet extracted.
- **Plan**：Download and extract `golang-1.26-src` into the same temporary root, then rerun the storage test without changing repository dependency files.
- **Status**：IN_PROGRESS.

### Recovery attempt 3 result

- `golang-1.26-src` was downloaded and extracted into the same temporary GOROOT.
- `go test ./internal/storage` passed after the complete GOROOT was available.
- C-001 storage source was committed as `98229bd`; D-001 remains the active source task.

### ISSUE-TOOLCHAIN-001 final status correction

- B-001 recovery is complete, not in progress.
- Temporary Go 1.26 toolchain remains available at the recorded local path for verification; commands use `GOTOOLCHAIN=local` to avoid automatic toolchain downloads.
- C-001, D-001, and E-001 verification completed successfully after recovery.
- Status: DONE; related commits: `98229bd`, `0b7b590`, `8c00f1a`.

| ISSUE-ROUTETEST-001 | G-001 | New header-rewrite assertion initially failed to compile with `undefined: upstream` | Test handler referenced the server variable while constructing that same server | G-001 verification was briefly blocked; production source was unchanged | No | Replace the self-reference with the request host, rerun targeted/full tests and vet | Replaced self-reference with `http://` plus request host; targeted/full tests and vet passed | DONE | `4e60097` |

## I-001 regression/security result

- No new issue was found during targeted redaction tests, authenticated managed-route API tests, full `go test ./...`, `go vet ./...`, and `git diff --check`.
- Static audit found no tracked secret-named files or minified/bundled/WASM/source-map/generated artifacts. Production logging paths use redacted request URI helpers where sensitive route data is involved.
- I-001 status: DONE; remaining open provenance and failover provider gaps are documented and are not silently closed by this regression pass.

| ISSUE-PROV-002 | J-001 | Authoritative release provenance artifacts cannot be completed from local repo evidence alone | Root rights-holder/year, upstream revision/license/scope, per-file provenance, and dependency license/SBOM decisions require owner or rightsholder confirmation | Blocks formal public distribution/release sign-off; does not block implementation or local testing | Yes for J-001 release gate; No for implementation | Preserve pending evidence matrix, request owner/rightsholder decisions, then update notices/license/SBOM without inventing facts | Local skeleton and checklist are complete; owner/rightsholder evidence still pending | BLOCKED | TBD |

### ISSUE-PUBLISH-001 authorization update

- Status: IN_PROGRESS.
- Owner authorized the current feature branch through the documented BWG bridge only.
- Planned evidence: local precheck/bundle verify, BWG branch/base/status/bundle/temp-ref/path whitelist, ff-only merge, feature-ref push, remote verification, and cleanup.
- Any failed sub-step must stop the bridge and be recorded before a retry.

| ISSUE-PUBLISH-002 | BWG publish bridge attempt 1 | Remote path whitelist command failed with an `awk` quoting parse error after bundle verification | Local shell quoting embedded an unnecessary escaped awk program in the SSH command | Merge and push did not run; BWG checkout remained unchanged | No | Clean the temporary ref/bundle, replace the remote whitelist check with a quoting-safe command, rerun all checks before merge | Temporary ref and bundle cleaned; BWG worktree remains clean; retry authorized under the same gate | DONE | TBD |
| ISSUE-PUBLISH-003 | BWG publish bridge attempt 2 | Push-precheck assertion expected the remote feature ref to equal the new target before push | Retry script compared remote ref to target instead of the expected base; the check was ordered before push | BWG fast-forward merge completed to `61de764`, but push was not attempted; remote stayed at `c7f475c` | No | Compare remote to base before push, then to target only after push | Corrected in the next retry; final push and remote verification passed | DONE | `5cbbe54` |
| ISSUE-PUBLISH-001 | BWG publish bridge readiness | Remote feature branch was clear and local target was ahead; direct publisher authentication was unavailable | Codex environment is not the trusted publisher; BWG bridge was required | Publish gate was blocked until owner authorized BWG bridge | Yes for publish only; No for implementation | Use the documented BWG bundle/ff-only/feature-ref flow after explicit authorization | Owner authorized bridge; BWG verified and pushed `5cbbe54` to the feature ref; remote verification passed | RESOLVED | `5cbbe54` |

| DEPLOY-001 | Deployment preflight | BWG sidecar target, service name, port occupancy, backup paths, and runtime boundaries require confirmation | Deployment state is external to the repository and must be checked read-only before mutation | Blocks deployment mutation only; does not affect local implementation | Yes for DEPLOY-002 onward | Use `ssh bwg` read-only checks; record exact target paths and conflicts; stop if target is ambiguous | PENDING preflight | IN_PROGRESS | TBD |

## Deployment issue recording rule

Every failed preflight, backup, artifact, service, health, or rollback check gets
its own issue row before another attempt. Secrets and credential material are never
recorded.
