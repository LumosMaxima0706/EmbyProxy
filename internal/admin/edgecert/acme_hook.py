#!/usr/bin/python3
import json
import os
import pathlib
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = pathlib.Path('/var/lib/embyproxy-edge')
PENDING = ROOT / 'acme-pending.json'

def config():
    with open('/etc/embyproxy-edge/edge-agent.json') as f:
        return json.load(f)

def call(action, domain, value):
    cfg = config()
    endpoint = cfg['controller'].rstrip('/') + '/api/edge/acme/' + cfg['node_id'] + '/' + action
    if not endpoint.startswith('https://'):
        raise RuntimeError('controller must use HTTPS')
    data = json.dumps({'domain': domain, 'validation': value}).encode()
    request = urllib.request.Request(endpoint, data=data, headers={
        'Content-Type': 'application/json', 'X-EmbyProxy-Node-Credential': cfg['credential']})
    with urllib.request.urlopen(request, timeout=180) as response:
        if response.status != 200 or not json.load(response).get('ok'):
            raise RuntimeError('controller refused ACME operation')

def cleanup():
    if not PENDING.exists():
        return
    pending = json.loads(PENDING.read_text())
    for attempt in range(3):
        try:
            call('cleanup', pending['domain'], pending['validation'])
            PENDING.unlink(missing_ok=True)
            return
        except urllib.error.HTTPError as error:
            code = json.loads(error.read()).get('error')
            if code == 'ACME_LEASE_MISMATCH':
                # This hook never writes TXT without owning the central lease.
                PENDING.unlink(missing_ok=True)
                return
            if attempt == 2:
                raise RuntimeError('ACME cleanup rejected: ' + str(code)) from None
        except Exception:
            if attempt == 2:
                raise RuntimeError('ACME cleanup failed; pending challenge retained') from None
        time.sleep(5)

def propagated(domain, value):
    query = urllib.parse.urlencode({'name': '_acme-challenge.' + domain, 'type': 'TXT'})
    for base in ('https://dns.google/resolve?', 'https://cloudflare-dns.com/dns-query?'):
        req = urllib.request.Request(base + query, headers={'Accept': 'application/dns-json'})
        with urllib.request.urlopen(req, timeout=10) as response:
            doc = json.load(response)
        if doc.get('Status') != 0 or value not in [str(x.get('data', '')).strip('"') for x in doc.get('Answer', []) if x.get('type') == 16]:
            return False
    return True

def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ('present', 'cleanup', 'recover'):
        raise RuntimeError('invalid hook action')
    if sys.argv[1] == 'recover':
        cleanup()
        return
    domain = os.environ.get('CERTBOT_DOMAIN', '').strip().lower().rstrip('.')
    value = os.environ.get('CERTBOT_VALIDATION', '').strip()
    expected = pathlib.Path('/etc/embyproxy-edge/business-domain').read_text().strip()
    if domain != expected or not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', value):
        raise RuntimeError('unauthorized challenge')
    if sys.argv[1] == 'cleanup':
        if PENDING.exists() and json.loads(PENDING.read_text()) != {'domain': domain, 'validation': value}:
            raise RuntimeError('cleanup challenge mismatch')
        cleanup()
        return
    cleanup()
    tmp = PENDING.with_suffix('.tmp')
    tmp.write_text(json.dumps({'domain': domain, 'validation': value}))
    tmp.chmod(0o600)
    tmp.replace(PENDING)
    try:
        deadline = time.monotonic() + 240
        while True:
            try:
                call('present', domain, value)
                break
            except urllib.error.HTTPError as error:
                code = json.loads(error.read()).get('error')
                if code != 'ACME_LEASE_CONFLICT' or time.monotonic() >= deadline:
                    raise RuntimeError('ACME present rejected: ' + str(code)) from None
                time.sleep(10)
        deadline = time.monotonic() + 240
        while time.monotonic() < deadline:
            try:
                if propagated(domain, value):
                    return
            except Exception:
                pass
            time.sleep(5)
        raise RuntimeError('TXT propagation timed out')
    except Exception:
        cleanup()
        raise

if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print('ACME hook failed: ' + str(error), file=sys.stderr)
        sys.exit(1)
