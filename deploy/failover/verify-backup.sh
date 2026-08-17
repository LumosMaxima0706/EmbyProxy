#!/bin/bash
set -euo pipefail

host_role=${1:?host role is required}
stamp=${2:?backup timestamp is required}
root="/var/backups/embyproxy-failover-policy/$stamp"

test "$(stat -c %a "$root")" = 700
cd /
sha256sum -c "$root/SHA256SUMS" >/dev/null
bash -n "$root/rollback-$host_role.sh"
sha256sum -c "$root/rollback-$host_role.sha256" >/dev/null
printf '%s_BACKUP_VERIFY=PASS\n' "${host_role^^}"

if [ "$host_role" = bwg ]; then
    printf 'LEGACY_TIMER=%s\n' "$(systemctl is-active stream-failover.timer)"
    printf 'NEW_TIMER=%s\n' "$(systemctl is-active embyproxy-failover-policy.timer 2>/dev/null || true)"
    python3 /opt/stream-failover/spaceship_dns.py read | python3 -c '
import json, sys
data = json.load(sys.stdin)
records = [item for item in data.get("project_records", [])
           if item.get("name") == "stream" and item.get("type") == "A"]
print("STREAM_RECORD_COUNT=" + str(len(records)))
allowed = {"45.143.130.11", "144.34.226.187"}
print("ACTIVE_TARGET=" + ("known_allowlisted" if len(records) == 1
      and records[0].get("address") in allowed else "invalid"))
'
else
    printf 'NGINX=%s\n' "$(systemctl is-active nginx)"
    printf 'CONTAINER=%s\n' "$(docker inspect --format '{{.State.Status}}' stream-erpgo-nosla)"
fi
