#!/bin/bash
set -euo pipefail

test "$(systemctl is-active nginx)" = active
test "$(docker inspect --format '{{.State.Status}}' stream-erpgo-nosla)" = running
ss -lntp | grep -q '127.0.0.1:18080'
nginx -t
if nginx -T 2>/dev/null | grep -Eq '^[[:space:]]*(proxy_cache_path|slice|background_update)'; then
    echo 'forbidden Nginx cache directive found' >&2
    exit 1
fi
for directive in 'proxy_cache off' 'proxy_buffering off' \
        'proxy_request_buffering off' 'proxy_set_header Range' \
        'proxy_set_header If-Range'; do
    nginx -T 2>/dev/null | grep -q "$directive"
done
if journalctl -u nginx.service --since '-30 min' --no-pager \
        | grep -Eiq '(panic|fatal|private.?key|authorization:|cookie:|password=|token=)'; then
    echo 'severe/secret marker found in NOSLA journal' >&2
    exit 1
fi
printf 'NOSLA_SERVICES=PASS\n'
printf 'NO_CACHE_NOSLA=PASS\n'
printf 'RANGE_HEADERS_NOSLA=PASS\n'
printf 'LOG_REDACTION_NOSLA=PASS\n'
