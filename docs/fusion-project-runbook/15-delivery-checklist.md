# Delivery Checklist

## Implementation

- [ ] Managed route storage CRUD is transaction-safe and tested.
- [ ] Authenticated Admin API can create, update, list, and delete managed routes.
- [ ] Admin UI can operate the API without changing existing authentication boundaries.
- [ ] Runtime resolver loads managed routes and preserves flag-off/unknown legacy fallback.
- [ ] Managed routes reach mediaproxy for HTTP, Range, WebSocket, headers, rewrite, and transport failures.
- [ ] Migration/backward compatibility and rollback behavior are documented and tested.
- [ ] Failover policy remains NOSLA primary, BWG fallback, DNS preferred, redirect optional.

## Tests And Security

- [ ] Changed Go files pass gofmt.
- [ ] Targeted packages pass.
- [ ] `go test ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] Auth, origin, target validation, WebSocket status mapping, unknown traffic, DNS mock, and log redaction checks pass.
- [ ] No secret, token, cookie, password, sensitive query, complete UUID, or subscription link is present in output or logs.

## Documentation And Audit

- [ ] Tracker has a status, validation result, next action, and commit for every step.
- [ ] Issue log records every blocker and its resolution evidence.
- [ ] Verification matrix reflects current results, not planned results.
- [ ] Progress log records each task close and external-impact boundary.
- [ ] Gap log distinguishes implementation blockers from release hygiene.

## Known Issues

- `ISSUE-TOOLCHAIN-001`: Go/gofmt unavailable in the current environment; source commit is not safe until recovered.
- Current managed-route source batch remains uncommitted until formatting and tests pass.
- `GAP-PROV-002`: license/provenance/SBOM/notices remain non-blocking for implementation but block formal release/public distribution until reviewed.

## Release Hygiene

- [ ] Root LICENSE/copyright notice confirmed and added by authorized owner decision.
- [ ] README upstream attribution has revision, license, scope, and notice requirements.
- [ ] mediaproxy/proxyadapter per-file provenance matrix is complete.
- [ ] Direct/indirect Go dependency license inventory and SBOM decision are complete.
- [ ] Third-party notices and release artifacts are reviewed.

## Final Gate

- [ ] Worktree and commit path whitelist are clean and reviewed.
- [ ] No push has occurred without the BWG publish bridge gate.
- [ ] No deployment, restart, SSH, Nginx/systemd, DNS, or real SQLite action is implied by this checklist.
- [ ] Human review approves the next gate before any publish or deployment action.
