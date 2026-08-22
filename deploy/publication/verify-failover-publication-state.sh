#!/bin/bash
set -euo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
password=$(tr -d '\r\n' <"$password_file")
response=$(mktemp)
trap 'rm -f "$response"' EXIT
curl --fail --silent --show-error --http1.1 -u "owner:$password" \
    https://owner-admin.149077530.xyz/api/admin/failover/status >"$response"
python3 - "$response" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle:
    data = json.load(handle)
state = data.get('state') or data
print('policy=' + str(state.get('mode') or data.get('mode') or ''))
print('active_target=' + str(state.get('activeTarget') or state.get('active_target') or ''))
print('manual_hold=' + str(state.get('manualHold') or state.get('manual_hold') or ''))
print('decision_reason=' + str(state.get('decisionReason') or state.get('decision_reason') or ''))
print('state_source=' + str(state.get('state_source') or ''))
PY
