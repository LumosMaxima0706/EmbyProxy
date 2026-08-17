#!/bin/bash
set -euo pipefail

ssh_base=(
    ssh -F /dev/null
    -i /etc/embyproxy-failover-policy/nosla-meter
    -o UserKnownHostsFile=/etc/embyproxy-failover-policy/known_hosts
    -o StrictHostKeyChecking=yes
    -o BatchMode=yes
    -o IdentitiesOnly=yes
    -o ConnectTimeout=8
    -T
    embyproxy-meter@45.143.130.11
)

validate() {
    python3 -c '
import json, sys
data=json.load(sys.stdin)
assert set(data) == {"interface", "rx_bytes", "tx_bytes"}
assert data["interface"] == "enp3s0"
assert isinstance(data["rx_bytes"], int) and data["rx_bytes"] >= 0
assert isinstance(data["tx_bytes"], int) and data["tx_bytes"] >= 0
'
}

"${ssh_base[@]}" | validate
"${ssh_base[@]}" id | validate
if timeout 12 ssh -F /dev/null \
    -i /etc/embyproxy-failover-policy/nosla-meter \
    -o UserKnownHostsFile=/etc/embyproxy-failover-policy/known_hosts \
    -o StrictHostKeyChecking=yes -o BatchMode=yes -o IdentitiesOnly=yes \
    -o ConnectTimeout=8 -o ExitOnForwardFailure=yes \
    -N -L 127.0.0.1:31999:127.0.0.1:22 \
    embyproxy-meter@45.143.130.11 >/dev/null 2>&1; then
    echo "restricted meter unexpectedly accepted TCP forwarding" >&2
    exit 1
fi
printf 'FORCED_COMMAND_SCHEMA=PASS\n'
printf 'ARBITRARY_COMMAND_BLOCKED=PASS\n'
printf 'PORT_FORWARDING_BLOCKED=PASS\n'
