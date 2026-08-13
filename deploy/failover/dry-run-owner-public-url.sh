#!/bin/bash
set -euo pipefail

artifact=${1:?artifact is required}
temporary=$(mktemp -d /root/staging/owner-public-url-dry-run.XXXXXX)
case "$temporary" in
    /root/staging/owner-public-url-dry-run.*) ;;
    *) echo 'unsafe temporary path' >&2; exit 1 ;;
esac
unit=embyproxy-owner-public-url-dry-run
cleanup() {
    systemctl stop "$unit.service" >/dev/null 2>&1 || true
    rm -rf "$temporary"
}
trap cleanup EXIT

install -o root -g root -m 600 /etc/embyproxy-gsy-sidecar/embyproxy.env "$temporary/test.env"
sed -i '/^PUBLIC_MEDIA_BASE_URL=/d;/^PUBLIC_MEDIA_NODE_PATHS_JSON=/d;/^LISTEN_ADDR=/d;/^DB_PATH=/d' "$temporary/test.env"
printf '%s\n' \
    'LISTEN_ADDR=127.0.0.1:28083' \
    "DB_PATH=$temporary/proxy.db" \
    'PUBLIC_MEDIA_BASE_URL=https://stream.149077530.xyz' \
    "PUBLIC_MEDIA_NODE_PATHS_JSON='{\"uhd\":\"/https/v1.uhdnow.com/443/\"}'" >>"$temporary/test.env"
chmod 600 "$temporary/test.env"

systemd-run --quiet --unit="$unit" --property=Type=simple \
    --property="EnvironmentFile=$temporary/test.env" \
    --property="WorkingDirectory=$temporary" "$artifact"
ready=false
for _ in $(seq 1 30); do
    if ss -lntH 'sport = :28083' | grep -q '127.0.0.1:28083'; then
        ready=true
        break
    fi
    sleep 0.5
done
test "$ready" = true
curl --http1.1 -sS --max-time 10 \
    -H 'Host: owner-admin.149077530.xyz' \
    -H 'X-Owner-Admin-Authenticated: 1' \
    -H 'Content-Type: application/json' \
    -d '{"action":"save","node":{"name":"uhd","target":"https://example.invalid"}}' \
    http://127.0.0.1:28083/admin/api >/dev/null
curl --http1.1 -sS --max-time 10 \
    -H 'Host: owner-admin.149077530.xyz' \
    -H 'X-Owner-Admin-Authenticated: 1' \
    -H 'Content-Type: application/json' -d '{"action":"list"}' \
    http://127.0.0.1:28083/admin/api >"$temporary/list.json"
python3 - "$temporary/list.json" <<'PY'
import json
import sys
import urllib.parse

with open(sys.argv[1], encoding="utf-8") as handle:
    payload = json.load(handle)
nodes = [node for node in payload.get("nodes", []) if node.get("name") == "uhd"]
if len(nodes) != 1:
    raise SystemExit("dry-run UHD node mismatch")
parsed = urllib.parse.urlsplit(nodes[0].get("publicUrl", ""))
if parsed.scheme != "https" or parsed.hostname != "stream.149077530.xyz" or parsed.path != "/https/v1.uhdnow.com/443/":
    raise SystemExit("dry-run public URL mismatch")
if parsed.username or parsed.password or parsed.query or parsed.fragment:
    raise SystemExit("dry-run public URL is unsafe")
PY
printf 'OWNER_PUBLIC_URL_DRY_RUN=PASS\n'
