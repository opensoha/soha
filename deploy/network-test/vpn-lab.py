#!/usr/bin/env python3
"""Isolated, loopback-only managed VPN lab. Uses public APIs, never database seeds."""
import argparse
import json
import http.client
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.request

REPO = Path(__file__).resolve().parents[2]
STATE = Path.home() / '.local/share/opensoha-vpn-lab'
BIN = Path('/private/tmp/soha-vpn-build')
API = 'http://127.0.0.1:18080/api/v1'
ROOT_STATE = '/var/db/opensoha/network-service'


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(value if isinstance(value, str) else json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def compose(*args):
    return run('docker', 'compose', '-p', 'soha-macos-vpn-lab', '-f', str(STATE / 'compose.json'), *args)


def certificate(name, uri=None):
    folder = STATE / 'tls' / name
    folder.mkdir(parents=True, exist_ok=True)
    if (folder / 'cert.pem').exists():
        return
    ca = STATE / 'tls'
    ext = 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\n'
    if uri:
        ext += 'extendedKeyUsage=clientAuth\nsubjectAltName=URI:' + uri + '\n'
    else:
        ext += 'extendedKeyUsage=serverAuth\nsubjectAltName=DNS:control,DNS:ingest,DNS:localhost,IP:127.0.0.1\n'
    write(folder / 'ext.cnf', ext)
    quiet = dict(stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    run('openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(folder / 'key.pem'), **quiet)
    run('openssl', 'req', '-new', '-key', str(folder / 'key.pem'), '-subj', '/CN=Soha VPN Lab ' + name, '-out', str(folder / 'request.pem'), **quiet)
    run('openssl', 'x509', '-req', '-in', str(folder / 'request.pem'), '-CA', str(ca / 'ca.pem'), '-CAkey', str(ca / 'ca-key.pem'), '-set_serial', str(secrets.randbits(120)), '-days', '7', '-extfile', str(folder / 'ext.cnf'), '-out', str(folder / 'cert.pem'), **quiet)
    (folder / 'key.pem').chmod(0o600)


def prepare():
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    STATE.chmod(0o700)
    if not (STATE / 'secrets.json').exists():
        write(STATE / 'secrets.json', {k: secrets.token_hex(24) for k in ['database', 'admin', 'jwt', 'encryption']})
    credentials = json.loads((STATE / 'secrets.json').read_text())
    write(STATE / 'admin-password', credentials['admin'] + '\n')
    ca = STATE / 'tls'
    ca.mkdir(exist_ok=True, mode=0o700)
    if not (ca / 'ca.pem').exists():
        run('openssl', 'req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:P-256', '-nodes', '-keyout', str(ca / 'ca-key.pem'), '-out', str(ca / 'ca.pem'), '-days', '7', '-subj', '/CN=Disposable Soha VPN Lab CA', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign', stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        (ca / 'ca-key.pem').chmod(0o600)
    for name in ['control', 'ingest', 'probe-a']:
        certificate(name)
    for name, identity in [('core-query', 'network-ingest/core/lab-core'), ('control-query', 'network-ingest/network-control/lab-control')]:
        certificate(name, 'spiffe://opensoha.local/' + identity)
    for kind in ['a']:
        for scope in ['control', 'ingest']:
            certificate('gateway-' + kind + '-' + scope, f'spiffe://opensoha.local/network-{scope}/gateway/lab-gateway-{kind}')
    for scope in ['control', 'ingest']:
        certificate('endpoint-' + scope, f'spiffe://opensoha.local/network-{scope}/endpoint/lab-macos')

    # Separate databases and volumes. No explicitly named user containers are reused.
    services = {}
    for name, database in [('postgres', 'soha'), ('ingest-postgres', 'soha_ingest')]:
        services[name] = {'image': 'pgvector/pgvector:0.8.5-pg18-trixie', 'environment': {'POSTGRES_USER': 'pgsql', 'POSTGRES_PASSWORD': credentials['database'], 'POSTGRES_DB': database}, 'volumes': [name + '-data:/var/lib/postgresql'], 'healthcheck': {'test': ['CMD-SHELL', 'pg_isready -U pgsql -d ' + database], 'interval': '2s', 'timeout': '2s', 'retries': 30}}
    query = lambda name: {'SOHA_NETWORK_INGEST_QUERY_URL': 'https://ingest:8083', 'SOHA_NETWORK_INGEST_QUERY_CA_FILE': '/lab/tls/ca.pem', 'SOHA_NETWORK_INGEST_QUERY_CERT_FILE': f'/lab/tls/{name}/cert.pem', 'SOHA_NETWORK_INGEST_QUERY_KEY_FILE': f'/lab/tls/{name}/key.pem', 'SOHA_NETWORK_INGEST_QUERY_SERVER_NAME': 'ingest'}
    for name, executable, port in [('core', 'server', 18080), ('control', 'control', 18082), ('ingest', 'ingest', 18083)]:
        env = {'SOHA_CONFIG_FILE': '/app/configs/config.yaml', 'SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY': credentials['encryption']}
        if name == 'core':
            env.update({'SOHA_DATABASE_HOST': 'postgres', 'SOHA_DATABASE_PASSWORD': credentials['database'], 'SOHA_AUTH_DEV_PRINCIPAL_PASSWORD': credentials['admin'], 'SOHA_AUTH_JWT_SECRET': credentials['jwt'], 'SOHA_AUTH_LOGIN_VERIFICATION_SLIDER_ENABLED': 'false', **query('core-query')})
        else:
            prefix = 'SOHA_NETWORK_CONTROL' if name == 'control' else 'SOHA_INGEST'
            env.update({prefix + '_DATABASE_HOST': 'postgres' if name == 'control' else 'ingest-postgres', prefix + '_DATABASE_PASSWORD': credentials['database'], prefix + '_TLS_CERT_FILE': f'/lab/tls/{name}/cert.pem', prefix + '_TLS_KEY_FILE': f'/lab/tls/{name}/key.pem', prefix + '_TLS_CLIENT_CA_FILE': '/lab/tls/ca.pem'})
            if name == 'control':
                env.update(query('control-query'))
        services[name] = {'image': 'soha-vpn-implementation:test', 'user': '0:0', 'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'], 'pids_limit': 256, 'init': True, 'entrypoint': ['/lab/bin/lab-' + executable], 'working_dir': '/app', 'environment': env, 'volumes': [str(BIN) + ':/lab/bin:ro', str(STATE / 'tls') + ':/lab/tls:ro', str(REPO / 'configs/config.yaml') + ':/app/configs/config.yaml:ro', str(REPO / 'migrations') + ':/app/migrations:ro', 'core-data:/app/data'], 'ports': [f'127.0.0.1:{port}:{port-10000}'], 'depends_on': {'postgres' if name != 'ingest' else 'ingest-postgres': {'condition': 'service_healthy'}}}
    for index, name in enumerate(['a']):
        env = {'SOHA_NETWORK_GATEWAY_RUNTIME_ID': 'lab-gateway-' + name, 'SOHA_NETWORK_GATEWAY_DEVICE_ID': 'lab-gateway-' + name, 'SOHA_NETWORK_GATEWAY_PRIVATE_KEY_FILE': '/state/wireguard.key', 'SOHA_NETWORK_GATEWAY_EGRESS_INTERFACE': 'eth0', 'SOHA_NETWORK_GATEWAY_POLL_INTERVAL': '2s', 'SOHA_NETWORK_GATEWAY_HEARTBEAT_INTERVAL': '5s', 'SOHA_NETWORK_GATEWAY_PROBE_ADDR': ':8443', 'SOHA_NETWORK_GATEWAY_PROBE_CERT_FILE': f'/tls/probe-{name}/cert.pem', 'SOHA_NETWORK_GATEWAY_PROBE_KEY_FILE': f'/tls/probe-{name}/key.pem', 'SOHA_NETWORK_GATEWAY_PROBE_CA_FILE': '/tls/ca.pem'}
        for scope, port in [('control', 8082), ('ingest', 8083)]:
            env.update({f'SOHA_NETWORK_GATEWAY_{scope.upper()}_URL': f'https://{scope}:{port}', f'SOHA_NETWORK_GATEWAY_{scope.upper()}_CA_FILE': '/tls/ca.pem', f'SOHA_NETWORK_GATEWAY_{scope.upper()}_CERT_FILE': f'/tls/gateway-{name}-{scope}/cert.pem', f'SOHA_NETWORK_GATEWAY_{scope.upper()}_KEY_FILE': f'/tls/gateway-{name}-{scope}/key.pem'})
        enrollment = STATE / ('gateway-' + name + '.json')
        if enrollment.exists():
            record = json.loads(enrollment.read_text())
            env.update({'SOHA_NETWORK_GATEWAY_ENROLLMENT_ID': record['id'], 'SOHA_NETWORK_GATEWAY_ENROLLMENT_CHALLENGE_ID': record['challengeId'], 'SOHA_NETWORK_GATEWAY_ENROLLMENT_TOKEN_FILE': '/enrollment/token'})
        services['gateway-' + name] = {'image': 'soha-vpn-gateway:test', 'entrypoint': ['/bin/sh', '/lab-entrypoint.sh'], 'command': ['run'], 'user': '0:0', 'cap_drop': ['ALL'], 'cap_add': ['NET_ADMIN'], 'security_opt': ['no-new-privileges:true'], 'sysctls': {'net.ipv4.ip_forward': '1'}, 'environment': env, 'volumes': [str(BIN / 'lab-gateway') + ':/lab-gateway:ro', str(REPO / 'deploy/network-test/vpn-lab-gateway.sh') + ':/lab-entrypoint.sh:ro', str(STATE / 'tls') + ':/tls:ro', str(STATE / ('enrollment-' + name)) + ':/enrollment:ro', 'gateway-' + name + '-data:/state'], 'ports': [f'127.0.0.1:{15182+index}:{15182+index}/udp', f'127.0.0.1:{18443+index}:8443', f'127.0.0.1:{18084+index}:8084']}
    services['resource'] = {'image': 'python:3.12-alpine', 'cap_add': ['NET_ADMIN'], 'environment': {'LAB_RESOURCE_IP': '10.252.250.10'}, 'command': ['python', '/resource.py'], 'volumes': [str(REPO / 'deploy/network-test/vpn-lab-resource.py') + ':/resource.py:ro'], 'networks': {'default': {'ipv4_address': '10.252.240.10'}}}
    services['protected-resource'] = {'image': 'python:3.12-alpine', 'cap_add': ['NET_ADMIN'], 'environment': {'LAB_RESOURCE_IP': '10.252.250.11'}, 'command': ['python', '/resource.py'], 'volumes': [str(REPO / 'deploy/network-test/vpn-lab-resource.py') + ':/resource.py:ro'], 'networks': {'default': {'ipv4_address': '10.252.240.11'}}}
    document = {'services': services, 'volumes': {k: {} for k in ['postgres-data', 'ingest-postgres-data', 'core-data', 'gateway-a-data']}, 'networks': {'default': {'ipam': {'config': [{'subnet': '10.252.240.0/24'}]}}}}
    write(STATE / 'compose.json', document)


class Client:
    def __init__(self):
        password = (STATE / 'admin-password').read_text().strip()
        self.token = ''
        auth = self.request('POST', '/auth/login', {'login': 'opensoha@soha.local', 'password': password})
        self.token = auth['tokens']['accessToken']
        self.subject = auth['user']['userId']

    def request(self, method, path, body=None):
        headers = {'Content-Type': 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        request = urllib.request.Request(API + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                value = json.load(response)
        except urllib.error.HTTPError as error:
            # Errors do not include submitted credentials or request bodies.
            raise RuntimeError(f'{method} {path}: HTTP {error.code}: {error.read(2048).decode()}') from None
        return value.get('data', value)


def seed():
    client = Client()
    marker = STATE / 'objects.json'
    objects = json.loads(marker.read_text()) if marker.exists() else {}
    def create(key, path, body):
        if key not in objects:
            objects[key] = client.request('POST', '/network-access/' + path, body)['id']
            write(marker, objects)
        return objects[key]
    device_id = (Path.home() / 'Library/Application Support/OpenSoha/Soha/device-id').read_text().strip()
    site = create('site', 'sites', {'name': 'macOS VPN Docker Lab', 'status': 'active', 'location': '本机隔离实验环境'})
    space = create('space', 'spaces', {'name': 'Docker Lab Resources', 'siteId': site, 'cidrs': ['10.252.250.0/24'], 'status': 'active'})
    client.request('PUT', '/network-access/devices/' + device_id + '/registration', {'name': 'macOS VPN Lab Endpoint', 'platform': 'macos', 'hostname': socket.gethostname(), 'deviceType': 'laptop'})
    client.request('PUT', '/network-access/devices/' + device_id, {'name': 'macOS VPN Lab Endpoint', 'status': 'active', 'siteId': site, 'postureStatus': 'compliant', 'ownershipType': 'company', 'deviceType': 'laptop'})
    protected = create('protected', 'resources', {'name': 'ProtectedSet deny check', 'spaceId': space, 'kind': 'ip', 'target': '10.252.250.11', 'protocol': 'tcp', 'ports': [8000], 'protected': True, 'pathMode': 'wireguard_ztna'})
    gateways = []
    for index, name in enumerate(['a']):
        gateway = create('gateway-' + name, 'gateways', {'name': name.upper() + ' 地 · Docker', 'runtimeId': 'lab-gateway-' + name, 'siteId': site, 'administrativeStatus': 'active', 'publicEndpointHost': '127.0.0.1', 'publicEndpointPort': 15182 + index, 'overlayCidr': f'10.252.{241+index}.0/24', 'routingMode': 'snat', 'advertisedCidrs': [], 'mtu': 1420, 'persistentKeepaliveSeconds': 25, 'dnsServers': ['10.252.250.10'], 'region': name.upper(), 'providerCode': 'lab-' + name, 'providerName': '实验供应商 ' + name.upper(), 'selectionPriority': (index+1)*10, 'acceptNewConnections': True, 'maxSessions': 20, 'probeURL': f'https://127.0.0.1:{18443+index}/vpn/probe'})
        gateways.append(gateway)
    create('access-policy', 'policies', {'name': 'Lab owner VPN access', 'enabled': True, 'priority': 10, 'effect': 'allow', 'accessProfile': 'full', 'modes': ['external_vpn'], 'siteIds': [site], 'resourceIds': [], 'subjects': {'users': [client.subject], 'teams': [], 'tags': []}, 'deviceStatuses': ['active'], 'postureStatuses': ['compliant']})
    client.request('POST', '/network-access/policies/compile', {})
    policy = create('selection-policy', 'vpn/selection-policies', {'expectedRevision': 0, 'configuration': {'name': 'Auto · 实测延迟优先', 'strategy': 'latency', 'providerOrder': [], 'providerPreference': 'prefer', 'maxLatencyMs': 5000, 'maxTimeoutPercent': 100, 'maxSampleAgeSeconds': 60, 'minSamples': 1, 'missingMeasurements': 'priority', 'maxAttempts': 3, 'retryCooldownSeconds': 10, 'failoverOnDisconnect': True, 'allowManualFallback': True}})
    current = client.request('GET', '/network-access/vpn/selection-policies/' + policy)
    if not current['publishedRevision']:
        client.request('POST', '/network-access/vpn/selection-policies/' + policy + '/publish', {'expectedRevision': current['revision']})
    profile = create('profile', 'vpn/profiles', {'expectedRevision': 0, 'configuration': {'name': 'macOS 原生 VPN · Docker 实验网', 'siteId': site, 'networkSpaceId': space, 'mode': 'external_vpn', 'resourceIds': [], 'gatewayIds': gateways, 'selectionPolicyId': policy, 'allowManualSelection': True, 'enabled': True, 'assignments': {'userIds': [client.subject], 'teamIds': [], 'deviceIds': [device_id]}}})
    current = client.request('GET', '/network-access/vpn/profiles/' + profile)
    if not current['publishedRevision']:
        client.request('POST', '/network-access/vpn/profiles/' + profile + '/publish', {'expectedRevision': current['revision']})
    for name in ['a']:
        if not (STATE / ('gateway-' + name + '-enrolled')).exists():
            record = client.request('POST', '/network-access/enrollments', {'runtimeId': 'lab-gateway-' + name, 'runtimeKind': 'gateway', 'deviceId': 'lab-gateway-' + name, 'subjectId': client.subject, 'ttlSeconds': 600})
            write(STATE / ('gateway-' + name + '.json'), record['enrollment'])
            write(STATE / ('enrollment-' + name) / 'token', record['token'])
            prepare()
            compose('run', '--rm', '--no-deps', 'gateway-' + name, 'enroll')
            write(STATE / ('gateway-' + name + '-enrolled'), 'enrolled\n')
    provision = STATE / 'endpoint-provision'
    provision.mkdir(exist_ok=True, mode=0o700)
    for scope in ['control', 'ingest']:
        for part in ['cert', 'key']:
            shutil.copyfile(STATE / 'tls' / ('endpoint-' + scope) / (part + '.pem'), provision / (scope + '-' + part + '.pem'))
        shutil.copyfile(STATE / 'tls/ca.pem', provision / (scope + '-ca.pem'))
    record = client.request('POST', '/network-access/enrollments', {'runtimeId': 'lab-macos', 'runtimeKind': 'endpoint', 'deviceId': device_id, 'subjectId': client.subject, 'ttlSeconds': 600})
    write(provision / 'enrollment-token', record['token'])
    config = {'version': 1, 'runtimeId': 'lab-macos', 'deviceId': device_id, 'allowedUserUid': os.getuid(), 'stateDirectory': ROOT_STATE, 'wireGuardExecutable': '', 'enrollmentId': record['enrollment']['id'], 'enrollmentChallengeId': record['enrollment']['challengeId'], 'enrollmentTokenFile': ROOT_STATE + '/enrollment-token'}
    for scope, port in [('control', 18082), ('ingest', 18083)]:
        config[scope + 'Url'] = f'https://127.0.0.1:{port}'
        for part in ['Ca', 'Cert', 'Key']:
            config[scope + part + 'File'] = ROOT_STATE + '/' + scope + '-' + part.lower() + '.pem'
    write(provision / 'service.json', config)
    for path in provision.iterdir():
        path.chmod(0o600)
    compose('up', '-d', 'gateway-a', 'resource', 'protected-resource')
    print('Lab ready: http://127.0.0.1:18080; user: opensoha@soha.local')
    print('Password file:', STATE / 'admin-password')
    print('macOS provisioning directory:', provision)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['prepare', 'up', 'seed', 'down', 'status'])
    args = parser.parse_args()
    if args.command == 'down':
        compose('down')  # Retain test data and credentials for the next run.
        return
    if args.command == 'status':
        compose('ps')
        return
    prepare()
    if args.command == 'up':
        for name in ['server', 'control', 'ingest', 'gateway']:
            if not (BIN / ('lab-' + name)).is_file():
                raise RuntimeError('Build the Linux lab binaries first; see vpn-lab.md')
        compose('up', '-d', 'postgres', 'ingest-postgres', 'core', 'ingest')
        for attempt in range(60):
            try:
                with urllib.request.urlopen('http://127.0.0.1:18080/readyz', timeout=2):
                    break
            except (urllib.error.URLError, TimeoutError, http.client.RemoteDisconnected):
                time.sleep(1)
        else:
            raise RuntimeError('Lab core did not become ready; inspect docker compose logs')
        compose('up', '-d', 'control')
        if (STATE / 'endpoint-provision/service.json').exists():
            compose('up', '-d', 'gateway-a', 'resource', 'protected-resource')
            print('Lab resumed at http://127.0.0.1:18080; use seed only to refresh initial provisioning.')
            return
    if args.command in ['up', 'seed']:
        seed()


if __name__ == '__main__':
    main()
