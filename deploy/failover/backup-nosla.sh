#!/bin/bash
set -euo pipefail

stamp=${1:?backup timestamp is required}
root="/var/backups/embyproxy-failover-policy/$stamp"
install -d -m 700 "$root" "$root/nosla"

copy_if_exists() {
    local source=$1 destination="$root/nosla$1"
    if [ -e "$source" ] || [ -L "$source" ]; then
        install -d -m 700 "$(dirname "$destination")"
        cp -a -- "$source" "$destination"
    fi
}

for path in \
    /etc/nginx/nginx.conf \
    /etc/nginx/conf.d/stream-proxy.conf \
    /etc/nginx/conf.d/stream-proxy-admin-locations.inc \
    /etc/systemd/system/emby-reverse-proxy-go-admin.service \
    /opt/stream-erpgo-nosla; do
    copy_if_exists "$path"
done

systemctl is-active nginx >"$root/nosla/nginx.active" || true
systemctl is-active emby-reverse-proxy-go-admin.service \
    >"$root/nosla/admin-sidecar.active" || true
docker inspect stream-erpgo-nosla >"$root/nosla/container-inspect.json"
ss -lnt >"$root/nosla/listeners.txt"
nginx -T >"$root/nosla/nginx-T.txt" 2>"$root/nosla/nginx-T.stderr"
chmod -R go-rwx "$root"

find "$root" -type f -print0 | sort -z | xargs -0 sha256sum \
    >"$root/SHA256SUMS"

cat >"$root/rollback-nosla.sh" <<'ROLLBACK'
#!/bin/bash
set -euo pipefail

rm -f /usr/local/sbin/embyproxy-traffic-meter
if id embyproxy-meter >/dev/null 2>&1; then
    userdel --remove embyproxy-meter
fi
nginx -t
systemctl is-active --quiet nginx
ROLLBACK
chmod 700 "$root/rollback-nosla.sh"
bash -n "$root/rollback-nosla.sh"
sha256sum "$root/rollback-nosla.sh" >"$root/rollback-nosla.sha256"

printf 'BACKUP_ROOT=%s\n' "$root"
printf 'FILE_COUNT=%s\n' "$(find "$root" -type f | wc -l)"
printf 'CHECKSUM_COUNT=%s\n' "$(wc -l <"$root/SHA256SUMS")"
printf 'ROLLBACK_BASH_N=PASS\n'
