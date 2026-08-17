#!/bin/bash
set -euo pipefail

stage=${1:?staging directory is required}

test "$(systemctl is-active stream-failover.timer)" = active
test "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)" != active
test -f /etc/embyproxy-failover-policy/nosla-meter
test -f /etc/embyproxy-failover-policy/known_hosts

install -o root -g root -m 755 "$stage/embyproxy_failover_policy.py" \
    /usr/local/sbin/embyproxy-failover-policy
install -o root -g root -m 755 "$stage/spaceship_owner_admin.py" \
    /opt/stream-failover/spaceship_owner_admin.py
install -o root -g root -m 600 "$stage/config.example.json" \
    /etc/embyproxy-failover-policy/config.json
install -o root -g root -m 600 "$stage/policy.env.example" \
    /etc/embyproxy-failover-policy/policy.env
install -o root -g root -m 644 "$stage/embyproxy-failover-policy.service" \
    /etc/systemd/system/embyproxy-failover-policy.service
install -o root -g root -m 644 "$stage/embyproxy-failover-policy.timer" \
    /etc/systemd/system/embyproxy-failover-policy.timer

python3 -m py_compile /usr/local/sbin/embyproxy-failover-policy \
    /opt/stream-failover/spaceship_owner_admin.py
python3 -m json.tool /etc/embyproxy-failover-policy/config.json >/dev/null
grep -qx 'FAILOVER_MODE=dry-run' /etc/embyproxy-failover-policy/policy.env
systemd-analyze verify /etc/systemd/system/embyproxy-failover-policy.service \
    /etc/systemd/system/embyproxy-failover-policy.timer
systemctl daemon-reload
test "$(systemctl is-enabled embyproxy-failover-policy.timer 2>/dev/null || true)" != enabled
test "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)" != active
test "$(systemctl is-active stream-failover.timer)" = active

printf 'BWG_STAGE_INSTALL=PASS\n'
printf 'NEW_TIMER=inactive_disabled\n'
printf 'LEGACY_TIMER=active\n'
