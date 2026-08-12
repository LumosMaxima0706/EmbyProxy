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

- [x] Worktree and commit path whitelist are clean and reviewed.
- [x] Feature branch publication used the BWG publish bridge; no force push or main/master push occurred.
- [x] Deployment was limited to the new BWG sidecar; no unapproved restart, Nginx
      server-block, DNS, traffic, real SQLite, or NOSLA action occurred.
- [x] Owner accepted autonomous deployment; optional operations review remains
      separate from the completed localhost sidecar gate.

## Deployment handoff

- [x] BWG target, checkout, independent service name, port, release path, config,
      log path, and rollback commands are recorded.
- [x] Read-only preflight passed and no existing service or port conflicts exist.
- [x] Timestamped first-deploy baseline/backup manifest exists before service start.
- [x] Artifact checksum matches between local build, BWG staging, and installed release.
- [x] Only the new localhost sidecar is started or reloaded.
- [x] Runtime healthcheck and smoke tests pass; logs contain no secrets.
- [x] Rollback target-specific commands and first-deploy baseline were verified.
- [x] No DNS, public traffic, existing Nginx block, NOSLA, or full-host reboot action occurred.
- [x] Deployment result and next gate are committed locally; publish uses the BWG bridge.

## Post-deploy stabilization

- [x] Local/BWG/origin feature refs are reconciled after stabilization docs publish.
- [x] Sidecar remains active/enabled without an unexpected restart.
- [x] Listener remains loopback-only.
- [x] Bounded logs contain no panic, ERROR/FATAL, or secret leakage.
- [x] Admin UI/auth/CRUD/upstream/fail-closed/fallback smoke passes again.
- [x] Rollback manifest and target-specific paths are complete and readable.
- [x] Owner SSH-tunnel self-use guide exists without exposing credentials.
- [x] No DNS, public traffic, existing Nginx block, rathole, or NOSLA change occurred.

## Day-2 operations

- [x] Owner operations guide covers tunnel, UI, service, listener, logs, route basics, and exposure boundary.
- [x] Troubleshooting guide covers access, service, port, auth, CRUD, proxy, and log failures.
- [x] Non-destructive backup/restore drill verifies manifest and scoped rollback.
- [x] Day-2 checklist covers use, route changes, incidents, rollback, and future cutover.
- [x] `DAY2-001` closed after current BWG read-only checks passed.

## Public cutover planning

- [x] Read-only Nginx/rathole/systemd/public-entry discovery recorded in `28`.
- [x] Execution, healthcheck, rollback, and owner decision gates recorded in `29-32`.
- [x] Exact public hostname approved by owner; `/s/` and Admin isolation are approved.
- [x] Existing secure DNS automation used for one verified canary record.
- [x] Pre-cutover backups created and verified.
- [x] Nginx staging dry-run and localhost validation passed.
- [x] BWG-only canary DNS/Nginx/TLS cutover executed and verified; rathole unchanged.
- [x] Rollback threshold and exact rollback script recorded; no failure required execution.
- [x] Owner-created `v1` route returns HTTP 200 publicly with valid TLS while Admin UI/API remain 404.
- [x] Post-route service/listener and bounded secret/severe log scans pass.

## Final owner self-use handoff

Historical BWG-only canary milestone; superseded for production target and
Admin access by the failover/secure-Admin gate below.

- [x] BWG-only public canary is accepted for owner self-use.
- [x] Retained managed route slug: `v1`.
- [x] Public entry: `canary.149077530.xyz`, media path `/s/v1/`.
- [x] Production traffic and existing production/staging entries remain unchanged.
- [x] Admin access remains owner-only through the documented SSH tunnel.
- [x] NOSLA was not accessed and automatic failover was not entered.

## NOSLA/BWG failover and secure Admin next gate

- [x] Phase 0 safety boundary is recorded.
- [x] BWG/NOSLA read-only topology discovery completed.
- [x] No media smoke, prefetch, warmup, or production apply was performed.
- [x] Owner provider-panel opening balances, quotas, reset cycles, and RX+TX billing definition confirmed.
- [x] Restricted host-counter auxiliary source deployed; it is explicitly documented as an estimate, not provider billing.
- [ ] Hybrid estimate calibrated against the provider panel at the next reset cycle.
- [x] Requested 85% policy and fail-closed dry-run scenario matrix implemented locally.
- [x] Policy dry-run/live apply and simulated rollback tested after accounting input.
- [x] Separate two-layer public Admin entry backed up, dry-run, applied, and verified.
- [x] Legacy timer is inactive/disabled; the new timer is the only controller.
- [x] Production small endpoints and retained canary pass; no media object was requested.
- [x] BWG/NOSLA no-cache, Range headers, services, Nginx hashes, and bounded logs pass.
- [x] Production `stream` is policy-controlled and currently targets NOSLA;
  canary, staging-stream, stream-b, and rathole remained unchanged.
- [x] Independent Admin hostname has Basic Auth plus application auth; canary
  Admin remains 404 and the SSH tunnel remains the private fallback method.
- [x] Corrected policy timer has a finite next trigger and completed a natural
  no-mutation cycle; the legacy timer remains inactive/disabled.
- [ ] ACME issuance/renewal is `blocked_by_acme_rate_limit`; action is
  `wait_and_retry_later`. Do not retry production ACME; validate with staging
  first and wait for the reported 429 cooldown.
