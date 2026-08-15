#!/bin/bash
set -euo pipefail

role=${1:?role must be bwg or nosla}
root=${2:?backup root is required}
case "$role:$root" in
  bwg:/var/backups/embyproxy-global-stats/*|nosla:/var/backups/embyproxy-global-stats/*) ;;
  *) echo 'backup root or role outside authorized scope' >&2; exit 1 ;;
esac
test ! -e "$root"
install -d -o root -g root -m 700 "$root"

copy_if_present() {
  local source=$1 destination=$root$1
  if [ -f "$source" ]; then
    install -d -o root -g root -m 700 "$(dirname "$destination")"
    install -o root -g root -m 600 "$source" "$destination"
  fi
}

if [ "$role" = bwg ]; then
  readlink -f /opt/embyproxy-gsy-sidecar/current >"$root/current-link-target"
  chmod 600 "$root/current-link-target"
  install -o root -g root -m 600 "$(readlink -f /opt/embyproxy-gsy-sidecar/current)/embyproxy" "$root/current-embyproxy"
  copy_if_present /etc/embyproxy-gsy-sidecar/embyproxy.env
  copy_if_present /var/lib/embyproxy-gsy-sidecar/global-stats.db
  for unit in embyproxy-stats-collector-bwg.service embyproxy-stats-collector-bwg.timer embyproxy-stats-sync.service embyproxy-stats-sync.timer; do copy_if_present "/etc/systemd/system/$unit"; done
else
  copy_if_present /usr/local/sbin/embyproxy-traffic-meter
  for unit in embyproxy-stats-collector-nosla.service embyproxy-stats-collector-nosla.timer; do copy_if_present "/etc/systemd/system/$unit"; done
  copy_if_present /var/lib/embyproxy-stats/nosla-snapshot.json
fi

cat >"$root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
if [ "$role" = bwg ]; then
  target=\$(cat '$root/current-link-target')
  test -d "\$target"
  install -o root -g root -m 755 '$root/current-embyproxy' "\$target/embyproxy"
  if [ -f '$root/etc/embyproxy-gsy-sidecar/embyproxy.env' ]; then install -o root -g root -m 600 '$root/etc/embyproxy-gsy-sidecar/embyproxy.env' /etc/embyproxy-gsy-sidecar/embyproxy.env; fi
  for unit in embyproxy-stats-collector-bwg.service embyproxy-stats-collector-bwg.timer embyproxy-stats-sync.service embyproxy-stats-sync.timer; do if [ -f '$root/etc/systemd/system/'\"\$unit\" ]; then install -o root -g root -m 644 '$root/etc/systemd/system/'\"\$unit\" /etc/systemd/system/\"\$unit\"; fi; done
  systemctl daemon-reload
  systemctl restart embyproxy-gsy-sidecar.service
else
  install -o root -g root -m 755 '$root/usr/local/sbin/embyproxy-traffic-meter' /usr/local/sbin/embyproxy-traffic-meter
  for unit in embyproxy-stats-collector-nosla.service embyproxy-stats-collector-nosla.timer; do if [ -f '$root/etc/systemd/system/'\"\$unit\" ]; then install -o root -g root -m 644 '$root/etc/systemd/system/'\"\$unit\" /etc/systemd/system/\"\$unit\"; fi; done
  systemctl daemon-reload
fi
EOF
chmod 700 "$root/rollback.sh"
bash -n "$root/rollback.sh"
printf 'GLOBAL_STATS_BACKUP=PASS\n'
printf 'ROLE=%s\n' "$role"
printf 'ROLLBACK_SCRIPT=%s\n' "$root/rollback.sh"
