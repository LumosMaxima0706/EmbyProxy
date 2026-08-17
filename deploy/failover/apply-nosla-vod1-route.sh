#!/bin/bash
set -eEuo pipefail

candidate=${1:?candidate is required}
backup_root=${2:?backup root is required}
live=/etc/nginx/conf.d/stream-proxy.conf
rollback_script=$backup_root/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -r "$candidate"
test -x "$rollback_script"
bash -n "$rollback_script"
test "$(grep -c 'location = /https/v1-vod1.uhdnow.com/443 {' "$candidate")" = 1
test "$(grep -c 'location \^~ /https/v1-vod1.uhdnow.com/443/' "$candidate")" = 1
test "$(grep -c 'proxy_cache off;' "$candidate")" = 3
install -o root -g root -m 644 "$candidate" "$live"
nginx -t
systemctl reload nginx
test "$(systemctl is-active nginx)" = active
nginx -t

trap - ERR
printf 'NOSLA_VOD1_APPLY=PASS\n'
