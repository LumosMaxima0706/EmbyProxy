#!/bin/bash
set -euo pipefail

public_key_file=${1:?public key file is required}
meter_script=${2:?meter script is required}

test -s "$public_key_file"
test -s "$meter_script"
if id embyproxy-meter >/dev/null 2>&1; then
    echo "meter account already exists; refusing overwrite" >&2
    exit 1
fi

useradd --system --create-home --home-dir /var/lib/embyproxy-meter \
    --shell /bin/sh embyproxy-meter
passwd -l embyproxy-meter >/dev/null
install -o root -g root -m 755 "$meter_script" \
    /usr/local/sbin/embyproxy-traffic-meter
install -d -o embyproxy-meter -g embyproxy-meter -m 700 \
    /var/lib/embyproxy-meter/.ssh

read -r key_type key_body _comment <"$public_key_file"
case "$key_type" in
    ssh-ed25519|sk-ssh-ed25519@openssh.com) ;;
    *) echo "unsupported meter key type" >&2; exit 1 ;;
esac
case "$key_body" in
    *[!A-Za-z0-9+/=]*|'') echo "invalid public key" >&2; exit 1 ;;
esac

printf 'restrict,command="/usr/local/sbin/embyproxy-traffic-meter" %s %s\n' \
    "$key_type" "$key_body" \
    > /var/lib/embyproxy-meter/.ssh/authorized_keys
chown embyproxy-meter:embyproxy-meter \
    /var/lib/embyproxy-meter/.ssh/authorized_keys
chmod 600 /var/lib/embyproxy-meter/.ssh/authorized_keys

test "$(stat -c %a /var/lib/embyproxy-meter/.ssh)" = 700
test "$(stat -c %a /var/lib/embyproxy-meter/.ssh/authorized_keys)" = 600
test "$(stat -c %U /var/lib/embyproxy-meter/.ssh/authorized_keys)" = embyproxy-meter
/usr/local/sbin/embyproxy-traffic-meter | python3 -c '
import json, sys
data=json.load(sys.stdin)
assert isinstance(data["rx_bytes"], int) and data["rx_bytes"] >= 0
assert isinstance(data["tx_bytes"], int) and data["tx_bytes"] >= 0
print("NOSLA_METER_LOCAL=PASS")
'
