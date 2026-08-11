# Deployment Execution Log

Status: IN_PROGRESS (preflight pending)

| Time | Gate | Action | Result | Impact / next action |
| --- | --- | --- | --- | --- |
| 2026-08-11 | Authorization | Owner authorized autonomous BWG self-use deployment | RECORDED | Continue with read-only preflight |
| 2026-08-11 | Local preflight | Branch `feature/failover-phase2-local`, HEAD `e0f2bb6`, worktree clean | PENDING VERIFICATION | Run local tests and artifact checks |
| 2026-08-11 | BWG preflight | Host, port, service, release, backup, and Nginx checks | PENDING | No mutation until recorded |

No server mutation, service reload/restart, DNS change, or traffic cutover has
been performed in this deployment attempt.

## Local verification before BWG preflight

- Branch: `feature/failover-phase2-local`.
- Local HEAD at plan commit: `591d966`.
- `gofmt` available through the recorded temporary local Go toolchain: PASS.
- `go test ./...`: PASS.
- `go vet ./...`: PASS.
- `git diff --check`: PASS.
- No source changes or server mutation occurred.

## BWG read-only preflight

- SSH alias `bwg`: reachable.
- Architecture: x86_64.
- Staging checkout: present, expected feature branch, HEAD `e0f2bb6`, clean.
- `127.0.0.1:18082`: free.
- `embyproxy-gsy-sidecar.service`: free.
- Independent release/config/data/log paths: free.
- Root filesystem free capacity: approximately 29 GiB.
- Nginx: active; configuration test PASS.
- rathole: active.
- Existing test location: absent.
- Staging `.env`: absent; staging data exists and will not be reused.
- Result: PASS. No files, services, Nginx, DNS, or traffic were changed.

Next gate: build a static artifact from the `e0f2bb6` source tree, verify its
checksum, create an independent backup manifest, and upload to a new release path.

## Artifact build

- Confirmed no `cmd`, `internal`, `go.mod`, or `go.sum` differences between
  `e0f2bb6` and the current docs-only HEAD.
- Built Linux amd64 with `CGO_ENABLED=0`, `-trimpath`, and embedded commit
  `e0f2bb6` using the verified local Go 1.26.4 toolchain.
- Version command: PASS (`sidecar-e0f2bb6`).
- Static ELF check: PASS.
- SHA-256: `bbd6d1fb69c3d988ca617ea0918e40be45acd5dde4b541535c23e225555632aa`.
- Local temporary artifact: `/tmp/embyproxy-sidecar-e0f2bb6.LiWZRw/embyproxy`.
- No repository file or remote host was changed by the build.

## Deployment configuration preparation

- Added an auditable systemd unit using a dedicated user and independent paths.
- Added a non-secret environment template fixed to `127.0.0.1:18082`, managed
  routes enabled, real DNS apply disabled, and mock fixture disabled.
- The required administrator credential will be generated on BWG with mode 0600;
  its value will not be printed, transferred back, or committed.

## DEPLOY-002 backup and installation result

- Backup manifest: `/root/backups/embyproxy-gsy-sidecar/20260811T151516Z/pre-deployment-manifest.txt`.
- Uploaded artifact and templates only to `/root/staging/embyproxy-sidecar-e0f2bb6`.
- Remote staging and installed SHA-256 matched the local artifact: PASS.
- Created the dedicated system user and independent release/config/data/log paths.
- Installed release `e0f2bb6` and a new `current` symlink.
- Generated the administrator credential on BWG without output; environment mode is 0600.
- Unit mode is 0644 and binary mode is 0755.
- `systemd-analyze verify`: PASS; `systemctl daemon-reload`: PASS.
- Pre-start state: service inactive and disabled, port free, database absent.
- Existing Nginx, rathole, staging checkout/data, DNS, and traffic were not changed.

DEPLOY-002 result: PASS. DEPLOY-003 sidecar start and localhost validation is next.

## DEPLOY-003 start and smoke attempt 1

- New service enable/start: PASS.
- Active/enabled, loopback-only listener, database initialization, startup log
  markers, and embedded version: PASS.
- First smoke script: INCONCLUSIVE. The temporary upstream initially refused a
  connection and the script did not emit later stage markers.
- Post-attempt safety check: sidecar, Nginx, and rathole active; sidecar listener
  intact; fixture port free; temporary smoke directory cleaned.
- Issue: `DEPLOY-SMOKE-001`. No rollback was required because the service health
  gate remained passing; a diagnostic smoke retry is required.

## DEPLOY-003 smoke attempt 2 diagnosis

- Fixture readiness: PASS.
- Admin UI embedded managed-route editor: PASS.
- Unauthenticated managed-route API rejection: PASS.
- Credential-based local login: PASS without credential output.
- Managed-route create/list: PASS.
- Proxy to loopback fixture: correctly rejected by mediaproxy private-target guard.
- Cleanup: PASS; service/Nginx/rathole remain active.
- Root cause: the fixture design conflicted with the production SSRF policy, not a
  runtime service defect.
- Next attempt will not relax the guard. It will use one read-only request to an
  existing owner-controlled public Emby entry and will not output response content.

## DEPLOY-003 final result

- Existing owner-controlled public Emby route request: PASS; response body was not
  printed. Gateway failure statuses were not observed.
- Managed-route disabled mode: PASS (404 fail-closed).
- Temporary route and credentials/cookies: cleaned from smoke workspace.
- Token/log scan: PASS; no token, authorization, cookie, password, secret query, or
  URI credential marker found.
- Service active/enabled and installed checksum: PASS.
- Loopback-only listener: PASS; no public 18082 listener.
- Existing Nginx/rathole active and `nginx -t`: PASS.
- Remote artifact staging files: cleaned; release, config, data, logs, and backup
  manifest retained.

DEPLOY-003 result: PASS. The new sidecar is usable through owner-controlled
localhost access. DEPLOY-004 documentation closeout is next.
