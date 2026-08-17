#!/bin/bash
set -euo pipefail

safe_dns_target() {
    python3 /opt/stream-failover/spaceship_dns.py read | python3 -c '
import json, sys
data=json.load(sys.stdin)
records=[item for item in data.get("project_records", [])
         if item.get("name") == "stream" and item.get("type") == "A"]
if len(records) != 1: raise SystemExit("stream record count invalid")
addresses={"45.143.130.11":"nosla", "144.34.226.187":"bwg"}
target=addresses.get(records[0].get("address"))
if not target: raise SystemExit("stream record outside allowlist")
print(target)
'
}

before=$(safe_dns_target)
test "$(systemctl is-active stream-failover.timer)" = active
test "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)" != active
systemctl start embyproxy-failover-policy.service
after=$(safe_dns_target)
test "$before" = "$after"
test "$(systemctl is-active stream-failover.timer)" = active
test "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)" != active
test "$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
test "$(systemctl is-active nginx)" = active

python3 - <<'PY'
import json
p='/var/lib/embyproxy-gsy-sidecar/failover-state.json'
with open(p, encoding='utf-8') as handle: data=json.load(handle)
required=('active_target','decision_reason','mode','manual_hold',
          'nosla_usage_gb','bwg_usage_gb','nosla_usage_percent',
          'bwg_usage_percent','usage_state')
for key in required:
    if key not in data: raise SystemExit('missing state field: '+key)
assert data['mode'] == 'dry-run'
print('DRY_RUN=PASS')
for key in required:
    print(key.upper()+'='+str(data[key]))
PY
printf 'DNS_UNCHANGED=PASS\n'
printf 'LEGACY_TIMER=active\n'
printf 'NEW_TIMER=inactive_disabled\n'
