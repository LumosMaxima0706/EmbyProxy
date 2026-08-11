# Delivery Checklist

## Implementation

- [x] Managed route storage CRUD is transaction-safe and tested.
- [x] Authenticated Admin API can create, update, list, and delete managed routes.
- [x] Admin UI can operate the API without changing existing authentication boundaries; browser review remains pending.
- [x] Runtime resolver loads managed routes and preserves flag-off/unknown legacy fallback.
- [x] Managed routes reach mediaproxy for HTTP, Range, WebSocket, headers, rewrite, and transport failures.
- [x] Migration/backward compatibility and rollback behavior are documented and tested.
- [ ] Failover policy remains NOSLA primary, BWG fallback, DNS preferred, redirect optional.

## Tests And Security

- [x] Changed Go files pass gofmt.
- [x] Targeted packages pass.
- [x] `go test ./...` passes.
- [x] `go vet ./...` passes.
- [x] Auth, origin, target validation, WebSocket status mapping, unknown traffic, DNS mock, and log redaction checks pass locally.
- [ ] No secret, token, cookie, password, sensitive query, complete UUID, or subscription link is present in output or logs.

## Documentation And Audit

- [x] Tracker has a status, validation result, next action, and commit or issue for every completed/blocked step.
- [x] Issue log records every blocker and its resolution evidence or owner-only dependency.
- [x] Verification matrix reflects current results, not planned results.
- [x] Progress log records each task close and external-impact boundary.
- [x] Gap log distinguishes implementation blockers from release hygiene.

## Known Issues

- `ISSUE-TOOLCHAIN-001`: resolved with a temporary user-local Go 1.26 toolchain; system package state was not changed.
- Browser automation is unavailable; E-001 still needs human browser review against a local authenticated instance.
- `GAP-PROV-002` / `ISSUE-PROV-002`: license/provenance/SBOM/notices remain non-blocking for implementation but block formal release/public distribution until owner/rightsholder review.
- `GAP-RUNTIME-001`, `GAP-TRAFFIC-001`, `GAP-DNS-001`, `GAP-POLICY-001`, and `GAP-REDIRECT-001` remain open for later failover phases; this route fusion batch does not close them.

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
