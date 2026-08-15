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
if grep -Fq "$include_directive" "$stream_config"; then
    echo "EDGE_HOOK=$node PRESENT"
    nginx -t >/dev/null
    exit 0
fi
echo "EDGE_HOOK=$node MISSING MODE=$mode"
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
python3 - "$stream_config" "$include_directive" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
directive = sys.argv[2]
lines = path.read_text().splitlines(True)
server = None
insert = None
depth = 0
for index, line in enumerate(lines):
    if 'server_name stream.149077530.xyz' in line:
        server = index
        # The opening brace is on the preceding server line in both edges.
        depth = 1
        continue
    if server is None:
        continue
    depth += line.count('{') - line.count('}')
    if depth <= 0:
        break
    if line.lstrip().startswith('location '):
        insert = index
        break
if insert is None:
    raise SystemExit('stream server block not found')
lines.insert(insert, '    ' + directive + '\n')
path.write_text(''.join(lines))
PY
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
