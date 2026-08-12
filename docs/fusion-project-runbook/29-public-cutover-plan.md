# Public Cutover Plan

Status: PHASE 2 COMPLETE - BWG-ONLY CANARY, HOSTNAME PENDING

## Preconditions

- Feature branch and origin ref reconcile at the approved feature commit.
- BWG sidecar remains active/enabled on `127.0.0.1:18082` with no unexpected
  restarts.
- Exact hostname/path, public exposure policy, DNS method, and canary scope are
  decided by the owner.
- No main/master push, force push, NOSLA SSH, or broad ingress replacement.

## Planned execution order

1. Record a read-only pre-cutover snapshot of Nginx/rathole/systemd/listeners,
   DNS observations, current config hashes, and the sidecar health baseline.
2. Create timestamped, mode-preserving backups of the exact Nginx files,
   rathole config/unit if in scope, sidecar unit/config/release pointers, and
   the pre-cutover manifest. Verify each backup is readable and hash it.
3. Render a staging Nginx file containing only the approved dedicated route.
   Run `nginx -t -c <staging-config> -p <staging-prefix>` where feasible, or
   validate an isolated include with the same installed binary. Do not alter
   the live config during dry-run.
4. Validate the sidecar locally and through the intended Host/path using a
   credential-free, bounded request. Confirm admin/API denial and route
   fail-closed behavior before any public change.
5. Apply the smallest approved Nginx change, run `nginx -t`, and reload only
   Nginx if the owner-approved route requires it. Immediately verify Nginx and
   sidecar status, listener boundary, and error logs.
6. Apply DNS only through the owner-approved provider path, after the Nginx
   route is ready. Verify authoritative answers and external reachability before
   considering the cutover successful.
7. Change rathole only if the approved topology explicitly requires it; validate
   its syntax and service state after the smallest possible unit-scoped reload.
8. Observe the abort threshold: any failed admin isolation, proxy smoke,
   fail-closed, service state, TLS, DNS convergence, or secret-redaction check
   triggers immediate rollback unless fixed without widening scope.

## Rollback decision

Rollback is automatic for a critical failure that is not resolved by a bounded
configuration correction. Restore the exact backed-up Nginx/rathole files,
validate syntax, reload only the affected service, and restore the prior DNS
record through the same provider path if DNS changed. Re-run the pre-cutover
health checks and record the incident before stopping.

## Healthcheck and smoke matrix

- Nginx/rathole/sidecar active, enabled, status zero, and no unexpected restart.
- Public TLS/HTTP entry reaches only the approved route.
- `/admin/`, `/api/admin`, and `/api/admin/` are denied unless explicitly
  approved; unauthenticated API requests are rejected.
- Authenticated managed-route CRUD remains available only through the approved
  owner channel.
- One owner-controlled proxy smoke passes without printing response bodies,
  credentials, or sensitive query values.
- Disabled route fails closed; legacy fallback behaves as designed.
- Access/error/service logs contain no panic, secret, token, cookie, password,
  complete UUID, or complete subscription URL.
- DNS authoritative answers and at least two external vantage points converge
  to the approved target; TTL and rollback record are captured.

## Owner decision points

Owner approval is required before: selecting the hostname/path, deciding admin
exposure, authorizing DNS provider access/apply, choosing BWG-only versus
NOSLA-primary failover, changing an existing Nginx server block, changing
rathole, or accepting a public traffic canary. No such action has been taken.

Owner-approved scope is now: new hostname, new Nginx file only, `/s/` public,
all Admin UI/API paths denied, BWG-only canary, no rathole change, no NOSLA, and
no modification to existing production/staging stream entries. The exact
hostname is still required before the staged server block and DNS record can be
rendered.
