#!/usr/bin/python3
import fcntl
import grp
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time
import urllib.parse

ROOT = pathlib.Path('/var/lib/embyproxy-edge')
TOOLS = pathlib.Path('/usr/local/lib/embyproxy-edge')

def run(args, **kwargs):
    os.environ.setdefault('HOME', '/root')
    return subprocess.run(args, check=True, timeout=4200, **kwargs)

def output(args):
    return subprocess.check_output(args, timeout=30)

def status(state):
    tmp = ROOT / 'certificate-status.tmp'
    tmp.write_text(json.dumps({'state': state, 'updated_at': int(time.time())}))
    tmp.chmod(0o600)
    tmp.replace(ROOT / 'certificate-status.json')

def deploy(domain, origin, live):
    chain = live / 'fullchain.pem'
    key = live / 'privkey.pem'
    matched = output(['openssl', 'x509', '-in', str(chain), '-noout', '-checkhost', domain])
    if b'does match certificate' not in matched:
        raise RuntimeError('certificate hostname mismatch')
    run(['openssl', 'x509', '-in', str(chain), '-noout', '-checkend', '86400'], stdout=subprocess.DEVNULL)
    certpub = output(['openssl', 'x509', '-in', str(chain), '-pubkey', '-noout'])
    keypub = output(['openssl', 'pkey', '-in', str(key), '-pubout'])
    if certpub != keypub:
        raise RuntimeError('certificate and private key do not match')
    run(['openssl', 'verify', '-CApath', '/etc/ssl/certs', '-untrusted', str(live / 'chain.pem'), str(live / 'cert.pem')], stdout=subprocess.DEVNULL)
    gid = grp.getgrnam('caddy').gr_gid
    os.chown(ROOT, 0, gid)
    ROOT.chmod(0o710)
    tls = ROOT / 'tls'
    tls.mkdir(exist_ok=True)
    os.chown(tls, 0, gid)
    tls.chmod(0o750)
    digest = hashlib.sha256(chain.read_bytes()).hexdigest()[:24]
    target = tls / ('business-' + digest)
    target.mkdir(exist_ok=True)
    os.chown(target, 0, gid)
    target.chmod(0o750)
    for src, name in ((chain, 'fullchain.pem'), (key, 'privkey.pem')):
        dst = target / name
        tmp = target / (name + '.tmp')
        shutil.copyfile(src, tmp)
        os.chown(tmp, 0, gid)
        tmp.chmod(0o640)
        tmp.replace(dst)
    site = '  encode gzip\n  @isolated path /__isolated-media/*\n  respond @isolated 404\n  reverse_proxy 127.0.0.1:18080\n'
    text = origin + ' {\n' + site + '}\n' if origin != domain else ''
    text += domain + ' {\n  tls ' + str(target / 'fullchain.pem') + ' ' + str(target / 'privkey.pem') + '\n' + site + '}\n'
    conf = pathlib.Path('/etc/caddy/Caddyfile')
    previous = conf.read_bytes()
    if previous == text.encode():
        run(['curl', '--fail', '--silent', '--show-error', '--noproxy', '*', '--max-time', '15', '--resolve', domain + ':443:127.0.0.1', 'https://' + domain + '/health'], stdout=subprocess.DEVNULL)
        print('BUSINESS TLS: already current')
        return
    temp = conf.with_name('Caddyfile.embyproxy-certificate')
    temp.write_text(text)
    os.chown(temp, 0, gid)
    temp.chmod(0o640)
    try:
        run(['caddy', 'fmt', '--overwrite', str(temp)])
        run(['caddy', 'validate', '--config', str(temp), '--adapter', 'caddyfile'])
        temp.replace(conf)
        try:
            run(['systemctl', 'reload', 'caddy.service'])
            run(['curl', '--fail', '--silent', '--show-error', '--noproxy', '*', '--max-time', '15', '--resolve', domain + ':443:127.0.0.1', 'https://' + domain + '/health'], stdout=subprocess.DEVNULL)
        except Exception:
            temp.write_bytes(previous)
            os.chown(temp, 0, gid)
            temp.chmod(0o640)
            temp.replace(conf)
            run(['systemctl', 'reload', 'caddy.service'])
            raise RuntimeError('certificate reload failed; previous configuration restored') from None
    finally:
        temp.unlink(missing_ok=True)
    print('BUSINESS TLS: certificate loaded and HTTPS verified')

def main():
    if sys.argv[1:] not in ([], ['--staging'], ['--dry-run']):
        raise RuntimeError('invalid manager arguments')
    staging = sys.argv[1:] == ['--staging']
    ROOT.mkdir(exist_ok=True)
    with open(ROOT / 'certificate.lock', 'a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError('certificate task already running') from None
        marker = ROOT / 'caddy-managed'
        if 'managed_by=embyproxy-edge' not in marker.read_text().splitlines():
            raise RuntimeError('Caddy ownership not confirmed')
        domain = pathlib.Path('/etc/embyproxy-edge/business-domain').read_text().strip()
        # Origin is recorded in the project ownership marker, never from a shell expression.
        origin = next(x.split('=', 1)[1] for x in marker.read_text().splitlines() if x.startswith('domain='))
        for host in (domain, origin):
            if not re.fullmatch(r'[A-Za-z0-9.-]+', host):
                raise RuntimeError('invalid certificate hostname')
        run([sys.executable, str(TOOLS / 'acme_hook.py'), 'recover'])
        acme = ROOT / ('acme-staging' if staging else 'acme')
        acme.mkdir(mode=0o700, exist_ok=True)
        hook = str(TOOLS / 'acme_hook.py')
        args = ['certbot', 'certonly', '--non-interactive', '--agree-tos', '--register-unsafely-without-email',
                '--manual', '--preferred-challenges', 'dns', '--manual-auth-hook', hook + ' present',
                '--manual-cleanup-hook', hook + ' cleanup', '--cert-name', 'embyproxy-business',
                '--keep-until-expiring', '--config-dir', str(acme / 'config'), '--work-dir', str(acme / 'work'),
                '--logs-dir', str(acme / 'logs'), '-d', domain]
        if sys.argv[1:] == ['--dry-run']:
            try:
                run(['certbot', 'renew', '--dry-run', '--cert-name', 'embyproxy-business', '--non-interactive', '--config-dir', str(acme / 'config'), '--work-dir', str(acme / 'work'), '--logs-dir', str(acme / 'logs')])
            finally:
                run([sys.executable, hook, 'recover'])
            print('RENEWAL DRY-RUN: PASS (production certificate unchanged)')
            return
        if staging:
            args.append('--staging')
        else:
            status('preparing')
        try:
            run(args)
        finally:
            run([sys.executable, hook, 'recover'])
        if staging:
            print('ACME STAGING: PASS (not loaded into Caddy)')
            return
        deploy(domain, origin, acme / 'config/live/embyproxy-business')
        status('ready')

if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        if '--staging' not in sys.argv and '--dry-run' not in sys.argv:
            status('failed')
        print('BUSINESS TLS: FAIL (' + type(error).__name__ + ')', file=sys.stderr)
        sys.exit(1)
