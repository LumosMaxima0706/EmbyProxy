#!/bin/bash
set -euo pipefail

env_file=/etc/embyproxy-failover-policy/policy.env
state_file=/var/lib/embyproxy-gsy-sidecar/failover-state.json
backup=$(mktemp /etc/embyproxy-failover-policy/policy.env.before-auto.XXXXXX)
cp -a "$env_file" "$backup"

rollback() {
    cp -a "$backup" "$env_file"
    systemctl enable --now embyproxy-failover-policy.timer 2>/dev/null || true
}
trap rollback ERR

python3 - "$state_file" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle: data=json.load(handle)
assert data['active_target'] == 'bwg'
assert data['decision_reason'] in ('nosla_recovered_below_return_threshold',
                                   'nosla_healthy_new_cycle')
assert data['nosla_consecutive_successes'] >= 3
assert data['usage_state'] == 'fresh_estimate'
assert data['nosla_usage_percent'] < 85
PY

systemctl disable --now embyproxy-failover-policy.timer
sed -i 's/^FAILOVER_MODE=.*/FAILOVER_MODE=auto/' "$env_file"
grep -qx 'FAILOVER_MODE=auto' "$env_file"
systemctl start embyproxy-failover-policy.service
python3 - "$state_file" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle: data=json.load(handle)
assert data['mode'] == 'auto'
assert data['active_target'] == 'nosla'
PY
systemctl enable --now embyproxy-failover-policy.timer
test "$(systemctl is-active embyproxy-failover-policy.timer)" = active
test "$(systemctl is-active stream-failover.timer 2>/dev/null || true)" != active
rm -f "$backup"
trap - ERR
printf 'POLICY_AUTO_APPLY=PASS\n'
printf 'ACTIVE_TARGET=nosla\n'
