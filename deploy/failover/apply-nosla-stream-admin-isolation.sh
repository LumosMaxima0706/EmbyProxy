#!/bin/bash
set -eEuo pipefail

staging=${1:-/root/staging/nosla-stream-public-locations.inc}
live=/etc/nginx/conf.d/stream-proxy-admin-locations.inc
backup_root=${2:?backup root is required}
rollback_script=$backup_root/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -r "$staging"
test -x "$rollback_script"
bash -n "$rollback_script"
test "$(grep -c 'return 404;' "$staging")" = 4
test "$(grep -c 'location \^~ /s/' "$staging")" = 1
test "$(grep -c 'proxy_cache off;' "$staging")" = 1
install -o root -g root -m 644 "$staging" "$live"
nginx -t
systemctl reload nginx
test "$(systemctl is-active nginx)" = active
nginx -t

trap - ERR
printf 'NOSLA_STREAM_ADMIN_ISOLATION_APPLY=PASS\n'
