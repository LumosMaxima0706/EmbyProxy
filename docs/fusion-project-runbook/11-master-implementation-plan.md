# Master Implementation Plan

## Operating Contract

This is the executable plan for the full fusion project. The project is the integration of EmbyProxy's management panel, SQLite-backed management APIs, node operations, and visual operations with `emby-reverse-proxy-go`-inspired automatic proxy/data-plane behavior as implemented in the local `mediaproxy` core. It is not a failover-only project.

The owner has authorized source modification, refactoring, rewriting, replacement, deletion, and integration for this fusion objective. Remaining provenance/license/SBOM/notices work is tracked as release hygiene and does not block implementation. No task may claim production readiness without the relevant verification evidence.

Every task is tracked in `12-step-execution-tracker.md`. Before work starts, set its status to `IN_PROGRESS`. After work, record changed files, validation evidence, result, next action, and commit hash or the blocking issue. Every new problem is recorded in `13-issue-resolution-log.md` before resolution work begins.

## Target Architecture

- BWG is the only management panel and fusion control-plane deployment location.
- NOSLA is the primary data-plane node; BWG is the fallback data-plane node.
- Normal media traffic prefers NOSLA. Health failure, maintenance, threshold, or outage moves traffic to BWG; a stable new cycle can move traffic back.
- DNS failover is preferred. 302 redirect is an optional fallback and must not be treated as the primary design.
- The management flow is: Admin UI -> authenticated Admin API -> SQLite storage -> managed-route resolver -> `proxyadapter` -> `mediaproxy` -> authorized upstream media target.
- Existing node routes and unknown routes remain on the legacy fallback until the managed-route feature flag and route contract explicitly select the new path.
- Failover policy/controller/scheduler, health and traffic abstractions, DNS mock/guards, redirect helper, and persistence are local components; real provider and deployment wiring remain later gates.

## Current Code Entrances

- `internal/admin/admin.go`: authentication, admin dispatch, and HTTP entrypoint.
- `internal/admin/failover_api.go`: existing authenticated failover, traffic, and DNS APIs.
- `internal/storage/store.go`: SQLite lifecycle and shared store.
- `internal/storage/managed_routes.go`: managed route schema and resolver queries; CRUD implementation is the current dirty batch.
- `internal/proxyadapter/storage_resolver.go`: managed slug/node resolution and legacy fallback boundary.
- `internal/proxyadapter/router.go`: production router selecting managed route, node route, or fallback.
- `internal/mediaproxy/`: target validation, HTTP/range/header/rewrite/transport/WebSocket data-plane executor.
- `cmd/embyproxy/main.go`: runtime wiring, feature flags, failover fixtures, proxy and admin handlers.
- `internal/failover/`: policy, controller, persistence contracts, health/traffic/DNS mocks, scheduler, and redirect helper.

## Phase Execution Map

Each phase has a single owner task gate. A phase can advance only when its listed acceptance criteria are recorded in the tracker and verification matrix.

### Phase A: Repo/runbook normalization

Tasks: confirm branch/HEAD/status; keep the runbook tracked; create this plan, tracker, issue log, verification matrix, and delivery checklist; classify existing dirty source changes without discarding them.

Depends on: current Git repository and owner authorization. Acceptance: runbook files exist, path whitelist passes, dirty paths are explicitly mapped, and no source change is lost. Verify with `git status --short --untracked-files=all`, `git diff --check`, and runbook path checks.

### Phase B: Toolchain and verification recovery

Tasks: locate an existing Go toolchain without installation; establish reproducible `gofmt`, targeted test, full test, vet, and optional UI test commands; record environment limitations.

Depends on: Phase A. Acceptance: `go version`, `gofmt -w` on changed Go files, and test commands are executable, or a BLOCKED issue contains a concrete recovery path. Verify with `command -v go`, `command -v gofmt`, `go test`, and `go vet`.

### Phase C: Managed route storage foundation

Tasks: finish and verify transactional managed-route list/save/delete storage; preserve schema compatibility, timestamps, line replacement atomicity, and foreign-key cleanup.

Depends on: Phase B toolchain or an explicitly documented verification block. Acceptance: storage tests cover create, list, update, line replacement, delete, rollback, duplicate constraints, and route/line ordering. Verify targeted storage tests and `go test ./internal/storage`.

### Phase D: Admin API integration

Tasks: finish authenticated managed-route GET/PUT/DELETE API; validate slug, node, line, target, public/enabled invariants; return stable redacted errors; add auth, invalid input, conflict, and persistence tests.

Depends on: Phase C. Acceptance: Admin API can drive the SQLite managed route without exposing secrets and unknown admin paths remain unchanged. Verify admin tests and API-to-storage integration tests.

### Phase E: Admin UI integration

Tasks: expose managed-route list/editor/status in the existing embedded admin UI; use the existing auth/session flow; support add/update/delete, enabled/public/default line, and validation errors without changing `/admin/` security boundaries.

Depends on: Phase D API contract. Acceptance: UI requests match the API contract, failures are visible, and no credentials/targets with sensitive queries are logged. Verify static asset/manual browser review and any existing UI test/build command.

### Phase F: Proxyadapter runtime loading

Tasks: wire runtime managed-route resolver construction from the configured store and identity context; keep feature flag default-safe; preserve node and unknown-route fallback; add startup and resolver lifecycle tests.

Depends on: Phase C and D. Acceptance: an API-created route is resolvable by the production router, disabled/private routes fail closed, and unknown routes use legacy fallback. Verify proxyadapter and `cmd/embyproxy` tests.

### Phase G: Mediaproxy routing integration

Tasks: complete managed slug -> mediaproxy execution for HTTP, Range, headers, WebSocket, rewrite, transport errors, and redacted logs; verify route target cannot be overridden by query/header input.

Depends on: Phase F. Acceptance: managed route data reaches the existing mediaproxy core while the legacy path remains intact. Verify targeted proxyadapter/mediaproxy tests and integration tests.

### Phase H: Migration/backward compatibility

Tasks: define any schema migration/versioning, route import from existing EmbyProxy node configuration, rollback to feature-flag-off behavior, and compatibility for existing node records and admin actions.

Depends on: Phase C-G contracts. Acceptance: temporary SQLite migration tests pass, old records remain readable, and disabling the flag restores fallback behavior. Verify migration, restart/restore mock, and rollback tests.

### Phase I: Tests/regression/security hardening

Tasks: run full unit/integration/race-appropriate tests, vet, diff hygiene, auth/origin checks, target validation, WebSocket status mapping, log redaction, unknown traffic handling, and failover mock scenarios.

Depends on: all implementation phases. Acceptance: verification matrix is complete or every failure is an explicit issue; no secret output; no production dependency. Verify `gofmt`, targeted/full `go test`, `go vet`, redaction scans, and manual review.

### Phase J: Release/docs hygiene

Tasks: complete `GAP-PROV-002` license/notice/attribution matrix, dependency inventory/SBOM decision, release notes, and operator rollback documentation.

Depends on: implementation evidence and owner/rightsholder inputs. Acceptance: release artifacts have required notices and provenance evidence. This phase can block formal release/public distribution, not implementation coding.

### Phase K: Final delivery checklist

Tasks: reconcile tracker/gap/verification/progress logs, confirm clean build/test evidence, review changed paths and commits, prepare the approved publish bridge bundle, and wait for explicit publish/deploy gates.

Depends on: Phase I and, for public release, Phase J. Acceptance: `15-delivery-checklist.md` is complete, no unreviewed blocker remains, and deployment/push are separately authorized. Verify Git status, commit path whitelist, bundle verification, and final manual review.

## Failure And Rollback Rules

- A failed test stops the current task; record the command, test, symptom, likely cause, and next action before any fix.
- Source rollback during development is a new corrective change; never reset or clean away user changes.
- Feature flag off, legacy fallback, and SQLite transaction rollback are the default implementation rollback paths.
- No real provider, DNS apply, server SSH, Nginx/systemd action, production SQLite write, deployment, or push is implied by this plan.
