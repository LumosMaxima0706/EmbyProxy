#!/bin/bash
set -euo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
test "$(stat -c %a "$password_file")" = 600
password=$(tr -d '\r\n' <"$password_file")
test -n "$password"

temporary_html=$(mktemp)
chmod 600 "$temporary_html"
trap 'rm -f "$temporary_html"' EXIT

unauthenticated_code=$(curl --http1.1 -sS -o /dev/null -w '%{http_code}' \
    --max-time 15 https://owner-admin.149077530.xyz/admin)
printf 'UNAUTHENTICATED_ADMIN_CODE=%s\n' "$unauthenticated_code"

admin_code=$(curl --http1.1 -sS -o "$temporary_html" -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/admin)
printf 'BASIC_ADMIN_CODE=%s\n' "$admin_code"
grep -Fq 'data-owner-admin-auth="basic_only"' "$temporary_html"
printf 'BASIC_ONLY_BODY_MARKER=PASS\n'
grep -Fq "const OWNER_ADMIN_BASIC_ONLY = document.body.dataset.ownerAdminAuth === 'basic_only';" "$temporary_html"
printf 'BASIC_ONLY_SCRIPT_MARKER=PASS\n'

status_result=$(curl --http1.1 -sS \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/admin/auth/status)
printf '%s' "$status_result" | grep -Fq '"authMethod":"basic_proxy"'
printf 'BASIC_PROXY_AUTH_STATUS=PASS\n'
unset status_result

api_code=$(curl --http1.1 -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/api/admin/managed-routes)
token_login_code=$(curl --http1.1 -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    -H 'Content-Type: application/json' -d '{}' \
    https://owner-admin.149077530.xyz/admin/auth/login)
media_code=$(curl --http1.1 -sS -o /dev/null -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/s/v1/)
unset password

printf 'BASIC_ONLY_ADMIN_API_CODE=%s\n' "$api_code"
printf 'TOKEN_LOGIN_DISABLED_CODE=%s\n' "$token_login_code"
printf 'ADMIN_MEDIA_BLOCK_CODE=%s\n' "$media_code"

test "$unauthenticated_code" = 401
test "$admin_code" = 200
test "$api_code" = 200
test "$token_login_code" = 404
test "$media_code" = 404
printf 'BASIC_ONLY_UI_MARKER=PASS\n'

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
