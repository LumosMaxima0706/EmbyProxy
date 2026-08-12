#!/bin/bash
set -euo pipefail

directory=/etc/embyproxy-failover-policy
private="$directory/nosla-meter"
public="$private.pub"

install -d -m 700 "$directory"
test ! -e "$private"
test ! -e "$public"
ssh-keygen -q -t ed25519 -N '' -C embyproxy-restricted-meter -f "$private"
chmod 600 "$private" "$public"
test "$(stat -c %a "$directory")" = 700
test "$(stat -c %a "$private")" = 600
test "$(stat -c %a "$public")" = 600
ssh-keygen -lf "$public" >/dev/null
printf 'METER_KEY_CREATED=PASS\n'
printf 'PRIVATE_MODE=%s\n' "$(stat -c %a "$private")"
printf 'PUBLIC_MODE=%s\n' "$(stat -c %a "$public")"
