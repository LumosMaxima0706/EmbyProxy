#!/bin/bash
set -euo pipefail

stamp=${1:?backup timestamp is required}
root="/var/backups/embyproxy-failover-policy/$stamp"
temporary="$root/SHA256SUMS.new"

find "$root" -type f \
    ! -name SHA256SUMS \
    ! -name SHA256SUMS.new \
    ! -name 'rollback-*.sha256' \
    -print0 | sort -z | xargs -0 sha256sum >"$temporary"
mv -f "$temporary" "$root/SHA256SUMS"
chmod 600 "$root/SHA256SUMS"
