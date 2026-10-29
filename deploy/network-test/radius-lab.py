#!/usr/bin/env python3
"""Isolated software RADIUS lab; does not certify physical NAS or OS networking."""

import argparse
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import subprocess
import time
import urllib.error
import urllib.request
import uuid


PROJECT = 'soha-task53-radius-lab'
STATE = Path('/opt/opensoha/task53-radius-lab')
CORE_IMAGE = 'soha-task53-core:20261014'
RADIUS_IMAGE = 'soha-task53-radius:20261014'
EAP_IMAGE = 'soha-task53-eap-client:20261014'
API = 'http://127.0.0.1:18180/api/v1'
RUNTIME_ID = 'radius-task53-lab'
NAS_ID = 'nas-task53-software'
DEVICE_ID = 'endpoint-task53-eap'


def run(*args, input=None):
    return subprocess.run(args, input=input, check=True, capture_output=True, text=True).stdout


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(value if isinstance(value, str) else json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def owned_state(create=False):
    if not STATE.is_absolute() or STATE == Path('/') or STATE.is_symlink():
        raise RuntimeError('The lab needs an absolute, non-symlink state directory')
    marker = STATE / 'lab.json'
    if marker.exists():
        if json.loads(marker.read_text()) != {'project': PROJECT, 'version': 1}:
            raise RuntimeError('The state directory belongs to a different lab')
    elif create and (not STATE.exists() or not any(STATE.iterdir())):
        STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
        STATE.chmod(0o700)
        write(marker, {'project': PROJECT, 'version': 1})
    else:
        raise RuntimeError('Refusing to use an unrecognized state directory')


def certificate(name, subject, uri=None, host=None):
    folder = STATE / 'tls' / name
    if (folder / 'tls.crt').exists():
        run('openssl', 'x509', '-in', str(folder / 'tls.crt'), '-checkend', '86400', '-noout')
        return
    folder.mkdir(parents=True, exist_ok=True, mode=0o700)
    extensions = 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\n'
    extensions += 'subjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n'
    if uri:
        extensions += 'extendedKeyUsage=clientAuth\nsubjectAltName=URI:' + uri + '\n'
    else:
        extensions += 'extendedKeyUsage=serverAuth\nsubjectAltName=DNS:' + host + ',IP:127.0.0.1\n'
    write(folder / 'extensions.cnf', extensions)
    run('openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(folder / 'tls.key'))
    (folder / 'tls.key').chmod(0o600)
    run('openssl', 'req', '-new', '-key', str(folder / 'tls.key'), '-subj', '/CN=' + subject, '-out', str(folder / 'request.pem'))
    ca = STATE / 'tls'
    run('openssl', 'x509', '-req', '-in', str(folder / 'request.pem'), '-CA', str(ca / 'ca.crt'),
        '-CAkey', str(ca / 'ca.key'), '-set_serial', str(secrets.randbits(120)), '-days', '30',
        '-extfile', str(folder / 'extensions.cnf'), '-out', str(folder / 'tls.crt'))
    write(folder / 'ca.crt', (ca / 'ca.crt').read_text())
    (folder / 'extensions.cnf').unlink()
    (folder / 'request.pem').unlink()


def prepare():
    owned_state(create=True)
    credentials_file = STATE / 'secrets.json'
    if not credentials_file.exists():
        write(credentials_file, {key: secrets.token_hex(32) for key in
                                 ('database', 'ingest_database', 'admin', 'jwt', 'runner', 'webhook', 'encryption')})
    ca = STATE / 'tls'
    ca.mkdir(exist_ok=True, mode=0o700)
    if not (ca / 'ca.crt').exists():
        run('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
            '-keyout', str(ca / 'ca.key'), '-out', str(ca / 'ca.crt'), '-days', '30',
            '-subj', '/CN=Disposable Task53 Radius Lab CA', '-addext', 'basicConstraints=critical,CA:TRUE,pathlen:0',
            '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-addext', 'subjectKeyIdentifier=hash')
        (ca / 'ca.key').chmod(0o600)
    run('openssl', 'x509', '-in', str(ca / 'ca.crt'), '-checkend', '86400', '-noout')
    for name in ('network-control', 'ingest', 'freeradius'):
        certificate(name, name, host=name)
    for name, uri in {
        'freeradius-control': 'network-control/nas/' + RUNTIME_ID,
        'freeradius-ingest': 'network-ingest/freeradius/' + RUNTIME_ID,
        'core-query': 'network-ingest/core/task53-lab',
        'control-query': 'network-ingest/network-control/task53-lab',
    }.items():
        certificate(name, name, uri='spiffe://opensoha.local/' + uri)
    if not (STATE / 'secrets' / 'shared-secret').exists():
        write(STATE / 'secrets' / 'shared-secret', secrets.token_hex(32) + '\n')
    (STATE / 'tls' / 'eap-client').mkdir(exist_ok=True, mode=0o700)
    prepare_eap_issuer()
    make_compose()
    print('Prepared isolated lab state; credentials were not printed')


def prepare_eap_issuer():
    folder = STATE / 'tls' / 'eap-issuer'
    folder.mkdir(exist_ok=True, mode=0o700)
    if not (folder / 'ca.crt').exists():
        run('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes',
            '-keyout', str(folder / 'ca.key'), '-out', str(folder / 'ca.crt'), '-days', '30',
            '-subj', '/CN=Disposable Task53 Dedicated EAP Issuer', '-addext', 'basicConstraints=critical,CA:TRUE,pathlen:0',
            '-addext', 'keyUsage=critical,keyCertSign,cRLSign', '-addext', 'subjectKeyIdentifier=hash')
        (folder / 'ca.key').chmod(0o600)
    run('openssl', 'x509', '-in', str(folder / 'ca.crt'), '-checkend', '86400', '-noout')
    # Preserve legacy fixtures while testing the separate EAP-only issuer.
    write(STATE / 'tls' / 'freeradius' / 'ca.crt', (STATE / 'tls' / 'ca.crt').read_text())
    write(STATE / 'tls' / 'freeradius' / 'client-ca.crt',
          (STATE / 'tls' / 'ca.crt').read_text() + (folder / 'ca.crt').read_text())


def query_env():
    return {'SOHA_NETWORK_INGEST_QUERY_URL': 'https://ingest:8083',
            'SOHA_NETWORK_INGEST_QUERY_CA_FILE': '/lab/query/ca.crt',
            'SOHA_NETWORK_INGEST_QUERY_CERT_FILE': '/lab/query/tls.crt',
            'SOHA_NETWORK_INGEST_QUERY_KEY_FILE': '/lab/query/tls.key',
            'SOHA_NETWORK_INGEST_QUERY_SERVER_NAME': 'ingest'}


def tls_mount(identity, target):
    return str(STATE / 'tls' / identity) + ':' + target + ':ro'


def compose_document(credentials):
    safety = {'user': '0:0', 'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'], 'init': True}
    common = {'SOHA_CONFIG_FILE': '/app/configs/config.yaml',
              'SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY': credentials['encryption']}
    services = {}
    for name, database, key in [('postgres', 'soha', 'database'), ('ingest-postgres', 'soha_ingest', 'ingest_database')]:
        services[name] = {'image': 'pgvector/pgvector:0.8.5-pg18-trixie',
                          'environment': {'POSTGRES_DB': database, 'POSTGRES_USER': 'pgsql', 'POSTGRES_PASSWORD': credentials[key]},
                          'volumes': [name + '-data:/var/lib/postgresql'],
                          'healthcheck': {'test': ['CMD-SHELL', 'pg_isready -U pgsql -d ' + database],
                                          'interval': '5s', 'timeout': '3s', 'retries': 24}}
    services['core'] = {
        **safety, 'image': CORE_IMAGE,
        'environment': {**common, **query_env(), 'SOHA_DATABASE_HOST': 'postgres',
                        'SOHA_DATABASE_PASSWORD': credentials['database'], 'SOHA_AUTH_DEV_PRINCIPAL_PASSWORD': credentials['admin'],
                        'SOHA_AUTH_LOGIN_VERIFICATION_SLIDER_ENABLED': 'false', 'SOHA_AUTH_JWT_SECRET': credentials['jwt'],
                        'SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN': credentials['runner'], 'SOHA_MONITORING_WEBHOOK_TOKEN': credentials['webhook']},
        'volumes': [tls_mount('core-query', '/lab/query'), 'core-data:/app/data'],
        'ports': ['127.0.0.1:18180:8080'], 'depends_on': {'postgres': {'condition': 'service_healthy'}},
        'healthcheck': {'test': ['CMD-SHELL', 'wget -q -O - http://127.0.0.1:8080/readyz >/dev/null'],
                        'interval': '5s', 'timeout': '3s', 'retries': 30},
    }
    for name, prefix, host, database, key, port in (
        ('network-control', 'SOHA_NETWORK_CONTROL', 'postgres', 'soha', 'database', 8082),
        ('ingest', 'SOHA_INGEST', 'ingest-postgres', 'soha_ingest', 'ingest_database', 8083),
    ):
        env = {**common, prefix + '_DATABASE_HOST': host, prefix + '_DATABASE_NAME': database,
               prefix + '_DATABASE_PASSWORD': credentials[key], prefix + '_TLS_CERT_FILE': '/lab/server/tls.crt',
               prefix + '_TLS_KEY_FILE': '/lab/server/tls.key', prefix + '_TLS_CLIENT_CA_FILE': '/lab/server/ca.crt'}
        volumes = [tls_mount(name, '/lab/server')]
        if name == 'network-control':
            env.update(query_env())
            env.update({'SOHA_NETWORK_CONTROL_EAP_ISSUER_CA_FILE': '/lab/eap-issuer/ca.crt',
                        'SOHA_NETWORK_CONTROL_EAP_ISSUER_KEY_FILE': '/lab/eap-issuer/ca.key'})
            volumes.append(tls_mount('control-query', '/lab/query'))
            volumes.append(tls_mount('eap-issuer', '/lab/eap-issuer'))
        services[name] = {
            **safety, 'image': CORE_IMAGE, 'command': ['/app/' + name],
            'environment': env, 'volumes': volumes,
            'ports': ['127.0.0.1:' + str(18180 + port - 8080) + ':' + str(port)],
            'depends_on': {host: {'condition': 'service_healthy'}, **({'core': {'condition': 'service_healthy'}} if name == 'network-control' else {})},
            'healthcheck': {'test': ['CMD-SHELL', 'wget --no-check-certificate -q -O - https://127.0.0.1:' + str(port) + '/readyz >/dev/null'],
                            'interval': '5s', 'timeout': '3s', 'retries': 30},
        }
    radius_env = {'SOHA_RADIUS_RUNTIME_ID': RUNTIME_ID, 'SOHA_RADIUS_NAS_ID': NAS_ID,
                  'SOHA_RADIUS_NAS_CIDR': '127.0.0.1/32', 'SOHA_RADIUS_SHARED_SECRET_FILE': '/lab/secrets/shared-secret',
                  'SOHA_RADIUS_CONTROL_URL': 'https://network-control:8082', 'SOHA_RADIUS_INGEST_URL': 'https://ingest:8083'}
    for kind, directory in [('CONTROL', 'control'), ('INGEST', 'ingest'), ('EAP', 'eap')]:
        for key, filename in [('KEY', 'tls.key'), ('CERT', 'tls.crt'), ('CA', 'ca.crt')]:
            radius_env['SOHA_RADIUS_' + kind + '_' + key + '_FILE'] = '/lab/' + directory + '/' + filename
    radius_env['SOHA_RADIUS_EAP_CA_FILE'] = '/lab/eap/client-ca.crt'
    secret_mount = str(STATE / 'secrets') + ':/lab/secrets:ro'
    services['freeradius'] = {
        'image': RADIUS_IMAGE, 'user': '0:0', 'environment': radius_env,
        'volumes': [secret_mount, tls_mount('freeradius-control', '/lab/control'),
                    tls_mount('freeradius-ingest', '/lab/ingest'), tls_mount('freeradius', '/lab/eap')],
        'depends_on': {name: {'condition': 'service_healthy'} for name in ('network-control', 'ingest')},
        'security_opt': ['no-new-privileges:true'],
        'healthcheck': {'test': ['CMD-SHELL', 'kill -0 1'], 'interval': '5s', 'timeout': '3s', 'retries': 12},
    }
    services['eap-client'] = {
        **safety, 'image': EAP_IMAGE, 'network_mode': 'service:freeradius', 'profiles': ['test'],
        'environment': {'SOHA_RADIUS_NAS_ID': NAS_ID, 'SOHA_RADIUS_SHARED_SECRET_FILE': '/lab/secrets/shared-secret',
                        'SOHA_RADIUS_EAP_TEST_CONFIG_FILE': '/run/soha-radius/eap-test/eapol-test.conf'},
        'volumes': [secret_mount, tls_mount('eap-client', '/run/soha-radius/eap-test')], 'read_only': True,
        'tmpfs': ['/tmp:size=16m,mode=1700'],
    }
    services['radius-client'] = {
        **safety, 'image': RADIUS_IMAGE, 'profiles': ['test'], 'network_mode': 'service:freeradius',
        'entrypoint': ['tail', '-f', '/dev/null'], 'volumes': [secret_mount],
    }
    services['eap-issued-client'] = {
        **services['eap-client'],
        'volumes': [secret_mount, tls_mount('eap-issued-client', '/run/soha-radius/eap-test')],
    }
    return {'name': PROJECT, 'services': services, 'volumes': {name + '-data': {} for name in ('postgres', 'ingest-postgres', 'core')}}


def make_compose():
    write(STATE / 'compose.json', compose_document(json.loads((STATE / 'secrets.json').read_text())))


def compose(*args, input=None):
    return run('docker', 'compose', '-f', str(STATE / 'compose.json'), *args, input=input)


def runtime_credentials():
    for image in (CORE_IMAGE, RADIUS_IMAGE, EAP_IMAGE):
        if run('docker', 'image', 'inspect', image, '--format', '{{.Architecture}}').strip() != 'amd64':
            raise RuntimeError('Build the Linux amd64 lab images before starting the lab')
    uid = int(run('docker', 'run', '--rm', '--entrypoint', 'id', RADIUS_IMAGE, '-u', 'freerad').strip())
    gid = int(run('docker', 'run', '--rm', '--entrypoint', 'id', RADIUS_IMAGE, '-g', 'freerad').strip())
    for name in ('freeradius', 'freeradius-control', 'freeradius-ingest'):
        folder = STATE / 'tls' / name
        os.chown(folder, uid, gid)
        for path in folder.iterdir():
            os.chown(path, uid, gid)
            path.chmod(0o600)


def request(method, path, token=None, body=None, context=None, origin=API):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    req = urllib.request.Request(origin + path, data=None if body is None else json.dumps(body).encode(),
                                 headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=context, timeout=20) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(method + ' ' + path + ': HTTP ' + str(error.code)) from None
    return result.get('data', result)


def admin_token():
    password = json.loads((STATE / 'secrets.json').read_text())['admin']
    return request('POST', '/auth/login', body={'login': 'opensoha@soha.local', 'password': password})['tokens']['accessToken']


def enroll(token, runtime_id, kind, subject_id, device_id, certificate_dir, capability):
    enrollment = request('POST', '/network-access/enrollments', token,
                         {'runtimeId': runtime_id, 'runtimeKind': kind, 'subjectId': subject_id, 'deviceId': device_id, 'ttlSeconds': 600})
    folder = STATE / 'tls' / certificate_dir
    public_key = run('openssl', 'pkey', '-in', str(folder / 'tls.key'), '-pubout')
    context = ssl.create_default_context(cafile=str(folder / 'ca.crt'))
    context.load_cert_chain(str(folder / 'tls.crt'), str(folder / 'tls.key'))
    now = datetime.now(timezone.utc)
    message = {'schemaVersion': 'network-runtime/v1alpha1', 'messageId': str(uuid.uuid4()),
               'messageType': 'runtime.enroll.request', 'producerId': runtime_id, 'runtimeId': runtime_id, 'runtimeKind': kind,
               'occurredAt': now.isoformat(), 'expiresAt': (now + timedelta(minutes=1)).isoformat(),
               'payload': {'enrollmentId': enrollment['enrollment']['id'], 'challengeId': enrollment['enrollment']['challengeId'],
                           'deviceId': device_id, 'devicePublicKey': public_key, 'platform': 'linux',
                           'clientVersion': 'task53-software-lab', 'capabilities': capability if isinstance(capability, list) else [capability]}}
    result = request('POST', '/runtimes/' + runtime_id + '/enroll', enrollment['token'], message, context,
                     'https://127.0.0.1:18182/api/network-control/v1')
    if not result['payload']['accepted']:
        raise RuntimeError('Runtime enrollment was not accepted')


def seed():
    owned_state()
    if (STATE / 'seed.json').exists():
        print('Lab is already seeded; existing identities were preserved')
        return
    token = admin_token()
    subject = request('GET', '/auth/me', token)['userId']
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._:-]{0,127}', subject):
        raise RuntimeError('The test subject ID cannot be encoded in RADIUS')
    device = request('PUT', '/network-access/devices/' + DEVICE_ID + '/registration', token,
                     {'name': 'Task53 EAP software client', 'hostname': 'middleware-lab', 'platform': 'linux', 'deviceType': 'desktop'})
    progress_file = STATE / 'bootstrap.json'
    progress = json.loads(progress_file.read_text()) if progress_file.exists() else {}

    def create_once(key, path, body):
        if key not in progress:
            progress[key] = request('POST', path, token, body)['id']
            write(progress_file, progress)
        return progress[key]

    site_id = create_once('siteId', '/network-access/sites',
                          {'name': 'Task53 software RADIUS lab', 'description': 'Protocol tests only; no physical SSID or NAS', 'status': 'active'})
    request('PUT', '/network-access/devices/' + device['id'], token,
            {'name': device['name'], 'siteId': site_id, 'status': 'active', 'postureStatus': 'compliant',
             'deviceType': 'desktop', 'ownershipType': 'company'})
    for profile, vlan in [('full', 10), ('restricted', 20), ('quarantine', 30)]:
        create_once('profile-' + profile, '/network-access/site-profile-bindings',
                {'siteId': site_id, 'accessProfile': profile, 'vlanId': vlan, 'filterId': 'task53-' + profile, 'sessionTimeoutSeconds': 300})
    policy_id = create_once('policyId', '/network-access/policies',
                     {'name': 'Task53 EAP software client only', 'enabled': True, 'priority': 100, 'effect': 'allow',
                      'subjects': {'users': [subject], 'teams': [], 'tags': []}, 'siteIds': [site_id], 'resourceIds': [],
                      'modes': ['internal_direct'], 'deviceStatuses': ['active'], 'postureStatuses': ['compliant'], 'accessProfile': 'full'})
    request('POST', '/network-access/policies/compile', token, {})
    time.sleep(5)
    certificate('eap-client', subject, uri='spiffe://opensoha.local/network-control/endpoint/' + DEVICE_ID)
    template = (Path(__file__).parent / 'eapol-test.conf.example').read_text()
    write(STATE / 'tls' / 'eap-client' / 'eapol-test.conf', template.replace('replace-with-soha-subject-id', subject))
    for key, runtime_id, kind, device_id, folder, capability in (
        ('nasEnrolled', RUNTIME_ID, 'nas', NAS_ID, 'freeradius-control', 'radius-aaa-v1'),
        ('endpointEnrolled', DEVICE_ID, 'endpoint', DEVICE_ID, 'eap-client', ['radius-eap-tls-v1', 'endpoint-presence-v1']),
    ):
        if not progress.get(key):
            enroll(token, runtime_id, kind, subject, device_id, folder, capability)
            progress[key] = True
            write(progress_file, progress)
    create_once('nasBindingId', '/network-access/nas-bindings',
            {'nasId': NAS_ID, 'runtimeId': RUNTIME_ID, 'siteId': site_id, 'name': 'eapol_test software authenticator',
             'accessMedium': 'wired', 'deviceType': 'switch', 'status': 'active', 'coaSupported': False, 'disconnectSupported': False})
    write(STATE / 'seed.json', {'subjectId': subject, 'deviceId': DEVICE_ID, 'siteId': site_id, 'policyId': policy_id})
    print('Seeded one isolated site, device, NAS binding and scoped admission policy through authenticated APIs')


def verify():
    owned_state()
    seed_data = json.loads((STATE / 'seed.json').read_text())
    token = admin_token()

    def sessions():
        result = request('GET', '/network-access/sessions?deviceId=' + DEVICE_ID + '&siteId=' + seed_data['siteId'], token)
        return result.get('items', result) if isinstance(result, dict) else result

    existing = {item['id'] for item in sessions()}
    for _ in range(2):
        output = compose('run', '--rm', '--no-deps', 'eap-client')
        if not re.search(r'^SUCCESS$', output, re.MULTILINE):
            raise RuntimeError('EAP-TLS authentication did not succeed')
    write(STATE / 'eap-test.log', 'Two independent EAP-TLS authentications succeeded; raw key material was not retained.\n')
    matching = [item for item in sessions() if item['id'] not in existing and item['nasId'] == NAS_ID and item['accessProfile'] == 'full']
    if len(matching) != 2:
        raise RuntimeError('Independent EAP authentications did not create distinct Core sessions')
    session = matching[0]
    # Accounting is synthesized, because eapol_test does not implement a physical NAS lifecycle.
    for status in ('Start', 'Interim-Update', 'Stop'):
        attributes = {'Acct-Status-Type': status, 'Acct-Session-Id': 'task53-' + session['id'], 'Class': session['id'],
                      'NAS-Identifier': NAS_ID, 'NAS-Port-Id': 'software-port-1', 'User-Name': seed_data['subjectId'],
                      'Calling-Station-Id': '02:00:00:00:00:01', 'Event-Timestamp': int(datetime.now(timezone.utc).timestamp()),
                      'Acct-Session-Time': 1, 'Message-Authenticator': '0x00'}
        packet = '\n'.join(key + ' = ' + (str(value) if isinstance(value, int) or key in ('Acct-Status-Type', 'Message-Authenticator') else '"' + value + '"')
                           for key, value in attributes.items()) + '\n'
        result = compose('run', '--rm', '--no-deps', '-T', '--entrypoint', '/usr/bin/radclient', 'radius-client',
                         '-x', '-r', '1', '-t', '5', '-S', '/lab/secrets/shared-secret', '127.0.0.1:1813', 'acct', input=packet)
        if 'Accounting-Response' not in result:
            raise RuntimeError('Accounting ' + status + ' was not acknowledged')
    folder = STATE / 'tls' / 'core-query'
    context = ssl.create_default_context(cafile=str(folder / 'ca.crt'))
    context.load_cert_chain(str(folder / 'tls.crt'), str(folder / 'tls.key'))
    summary = request('GET', '/query/summary?producerId=' + RUNTIME_ID, context=context,
                      origin='https://127.0.0.1:18183/api/ingest/v1')
    if summary['radiusAccountingCount'] < 3:
        raise RuntimeError('Ingest did not persist all three Accounting lifecycle packets')
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', session['id']):
        raise RuntimeError('Unexpected session ID')
    query = "SELECT count(DISTINCT payload->>'statusType') FROM network_ingest_events WHERE producer_id = '" + RUNTIME_ID
    query += "' AND event_type = 'radius.accounting' AND payload->>'sessionId' = '" + session['id'] + "';"
    count = compose('exec', '-T', 'ingest-postgres', 'psql', '-U', 'pgsql', '-d', 'soha_ingest', '-tAc', query).strip()
    if count != '3':
        raise RuntimeError('Accounting records did not retain the authorized Core session ID')
    policy_path = '/network-access/policies/' + seed_data['policyId']
    policy = request('GET', policy_path, token)
    if (policy['name'] != 'Task53 EAP software client only' or policy['siteIds'] != [seed_data['siteId']]
            or policy['subjects']['users'] != [seed_data['subjectId']] or not policy['enabled']):
        raise RuntimeError('The lab policy changed; refusing to run its temporary deny exercise')
    original = {key: policy[key] for key in ('name', 'enabled', 'priority', 'effect', 'subjects', 'siteIds',
                                            'resourceIds', 'modes', 'deviceStatuses', 'postureStatuses', 'accessProfile')}
    try:
        request('PUT', policy_path, token, {**original, 'enabled': False})
        request('POST', '/network-access/policies/compile', token, {})
        time.sleep(5)
        try:
            compose('run', '--rm', '--no-deps', 'eap-client')
        except subprocess.CalledProcessError as error:
            if 'Access-Reject' not in error.stdout:
                raise RuntimeError('EAP failed without an explicit RADIUS policy rejection') from None
        else:
            raise RuntimeError('Disabled admission policy still allowed a new EAP authentication')
    finally:
        request('PUT', policy_path, token, original)
        request('POST', '/network-access/policies/compile', token, {})
        time.sleep(5)
    recovered = compose('run', '--rm', '--no-deps', 'eap-client')
    if not re.search(r'^SUCCESS$', recovered, re.MULTILINE):
        raise RuntimeError('The restored policy did not restore EAP authentication')
    write(STATE / 'verification.json', {'verifiedAt': datetime.now(timezone.utc).isoformat(), 'sessionId': session['id'],
                                     'eapTLS': 'passed', 'accountingPackets': 'start/interim/stop acknowledged', 'ingestSummary': summary,
                                     'distinctAuthenticationSessions': [item['id'] for item in matching],
                                     'policyDenyAndRecovery': 'passed',
                                     'physicalNAS': 'not tested', 'VLANACL': 'not tested', 'endpointPresenceResponse': 'not implemented'})
    print('EAP-TLS, Core session mapping, policy deny/recovery and synthesized Accounting Start/Interim/Stop passed')
    print('Physical NAS, VLAN/ACL, OS connection and endpoint offline response remain unverified')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'up', 'seed', 'verify', 'status', 'stop'])
    action = parser.parse_args().action
    if action == 'prepare':
        prepare()
        return
    owned_state()
    if action == 'up':
        runtime_credentials()
        compose('up', '-d', '--wait', '--wait-timeout', '180', 'core', 'ingest')
        compose('up', '-d', 'network-control')
        seed()
        compose('up', '-d', '--wait', '--wait-timeout', '180', 'network-control', 'freeradius')
        print('Core, network-control, ingest and FreeRADIUS are healthy in the isolated lab')
    elif action == 'seed':
        seed()
    elif action == 'verify':
        verify()
    elif action == 'status':
        print(compose('ps'))
    elif action == 'stop':
        compose('stop')
        print('Lab containers stopped; its state and data volumes were preserved')


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as error:
        raise SystemExit('Lab command failed with exit code ' + str(error.returncode) + '; credentials and command output were suppressed') from None
