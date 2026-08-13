#!/bin/bash
set -eEuo pipefail

backup_root=${1:?backup root is required}
env_file=/etc/embyproxy-gsy-sidecar/embyproxy.env
rollback_script=$backup_root/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -x "$rollback_script"
bash -n "$rollback_script"
sed -i '/^PUBLIC_MEDIA_NODE_PATHS_JSON=/d' "$env_file"
printf '%s\n' \
    "PUBLIC_MEDIA_NODE_PATHS_JSON='{\"uhd\":\"/https/v1.uhdnow.com/443\"}'" >>"$env_file"
chown root:root "$env_file"
chmod 600 "$env_file"
systemctl restart embyproxy-gsy-sidecar.service
test "$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
test "$(systemctl show embyproxy-gsy-sidecar.service -p NRestarts --value)" = 0
ready=false
for _ in $(seq 1 30); do
    if ss -lntH 'sport = :18082' | grep -q '127.0.0.1:18082'; then
        ready=true
        break
    fi
    sleep 0.5
done
test "$ready" = true
if ss -lntH 'sport = :18082' | grep -Eq '(^|[[:space:]])(0\.0\.0\.0|\[::\]|\*):18082'; then
    echo 'sidecar listener escaped loopback' >&2
    exit 1
fi

trap - ERR
printf 'OWNER_PUBLIC_URL_PATH_APPLY=PASS\n'
