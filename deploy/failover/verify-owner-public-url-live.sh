#!/bin/bash
set -euo pipefail

password_file=/etc/embyproxy-failover-policy/owner-admin-password
test "$(stat -c %a "$password_file")" = 600
password=$(tr -d '\r\n' <"$password_file")
test -n "$password"
response=$(mktemp)
html=$(mktemp)
trap 'rm -f "$response" "$html"' EXIT
chmod 600 "$response" "$html"

unauthenticated=$(curl --http1.1 -sS -o /dev/null -w '%{http_code}' \
    --max-time 15 https://owner-admin.149077530.xyz/admin)
authenticated=$(curl --http1.1 -sS -o "$html" -w '%{http_code}' \
    -u "owner:$password" --max-time 15 \
    https://owner-admin.149077530.xyz/admin)
curl --http1.1 -sS -u "owner:$password" --max-time 15 \
    -H 'Content-Type: application/json' -d '{"action":"list"}' \
    https://owner-admin.149077530.xyz/admin/api >"$response"
unset password

python3 - "$response" <<'PY'
import json
import sys
import urllib.parse

with open(sys.argv[1], encoding="utf-8") as handle:
    payload = json.load(handle)
nodes = [node for node in payload.get("nodes", []) if node.get("name") == "uhd"]
if len(nodes) != 1:
    raise SystemExit("UHD node is missing or duplicated")
public_url = nodes[0].get("publicUrl", "")
parsed = urllib.parse.urlsplit(public_url)
if parsed.scheme != "https" or parsed.hostname != "stream.149077530.xyz":
    raise SystemExit("UHD public host is invalid")
if parsed.path != "/https/v1.uhdnow.com/443":
    raise SystemExit("UHD public path is invalid")
if parsed.username or parsed.password or parsed.query or parsed.fragment:
    raise SystemExit("UHD public URL contains sensitive or unsupported parts")
PY

grep -Fq "const proxyUrl = String(n.publicUrl || '');" "$html"
grep -Fq 'data-copy="${attr(proxyUrl)}"' "$html"
grep -Fq 'data-public-url="${attr(proxyUrl)}"' "$html"
grep -Fq 'function openPublicMediaUrl(rawURL)' "$html"
if grep -Fq '`${location.origin}/${n.name}' "$html"; then
    echo 'Admin UI still derives media URLs from its origin' >&2
    exit 1
fi

test "$unauthenticated" = 401
test "$authenticated" = 200
printf 'OWNER_ADMIN_UNAUTHENTICATED=%s\n' "$unauthenticated"
printf 'OWNER_ADMIN_AUTHENTICATED=%s\n' "$authenticated"
printf 'UHD_PUBLIC_URL_CONTRACT=PASS\n'
printf 'DISPLAY_COPY_PREVIEW_CONTRACT=PASS\n'
