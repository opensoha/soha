#!/usr/bin/env python3
"""Disposable, loopback-only Compose lab for the three managed proxy engines."""

import argparse
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request


STATE = Path(os.environ.get('SOHA_PROXY_LAB_STATE', '/opt/opensoha/proxy-runtime-lab'))
PROJECT = 'soha-proxy-runtime-lab'
CORE_IMAGE = 'soha-proxy-lab-core:20260927'
ENGINES = {
    'mihomo': ('proxy-mihomo', 27890, 7890),
    'sing-box': ('proxy-sing-box', 27891, 7890),
    'v2ray': ('proxy-v2ray', 27892, 10808),
}


def run(*args):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL)


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(value if isinstance(value, str) else json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def certificate(name, uri=None):
    folder = STATE / 'tls' / name
    folder.mkdir(parents=True, exist_ok=True, mode=0o700)
    if (folder / 'cert.pem').exists():
        run('openssl', 'x509', '-in', str(folder / 'cert.pem'), '-checkend', '86400', '-noout')
        return
    ca = STATE / 'tls'
    extensions = 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\n'
    if uri:
        extensions += 'extendedKeyUsage=clientAuth\nsubjectAltName=URI:' + uri + '\n'
    else:
        extensions += 'extendedKeyUsage=serverAuth\nsubjectAltName=DNS:control,DNS:ingest,IP:127.0.0.1\n'
    write(folder / 'extensions.cnf', extensions)
    run('openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(folder / 'key.pem'))
    run('openssl', 'req', '-new', '-key', str(folder / 'key.pem'), '-subj', '/CN=Soha proxy lab ' + name, '-out', str(folder / 'request.pem'))
    run('openssl', 'x509', '-req', '-in', str(folder / 'request.pem'), '-CA', str(ca / 'ca.pem'), '-CAkey', str(ca / 'ca-key.pem'),
        '-set_serial', str(secrets.randbits(120)), '-days', '90', '-extfile', str(folder / 'extensions.cnf'), '-out', str(folder / 'cert.pem'))
    (folder / 'key.pem').chmod(0o600)


def prepare():
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    STATE.chmod(0o700)
    secrets_file = STATE / 'secrets.json'
    if not secrets_file.exists():
        write(secrets_file, {key: secrets.token_hex(32) for key in
                             ('core_database', 'ingest_database', 'admin', 'jwt', 'runner', 'webhook', 'encryption')})
    ca = STATE / 'tls'
    ca.mkdir(exist_ok=True, mode=0o700)
    if not (ca / 'ca.pem').exists():
        run('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
            '-keyout', str(ca / 'ca-key.pem'), '-out', str(ca / 'ca.pem'), '-days', '90',
            '-subj', '/CN=Disposable Soha proxy lab CA', '-addext', 'basicConstraints=critical,CA:TRUE',
            '-addext', 'keyUsage=critical,keyCertSign,cRLSign')
        (ca / 'ca-key.pem').chmod(0o600)
    else:
        run('openssl', 'x509', '-in', str(ca / 'ca.pem'), '-checkend', '86400', '-noout')
    certificate('control')
    certificate('ingest')
    certificate('core-query', 'spiffe://opensoha.local/network-ingest/core/proxy-lab')
    certificate('control-query', 'spiffe://opensoha.local/network-ingest/network-control/proxy-lab')
    for _, (runtime_id, _, _) in ENGINES.items():
        certificate(runtime_id, 'spiffe://opensoha.local/network-control/proxy/' + runtime_id)
        certificate(runtime_id + '-ingest', 'spiffe://opensoha.local/network-ingest/proxy/' + runtime_id)
    secret_dir = STATE / 'secrets'
    secret_dir.mkdir(exist_ok=True, mode=0o700)
    for engine, (runtime_id, _, _) in ENGINES.items():
        if engine != 'v2ray' and not (secret_dir / (runtime_id + '-controller')).exists():
            write(secret_dir / (runtime_id + '-controller'), secrets.token_hex(32) + '\n')
    make_compose()


def query_env(name):
    return {
        'SOHA_NETWORK_INGEST_QUERY_URL': 'https://ingest:8083',
        'SOHA_NETWORK_INGEST_QUERY_CA_FILE': '/lab/tls/ca.pem',
        'SOHA_NETWORK_INGEST_QUERY_CERT_FILE': '/lab/tls/' + name + '/cert.pem',
        'SOHA_NETWORK_INGEST_QUERY_KEY_FILE': '/lab/tls/' + name + '/key.pem',
        'SOHA_NETWORK_INGEST_QUERY_SERVER_NAME': 'ingest',
    }


def make_compose():
    credentials = json.loads((STATE / 'secrets.json').read_text())
    enrollments_file = STATE / 'enrollments.json'
    enrollments = json.loads(enrollments_file.read_text()) if enrollments_file.exists() else {}
    services = {}
    for name, db, password in (
        ('postgres', 'soha', credentials['core_database']),
        ('ingest-postgres', 'soha_ingest', credentials['ingest_database']),
    ):
        services[name] = {
            'image': 'pgvector/pgvector:0.8.5-pg18-trixie',
            'environment': {'POSTGRES_DB': db, 'POSTGRES_USER': 'pgsql', 'POSTGRES_PASSWORD': password},
            'volumes': [name + '-data:/var/lib/postgresql'],
            'healthcheck': {'test': ['CMD-SHELL', 'pg_isready -U pgsql -d ' + db], 'interval': '5s', 'timeout': '3s', 'retries': 30},
        }
    services['traffic-target'] = {
        'image': 'python:3.12-alpine', 'user': '0:0', 'cap_drop': ['ALL'],
        'security_opt': ['no-new-privileges:true'], 'read_only': True,
        'tmpfs': ['/tmp'],
        'command': ['sh', '-c', 'dd if=/dev/zero of=/tmp/blob bs=1024 count=32768 2>/dev/null && printf ok >/tmp/healthz && exec python3 -m http.server 8088 --directory /tmp'],
        'healthcheck': {'test': ['CMD-SHELL', 'wget -q -O - http://127.0.0.1:8088/healthz >/dev/null'],
                        'interval': '5s', 'timeout': '3s', 'retries': 12},
    }
    common = {'SOHA_CONFIG_FILE': '/app/configs/config.yaml',
              'SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY': credentials['encryption']}
    safety = {'user': '0:0', 'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'], 'init': True}
    tls_volume = str(STATE / 'tls') + ':/lab/tls:ro'
    services['core'] = {
        **safety, 'image': CORE_IMAGE,
        'environment': {**common, **query_env('core-query'),
                        'SOHA_DATABASE_HOST': 'postgres', 'SOHA_DATABASE_PASSWORD': credentials['core_database'],
                        'SOHA_AUTH_DEV_PRINCIPAL_PASSWORD': credentials['admin'],
                        'SOHA_AUTH_LOGIN_VERIFICATION_SLIDER_ENABLED': 'false',
                        'SOHA_AUTH_JWT_SECRET': credentials['jwt'],
                        'SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN': credentials['runner'],
                        'SOHA_MONITORING_WEBHOOK_TOKEN': credentials['webhook']},
        'volumes': [tls_volume, 'core-data:/app/data'],
        'ports': ['127.0.0.1:18080:8080'],
        'depends_on': {'postgres': {'condition': 'service_healthy'}},
        'healthcheck': {'test': ['CMD-SHELL', 'wget -q -O - http://127.0.0.1:8080/readyz >/dev/null'],
                        'interval': '10s', 'timeout': '5s', 'retries': 24},
    }
    for name, binary, db_host, password, port in (
        ('control', '/app/network-control', 'postgres', credentials['core_database'], 18082),
        ('ingest', '/app/ingest', 'ingest-postgres', credentials['ingest_database'], 18083),
    ):
        prefix = 'SOHA_NETWORK_CONTROL' if name == 'control' else 'SOHA_INGEST'
        env = {**common, prefix + '_DATABASE_HOST': db_host, prefix + '_DATABASE_PASSWORD': password,
               prefix + '_TLS_CERT_FILE': '/lab/tls/' + name + '/cert.pem',
               prefix + '_TLS_KEY_FILE': '/lab/tls/' + name + '/key.pem',
               prefix + '_TLS_CLIENT_CA_FILE': '/lab/tls/ca.pem'}
        if name == 'control':
            env.update(query_env('control-query'))
        services[name] = {
            **safety, 'image': CORE_IMAGE, 'command': [binary], 'environment': env,
            'volumes': [tls_volume], 'ports': ['127.0.0.1:' + str(port) + ':808' + ('2' if name == 'control' else '3')],
            'depends_on': {db_host: {'condition': 'service_healthy'},
                           **({'core': {'condition': 'service_healthy'}} if name == 'control' else {})},
            'healthcheck': {'test': ['CMD-SHELL', 'wget --no-check-certificate -q -O - https://127.0.0.1:808' + ('2' if name == 'control' else '3') + '/readyz >/dev/null'],
                            'interval': '10s', 'timeout': '5s', 'retries': 18},
        }
    for engine, (runtime_id, host_port, proxy_port) in ENGINES.items():
        env = {
            'SOHA_PROXY_RUNTIME_ID': runtime_id, 'SOHA_PROXY_CONTROL_URL': 'https://control:8082',
            'SOHA_PROXY_CONTROL_SERVER_NAME': 'control',
            'SOHA_PROXY_CONTROL_CA_FILE': '/lab/tls/ca.pem',
            'SOHA_PROXY_CONTROL_CERT_FILE': '/lab/tls/' + runtime_id + '/cert.pem',
            'SOHA_PROXY_CONTROL_KEY_FILE': '/lab/tls/' + runtime_id + '/key.pem',
            'SOHA_PROXY_INGEST_URL': 'https://ingest:8083',
            'SOHA_PROXY_INGEST_SERVER_NAME': 'ingest',
            'SOHA_PROXY_INGEST_CA_FILE': '/lab/tls/ca.pem',
            'SOHA_PROXY_INGEST_CERT_FILE': '/lab/tls/' + runtime_id + '-ingest/cert.pem',
            'SOHA_PROXY_INGEST_KEY_FILE': '/lab/tls/' + runtime_id + '-ingest/key.pem',
            'SOHA_PROXY_CONTROLLER_URL': 'http://127.0.0.1:' + ('10085' if engine == 'v2ray' else '9090'),
        }
        if engine != 'v2ray':
            env['SOHA_PROXY_CONTROLLER_SECRET_FILE'] = '/lab/secrets/' + runtime_id + '-controller'
        if runtime_id in enrollments:
            env.update({'SOHA_PROXY_ENROLLMENT_ID': enrollments[runtime_id]['id'],
                        'SOHA_PROXY_ENROLLMENT_CHALLENGE_ID': enrollments[runtime_id]['challengeId'],
                        'SOHA_PROXY_ENROLLMENT_TOKEN_FILE': '/lab/secrets/' + runtime_id + '-token'})
        services[runtime_id] = {
            **safety, 'image': 'soha-proxy-lab-' + engine + ':20260927', 'environment': env,
            'volumes': [tls_volume, str(STATE / 'secrets') + ':/lab/secrets:ro', runtime_id + '-data:/var/lib/soha-proxy-runtime'],
            'ports': ['127.0.0.1:' + str(host_port) + ':' + str(proxy_port)],
            'depends_on': {'control': {'condition': 'service_healthy'}, 'ingest': {'condition': 'service_healthy'}},
        }
    volumes = {name + '-data': {} for name in ('postgres', 'ingest-postgres', 'core', *(x[0] for x in ENGINES.values()))}
    write(STATE / 'compose.json', {'name': PROJECT, 'services': services, 'volumes': volumes})


def compose(*args):
    subprocess.run(['docker', 'compose', '-f', str(STATE / 'compose.json'), *args], check=True)


def request(method, path, token=None, body=None):
    data = None if body is None else json.dumps(body).encode()
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request('http://127.0.0.1:18080/api/v1' + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f'{method} {path}: HTTP {error.code}') from None
    return result.get('data', result)


def admin_token():
    password = json.loads((STATE / 'secrets.json').read_text())['admin']
    login = request('POST', '/auth/login', body={'login': 'opensoha@soha.local', 'password': password})
    return login['tokens']['accessToken']


def configurations():
    return {
        'mihomo': 'mixed-port: 7890\nallow-lan: true\nbind-address: "*"\nmode: rule\nlog-level: warning\nrules:\n  - MATCH,DIRECT\n',
        'sing-box': json.dumps({'log': {'level': 'warn'}, 'inbounds': [{'type': 'mixed', 'tag': 'mixed-in',
                        'listen': '0.0.0.0', 'listen_port': 7890}], 'outbounds': [{'type': 'direct', 'tag': 'direct'}],
                        'route': {'final': 'direct'}}),
        'v2ray': json.dumps({'log': {'loglevel': 'warning'}, 'inbounds': [{'tag': 'proxy', 'listen': '0.0.0.0',
                        'port': 10808, 'protocol': 'socks', 'settings': {'auth': 'noauth', 'udp': False}}],
                        'outbounds': [{'protocol': 'freedom', 'tag': 'direct'}]}),
    }


def seed():
    token = admin_token()
    request('POST', '/network-access/policies/compile', token, {})
    enrollments = {}
    for engine, (runtime_id, _, _) in ENGINES.items():
        try:
            item = request('GET', '/network-access/proxy-instances/' + runtime_id, token)
        except RuntimeError as error:
            if 'HTTP 404' not in str(error):
                raise
            item = request('POST', '/network-access/proxy-instances', token,
                           {'id': runtime_id, 'name': engine + ' · middleware lab', 'host': 'middleware', 'engine': engine})
        if item['desiredRevision'] == 0:
            item = request('PUT', '/network-access/proxy-instances/' + runtime_id + '/configuration', token,
                           {'expectedRevision': 0, 'enabled': True, 'content': configurations()[engine]})
        if item['status'] == 'unregistered':
            enrollment = request('POST', '/network-access/enrollments', token,
                                 {'runtimeId': runtime_id, 'runtimeKind': 'proxy', 'deviceId': runtime_id,
                                  'subjectId': runtime_id, 'ttlSeconds': 600})
            enrollments[runtime_id] = {'id': enrollment['enrollment']['id'],
                                       'challengeId': enrollment['enrollment']['challengeId']}
            write(STATE / 'secrets' / (runtime_id + '-token'), enrollment['token'] + '\n')
        print(f'{runtime_id}: desired revision {item["desiredRevision"]}, status={item["status"]}')
    write(STATE / 'enrollments.json', enrollments)
    make_compose()


def verify():
    token = admin_token()
    for _, (runtime_id, _, _) in ENGINES.items():
        item = request('GET', '/network-access/proxy-instances/' + runtime_id, token)
        traffic = request('GET', '/network-access/proxy-instances/' + runtime_id + '/traffic', token)
        connections = request('GET', '/network-access/proxy-instances/' + runtime_id + '/connections', token)
        print(f'{runtime_id}: status={item["status"]} revision={item["observedRevision"]}/{item["desiredRevision"]} '
              f'samples={len(traffic.get("samples", []))} connections={connections.get("state")}')
        expected_connections = 'unsupported' if item['engine'] == 'v2ray' else 'available'
        if (item['status'] != 'online' or item['observedRevision'] != item['desiredRevision']
                or not item.get('engineVersion') or not traffic.get('samples')
                or connections.get('state') != expected_connections):
            raise RuntimeError(runtime_id + ' did not pass runtime verification')
    context = ssl.create_default_context(cafile=str(STATE / 'tls' / 'ca.pem'))
    context.load_cert_chain(str(STATE / 'tls' / 'core-query' / 'cert.pem'), str(STATE / 'tls' / 'core-query' / 'key.pem'))
    with urllib.request.urlopen('https://127.0.0.1:18083/api/ingest/v1/query/summary', context=context, timeout=10) as response:
        print('ingest query: HTTP', response.status)


def exercise():
    token = admin_token()
    baseline = {}
    for _, (runtime_id, _, _) in ENGINES.items():
        samples = request('GET', '/network-access/proxy-instances/' + runtime_id + '/traffic', token)['samples']
        baseline[runtime_id] = samples[-1]['downloadTotal'] if samples else 0
    for engine, (_, host_port, _) in ENGINES.items():
        command = ['curl', '--fail', '--silent', '--show-error', '--noproxy', '', '--max-time', '20',
                   '--output', '/dev/null']
        if engine == 'v2ray':
            command += ['--socks5-hostname', '127.0.0.1:' + str(host_port)]
        else:
            command += ['--proxy', 'http://127.0.0.1:' + str(host_port)]
        run(*(command + ['http://traffic-target:8088/blob']))
        print(engine + ': proxied 32 MiB through the isolated traffic target')
    for _ in range(20):
        pending = []
        for _, (runtime_id, _, _) in ENGINES.items():
            samples = request('GET', '/network-access/proxy-instances/' + runtime_id + '/traffic', token)['samples']
            if not samples or samples[-1]['downloadTotal'] - baseline[runtime_id] < 32 * 1024 * 1024:
                pending.append(runtime_id)
        if not pending:
            print('all three engines reported the proxied bytes to Soha')
            return
        time.sleep(2)
    raise RuntimeError('traffic counters did not advance for: ' + ', '.join(pending))


def close_active():
    token = admin_token()
    for engine in ('mihomo', 'sing-box'):
        runtime_id = ENGINES[engine][0]
        snapshot = request('GET', '/network-access/proxy-instances/' + runtime_id + '/connections', token)
        active = [item for item in snapshot['connections'] if item['destination'] == 'traffic-target']
        if not active:
            raise RuntimeError(runtime_id + ' has no active traffic-target connection')
        command = request('POST', '/network-access/proxy-instances/' + runtime_id + '/connections/' +
                          urllib.parse.quote(active[0]['id'], safe='') + '/close', token)
        if command['status'] != 'pending':
            raise RuntimeError(runtime_id + ' close command was not queued')
        print(runtime_id + ': close command queued')


def wait_runtime(token, runtime_id, desired_revision, status):
    for _ in range(20):
        item = request('GET', '/network-access/proxy-instances/' + runtime_id, token)
        if item['desiredRevision'] == desired_revision and item['status'] == status:
            if status != 'online' or item['observedRevision'] == desired_revision:
                return item
        time.sleep(2)
    raise RuntimeError(runtime_id + ' did not reach ' + status)


def exercise_configuration():
    token = admin_token()
    invalid = {
        'mihomo': 'mixed-port: [invalid]\n',
        'sing-box': '{"inbounds":"invalid"}',
        'v2ray': '{"inbounds":"invalid"}',
    }
    for engine, (runtime_id, host_port, _) in ENGINES.items():
        path = '/network-access/proxy-instances/' + runtime_id
        original = request('GET', path, token)
        if original['status'] != 'online' or original['observedRevision'] != original['desiredRevision']:
            raise RuntimeError(runtime_id + ' is not ready for configuration exercise')
        revision = original['desiredRevision']
        try:
            request('PUT', path + '/configuration', token,
                    {'expectedRevision': revision - 1, 'enabled': True, 'content': configurations()[engine]})
        except RuntimeError as error:
            if 'HTTP 409' not in str(error):
                raise
        else:
            raise RuntimeError(runtime_id + ' accepted stale configuration revision')
        changed = request('PUT', path + '/configuration', token,
                          {'expectedRevision': revision, 'enabled': True, 'content': invalid[engine]})
        try:
            if invalid[engine] in json.dumps(changed) or invalid[engine] in json.dumps(request('GET', path, token)):
                raise RuntimeError(runtime_id + ' exposed configuration content in management response')
            query = "SELECT desired_content_encrypted FROM network_proxy_instances WHERE id = '" + runtime_id + "'"
            stored = subprocess.run(['docker', 'compose', '-f', str(STATE / 'compose.json'), 'exec', '-T',
                                     'postgres', 'psql', '-U', 'pgsql', '-d', 'soha', '-tAc', query],
                                    check=True, capture_output=True, text=True).stdout.strip()
            if not stored or invalid[engine] in stored or stored == invalid[engine]:
                raise RuntimeError(runtime_id + ' configuration is not encrypted at rest')
            failed = wait_runtime(token, runtime_id, revision + 1, 'degraded')
            if failed['observedRevision'] != revision or not failed.get('reasonCode'):
                raise RuntimeError(runtime_id + ' did not preserve prior configuration after rejection')
            command = ['curl', '--fail', '--silent', '--show-error', '--noproxy', '', '--max-time', '10', '--output', '/dev/null']
            if engine == 'v2ray':
                command += ['--socks5-hostname', '127.0.0.1:' + str(host_port)]
            else:
                command += ['--proxy', 'http://127.0.0.1:' + str(host_port)]
            run(*(command + ['http://traffic-target:8088/healthz']))
            print(runtime_id + ': stale revision rejected, content protected, invalid config degraded; old engine served traffic')
        finally:
            current = request('GET', path, token)
            recovered = request('PUT', path + '/configuration', token,
                                {'expectedRevision': current['desiredRevision'], 'enabled': True,
                                 'content': configurations()[engine]})
            wait_runtime(token, runtime_id, recovered['desiredRevision'], 'online')
            print(runtime_id + ': valid configuration restored')


def verify_permissions():
    admin = admin_token()
    role_id = 'proxy-lab-viewer'
    username = 'proxy-lab-viewer'
    permissions = ['network_access.proxy_instances.view', 'network_access.proxy_connections.view']
    roles = request('GET', '/access/roles', admin)['items']
    if not any(item['id'] == role_id for item in roles):
        request('POST', '/access/roles', admin,
                {'id': role_id, 'name': 'Proxy Lab Viewer', 'scope': 'custom', 'permissionKeys': permissions})
    secrets_file = STATE / 'secrets.json'
    credentials = json.loads(secrets_file.read_text())
    if 'viewer_password' not in credentials:
        credentials['viewer_password'] = secrets.token_hex(24)
        write(secrets_file, credentials)
    users = request('GET', '/access/users', admin)['items']
    if not any(item['username'] == username for item in users):
        request('POST', '/access/users', admin,
                {'username': username, 'email': username + '@soha.local',
                 'displayName': 'Proxy Lab Viewer', 'status': 'active', 'roleIds': [role_id],
                 'password': credentials['viewer_password']})
    login = request('POST', '/auth/login', body={'login': username + '@soha.local',
                                               'password': credentials['viewer_password']})
    viewer = login['tokens']['accessToken']
    snapshot = request('GET', '/access/permission-snapshot', viewer)
    if not all(key in snapshot['permissionKeys'] for key in permissions):
        raise RuntimeError('viewer role is missing proxy read permissions')
    if 'network_access.proxy_instances.update' in snapshot['permissionKeys'] or 'network_access.proxy_connections.close' in snapshot['permissionKeys']:
        raise RuntimeError('viewer role has proxy write permissions')
    for path in ('/network-access/proxy-instances',
                 '/network-access/proxy-instances/proxy-mihomo/traffic',
                 '/network-access/proxy-instances/proxy-mihomo/connections'):
        request('GET', path, viewer)
    for method, path, body in (
        ('PUT', '/network-access/proxy-instances/proxy-mihomo/configuration',
         {'expectedRevision': -1, 'enabled': True, 'content': 'invalid'}),
        ('POST', '/network-access/proxy-instances/proxy-mihomo/connections/connection-1/close', None),
    ):
        try:
            request(method, path, viewer, body)
        except RuntimeError as error:
            if 'HTTP 403' not in str(error):
                raise
        else:
            raise RuntimeError('viewer write request unexpectedly succeeded')
    print('read-only viewer: proxy reads succeeded; configuration update and connection close returned HTTP 403')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('prepare', 'start-base', 'seed', 'start-runtimes', 'exercise', 'exercise-config', 'close-active', 'verify', 'verify-permissions', 'status'))
    args = parser.parse_args()
    if args.action == 'prepare':
        prepare()
    elif args.action == 'start-base':
        compose('up', '-d', 'postgres', 'ingest-postgres', 'traffic-target', 'core', 'control', 'ingest')
    elif args.action == 'seed':
        seed()
    elif args.action == 'start-runtimes':
        compose('up', '-d', *(runtime_id for runtime_id, _, _ in ENGINES.values()))
    elif args.action == 'exercise':
        exercise()
    elif args.action == 'exercise-config':
        exercise_configuration()
    elif args.action == 'verify-permissions':
        verify_permissions()
    elif args.action == 'close-active':
        close_active()
    elif args.action == 'verify':
        verify()
    elif args.action == 'status':
        compose('ps')


if __name__ == '__main__':
    main()
