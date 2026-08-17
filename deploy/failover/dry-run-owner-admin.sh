#!/bin/bash
set -euo pipefail

stage=${1:?staging directory is required}
template=${2:?Nginx template is required}
wrapper=${3:?DNS wrapper is required}

test ! -e /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
test ! -e /etc/nginx/owner-admin.htpasswd
python3 -m py_compile "$wrapper"

status=$(python3 "$wrapper" status)
python3 -c '
import json, sys
data=json.loads(sys.argv[1])
assert data.get("ok") is True
assert data.get("provider_ready") is True
assert data.get("dns_apply_enabled") is True
assert data.get("exists") is False
' "$status"

install -d -m 755 "$stage" "$stage/conf.d" "$stage/cert" "$stage/log" "$stage/acme"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -subj /CN=owner-admin.149077530.xyz \
    -keyout "$stage/cert/privkey.pem" -out "$stage/cert/fullchain.pem" \
    >/dev/null 2>&1
chmod 600 "$stage/cert/privkey.pem" "$stage/cert/fullchain.pem"
printf 'staging:%s\n' "$(openssl passwd -6 staging-only)" >"$stage/htpasswd"
chmod 600 "$stage/htpasswd"
chgrp www-data "$stage/htpasswd"
chmod 640 "$stage/htpasswd"

sed \
    -e "s#/etc/letsencrypt/live/owner-admin.149077530.xyz#$stage/cert#g" \
    -e "s#/etc/nginx/owner-admin.htpasswd#$stage/htpasswd#g" \
    -e "s#/var/log/nginx/owner-admin.access.log#$stage/log/access.log#g" \
    -e "s#/var/log/nginx/owner-admin.error.log#$stage/log/error.log#g" \
    -e "s#root /var/lib/letsencrypt;#root $stage/acme;#g" \
    -e 's/listen 80;/listen 127.0.0.1:18090;/' \
    -e '/listen \[::\]:80;/d' \
    -e 's/listen 443 ssl http2;/listen 127.0.0.1:18443 ssl http2;/' \
    -e '/listen \[::\]:443 ssl http2;/d' \
    "$template" >"$stage/conf.d/owner-admin.conf"

cat >"$stage/nginx.conf" <<EOF
worker_processes 1;
user www-data;
pid $stage/nginx.pid;
error_log $stage/error.log;
events { worker_connections 32; }
http {
    include /etc/nginx/mime.types;
    include /etc/nginx/proxy_params;
    include $stage/conf.d/*.conf;
}
EOF
nginx -t -p "$stage" -c "$stage/nginx.conf"
nginx -p "$stage" -c "$stage/nginx.conf"
cleanup() {
    code=$?
    if [ "$code" -ne 0 ]; then
        printf 'STAGING_FAILURE_LOG\n'
        tail -20 "$stage/error.log" "$stage/log/error.log" 2>/dev/null \
            | sed -E 's/(password|token|authorization|cookie|secret)[^ ,;]*/\1=[REDACTED]/Ig' || true
    fi
    nginx -p "$stage" -c "$stage/nginx.conf" -s quit >/dev/null 2>&1 || true
    exit "$code"
}
trap cleanup EXIT
sleep 1

code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Host: owner-admin.149077530.xyz' \
    http://127.0.0.1:18090/admin)
printf 'HTTP_REDIRECT_CODE=%s\n' "$code"
test "$code" = 308
code=$(curl -k -sS -o /dev/null -w '%{http_code}' \
    --resolve owner-admin.149077530.xyz:18443:127.0.0.1 \
    https://owner-admin.149077530.xyz:18443/admin)
printf 'ADMIN_NO_BASIC_CODE=%s\n' "$code"
test "$code" = 401
code=$(curl -k -sS -o /dev/null -w '%{http_code}' \
    --resolve owner-admin.149077530.xyz:18443:127.0.0.1 \
    https://owner-admin.149077530.xyz:18443/s/v1/)
printf 'MEDIA_NO_BASIC_CODE=%s\n' "$code"
test "$code" = 404

# Basic Auth is server-wide by design; after it succeeds, non-Admin paths fail closed.
code=$(curl -k -sS -o /dev/null -w '%{http_code}' \
    -u staging:staging-only \
    --resolve owner-admin.149077530.xyz:18443:127.0.0.1 \
    https://owner-admin.149077530.xyz:18443/s/v1/)
printf 'MEDIA_WITH_BASIC_CODE=%s\n' "$code"
test "$code" = 404
code=$(curl -k -sS -o /dev/null -w '%{http_code}' \
    -u staging:staging-only \
    --resolve owner-admin.149077530.xyz:18443:127.0.0.1 \
    https://owner-admin.149077530.xyz:18443/admin)
printf 'ADMIN_WITH_BASIC_CODE=%s\n' "$code"
test "$code" = 200
code=$(curl -k -sS -o /dev/null -w '%{http_code}' \
    -u staging:staging-only \
    --resolve owner-admin.149077530.xyz:18443:127.0.0.1 \
    https://owner-admin.149077530.xyz:18443/api/admin/status)
printf 'APP_STATUS_WITH_BASIC_CODE=%s\n' "$code"
test "$code" = 401

printf 'OWNER_ADMIN_DNS_READINESS=PASS\n'
printf 'OWNER_ADMIN_NGINX_STAGING=PASS\n'
printf 'OWNER_ADMIN_OUTER_AUTH=PASS\n'
printf 'OWNER_ADMIN_APP_AUTH=PASS\n'
printf 'OWNER_ADMIN_PATH_ISOLATION=PASS\n'
