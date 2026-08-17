#!/bin/bash
set -euo pipefail

root=$(dirname "$(readlink -f "$0")")
case "$root" in
  /var/backups/embyproxy-global-stats/*-bwg) role=bwg ;;
  /var/backups/embyproxy-global-stats/*-nosla) role=nosla ;;
  *) echo 'rollback path outside authorized backup root' >&2; exit 1 ;;
esac

restore_if_present() {
  local source=$root$1 destination=$1 mode=$2 owner=$3 group=$4
  if [ -f "$source" ]; then
    install -d -o "$owner" -g "$group" -m 750 "$(dirname "$destination")"
    install -o "$owner" -g "$group" -m "$mode" "$source" "$destination"
  fi
}

if [ "$role" = bwg ]; then
  target=$(cat "$root/current-link-target")
  case "$target" in /opt/embyproxy-gsy-sidecar/releases/*) ;; *) exit 1 ;; esac
  test -d "$target"
  systemctl stop embyproxy-gsy-sidecar.service
  install -o root -g root -m 755 "$root/current-embyproxy" "$target/embyproxy"
  restore_if_present /etc/embyproxy-gsy-sidecar/embyproxy.env 600 root root
  restore_if_present /var/lib/embyproxy-gsy-sidecar/global-stats.db 600 embyproxy-gsy-sidecar embyproxy-gsy-sidecar
  rm -f /var/lib/embyproxy-gsy-sidecar/global-stats.db-wal /var/lib/embyproxy-gsy-sidecar/global-stats.db-shm
  if [ -f "$root/current-stats-collector" ]; then install -o root -g root -m 755 "$root/current-stats-collector" /usr/local/sbin/embyproxy-stats-collector; fi
  if [ -f "$root/current-stats-sync" ]; then install -o root -g root -m 755 "$root/current-stats-sync" /usr/local/sbin/embyproxy-stats-sync; fi
  for unit in embyproxy-stats-collector-bwg.service embyproxy-stats-collector-bwg.timer embyproxy-stats-sync.service embyproxy-stats-sync.timer; do
    restore_if_present "/etc/systemd/system/$unit" 644 root root
  done
  ln -s "$target" /opt/embyproxy-gsy-sidecar/current.rollback
  mv -Tf /opt/embyproxy-gsy-sidecar/current.rollback /opt/embyproxy-gsy-sidecar/current
  systemctl daemon-reload
  systemctl restart embyproxy-gsy-sidecar.service
else
  restore_if_present /usr/local/sbin/embyproxy-traffic-meter 755 root root
  if [ -f "$root/current-stats-collector" ]; then install -o root -g root -m 755 "$root/current-stats-collector" /usr/local/sbin/embyproxy-stats-collector; fi
  for unit in embyproxy-stats-collector-nosla.service embyproxy-stats-collector-nosla.timer; do
    restore_if_present "/etc/systemd/system/$unit" 644 root root
  done
  systemctl daemon-reload
fi

printf 'GLOBAL_STATS_ROLLBACK=PASS\n'
