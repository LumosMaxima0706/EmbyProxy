#!/bin/bash
set -euo pipefail

test "$(id -u)" = 0
public_key_file=${1:?public key file is required}
test -s "$public_key_file"
read -r key_type key_body _comment <"$public_key_file"
test "$key_type" = ssh-ed25519
case "$key_body" in *[!A-Za-z0-9+/=]*|'') echo invalid-key >&2; exit 2 ;; esac
authorized=/root/.ssh/authorized_keys
install -d -o root -g root -m 700 /root/.ssh
touch "$authorized"
chown root:root "$authorized"
chmod 600 "$authorized"
command='restrict,command="/usr/local/sbin/embyproxy-publication-edge --mode=edge --config /etc/embyproxy-publication-agent/edge-nosla.json"'
if grep -Fq "$key_body" "$authorized"; then
    grep -Fq "$command" "$authorized" || { echo key-conflict >&2; exit 1; }
    echo FORCED_KEY=present
    exit 0
fi
printf '%s %s %s\n' "$command" "$key_type" "$key_body" >>"$authorized"
chmod 600 "$authorized"
echo FORCED_KEY=installed
