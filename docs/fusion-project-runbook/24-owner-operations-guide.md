# Owner Operations Guide

Status: READY FOR OWNER SELF-USE

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
