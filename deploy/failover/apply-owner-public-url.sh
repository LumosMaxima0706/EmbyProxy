#!/bin/bash
set -eEuo pipefail

artifact=${1:?artifact is required}
release=${2:?release name is required}
backup_root=${3:?backup root is required}
release_dir=/opt/embyproxy-gsy-sidecar/releases/$release
env_file=/etc/embyproxy-gsy-sidecar/embyproxy.env
rollback_script=$backup_root/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -x "$artifact"
test -x "$rollback_script"
bash -n "$rollback_script"
version_output=$($artifact version)
case "$version_output" in
    *"($release, built "*) ;;
    *) echo 'artifact commit mismatch' >&2; exit 1 ;;
esac
unset version_output

install -d -o root -g root -m 755 "$release_dir"
install -o root -g root -m 755 "$artifact" "$release_dir/embyproxy"
sed -i '/^PUBLIC_MEDIA_BASE_URL=/d;/^PUBLIC_MEDIA_NODE_PATHS_JSON=/d' "$env_file"
printf '%s\n' \
    'PUBLIC_MEDIA_BASE_URL=https://stream.149077530.xyz' \
    "PUBLIC_MEDIA_NODE_PATHS_JSON='{\"uhd\":\"/https/v1.uhdnow.com/443\"}'" >>"$env_file"
chown root:root "$env_file"
chmod 600 "$env_file"
ln -sfn "$release_dir" /opt/embyproxy-gsy-sidecar/current
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
printf 'OWNER_PUBLIC_URL_APPLY=PASS\n'
printf 'DEPLOYED_RELEASE=%s\n' "$release"
