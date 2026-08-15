#!/bin/bash
set -euo pipefail

mode=dry-run
node=${1:?node must be bwg or nosla}
stream_config=${2:?stream config path is required}
case "$node" in bwg|nosla) ;; *) echo "invalid node" >&2; exit 2 ;; esac
case "$stream_config" in /etc/nginx/*) ;; *) echo "stream config must be below /etc/nginx" >&2; exit 2 ;; esac
if [ "${3:-}" = "--apply" ]; then mode=apply; fi

include_dir=/etc/nginx/conf.d/embyproxy-publications
include_directive='include /etc/nginx/conf.d/embyproxy-publications/*.conf;'
backup_root=/var/backups/embyproxy-publication-agent
test -f "$stream_config"
hook_count=$(grep -Fc "$include_directive" "$stream_config" || true)
if [ "$hook_count" -ge 2 ]; then
    echo "EDGE_HOOK=$node PRESENT"
    nginx -t >/dev/null
    exit 0
fi
echo "EDGE_HOOK=$node INCOMPLETE CURRENT=$hook_count REQUIRED=2 MODE=$mode"
if [ "$mode" != apply ]; then
    exit 0
fi
test "$(id -u)" = 0
stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup="$backup_root/${stamp}-${node}-hook"
install -d -o root -g root -m 700 "$backup"
install -o root -g root -m 600 "$stream_config" "$backup/stream.conf"
edge_group=root
if getent group embyproxy-gsy-sidecar >/dev/null 2>&1; then edge_group=embyproxy-gsy-sidecar; fi
install -d -o root -g "$edge_group" -m 2770 "$include_dir"
candidate="$backup/stream.candidate.conf"
install -o root -g root -m 600 "$stream_config" "$candidate"
python3 - "$candidate" "$include_directive" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
directive = sys.argv[2]
lines = path.read_text().splitlines(True)
blocks = []
index = 0
while index < len(lines):
    if lines[index].strip() != 'server {':
        index += 1
        continue
    start = index
    depth = 0
    while index < len(lines):
        depth += lines[index].count('{') - lines[index].count('}')
        if depth == 0:
            break
        index += 1
    end = index
    body = ''.join(lines[start:end + 1])
    if 'server_name stream.149077530.xyz' in body and ('listen 80' in body or 'listen 443' in body):
        blocks.append((start, end, body))
    index += 1

http = [block for block in blocks if 'listen 80' in block[2]]
https = [block for block in blocks if 'listen 443' in block[2]]
if not http or not https:
    raise SystemExit('stream HTTP/HTTPS server blocks not found')

inserts = []
for start, end, body in blocks:
    if directive in body:
        continue
    insert = next((i for i in range(start + 1, end) if lines[i].lstrip().startswith('location ')), end)
    inserts.append(insert)
for insert in sorted(inserts, reverse=True):
    lines.insert(insert, '    ' + directive + '\n')
path.write_text(''.join(lines))
PY
test "$(grep -Fc "$include_directive" "$candidate")" -ge 2
install -o root -g root -m 600 "$candidate" "$stream_config"
if ! nginx -t >/dev/null; then
    install -o root -g root -m 600 "$backup/stream.conf" "$stream_config"
    echo "NGINX_TEST=FAIL ROLLED_BACK=YES" >&2
    exit 1
fi
systemctl reload nginx.service
cat >"$backup/rollback.sh" <<EOF
#!/bin/bash
set -euo pipefail
install -o root -g root -m 600 '$backup/stream.conf' '$stream_config'
nginx -t
systemctl reload nginx.service
EOF
chmod 700 "$backup/rollback.sh"
bash -n "$backup/rollback.sh"
printf 'EDGE_HOOK_APPLIED=%s BACKUP=%s\n' "$node" "$backup"
