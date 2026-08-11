# Verification Matrix

| Verification | When to run | Success standard | Failure handling | Current result |
| --- | --- | --- | --- | --- |
| `git branch --show-current`, `git rev-parse --short HEAD` | Every task start and commit gate | Expected feature branch and recorded base/HEAD | Stop, record mismatch; no reset | Branch verified; HEAD `aee3871` |
| `git status --short --untracked-files=all` | Before/after every task | Only declared task paths are dirty | Stop and classify unexpected paths | Current dirty source/docs state recorded in ISSUE-WORKTREE-001 |
| `git diff --check` | Before every commit and after edits | No whitespace errors | Fix only in task scope, re-run | Passed for current tracked changes |
| `gofmt -w <changed Go files>` | Before source tests/commit | Changed Go files format successfully | Record toolchain or format issue; do not commit | BLOCKED: `gofmt` unavailable |
| `go test ./internal/storage ./internal/admin ./internal/proxyadapter` | After C/D source changes | Targeted packages pass | Record failing command/test; do not hard-commit | BLOCKED: `go` unavailable |
| `go test ./...` | After implementation batches and before delivery | All Go packages pass | Stop, record issue, do not auto-fix outside scope | Not run; toolchain unavailable |
| `go vet ./...` | Before source delivery/release | No vet findings | Record findings and stop current gate | Not run; toolchain unavailable |
| Admin UI build/test | After E-001 | Existing project UI checks pass, or manual review evidence exists | Record unavailable automation and perform documented manual review | Not applicable yet; UI batch not started |
| Manual admin review | When UI automation unavailable | Auth, CRUD, validation errors, no secret display, fallback preserved | Record checklist failures | Pending E-001 |
| Secret redaction check | Every proxy/admin logging batch | No token, cookie, password, sensitive query, or credential in logs/output | Stop, redact, add regression test | Existing redaction tests identified; current batch not executed |
| No deploy/restart/SSH check | Every task close | No server, process, Nginx/systemd, DNS, real SQLite, or SSH action | Stop and record unauthorized action | Passed; none performed |

## Evidence Requirements

Each completed row requires command, timestamp, result summary, and link to tracker/progress entry. A blocked row must name the issue ID and recovery step. A passing local test does not authorize push or deployment.
