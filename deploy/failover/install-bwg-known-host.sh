#!/bin/bash
set -euo pipefail

source_file=${1:?NOSLA public host key file is required}
destination=/etc/embyproxy-failover-policy/known_hosts
read -r key_type key_data _comment <"$source_file"
test "$key_type" = ssh-ed25519
case "$key_data" in
    *[!A-Za-z0-9+/=]*|'') echo "invalid host public key" >&2; exit 1 ;;
esac
umask 077
printf '45.143.130.11 %s %s\n' "$key_type" "$key_data" >"$destination"
chown root:root "$destination"
chmod 600 "$destination"
ssh-keygen -F 45.143.130.11 -f "$destination" >/dev/null
printf 'PINNED_HOST_KEY=PASS\n'
