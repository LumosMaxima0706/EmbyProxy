#!/bin/bash
set -euo pipefail

backup_root=${1:?backup path is required}
node=${2:?node must be bwg or nosla}
case "$backup_root" in
    /var/backups/embyproxy-publication/*) ;;
    *) echo "invalid backup path" >&2; exit 2 ;;
esac
case "$node" in
    bwg|nosla) ;;
    *) echo "invalid node" >&2; exit 2 ;;
esac

test ! -e "$backup_root"
install -d -o root -g root -m 700 "$backup_root"
install -o root -g root -m 700 /usr/local/sbin/embyproxy-publication-edge "$backup_root/embyproxy-publication-edge"
install -o root -g root -m 600 "/etc/embyproxy-publication-agent/edge-$node.json" "$backup_root/edge.json"

fragment=/etc/nginx/conf.d/embyproxy-publications/feimu.conf
if test -f "$fragment"; then
    install -o root -g root -m 600 "$fragment" "$backup_root/feimu.conf"
else
    : >"$backup_root/feimu-fragment.absent"
    chmod 600 "$backup_root/feimu-fragment.absent"
fi

if test "$node" = bwg; then
    install -o root -g root -m 700 /usr/local/sbin/embyproxy-publication-agent "$backup_root/embyproxy-publication-agent"
fi

cat >"$backup_root/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
backup_root='$backup_root'
node='$node'
fragment=/etc/nginx/conf.d/embyproxy-publications/feimu.conf
install -o root -g root -m 755 "\$backup_root/embyproxy-publication-edge" /usr/local/sbin/embyproxy-publication-edge
if test "\$node" = bwg; then
    install -o root -g root -m 755 "\$backup_root/embyproxy-publication-agent" /usr/local/sbin/embyproxy-publication-agent
    systemctl restart embyproxy-publication-agent.service
    systemctl is-active --quiet embyproxy-publication-agent.service
fi
if test -f "\$backup_root/feimu.conf"; then
    install -o root -g root -m 640 "\$backup_root/feimu.conf" "\$fragment"
else
    rm -f "\$fragment"
fi
nginx -t
systemctl reload nginx.service
EOF
chmod 700 "$backup_root/rollback.sh"
bash -n "$backup_root/rollback.sh"
sha256sum "$backup_root/embyproxy-publication-edge" "$backup_root/edge.json" >"$backup_root/SHA256SUMS"
printf 'BACKUP_READY=%s\n' "$backup_root"
