#!/bin/bash
set -euo pipefail

backup_root=${1:?backup root is required}
case "$backup_root" in
    /var/backups/embyproxy-owner-public-url/*) ;;
    *) echo 'backup root is outside the authorized prefix' >&2; exit 1 ;;
esac
test ! -e "$backup_root"
install -d -o root -g root -m 700 "$backup_root/release"
current=$(readlink -f /opt/embyproxy-gsy-sidecar/current)
test -n "$current"
printf '%s\n' "$current" >"$backup_root/current-link-target"
install -o root -g root -m 600 "$current/embyproxy" "$backup_root/release/embyproxy"
install -o root -g root -m 600 /etc/embyproxy-gsy-sidecar/embyproxy.env "$backup_root/embyproxy.env"
install -o root -g root -m 600 /etc/systemd/system/embyproxy-gsy-sidecar.service "$backup_root/embyproxy-gsy-sidecar.service"
install -o root -g root -m 600 /etc/nginx/conf.d/owner-admin.149077530.xyz.conf "$backup_root/owner-admin.nginx.conf"
systemctl is-active embyproxy-gsy-sidecar.service >"$backup_root/sidecar.active"
systemctl is-active embyproxy-failover-policy.timer >"$backup_root/timer.active"

cat >"$backup_root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
target=\$(cat '$backup_root/current-link-target')
test -d "\$target"
install -o root -g root -m 600 '$backup_root/embyproxy.env' /etc/embyproxy-gsy-sidecar/embyproxy.env
install -o root -g root -m 644 '$backup_root/embyproxy-gsy-sidecar.service' /etc/systemd/system/embyproxy-gsy-sidecar.service
install -o root -g root -m 600 '$backup_root/owner-admin.nginx.conf' /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
ln -sfn "\$target" /opt/embyproxy-gsy-sidecar/current
systemctl daemon-reload
nginx -t
systemctl restart embyproxy-gsy-sidecar.service
test "\$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
ss -lntH 'sport = :18082' | grep -q '127.0.0.1:18082'
EOF
chmod 700 "$backup_root/rollback.sh"
(
    cd "$backup_root"
    sha256sum current-link-target release/embyproxy embyproxy.env \
        embyproxy-gsy-sidecar.service owner-admin.nginx.conf sidecar.active \
        timer.active rollback.sh >SHA256SUMS
    sha256sum -c SHA256SUMS
)
bash -n "$backup_root/rollback.sh"
printf 'OWNER_PUBLIC_URL_BACKUP=PASS\n'
printf 'BACKUP_ROOT=%s\n' "$backup_root"
printf 'ROLLBACK_SCRIPT=%s\n' "$backup_root/rollback.sh"
