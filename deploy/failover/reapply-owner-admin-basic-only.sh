#!/bin/bash
set -euo pipefail

artifact=${1:-/root/staging/embyproxy-owner-auth-0fc2334.bin}
nginx_staging=${2:-/root/staging/owner-admin-0fc2334.conf}
release_dir=/opt/embyproxy-gsy-sidecar/releases/0fc2334
current_link=/opt/embyproxy-gsy-sidecar/current
env_file=/etc/embyproxy-gsy-sidecar/embyproxy.env
nginx_file=/etc/nginx/conf.d/owner-admin.149077530.xyz.conf
rollback_script=/var/backups/embyproxy-owner-admin-basic-only/20260812T143642Z/rollback.sh

rollback() {
    "$rollback_script"
}
trap rollback ERR

test -x "$artifact"
test -r "$nginx_staging"
bash -n "$rollback_script"
test "$(stat -c %a "$env_file")" = 600
test "$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
test "$(systemctl is-active nginx)" = active
test "$(systemctl is-active embyproxy-failover-policy.timer)" = active
test "$(systemctl is-active stream-failover.timer 2>/dev/null || true)" = inactive

install -d -o root -g root -m 755 "$release_dir"
install -o root -g root -m 755 "$artifact" "$release_dir/embyproxy"
version_output=$($release_dir/embyproxy version)
case "$version_output" in
    'EmbyProxy sidecar-0fc2334 (0fc2334, built '*) ;;
    *) echo 'staged artifact version mismatch' >&2; exit 1 ;;
esac
unset version_output

sed -i '/^OWNER_ADMIN_AUTH_MODE=/d;/^OWNER_ADMIN_HOST=/d' "$env_file"
printf '%s\n' \
    'OWNER_ADMIN_AUTH_MODE=basic_only' \
    'OWNER_ADMIN_HOST=owner-admin.149077530.xyz' >>"$env_file"
chown root:root "$env_file"
chmod 600 "$env_file"

install -o root -g root -m 600 "$nginx_staging" "$nginx_file"
nginx -t
ln -sfn "$release_dir" "$current_link"
systemctl restart embyproxy-gsy-sidecar.service
test "$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
test "$(systemctl show embyproxy-gsy-sidecar.service -p NRestarts --value)" = 0
listener_ready=false
for _ in $(seq 1 30); do
    if ss -lntH 'sport = :18082' | grep -q '127.0.0.1:18082'; then
        listener_ready=true
        break
    fi
    sleep 0.5
done
test "$listener_ready" = true
unset listener_ready
if ss -lntH 'sport = :18082' | grep -Eq '(^|[[:space:]])(0\.0\.0\.0|\[::\]|\*):18082'; then
    echo 'sidecar listener escaped loopback' >&2
    exit 1
fi
systemctl reload nginx
test "$(systemctl is-active nginx)" = active
nginx -t

trap - ERR
printf 'OWNER_ADMIN_BASIC_ONLY_APPLY=PASS\n'
printf 'DEPLOYED_RELEASE=0fc2334\n'
