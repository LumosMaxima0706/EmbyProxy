# Owner Self-Use Access Guide

Status: READY FOR OWNER LOCALHOST USE

The deployed sidecar is intentionally not exposed through public Nginx, DNS, or
rathole. Access it through an SSH tunnel to BWG.

## Start a tunnel

Use an unused local port. For example:

```bash
ssh -N -L 28082:127.0.0.1:18082 bwg
```

Keep that terminal open, then open the Admin UI at:

```text
http://127.0.0.1:28082/admin
```

Stop the tunnel with Ctrl-C. The tunnel does not change BWG firewall, Nginx, DNS,
or public traffic.

## Administrator credential

- The administrator token exists only in the mode-0600 BWG environment file at
  `/etc/embyproxy-gsy-sidecar/embyproxy.env`.
- Retrieve it only through an approved interactive owner channel on BWG.
- Do not paste it into chat, shell history, runbook, logs, URLs, or screenshots.
- The first authenticated session may offer 2FA setup; keep recovery handling in an
  owner-controlled channel.

## Normal use

1. Start the SSH tunnel.
2. Open the local Admin UI.
3. Authenticate with the owner-held token and complete 2FA when configured.
4. Use the Managed Routes tab for route CRUD.
5. Keep upstream targets free of embedded credentials and sensitive query strings.
6. Close the browser session and stop the tunnel when finished.

## Read-only operational checks

```bash
ssh bwg 'systemctl is-active embyproxy-gsy-sidecar.service'
ssh bwg 'ss -ltnH "sport = :18082"'
ssh bwg 'curl -fsS -o /dev/null http://127.0.0.1:18082/admin'
```

Do not expose local port 28082 on a non-loopback interface. Do not add a public
Nginx location, DNS record, or rathole mapping as part of self-use access.

For recurring checks and incident handling, use `24-owner-operations-guide.md`,
`25-troubleshooting-guide.md`, and `27-day2-checklist.md`.

Day-2 tunnel validation passed. The owner may use this localhost access method;
public exposure remains intentionally disabled.
