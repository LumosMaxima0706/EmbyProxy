#!/bin/sh
set -eu

state=/var/lib/embyproxy-vps-control-plane-test
controller=http://127.0.0.1:18180
test_name=edge-bwg-logical-b
token=$(sed -n 's/^ADMIN_TOKEN=//p' "$state/controller.env")

response=$(curl --fail --silent --show-error \
  -H "X-Admin-Token: $token" -H 'Content-Type: application/json' \
  -X POST --data '{"name":"edge-bwg-logical-b","public_address":"http://127.0.0.1:18182","quota_bytes":100000000,"reset_day":1,"reset_timezone":"UTC","priority":2}' \
  "$controller/api/admin/proxy-nodes")
command=$(printf '%s' "$response" | grep -o '"install_command":"[^"]*"' | cut -d '"' -f4)
bootstrap_url=$(printf '%s' "$command" | sed -n 's#.* \(http[^ ]*\) | sudo sh#\1#p')
test -n "$bootstrap_url"
script=$(curl --fail --silent --show-error "$bootstrap_url")
test -n "$script"
enroll_url=$(printf '%s\n' "$bootstrap_url" | sed 's#/api/edge/bootstrap/#/api/edge/enroll/#')
test -n "$enroll_url"
enrolled=$(curl --fail --silent --show-error -H 'Content-Type: application/json' \
  -X POST --data '{"version":"isolated-logical","commit":"f9b8be5"}' "$enroll_url")
node_id=$(printf '%s' "$enrolled" | grep -o '"node_id":"[^"]*"' | cut -d '"' -f4)
credential=$(printf '%s' "$enrolled" | grep -o '"credential":"[^"]*"' | cut -d '"' -f4)
test -n "$node_id"
test -n "$credential"
printf '%s' "$credential" > "$state/logical-b.credential"
chmod 0600 "$state/logical-b.credential"

curl --fail --silent --show-error -H 'Content-Type: application/json' -X POST \
  --data "{\"credential\":\"$credential\",\"version\":\"isolated-logical\",\"commit\":\"f9b8be5\",\"state\":\"healthy\",\"playbackHealthy\":true,\"configSynced\":true}" \
  "$controller/api/edge/heartbeat/$node_id" >/dev/null
curl --fail --silent --show-error -H "X-Admin-Token: $token" -H 'Content-Type: application/json' \
  -X PATCH --data '{"priority":1}' "$controller/api/admin/proxy-nodes/$node_id" >/dev/null

headers=$(mktemp)
bytes=$(curl --fail --silent --show-error -H 'Range: bytes=0-1023' -D "$headers" \
  "$controller/s/vps-control-plane-test/video" | wc -c)
status=$(awk 'NR==1 {print $2}' "$headers")
rm -f "$headers"
test "$status" = 206
test "$bytes" -eq 1024

curl --fail --silent --show-error -H "X-Admin-Token: $token" \
  "$controller/api/admin/proxy-nodes" > "$state/manual-priority.json"
printf 'logical_node=%s manual_status=%s bytes=%s\n' "$test_name" "$status" "$bytes"
