#!/bin/bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

restore() {
    local source="$root/bwg$1" destination=$1
    if [ -e "$source" ] || [ -L "$source" ]; then
        rm -rf -- "$destination"
        install -d -m 755 "$(dirname "$destination")"
        cp -a -- "$source" "$destination"
    fi
}

# Remove only the new Admin ingress while its exact-allowlist adapter exists.
if [ -x /opt/stream-failover/spaceship_owner_admin.py ]; then
    python3 /opt/stream-failover/spaceship_owner_admin.py delete
fi
rm -f /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
if command -v certbot >/dev/null 2>&1 \
        && [ -d /etc/letsencrypt/live/owner-admin.149077530.xyz ]; then
    certbot delete --non-interactive --cert-name owner-admin.149077530.xyz
fi
rm -f /etc/nginx/owner-admin.htpasswd

# Stop the new controller before restoring the legacy one.
systemctl disable --now embyproxy-failover-policy.timer 2>/dev/null || true
rm -f /etc/systemd/system/embyproxy-failover-policy.timer
rm -f /etc/systemd/system/embyproxy-failover-policy.service
rm -f /usr/local/sbin/embyproxy-failover-policy
rm -f /opt/stream-failover/spaceship_owner_admin.py
rm -rf /etc/embyproxy-failover-policy
rm -f /var/lib/embyproxy-gsy-sidecar/failover-state.json

restore /etc/systemd/system/stream-failover.timer
restore /etc/systemd/system/stream-failover-check.service
restore /opt/stream-failover/failover.py
restore /opt/stream-failover/spaceship_dns.py
restore /etc/stream-failover
restore /var/lib/stream-failover

systemctl daemon-reload
systemctl enable --now stream-failover.timer
nginx -t
systemctl reload nginx

# Restore the exact pre-stage production A record through the restricted adapter.
python3 - "$root/bwg/dns-before.json" <<'PY'
import json
import subprocess
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    data = json.load(handle)
records = [item for item in data.get("project_records", [])
           if item.get("name") == "stream" and item.get("type") == "A"]
if len(records) != 1:
    raise SystemExit("saved stream record invalid")
address = records[0].get("address")
if address not in ("45.143.130.11", "144.34.226.187"):
    raise SystemExit("saved stream address outside allowlist")
subprocess.run(["python3", "/opt/stream-failover/spaceship_dns.py",
                "apply-stream", "--ip", address], check=True)
PY
