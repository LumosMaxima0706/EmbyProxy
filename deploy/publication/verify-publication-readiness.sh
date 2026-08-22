#!/bin/bash
set -euo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
test "$(stat -c %a "$password_file")" = 600
password=$(tr -d '\r\n' <"$password_file")
response=$(mktemp)
trap 'rm -f "$response"' EXIT
status=$(curl --http1.1 -sS -u "owner:$password" -o "$response" -w '%{http_code}' \
    -X POST https://owner-admin.149077530.xyz/api/admin/emby-servers/feimu/publish/dry-run)
python3 - "$status" "$response" <<'PY'
import json, sys
code, filename = sys.argv[1:]
with open(filename, encoding='utf-8') as handle:
    data = json.load(handle)
print('HTTP=' + code)
print('status=' + str(data.get('status', '')) + ' adapter_ready=' + str(data.get('adapter_ready', False)).lower())
readiness = data.get('readiness') or {}
for edge in ('nosla', 'bwg'):
    result = readiness.get(edge) or {}
    print(edge + '_status=' + str(result.get('status', '')) +
          ' reason=' + str(result.get('error_code') or result.get('reason') or ''))
plan = data.get('plan') or {}
print('route_slug=' + str(plan.get('route_slug', '')) +
      ' public_path_shape=' + str(plan.get('public_path', '')))
PY
