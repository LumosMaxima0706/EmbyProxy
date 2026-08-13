#!/bin/bash
set -euo pipefail

backup_root=${1:?backup root is required}
live=/etc/nginx/conf.d/stream-proxy.conf

case "$backup_root" in
    /var/backups/embyproxy-playback-vod1/*) ;;
    *) echo 'backup root is outside the authorized prefix' >&2; exit 1 ;;
esac
test ! -e "$backup_root"
install -d -o root -g root -m 700 "$backup_root"
install -o root -g root -m 600 "$live" "$backup_root/stream-proxy.conf"
systemctl is-active nginx >"$backup_root/nginx.active"
systemctl is-enabled nginx >"$backup_root/nginx.enabled"
nginx -T >"$backup_root/nginx-T.txt" 2>"$backup_root/nginx-T.stderr"

cat >"$backup_root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
install -o root -g root -m 644 \
    '$backup_root/stream-proxy.conf' \
    /etc/nginx/conf.d/stream-proxy.conf
nginx -t
systemctl reload nginx
test "\$(systemctl is-active nginx)" = active
nginx -t
EOF
chmod 700 "$backup_root/rollback.sh"
(
    cd "$backup_root"
    sha256sum stream-proxy.conf nginx.active nginx.enabled nginx-T.txt \
        nginx-T.stderr rollback.sh >SHA256SUMS
    sha256sum -c SHA256SUMS
)
bash -n "$backup_root/rollback.sh"
printf 'NOSLA_VOD1_BACKUP=PASS\n'
printf 'BACKUP_ROOT=%s\n' "$backup_root"
printf 'ROLLBACK_SCRIPT=%s\n' "$backup_root/rollback.sh"
