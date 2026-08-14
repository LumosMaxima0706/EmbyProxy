#!/bin/bash
set -eEuo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
base=https://owner-admin.149077530.xyz
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT

test "$(stat -c %a "$password_file")" = 600
password=$(cat "$password_file")

curl --fail --silent --show-error --max-time 15 \
  --user "owner:$password" \
  "$base/api/admin/failover/status" >"$temporary/failover.json"

curl --fail --silent --show-error --max-time 15 \
  --user "owner:$password" \
  "$base/api/admin/failover/events?limit=20" >"$temporary/events.json"

curl --fail --silent --show-error --max-time 15 \
  --user "owner:$password" \
  "$base/admin" >"$temporary/admin.html"

curl --fail --silent --show-error --max-time 15 \
  --user "owner:$password" \
  -H "Origin: $base" \
  -H 'Content-Type: application/json' \
  --data '{"action":"stats.get","days":7}' \
  "$base/admin/api" >"$temporary/stats.json"

python3 - "$temporary/failover.json" "$temporary/stats.json" "$temporary/events.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    failover = json.load(handle)
with open(sys.argv[2], encoding="utf-8") as handle:
    stats = json.load(handle)
with open(sys.argv[3], encoding="utf-8") as handle:
    event_response = json.load(handle)

state = failover.get("state", {})
assert failover.get("ok") is True
assert state.get("active_target") in {"nosla", "bwg"}
assert state.get("activeTarget") in {"NOSLA", "BWG"}
assert state.get("state_source") == "policy_state_file"
assert stats.get("ok") is True
assert stats.get("stats_source") == "local_sidecar_store"
assert isinstance(stats.get("stats_available"), bool)
events = event_response.get("events", [])
assert event_response.get("ok") is True
for event in events:
    assert set(event).issubset({"created_at", "event_type", "from_node_id", "to_node_id", "reason_code", "success"})

print("FAILOVER_STATUS_API=PASS")
print("ACTIVE_TARGET=" + state["activeTarget"])
print("MODE=" + str(state.get("mode", "")))
print("MANUAL_HOLD=" + str(state.get("manual_hold", "")))
print("DECISION_REASON=" + str(state.get("decision_reason", "")))
print("STATE_SOURCE=" + state["state_source"])
print("STATS_API=PASS")
print("STATS_SOURCE=" + stats["stats_source"])
print("STATS_AVAILABLE=" + str(stats["stats_available"]).lower())
print("STATS_REASON=" + str(stats.get("stats_unavailable_reason", "")))
print("FAILOVER_EVENTS_API=PASS")
print("FAILOVER_EVENT_COUNT=" + str(len(events)))
PY

grep -q 'state.active_target' "$temporary/admin.html"
grep -q 'r.stats_available === false' "$temporary/admin.html"
printf 'ADMIN_UI_CONTRACT=PASS\n'
