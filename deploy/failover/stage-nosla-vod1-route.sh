#!/bin/bash
set -euo pipefail

locations=${1:-/root/staging/nosla-vod1-locations.inc}
staging_dir=${2:-/root/staging/nosla-vod1-route}
live=/etc/nginx/conf.d/stream-proxy.conf
candidate=$staging_dir/stream-proxy.conf

test -r "$locations"
install -d -o root -g root -m 700 "$staging_dir"
awk -v locations="$locations" '
    index($0, "include /etc/nginx/conf.d/stream-proxy-admin-locations.inc;") {
        while ((getline line < locations) > 0) print line
        close(locations)
    }
    { print }
' "$live" >"$candidate"
chmod 600 "$candidate"
test "$(grep -c 'location = /https/v1-vod1.uhdnow.com/443 {' "$candidate")" = 1
test "$(grep -c 'location \^~ /https/v1-vod1.uhdnow.com/443/' "$candidate")" = 1
test "$(grep -c 'proxy_set_header Range \$http_range;' "$candidate")" = 3
test "$(grep -c 'proxy_set_header If-Range \$http_if_range;' "$candidate")" = 3
test "$(grep -c 'proxy_cache off;' "$candidate")" = 3
test "$(grep -c 'proxy_buffering off;' "$candidate")" = 3

cat >"$staging_dir/nginx.conf" <<EOF
pid $staging_dir/nginx.pid;
error_log stderr notice;
events {}
http {
    include /etc/nginx/mime.types;
    include $candidate;
}
EOF
chmod 600 "$staging_dir/nginx.conf"
nginx -t -p / -c "$staging_dir/nginx.conf"
printf 'NOSLA_VOD1_DRY_RUN=PASS\n'
printf 'CANDIDATE=%s\n' "$candidate"
