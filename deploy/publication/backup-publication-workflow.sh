#!/bin/bash
set -euo pipefail

backup_root=${1:?backup path is required}
case "$backup_root" in
    /var/backups/embyproxy-publication/*) ;;
    *) echo "backup path must be below /var/backups/embyproxy-publication" >&2; exit 2 ;;
esac
test ! -e "$backup_root"
install -d -o root -g root -m 700 "$backup_root"

current_link=/opt/embyproxy-gsy-sidecar/current
env_file=/etc/embyproxy-gsy-sidecar/embyproxy.env
unit_file=/etc/systemd/system/embyproxy-gsy-sidecar.service
db_file=/var/lib/embyproxy-gsy-sidecar/proxy.db

current_target=$(readlink -f "$current_link")
test -n "$current_target"
printf '%s\n' "$current_target" >"$backup_root/current-target"
chmod 600 "$backup_root/current-target"

install -o root -g root -m 600 "$env_file" "$backup_root/embyproxy.env"
install -o root -g root -m 600 "$unit_file" "$backup_root/embyproxy-gsy-sidecar.service"
cp -a /etc/nginx/conf.d "$backup_root/nginx-conf.d"
install -o root -g root -m 700 "$current_target/embyproxy" "$backup_root/embyproxy"

python3 - "$db_file" "$backup_root/proxy.db" <<'PY'
import sqlite3
import sys

source = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
destination = sqlite3.connect(sys.argv[2])
with destination:
    source.backup(destination)
destination.close()
source.close()
PY
chmod 600 "$backup_root/proxy.db"

systemctl show embyproxy-gsy-sidecar.service \
    -p ActiveState -p SubState -p MainPID -p NRestarts >"$backup_root/service-state.txt"
sha256sum "$backup_root/embyproxy" "$backup_root/proxy.db" \
    "$backup_root/embyproxy.env" "$backup_root/embyproxy-gsy-sidecar.service" \
    >"$backup_root/SHA256SUMS"

cat >"$backup_root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
backup_root='$backup_root'
old_target=\$(cat "\$backup_root/current-target")
test -x "\$old_target/embyproxy"
if [ "\${1:-}" = "--restore-db" ]; then
    systemctl stop embyproxy-gsy-sidecar.service
    install -o embyproxy-gsy-sidecar -g embyproxy-gsy-sidecar -m 600 \
        "\$backup_root/proxy.db" /var/lib/embyproxy-gsy-sidecar/proxy.db
fi
ln -sfn "\$old_target" /opt/embyproxy-gsy-sidecar/current
systemctl restart embyproxy-gsy-sidecar.service
systemctl is-active --quiet embyproxy-gsy-sidecar.service
nginx -t
EOF
chmod 700 "$backup_root/rollback.sh"
bash -n "$backup_root/rollback.sh"
printf 'BACKUP_READY=%s\n' "$backup_root"
