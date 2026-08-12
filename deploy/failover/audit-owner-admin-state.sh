#!/bin/bash
set -euo pipefail

temporary=$(mktemp)
trap 'rm -f "$temporary" /tmp/owner-admin-status.json' EXIT
python3 /opt/stream-failover/spaceship_owner_admin.py status >"$temporary"
python3 - "$temporary" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as handle: data=json.load(handle)
print('DNS_EXISTS=' + str(bool(data.get('exists'))).lower())
valid = data.get('address') == '144.34.226.187' and data.get('ttl') == 60
print('DNS_SCHEMA_OK=' + str(valid).lower())
PY

if [ -f /etc/nginx/conf.d/owner-admin.149077530.xyz.conf ]; then
    echo NGINX_FILE=present
else
    echo NGINX_FILE=absent
fi
if [ -r /etc/letsencrypt/live/owner-admin.149077530.xyz/fullchain.pem ] \
        && [ -r /etc/letsencrypt/live/owner-admin.149077530.xyz/privkey.pem ]; then
    echo CERT_FILES=readable
    if openssl x509 -in /etc/letsencrypt/live/owner-admin.149077530.xyz/fullchain.pem \
            -noout -checkend 86400 >/dev/null; then
        echo CERT_VALID_GT_24H=true
    else
        echo CERT_VALID_GT_24H=false
    fi
    openssl x509 -in /etc/letsencrypt/live/owner-admin.149077530.xyz/fullchain.pem \
        -noout -dates
else
    echo CERT_FILES=unavailable
fi
nginx -t
printf 'NGINX=%s\n' "$(systemctl is-active nginx)"
printf 'SIDECAR=%s\n' "$(systemctl is-active embyproxy-gsy-sidecar.service)"
printf 'NEW_TIMER=%s\n' "$(systemctl is-active embyproxy-failover-policy.timer)"
printf 'LEGACY_TIMER=%s\n' "$(systemctl is-active stream-failover.timer 2>/dev/null || true)"
systemctl show embyproxy-gsy-sidecar.service -p NRestarts --no-pager
ss -lntp | grep '127.0.0.1:18082'
