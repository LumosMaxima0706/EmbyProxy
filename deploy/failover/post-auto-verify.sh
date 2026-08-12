#!/bin/bash
set -euo pipefail

state=/var/lib/embyproxy-gsy-sidecar/failover-state.json
snapshot=/var/backups/embyproxy-failover-policy/20260812T085807Z/pre-auto

python3 - "$state" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle: data=json.load(handle)
assert data['mode'] == 'auto'
assert data['active_target'] == 'nosla'
assert data['previous_target'] == 'bwg'
assert data['usage_state'] == 'fresh_estimate'
assert data['nosla_usage_percent'] < 85
assert data['decision_reason'] in ('nosla_recovered_below_return_threshold',
                                   'nosla_healthy_new_cycle')
history=data.get('switch_history', [])
assert history and history[-1]['target'] == 'nosla'
assert history[-1]['result'] == 'verified'
print('POLICY_STATE=PASS')
for key in ('active_target','decision_reason','nosla_usage_gb',
            'nosla_usage_percent','bwg_usage_gb','bwg_usage_percent'):
    print(key.upper()+'='+str(data[key]))
PY

test "$(systemctl is-active embyproxy-failover-policy.timer)" = active
test "$(systemctl is-enabled embyproxy-failover-policy.timer)" = enabled
test "$(systemctl show embyproxy-failover-policy.timer \
    -p NextElapseUSecMonotonic --value)" != infinity
test "$(systemctl is-active stream-failover.timer 2>/dev/null || true)" != active
test "$(systemctl is-enabled stream-failover.timer 2>/dev/null || true)" != enabled
test "$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
test "$(systemctl show embyproxy-gsy-sidecar.service -p NRestarts --value)" = 0
ss -lntp | grep -q '127.0.0.1:18082'
nginx -t
sha256sum -c "$snapshot/nginx.sha256" >/dev/null

python3 /opt/stream-failover/spaceship_dns.py read | python3 -c '
import json, sys
data=json.load(sys.stdin)
records=[item for item in data.get("project_records", [])
         if item.get("name") == "stream" and item.get("type") == "A"]
assert len(records) == 1 and records[0].get("address") == "45.143.130.11"
assert int(records[0].get("ttl") or 0) == 60
print("PRODUCTION_DNS=nosla")
'

if nginx -T 2>/dev/null | grep -Eq '^[[:space:]]*(proxy_cache_path|slice|background_update)'; then
    echo 'forbidden Nginx cache directive found' >&2
    exit 1
fi
if ! nginx -T 2>/dev/null | grep -q 'proxy_cache off'; then
    echo 'explicit proxy_cache off missing' >&2
    exit 1
fi
if journalctl -u embyproxy-failover-policy.service \
        -u embyproxy-gsy-sidecar.service --since '-30 min' --no-pager \
        | grep -Eiq '(panic|fatal|private.?key|authorization:|cookie:|password=|token=)'; then
    echo 'severe/secret marker found in bounded journal' >&2
    exit 1
fi
printf 'SINGLE_CONTROLLER=PASS\n'
printf 'SIDECAR_LOOPBACK=PASS\n'
printf 'NGINX_HASHES_UNCHANGED=PASS\n'
printf 'NO_CACHE_BWG=PASS\n'
printf 'LOG_REDACTION_BWG=PASS\n'
