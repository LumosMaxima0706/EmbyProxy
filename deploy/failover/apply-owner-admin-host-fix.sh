#!/bin/bash
set -eEuo pipefail

staging=${1:-/root/staging/owner-admin-0fc2334.conf}
live=/etc/nginx/conf.d/owner-admin.149077530.xyz.conf
rollback_script=/var/backups/embyproxy-owner-admin-basic-only/20260812T143642Z/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -r "$staging"
test "$(grep -c 'include /etc/nginx/proxy_params' "$staging" || true)" = 0
test "$(grep -c 'proxy_set_header Host owner-admin.149077530.xyz;' "$staging")" = 4
install -o root -g root -m 600 "$staging" "$live"
nginx -t
systemctl reload nginx
test "$(systemctl is-active nginx)" = active
nginx -t

trap - ERR
printf 'OWNER_ADMIN_HOST_FIX_APPLY=PASS\n'
