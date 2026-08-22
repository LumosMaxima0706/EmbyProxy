#!/bin/bash
set -euo pipefail

database=/var/lib/embyproxy-gsy-sidecar/proxy.db
python3 - "$database" <<'PY'
import sqlite3, sys
db = sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True)
for table in ('emby_publications', 'managed_routes', 'managed_route_lines'):
    print(table + '_rows=' + str(db.execute('SELECT count(*) FROM ' + table).fetchone()[0]))
print('feimu_publication_rows=' + str(db.execute("SELECT count(*) FROM emby_publications WHERE node_name='feimu'").fetchone()[0]))
print('feimu_route_rows=' + str(db.execute("SELECT count(*) FROM managed_routes WHERE slug='feimu'").fetchone()[0]))
print('feimu_line_rows=' + str(db.execute("SELECT count(*) FROM managed_route_lines WHERE route_slug='feimu'").fetchone()[0]))
PY
if find /etc/nginx/conf.d/embyproxy-publications -maxdepth 1 -type f -name '*.conf' | grep -q .; then
    echo publication_fragments=present
else
    echo publication_fragments=none
fi
