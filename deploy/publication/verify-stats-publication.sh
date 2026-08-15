#!/bin/bash
set -euo pipefail

password=$(tr -d '\r\n' </etc/embyproxy-failover-policy/owner-admin-password)
response=$(mktemp)
trap 'rm -f "$response"' EXIT
curl --fail --silent --show-error --http1.1 -u "owner:$password" \
    -H 'Content-Type: application/json' --data '{"action":"stats.get","days":7}' \
    https://owner-admin.149077530.xyz/admin/api >"$response"
python3 - "$response" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle:
    data = json.load(handle)
print('stats_ok=' + str(data.get('ok', False)).lower())
print('stats_source=' + str(data.get('stats_source', '')))
print('stats_available=' + str(data.get('stats_available', False)).lower())
recent = data.get('recent') or data.get('recentActivity') or []
print('recent_count=' + str(len(recent)))
PY
