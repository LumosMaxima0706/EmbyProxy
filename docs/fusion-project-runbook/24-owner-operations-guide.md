# Owner Operations Guide

Status: VERIFIED - READY FOR OWNER SELF-USE

The sidecar is available only on BWG loopback at `127.0.0.1:18082`. It is not
published through DNS, public traffic, an existing Nginx server block, or a
rathole mapping.

## SSH tunnel and UI

```bash
ssh -N -L 28082:127.0.0.1:18082 bwg
```

Keep the tunnel open and visit `http://127.0.0.1:28082/admin`. Stop it with
Ctrl-C. Keep the local forward loopback-only.

## Read-only service checks

```bash
ssh bwg 'systemctl is-active embyproxy-gsy-sidecar.service'
ssh bwg 'systemctl is-enabled embyproxy-gsy-sidecar.service'
ssh bwg 'systemctl show embyproxy-gsy-sidecar.service -p ActiveState -p SubState -p ExecMainStatus -p NRestarts'
ssh bwg 'ss -ltnH | grep "127.0.0.1:18082"'
ssh bwg 'journalctl -u embyproxy-gsy-sidecar.service -n 80 --no-pager'
```

Expected state is active/enabled, status zero, no unexpected restarts, and a
loopback-only listener.

## Managed-route basics

Authenticate in the Admin UI with the owner-held credential through the approved
interactive channel. Use the Managed Routes tab to list, add, edit, save,
refresh, and delete routes. Exercise only owner-controlled upstreams, remove
temporary test routes, and keep credentials and sensitive query values out of
targets.

Credentials must never be written to this guide, URLs, shell history, logs, or
screenshots.

## Public exposure boundary

Confirm the listener remains loopback-only. Do not alter Nginx, DNS, rathole, or
firewall configuration for self-use; public cutover is a separate approved gate.

Day-2 validation confirmed this access path, service state, listener boundary,
health smoke, and rollback readiness.

## Public cutover gate

Public cutover is planned but blocked. The owner must select the public
hostname/path, decide whether Admin UI/API remain private (recommended),
authorize DNS provider operations, and choose BWG-only canary versus
NOSLA-primary failover. Existing Nginx/rathole topology remains unchanged until
those decisions are recorded in `28-29`.

The owner has approved a new BWG-only canary exposing `/s/` only. Admin UI/API
remain private through the documented SSH tunnel. Existing production and
staging stream entries remain untouched. Exact canary hostname is pending.

The canary is live at `canary.149077530.xyz`. Public users may access only
managed media routes below `/s/`; all Admin UI/API and unmatched paths return
404. Continue using the SSH tunnel for administration. The canary does not
change production traffic or the later NOSLA/BWG failover policy.
