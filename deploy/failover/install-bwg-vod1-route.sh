#!/bin/bash
set -eEuo pipefail

mode=${1:-dry-run}
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
template=$script_dir/bwg-vod1-locations.inc
include_dir=/etc/nginx/conf.d/embyproxy-publications
target=$include_dir/uhd-vod1-redirect.conf
backup_prefix=/var/backups/embyproxy-legacy-uhd-bwg

case "$mode" in
    dry-run|apply) ;;
    rollback)
        backup=${2:?rollback requires an operation backup directory}
        case "$backup" in
            "$backup_prefix"/*) ;;
            *) echo 'rollback path is outside the authorized prefix' >&2; exit 2 ;;
        esac
        test -x "$backup/rollback.sh"
        exec "$backup/rollback.sh"
        ;;
    *) echo 'usage: install-bwg-vod1-route.sh [dry-run|apply|rollback BACKUP_DIR]' >&2; exit 2 ;;
esac

test "$(id -u)" = 0
test -r "$template"
test "$(grep -Fc 'location = /https/v1-vod1.uhdnow.com/443 {' "$template")" = 1
test "$(grep -Fc 'location ^~ /https/v1-vod1.uhdnow.com/443/ {' "$template")" = 1
test "$(grep -Fc 'proxy_set_header Connection $stream_bwg_connection_upgrade;' "$template")" = 1
test "$(grep -Fc 'proxy_set_header Range $http_range;' "$template")" = 1
test "$(grep -Fc 'proxy_set_header If-Range $http_if_range;' "$template")" = 1
test "$(grep -Fc 'proxy_buffering off;' "$template")" = 1
test "$(grep -Fc 'proxy_request_buffering off;' "$template")" = 1
test "$(grep -Fc 'proxy_cache off;' "$template")" = 1

work=$(mktemp -d /run/embyproxy-bwg-vod1.XXXXXX)
cleanup() {
    case "$work" in
        /run/embyproxy-bwg-vod1.*)
            rm -f -- "$work/candidate.conf" "$work/nginx.conf" "$work/nginx.pid"
            rmdir -- "$work" 2>/dev/null || true
            ;;
    esac
}
trap cleanup EXIT
install -o root -g root -m 600 "$template" "$work/candidate.conf"
cat >"$work/nginx.conf" <<EOF
pid $work/nginx.pid;
error_log stderr notice;
events {}
http {
    map \$http_upgrade \$stream_bwg_connection_upgrade { default upgrade; '' close; }
    server {
        listen 127.0.0.1:18089;
        include $work/candidate.conf;
    }
}
EOF
chmod 600 "$work/nginx.conf"
nginx -t -p / -c "$work/nginx.conf"
nginx -t
test "$(nginx -T 2>/dev/null | grep -Fc 'include /etc/nginx/conf.d/embyproxy-publications/*.conf;')" -ge 2

if [ "$mode" = dry-run ]; then
    state=absent
    if [ -e "$target" ]; then
        state=different
        cmp -s "$work/candidate.conf" "$target" && state=current
    fi
    printf 'BWG_VOD1_ROUTE_DRY_RUN=PASS\n'
    printf 'TARGET_STATE=%s\n' "$state"
    exit 0
fi

if [ -e "$target" ] && cmp -s "$work/candidate.conf" "$target"; then
    printf 'BWG_VOD1_ROUTE_APPLY=ALREADY_CURRENT\n'
    exit 0
fi

stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup=$backup_prefix/$stamp
test ! -e "$backup"
install -d -o root -g root -m 700 "$backup"
had_previous=no
if [ -e "$target" ]; then
    test -f "$target"
    install -o root -g root -m 600 "$target" "$backup/previous.conf"
    had_previous=yes
fi
printf '%s\n' "$had_previous" >"$backup/had-previous"
chmod 600 "$backup/had-previous"

cat >"$backup/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
target='$target'
if [ "\$(cat '$backup/had-previous')" = yes ]; then
    temporary=\$(mktemp '$include_dir/.uhd-vod1.rollback.XXXXXX')
    install -o root -g root -m 640 '$backup/previous.conf' "\$temporary"
    mv -f "\$temporary" "\$target"
else
    rm -f -- "\$target"
fi
nginx -t
systemctl reload nginx.service
test "\$(systemctl is-active nginx.service)" = active
nginx -t
EOF
chmod 700 "$backup/rollback.sh"
bash -n "$backup/rollback.sh"

rollback() {
    "$backup/rollback.sh" >/dev/null
}
trap rollback ERR
install -d -o root -g root -m 750 "$include_dir"
temporary=$(mktemp "$include_dir/.uhd-vod1.apply.XXXXXX")
install -o root -g root -m 640 "$work/candidate.conf" "$temporary"
mv -f "$temporary" "$target"
nginx -t
systemctl reload nginx.service
test "$(systemctl is-active nginx.service)" = active
nginx -t
trap - ERR

sha256sum "$target" >"$backup/applied.sha256"
chmod 600 "$backup/applied.sha256"
printf 'BWG_VOD1_ROUTE_APPLY=PASS\n'
printf 'BACKUP=%s\n' "$backup"
printf 'ROLLBACK=%s\n' "$backup/rollback.sh"
