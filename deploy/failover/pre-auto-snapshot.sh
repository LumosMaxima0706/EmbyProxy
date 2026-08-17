#!/bin/bash
set -euo pipefail

root=/var/backups/embyproxy-failover-policy/20260812T085807Z/pre-auto
rm -rf -- "$root"
install -d -m 700 "$root"
python3 /opt/stream-failover/spaceship_dns.py read >"$root/dns.json"
cp -a /var/lib/embyproxy-gsy-sidecar/failover-state.json "$root/failover-state.json"
cp -a /etc/embyproxy-failover-policy/policy.env "$root/policy.env"
systemctl is-active embyproxy-failover-policy.timer >"$root/new-timer.active"
systemctl is-enabled embyproxy-failover-policy.timer >"$root/new-timer.enabled"
systemctl is-active stream-failover.timer >"$root/legacy-timer.active" || true
nginx_files=(
    /etc/nginx/conf.d/embyproxy-phase2-staging.conf
    /etc/nginx/conf.d/embyproxy-gsy-canary.conf
    /etc/nginx/conf.d/stream-b-proxy.conf
    /etc/nginx/conf.d/stream-failover-web.conf
    /etc/nginx/conf.d/stream-proxy-admin-locations.inc
    /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
)
for file in "${nginx_files[@]}"; do test -f "$file"; done
sha256sum "${nginx_files[@]}" >"$root/nginx.sha256"
chmod -R go-rwx "$root"
find "$root" -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum \
    >"$root/SHA256SUMS"
cd /
sha256sum -c "$root/SHA256SUMS" >/dev/null
printf 'PRE_AUTO_SNAPSHOT=PASS\n'
