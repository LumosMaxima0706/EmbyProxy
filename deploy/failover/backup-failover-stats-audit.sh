#!/bin/bash
set -euo pipefail

role=${1:?role must be bwg or nosla}
root=${2:?backup root is required}
case "$role:$root" in
  bwg:/var/backups/embyproxy-failover-stats-audit/*|nosla:/var/backups/embyproxy-failover-stats-audit/*) ;;
  *) echo 'backup root or role outside authorized scope' >&2; exit 1 ;;
esac
test ! -e "$root"
install -d -o root -g root -m 700 "$root"

copy_file() {
  local source=$1 destination=$root$1
  test -f "$source"
  install -d -o root -g root -m 700 "$(dirname "$destination")"
  install -o root -g root -m 600 "$source" "$destination"
}

if [ "$role" = bwg ]; then
  current=$(readlink -f /opt/embyproxy-gsy-sidecar/current)
  printf '%s\n' "$current" >"$root/current-link-target"
  chmod 600 "$root/current-link-target"
  install -d -o root -g root -m 700 "$root/release"
  install -o root -g root -m 600 "$current/embyproxy" "$root/release/embyproxy"
  copy_file /etc/embyproxy-gsy-sidecar/embyproxy.env
  copy_file /etc/systemd/system/embyproxy-gsy-sidecar.service
  copy_file /etc/systemd/system/embyproxy-failover-policy.service
  copy_file /etc/systemd/system/embyproxy-failover-policy.timer
  copy_file /etc/embyproxy-failover-policy/policy.env
  copy_file /etc/embyproxy-failover-policy/config.json
  copy_file /var/lib/embyproxy-gsy-sidecar/failover-state.json
  copy_file /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
  systemctl is-active embyproxy-gsy-sidecar.service >"$root/sidecar.active"
  systemctl is-active embyproxy-failover-policy.timer >"$root/policy.timer.active"
  systemctl is-active stream-failover.timer >"$root/legacy.timer.active" || true
  cat >"$root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
target=\$(cat '$root/current-link-target')
test -d "\$target"
install -o root -g root -m 600 '$root/etc/embyproxy-gsy-sidecar/embyproxy.env' /etc/embyproxy-gsy-sidecar/embyproxy.env
install -o root -g root -m 644 '$root/etc/systemd/system/embyproxy-gsy-sidecar.service' /etc/systemd/system/embyproxy-gsy-sidecar.service
install -o root -g root -m 644 '$root/etc/systemd/system/embyproxy-failover-policy.service' /etc/systemd/system/embyproxy-failover-policy.service
install -o root -g root -m 644 '$root/etc/systemd/system/embyproxy-failover-policy.timer' /etc/systemd/system/embyproxy-failover-policy.timer
install -o root -g root -m 600 '$root/etc/embyproxy-failover-policy/policy.env' /etc/embyproxy-failover-policy/policy.env
install -o root -g root -m 600 '$root/etc/embyproxy-failover-policy/config.json' /etc/embyproxy-failover-policy/config.json
install -o root -g root -m 600 '$root/var/lib/embyproxy-gsy-sidecar/failover-state.json' /var/lib/embyproxy-gsy-sidecar/failover-state.json
install -o root -g root -m 600 '$root/etc/nginx/conf.d/owner-admin.149077530.xyz.conf' /etc/nginx/conf.d/owner-admin.149077530.xyz.conf
ln -sfn "\$target" /opt/embyproxy-gsy-sidecar/current
systemctl daemon-reload
nginx -t
systemctl restart embyproxy-gsy-sidecar.service
test "\$(systemctl is-active embyproxy-gsy-sidecar.service)" = active
EOF
else
  copy_file /etc/nginx/conf.d/stream-proxy.conf
  copy_file /etc/nginx/conf.d/stream-proxy-admin-locations.inc
  copy_file /etc/systemd/system/emby-reverse-proxy-go-admin.service
  copy_file /etc/emby-reverse-proxy-go/admin.env
  install -d -o root -g root -m 700 "$root/opt/emby-reverse-proxy-go-admin"
  install -o root -g root -m 600 /opt/emby-reverse-proxy-go-admin/emby-admin-sidecar "$root/opt/emby-reverse-proxy-go-admin/emby-admin-sidecar"
  systemctl is-active nginx >"$root/nginx.active"
  systemctl is-active emby-reverse-proxy-go-admin.service >"$root/sidecar.active"
  cat >"$root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
install -o root -g root -m 644 '$root/etc/nginx/conf.d/stream-proxy.conf' /etc/nginx/conf.d/stream-proxy.conf
install -o root -g root -m 644 '$root/etc/nginx/conf.d/stream-proxy-admin-locations.inc' /etc/nginx/conf.d/stream-proxy-admin-locations.inc
install -o root -g root -m 644 '$root/etc/systemd/system/emby-reverse-proxy-go-admin.service' /etc/systemd/system/emby-reverse-proxy-go-admin.service
install -o root -g root -m 600 '$root/etc/emby-reverse-proxy-go/admin.env' /etc/emby-reverse-proxy-go/admin.env
install -o root -g root -m 755 '$root/opt/emby-reverse-proxy-go-admin/emby-admin-sidecar' /opt/emby-reverse-proxy-go-admin/emby-admin-sidecar
systemctl daemon-reload
nginx -t
systemctl reload nginx
systemctl restart emby-reverse-proxy-go-admin.service
test "\$(systemctl is-active nginx)" = active
test "\$(systemctl is-active emby-reverse-proxy-go-admin.service)" = active
EOF
fi

chmod 700 "$root/rollback.sh"
(
  cd "$root"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum >SHA256SUMS
  sha256sum -c SHA256SUMS >/dev/null
)
bash -n "$root/rollback.sh"
printf 'FAILOVER_STATS_AUDIT_BACKUP=PASS\n'
printf 'ROLE=%s\n' "$role"
printf 'BACKUP_ROOT=%s\n' "$root"
printf 'ROLLBACK_SCRIPT=%s\n' "$root/rollback.sh"
