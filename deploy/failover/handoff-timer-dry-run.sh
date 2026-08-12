#!/bin/bash
set -euo pipefail

rollback() {
    systemctl disable --now embyproxy-failover-policy.timer 2>/dev/null || true
    systemctl enable --now stream-failover.timer
}
trap 'rollback' ERR

grep -qx 'FAILOVER_MODE=dry-run' /etc/embyproxy-failover-policy/policy.env
test "$(systemctl is-active stream-failover.timer)" = active
test "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)" != active
systemctl disable --now stream-failover.timer
while systemctl is-active --quiet stream-failover-check.service; do sleep 1; done
systemctl enable --now embyproxy-failover-policy.timer
test "$(systemctl is-active stream-failover.timer 2>/dev/null || true)" != active
test "$(systemctl is-enabled stream-failover.timer 2>/dev/null || true)" != enabled
test "$(systemctl is-active embyproxy-failover-policy.timer)" = active
test "$(systemctl is-enabled embyproxy-failover-policy.timer)" = enabled
trap - ERR
printf 'SINGLE_CONTROLLER_HANDOFF=PASS\n'
printf 'POLICY_MODE=dry-run\n'
