#!/bin/bash
set -euo pipefail

staging_dir=${1:-/root/staging/owner-admin-basic-only}
new_include=${2:-/root/staging/nosla-stream-public-locations.inc}
live_server=/etc/nginx/conf.d/stream-proxy.conf

install -d -o root -g root -m 700 "$staging_dir"
test -r "$new_include"
install -o root -g root -m 600 "$new_include" "$staging_dir/stream-proxy-admin-locations.inc"
sed "s#/etc/nginx/conf.d/stream-proxy-admin-locations.inc#$staging_dir/stream-proxy-admin-locations.inc#" \
    "$live_server" >"$staging_dir/stream-proxy.conf"
chmod 600 "$staging_dir/stream-proxy.conf"
cat >"$staging_dir/nginx.conf" <<EOF
pid $staging_dir/nginx.pid;
error_log stderr notice;
events {}
http {
    include /etc/nginx/mime.types;
    include $staging_dir/stream-proxy.conf;
}
EOF
chmod 600 "$staging_dir/nginx.conf"
nginx -t -p / -c "$staging_dir/nginx.conf"
test "$(grep -c 'return 404;' "$staging_dir/stream-proxy-admin-locations.inc")" = 4
test "$(grep -c 'proxy_cache off;' "$staging_dir/stream-proxy-admin-locations.inc")" = 1
printf 'NOSLA_STREAM_ADMIN_DRY_RUN=PASS\n'
