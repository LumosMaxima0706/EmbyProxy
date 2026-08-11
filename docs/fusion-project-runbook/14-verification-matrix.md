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
| Admin UI build/test | After E-001 | Existing project UI checks pass, or manual review evidence exists | Record unavailable automation and perform documented manual review | Not applicable yet; UI batch not started |
| Manual admin review | When UI automation unavailable | Auth, CRUD, validation errors, no secret display, fallback preserved | Record checklist failures | Pending E-001 |
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
