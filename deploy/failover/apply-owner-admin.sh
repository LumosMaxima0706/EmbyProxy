#!/bin/bash
set -euo pipefail

template=${1:?Nginx template is required}
wrapper=${2:?DNS wrapper is required}
password_file=/etc/embyproxy-failover-policy/owner-admin-password
htpasswd_file=/etc/nginx/owner-admin.htpasswd
nginx_file=/etc/nginx/conf.d/owner-admin.149077530.xyz.conf
webroot=/var/lib/letsencrypt

rollback() {
    rm -f "$nginx_file" "$htpasswd_file"
    nginx -t && systemctl reload nginx || true
    python3 /opt/stream-failover/spaceship_owner_admin.py delete || true
    if command -v certbot >/dev/null 2>&1 \
            && [ -d /etc/letsencrypt/live/owner-admin.149077530.xyz ]; then
        certbot delete --non-interactive --cert-name owner-admin.149077530.xyz || true
    fi
}
trap rollback ERR

test ! -e "$nginx_file"
test ! -e "$htpasswd_file"
test ! -e "$password_file"
install -o root -g root -m 755 "$wrapper" \
    /opt/stream-failover/spaceship_owner_admin.py
python3 /opt/stream-failover/spaceship_owner_admin.py ensure

install -d -o root -g www-data -m 750 "$webroot/.well-known/acme-challenge"
cat >"$nginx_file" <<'HTTP_ONLY'
server {
    listen 80;
    listen [::]:80;
    server_name owner-admin.149077530.xyz;
    location ^~ /.well-known/acme-challenge/ {
        root /var/lib/letsencrypt;
        default_type text/plain;
    }
    location / { return 308 https://$host$request_uri; }
}
HTTP_ONLY
nginx -t
systemctl reload nginx
certbot certonly --non-interactive --webroot -w "$webroot" \
    -d owner-admin.149077530.xyz --keep-until-expiring

umask 077
openssl rand -base64 36 | tr -d '\n' >"$password_file"
printf '\n' >>"$password_file"
password_hash=$(openssl passwd -6 -stdin <"$password_file")
printf 'owner:%s\n' "$password_hash" >"$htpasswd_file"
chown root:www-data "$htpasswd_file"
chmod 640 "$htpasswd_file"
install -o root -g root -m 644 "$template" "$nginx_file"
nginx -t
systemctl reload nginx

test "$(stat -c %a "$password_file")" = 600
test "$(stat -c %a "$htpasswd_file")" = 640
test "$(stat -c %U:%G "$htpasswd_file")" = root:www-data
trap - ERR
printf 'OWNER_ADMIN_APPLY=PASS\n'
printf 'PASSWORD_FILE=%s\n' "$password_file"
