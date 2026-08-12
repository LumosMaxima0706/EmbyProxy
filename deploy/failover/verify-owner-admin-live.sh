#!/bin/bash
set -euo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
test "$(stat -c %a "$password_file")" = 600
password=$(tr -d '\r\n' <"$password_file")
test -n "$password"

admin_code=$(curl -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/admin)
api_code=$(curl -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/api/admin/status)
media_code=$(curl -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/s/v1/)
unset password

test "$admin_code" = 200
test "$api_code" = 401
test "$media_code" = 404
printf 'BASIC_ADMIN_CODE=%s\n' "$admin_code"
printf 'APP_AUTH_REQUIRED_CODE=%s\n' "$api_code"
printf 'ADMIN_MEDIA_BLOCK_CODE=%s\n' "$media_code"

if grep -Eiq '(authorization|cookie|token|password|private.?key|[?&][^ ]+=)' \
        /var/log/nginx/owner-admin.access.log; then
    echo 'owner Admin access log redaction failed' >&2
    exit 1
fi
if journalctl -u embyproxy-gsy-sidecar.service --since '-20 min' --no-pager \
        | grep -Eiq '(panic|fatal|private.?key|authorization:|cookie:|password=|token=)'; then
    echo 'sidecar severe/secret marker scan failed' >&2
    exit 1
fi
printf 'OWNER_ADMIN_LOG_REDACTION=PASS\n'
