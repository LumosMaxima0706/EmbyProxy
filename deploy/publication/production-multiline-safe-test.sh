#!/bin/bash
set -euo pipefail

slug=${1:-mlsafe0816}
case "$slug" in
    mlsafe[0-9]*) ;;
    *) echo 'invalid safe-test slug' >&2; exit 2 ;;
esac

base=${OWNER_ADMIN_BASE_URL:?set OWNER_ADMIN_BASE_URL to the owner-admin HTTPS origin}
password_file=${OWNER_ADMIN_PASSWORD_FILE:?set OWNER_ADMIN_PASSWORD_FILE to a root-readable Basic Auth password file}
test "${CONFIRM_SINGLE_SLUG_TEST:-}" = "yes" || {
    echo 'set CONFIRM_SINGLE_SLUG_TEST=yes after reviewing the single-slug plan' >&2
    exit 2
}
case "$base" in
    *\?*|*\#*) echo 'OWNER_ADMIN_BASE_URL must not contain a query or fragment' >&2; exit 2 ;;
esac
case "$base" in
    https://*) ;;
    *) echo 'OWNER_ADMIN_BASE_URL must use HTTPS' >&2; exit 2 ;;
esac
case "${base#https://}" in
    */*) echo 'OWNER_ADMIN_BASE_URL must not contain a path' >&2; exit 2 ;;
esac
test -r "$password_file"
password=$(tr -d '\r\n' <"$password_file")
test -n "$password"
work=$(mktemp -d)
published=false
saved=false

cleanup() {
    if $published; then
        curl -sS -u "owner:$password" -X POST -o /dev/null \
            "$base/api/admin/emby-servers/$slug/unpublish" || true
    fi
    if $saved; then
        curl -sS -u "owner:$password" -H 'Content-Type: application/json' \
            --data "{\"action\":\"delete\",\"name\":\"$slug\"}" \
            -o /dev/null "$base/admin/api" || true
    fi
    rm -rf -- "$work"
}
trap cleanup EXIT

post_admin() {
    local payload=$1 output=$2
    curl --fail --silent --show-error --http1.1 -u "owner:$password" \
        -H 'Content-Type: application/json' --data "$payload" \
        -o "$output" "$base/admin/api"
}

post_publication() {
    local action=$1 output=$2
    curl --fail --silent --show-error --http1.1 -u "owner:$password" \
        -X POST -o "$output" "$base/api/admin/emby-servers/$slug/$action"
}

post_admin "{\"action\":\"save\",\"node\":{\"name\":\"$slug\",\"target\":\"https://primary.$slug.invalid\\nhttps://backup.$slug.invalid\"}}" "$work/save.json"
saved=true
python3 - "$work/save.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok'):raise SystemExit('two-line save failed')
print('save_two_lines=PASS')
PY

post_admin "{\"action\":\"checkStatus\",\"name\":\"$slug\"}" "$work/check.json"
python3 - "$work/check.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
rows=d.get('results') or []
lines=(rows[0].get('lines') or []) if rows else []
if len(lines)!=2 or [x.get('line_id') for x in lines]!=['main','backup-2']:
 raise SystemExit('per-line detection missing')
print('line_detection=PASS line_count=2 health=' + ','.join(str(x.get('health','unknown')) for x in lines))
PY

post_publication publish/dry-run "$work/dry-run.json"
python3 - "$work/dry-run.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
plan=d.get('plan') or {}
if not d.get('ok') or plan.get('line_count')!=2 or not d.get('adapter_ready'):
 raise SystemExit('multi-line dry-run failed')
print('dry_run=PASS line_count=2 adapter_ready=true')
PY

post_publication publish "$work/publish.json"
python3 - "$work/publish.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok') or d.get('status')!='published':
 raise SystemExit('multi-line publish failed: '+str(d.get('reason','unknown')))
print('publish=PASS legacy_exactly_one_error=false')
PY
published=true

curl --fail --silent --show-error --http1.1 -u "owner:$password" \
    -o "$work/status-before.json" "$base/api/admin/emby-servers/$slug/publish-status"
python3 - "$work/status-before.json" "$work/public-url.hash" <<'PY'
import hashlib,json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
p=d.get('publication') or {};url=p.get('public_url','')
if p.get('status')!='published' or p.get('nosla_status')!='synced' or p.get('bwg_status')!='synced' or not url:
 raise SystemExit('published edge status invalid')
with open(sys.argv[2],'w',encoding='ascii') as h:h.write(hashlib.sha256(url.encode()).hexdigest())
print('edge_sync=PASS public_url=present')
PY

post_admin "{\"action\":\"save\",\"node\":{\"name\":\"$slug\",\"oldName\":\"$slug\",\"target\":\"https://primary.$slug.invalid\\nhttps://backup.$slug.invalid\\nhttps://backup3.$slug.invalid\"}}" "$work/add.json"
python3 - "$work/add.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok') or d.get('publication_sync')!='synced':raise SystemExit('backup add failed')
print('add_backup=PASS')
PY

curl --fail --silent --show-error --http1.1 -u "owner:$password" -o "$work/routes-add.json" "$base/api/admin/managed-routes"
python3 - "$work/routes-add.json" "$slug" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
route=next((x for x in d.get('routes',[]) if x.get('slug')==sys.argv[2]),None)
if not route or len(route.get('lines') or [])!=3:raise SystemExit('three managed route lines missing')
print('managed_route_lines_after_add=3')
PY

post_admin "{\"action\":\"save\",\"node\":{\"name\":\"$slug\",\"oldName\":\"$slug\",\"target\":\"https://primary.$slug.invalid\\nhttps://backup.$slug.invalid\"}}" "$work/remove.json"
python3 - "$work/remove.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok') or d.get('publication_sync')!='synced':raise SystemExit('backup remove failed')
print('remove_backup=PASS')
PY

curl --fail --silent --show-error --http1.1 -u "owner:$password" -o "$work/routes-remove.json" "$base/api/admin/managed-routes"
curl --fail --silent --show-error --http1.1 -u "owner:$password" -o "$work/status-after.json" "$base/api/admin/emby-servers/$slug/publish-status"
python3 - "$work/routes-remove.json" "$work/status-after.json" "$work/public-url.hash" "$slug" <<'PY'
import hashlib,json,sys
with open(sys.argv[1],encoding='utf-8') as h:r=json.load(h)
with open(sys.argv[2],encoding='utf-8') as h:s=json.load(h)
with open(sys.argv[3],encoding='ascii') as h:before=h.read()
route=next((x for x in r.get('routes',[]) if x.get('slug')==sys.argv[4]),None)
url=(s.get('publication') or {}).get('public_url','')
if not route or len(route.get('lines') or [])!=2:raise SystemExit('two managed route lines not restored')
if hashlib.sha256(url.encode()).hexdigest()!=before:raise SystemExit('public URL changed')
print('managed_route_lines_after_remove=2 public_url_unchanged=PASS')
PY

post_publication unpublish "$work/unpublish.json"
python3 - "$work/unpublish.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok') or d.get('status')!='saved_unpublished':raise SystemExit('single-slug unpublish failed')
print('single_slug_unpublish=PASS')
PY
published=false

post_admin "{\"action\":\"delete\",\"name\":\"$slug\"}" "$work/delete.json"
python3 - "$work/delete.json" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if not d.get('ok'):raise SystemExit('test slug delete failed')
print('single_slug_delete=PASS')
PY
saved=false

curl --fail --silent --show-error --http1.1 -u "owner:$password" -o "$work/routes-final.json" "$base/api/admin/managed-routes"
python3 - "$work/routes-final.json" "$slug" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as h:d=json.load(h)
if any(x.get('slug')==sys.argv[2] for x in d.get('routes',[])):raise SystemExit('orphan managed route')
print('single_slug_cleanup=PASS orphan_route=false')
PY

trap - EXIT
rm -rf -- "$work"
