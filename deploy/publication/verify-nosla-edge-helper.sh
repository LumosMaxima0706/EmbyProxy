#!/bin/bash
set -euo pipefail

database=/var/lib/embyproxy-gsy-sidecar/proxy.db
identity=/etc/embyproxy-publication-agent/nosla_ed25519
known_hosts=/etc/embyproxy-publication-agent/known_hosts
manifest=$(mktemp)
response=$(mktemp)
trap 'rm -f "$manifest" "$response"' EXIT
chmod 600 "$manifest" "$response"
python3 - "$database" >"$manifest" <<'PY'
import json, sqlite3, sys
db = sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True)
packed = db.execute("SELECT v FROM proxy_kv WHERE k='u:admin:node:feimu'").fetchone()[0]
target = json.loads(packed).get('t', '')
from urllib.parse import urlsplit
parsed = urlsplit(target)
port = int(parsed.port or 443)
host = parsed.hostname.lower()
base = parsed.path.strip('/.')
json.dump({'version': 1, 'action': 'check', 'operation_id': 'readiness-check',
           'slug': 'feimu', 'upstream_host': host, 'upstream_port': port,
           'base_path': base}, sys.stdout)
print()
PY
ssh -F /dev/null -i "$identity" \
    -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes \
    -o BatchMode=yes -o IdentitiesOnly=yes -o ConnectTimeout=10 -T \
    root@45.143.130.11 <"$manifest" >"$response" || true
python3 - "$response" <<'PY'
import json, sys
try:
    value = json.load(open(sys.argv[1]))
except Exception:
    print('NOSLA_EDGE_RESPONSE=invalid')
    raise SystemExit(0)
print('NOSLA_EDGE_RESPONSE=' + str(value.get('status', '')) +
      ' error=' + str(value.get('error_code', '')) +
      ' step=' + str(value.get('failed_step', '')))
PY
