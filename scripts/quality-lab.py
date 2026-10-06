#!/usr/bin/env python3
"""Dedicated local Core + middleware K3s/Agent acceptance lab. Never uses user auth."""
import argparse
import base64
import fcntl
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import subprocess
import tarfile
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
LAB = ROOT / 'soha/.tmp/quality-lab'
STATE = LAB / 'private.json'
ENV = dict(os.environ, GOCACHE='/private/tmp/opensoha-cache/go-build',
           GOMODCACHE='/private/tmp/opensoha-cache/go-mod',
           npm_config_cache='/private/tmp/opensoha-cache/npm')


def run(*args, data=None, timeout=180, env=None, cwd=None):
    result = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=timeout, env=env or ENV, cwd=cwd)
    if result.returncode:
        # Output may contain kubeconfig/API bodies; retain privately, never print them.
        private(LAB / 'last-error.txt', result.stderr.decode(errors='replace'))
        raise RuntimeError(f'{Path(args[0]).name} failed ({result.returncode}); private last-error.txt')
    return result.stdout.decode()


def private(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(path.parent, 0o700)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as output:
        output.write(value if isinstance(value, str) else json.dumps(value, indent=2))


def state():
    if STATE.exists():
        return json.loads(STATE.read_text())
    value = {'runId': str(uuid.uuid4()), 'secrets': {key: secrets.token_urlsafe(36)
             for key in ['admin', 'db', 'jwt', 'runner', 'webhook', 'encryption', 'agent', 'readonly', 'writer']}}
    value['name'] = 'soha-quality-' + value['runId'][:8]
    private(STATE, value)
    return value


def save(value):
    with (LAB / 'state.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        merged = json.loads(STATE.read_text()) if STATE.exists() else {}
        merged.update(value)
        temporary = LAB / 'private.next.json'
        private(temporary, merged)
        temporary.replace(STATE)


def snapshot(repo, destination):
    paths = run('git', '-C', str(repo), 'ls-files', '--cached', '--others', '--exclude-standard', '-z').split('\0')
    digest = hashlib.sha256()
    for name in sorted(set(filter(None, paths))):
        source = repo / name
        if '.tmp' in source.relative_to(repo).parts:
            continue
        if not source.is_file() or name.startswith('internal/staticassets/web/dist/'):
            continue
        if source.is_symlink() or source.name == '.env' or source.suffix in ('.pem', '.key'):
            raise RuntimeError('Sensitive/symlink build input rejected')
        content = source.read_bytes()
        digest.update(name.encode() + b'\0' + content)
        target = destination / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        shutil.copymode(source, target)
    return {'commit': run('git', '-C', str(repo), 'rev-parse', 'HEAD').strip(),
            'artifactDigest': digest.hexdigest()}


def stop(s):
    # Recheck all remaining resources before each attempt; allow absence only after verified cleanup began.
    resuming = bool(s.get('cleanupStarted'))
    containers = {}
    for key, remote in [('coreId', False), ('pgId', False), ('k3sId', True)]:
        command = ['docker', 'ps', '-aq', '--filter', 'id=' + s[key], '--no-trunc']
        present = run('ssh', 'middleware', ' '.join(command)) if remote else run(*command)
        if not present.strip():
            if not resuming:
                raise RuntimeError('Owned container missing before cleanup validation')
            continue
        containers[key] = inspect(s, key, remote=remote)
    for key, image in [('coreId', 'coreImage'), ('k3sId', 'k3sImage')]:
        if key in containers and containers[key]['Image'] != s[image]:
            raise RuntimeError('Image ownership mismatch')
    network = None
    networks = run('docker', 'network', 'ls', '-q', '--no-trunc', '--filter', 'name=^' + s['name'] + '$').split()
    if networks:
        network = json.loads(run('docker', 'network', 'inspect', networks[0]))[0]
        if (len(networks) != 1 or network['Labels'].get('soha.test.run') != s['runId'] or
                not set(network['Containers']).issubset({s['coreId'], s['pgId']}) or
                (resuming and network['Id'] != s['cleanupNetworkId'])):
            raise RuntimeError('Network has unknown identity/consumer')
    elif not resuming:
        raise RuntimeError('Owned network missing before cleanup validation')
    volume = s['name'] + '-k3s'
    volumes = run('ssh', 'middleware', f'docker volume ls -q --filter name=^{volume}$').split()
    volume_created = None
    if volumes:
        item = json.loads(run('ssh', 'middleware', f'docker volume inspect {volume}'))[0]
        volume_created = item['CreatedAt']
        if volumes != [volume] or (resuming and volume_created != s['cleanupVolumeCreatedAt']):
            raise RuntimeError('K3s volume identity mismatch')
        cluster = containers.get('k3sId')
        if cluster and not any(x.get('Name') == volume and x.get('Destination') == '/var/lib/rancher/k3s' for x in cluster['Mounts']):
            raise RuntimeError('K3s volume attachment mismatch')
        consumers = run('ssh', 'middleware', f'docker ps -aq --filter volume={volume} --no-trunc').split()
        if not set(consumers).issubset({s['k3sId']}):
            raise RuntimeError('K3s volume has another consumer')
    elif not resuming:
        raise RuntimeError('Owned volume missing before cleanup validation')
    if s.get('tunnelPid'):
        process = subprocess.run(['ps', '-p', str(s['tunnelPid']), '-o', 'command='], capture_output=True, text=True)
        if process.returncode == 0:
            expected = [f'127.0.0.1:{s["tunnelPorts"][0]}:127.0.0.1:{s["k3sPort"]}',
                        f'127.0.0.1:{s["tunnelPorts"][1]}:']
            if not all(x in process.stdout for x in expected) or 'middleware' not in process.stdout or 'ssh -N' not in process.stdout:
                raise RuntimeError('SSH process identity mismatch')
    if not resuming:
        s.update(cleanupStarted=True, cleanupNetworkId=network['Id'], cleanupVolumeCreatedAt=volume_created)
        save(s)
    if s.get('tunnelPid'):
        if process.returncode == 0:
            os.kill(s['tunnelPid'], 15)
        s.pop('tunnelPid')
        save(s)
    local = [containers[key]['Id'] for key in ['coreId', 'pgId'] if key in containers]
    if local:
        run('docker', 'stop', '--time', '10', *local)
        run('docker', 'rm', '-v', *local)
    if network:
        run('docker', 'network', 'rm', network['Id'])
    if 'k3sId' in containers:
        run('ssh', 'middleware', f'docker stop -t 10 {s["k3sId"]}')
        run('ssh', 'middleware', f'docker rm {s["k3sId"]}')
    if volumes:
        run('ssh', 'middleware', f'docker volume rm {volume}')
    s['stopped'] = True
    save(s)


def build(s):
    builddir = LAB / 'build'
    if builddir.exists():
        shutil.rmtree(builddir)
    builddir.mkdir()
    packed = json.loads(run('npm', 'pack', '--ignore-scripts', '--json', '--pack-destination', str(builddir),
                            cwd=ROOT / 'soha-contracts'))[0]
    archive = builddir / packed['filename']
    with tarfile.open(archive) as tar:
        tar.extractall(builddir, filter='data')
    sdk = builddir / 'package'
    s['sources'] = {
        'contracts': {'commit': run('git', '-C', str(ROOT / 'soha-contracts'), 'rev-parse', 'HEAD').strip(),
                      'artifactDigest': hashlib.sha256(archive.read_bytes()).hexdigest()},
        'core': snapshot(ROOT / 'soha', builddir / 'core'),
        'agent': snapshot(ROOT / 'soha-agent', builddir / 'agent'),
    }
    web = ROOT / 'soha-web/dist'
    if not (web / 'index.html').is_file():
        raise RuntimeError('Build Web first using its existing npm run build')
    digest = hashlib.sha256()
    for path in sorted(web.rglob('*')):
        if path.is_file():
            digest.update(str(path.relative_to(web)).encode() + b'\0' + path.read_bytes())
    s['sources']['web'] = {'commit': run('git', '-C', str(ROOT / 'soha-web'), 'rev-parse', 'HEAD').strip(),
                           'artifactDigest': digest.hexdigest()}
    shutil.copytree(web, builddir / 'core/internal/staticassets/web/dist')
    # Real owner Dockerfiles consume this extracted SDK package, never sibling source.
    for kind, repo, target in [('core', 'soha', 'soha-runtime'), ('agent', 'soha-agent', 'agent-runtime')]:
        image = s['name'] + '-' + kind
        with (LAB / (kind + '-build.log')).open('w') as log:
            result = subprocess.run(['docker', 'build', '--build-context', f'contracts={sdk}',
                                     '--target', target, '-f', str(builddir / kind / 'deploy/Dockerfile'),
                                     '-t', image, str(builddir / kind)], stdout=log, stderr=subprocess.STDOUT,
                                    env=ENV, timeout=1200)
        if result.returncode:
            raise RuntimeError(f'{kind} Docker build failed; see private build log')
        s[kind + 'Image'] = json.loads(run('docker', 'image', 'inspect', image))[0]['Id']
        save(s)
    mod = LAB / 'core.mod'
    shutil.copy(ROOT / 'soha/go.mod', mod)
    shutil.copy(ROOT / 'soha/go.sum', mod.with_suffix('.sum'))
    run('go', 'mod', 'edit', '-modfile=' + str(mod), '-replace=github.com/opensoha/soha-contracts=' + str(sdk),
        cwd=ROOT / 'soha', env=dict(ENV, GOWORK='off'))
    with (LAB / 'isolated-resource-tests.json').open('w') as log:
        result = subprocess.run(['go', 'test', '-modfile=' + str(mod), '-race', '-count=1', '-json',
                                 './internal/application/resource', './internal/application/cluster',
                                 '-run', 'TestCRD|TestDeleteFailureAndRetryPreserveOtherClusters|TestDeleteCluster'],
                                cwd=ROOT / 'soha', env=dict(ENV, GOWORK='off'), stdout=log,
                                stderr=subprocess.PIPE, timeout=600)
    private(LAB / 'isolated-resource-tests.stderr', result.stderr.decode())
    s['isolatedTestsExit'] = result.returncode
    save(s)
    if result.returncode:
        raise RuntimeError('Isolated packaged-SDK tests failed')


def inspect(s, key, remote=False):
    command = ['docker', 'inspect', s[key]]
    raw = run('ssh', 'middleware', ' '.join(command)) if remote else run(*command)
    item = json.loads(raw)[0]
    if item['Id'] != s[key] or item['Config']['Labels'].get('soha.test.run') != s['runId']:
        raise RuntimeError('Ownership mismatch; refusing operation')
    return item


def kubectl(s, *args, data=None):
    inspect(s, 'k3sId', remote=True)
    import shlex
    command = shlex.join(['docker', 'exec', '-i', s['k3sId'], 'kubectl',
                          '--kubeconfig=/etc/rancher/k3s/k3s.yaml', *args])
    return run('ssh', 'middleware', command, data=data)


def cluster(s):
    if 'k3sId' in s:
        inspect(s, 'k3sId', remote=True)
        return
    image = 'sha256:d0f79175794edd9694b4a12bafc5c52ae1977369a2f7cf256264e7bd2dae0be9'
    # Fresh Docker volume; API exposed only on remote loopback. No host sysctl changes.
    import shlex
    args = ['docker', 'run', '-d', '--name', s['name'] + '-k3s', '--privileged', '--memory=4g', '--cpus=4',
            '--label', 'soha.test.run=' + s['runId'], '-p', '127.0.0.1::6443',
            '-v', s['name'] + '-k3s:/var/lib/rancher/k3s', image, 'server',
            '--disable=traefik,servicelb,metrics-server', '--write-kubeconfig-mode=600',
            '--tls-san=host.docker.internal', '--cluster-cidr=10.221.0.0/16',
            '--service-cidr=10.231.0.0/16', '--cluster-dns=10.231.0.10']
    s['k3sId'] = run('ssh', 'middleware', shlex.join(args)).strip()
    save(s)
    item = inspect(s, 'k3sId', remote=True)
    s['k3sImage'] = item['Image']
    s['k3sPort'] = item['NetworkSettings']['Ports']['6443/tcp'][0]['HostPort']
    save(s)
    for attempt in range(90):
        try:
            nodes = json.loads(kubectl(s, 'get', 'nodes', '-o', 'json'))['items']
            if nodes and any(x['type'] == 'Ready' and x['status'] == 'True' for x in nodes[0]['status']['conditions']):
                break
        except RuntimeError:
            pass
        time.sleep(2)
    else:
        raise RuntimeError('Dedicated K3s not Ready within 180s')
    config = run('ssh', 'middleware', shlex.join(['docker', 'exec', s['k3sId'], 'cat', '/etc/rancher/k3s/k3s.yaml']))
    private(LAB / 'kubeconfig', config)
    save(s)


def start(s):
    if 'coreId' in s:
        inspect(s, 'coreId')
        return
    name = s['name']
    if 'pgId' not in s:
        run('docker', 'network', 'create', '--label', 'soha.test.run=' + s['runId'], name)
        private(LAB / 'postgres.env', f'POSTGRES_USER=quality\nPOSTGRES_DB=quality\nPOSTGRES_PASSWORD={s["secrets"]["db"]}\n')
        s['pgId'] = run('docker', 'run', '-d', '--name', name + '-pg', '--network', name,
                    '--label', 'soha.test.run=' + s['runId'], '--env-file', str(LAB / 'postgres.env'),
                    'pgvector/pgvector:0.8.5-pg18-trixie').strip()
        save(s)
    inspect(s, 'pgId')
    for attempt in range(40):
        try:
            run('docker', 'exec', s['pgId'], 'pg_isready', '-U', 'quality', '-d', 'quality')
            break
        except RuntimeError:
            time.sleep(1)
    env = {'SOHA_DATABASE_HOST': name + '-pg', 'SOHA_DATABASE_USER': 'quality', 'SOHA_DATABASE_NAME': 'quality',
           'SOHA_DATABASE_PASSWORD': s['secrets']['db'], 'SOHA_AUTH_DEV_PRINCIPAL_PASSWORD': s['secrets']['admin'],
           'SOHA_AUTH_JWT_SECRET': s['secrets']['jwt'], 'SOHA_RUNTIME_EXECUTION_RUNNER_TOKEN': s['secrets']['runner'],
           'SOHA_MONITORING_WEBHOOK_TOKEN': s['secrets']['webhook'],
           'SOHA_SECURITY_CREDENTIAL_ENCRYPTION_KEY': s['secrets']['encryption']}
    private(LAB / 'core.env', ''.join(f'{k}={v}\n' for k, v in env.items()))
    args = ['docker', 'run', '-d', '--name', name + '-core', '--network', name, '--label', 'soha.test.run=' + s['runId'],
            '-p', '127.0.0.1::8080', '--env-file', str(LAB / 'core.env')]
    for key, evidence in s['sources'].items():
        args += ['--label', f'soha.test.{key}-sha={evidence["commit"]}',
                 '--label', f'soha.test.{key}-artifact={evidence["artifactDigest"]}']
    s['coreId'] = run(*args, s['coreImage']).strip()
    save(s)
    item = inspect(s, 'coreId')
    s['baseURL'] = 'http://127.0.0.1:' + item['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort']
    save(s)
    for attempt in range(90):
        try:
            with urllib.request.urlopen(s['baseURL'] + '/readyz', timeout=3) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(2)
    raise RuntimeError('Dedicated Core not ready within 180s; inspect private logs')


def refresh(s):
    inspect(s, 'coreId')
    image = s.get('nextCoreImage')
    if not image or json.loads(run('docker', 'image', 'inspect', image))[0]['Id'] != image:
        raise RuntimeError('Missing exact rebuilt Core image')
    run('docker', 'stop', '--time', '10', s['coreId'])
    run('docker', 'rm', s['coreId'])
    s['coreImage'] = image
    del s['coreId']
    # Persist removal explicitly; the remaining resources and secrets are unchanged.
    private(STATE, s)
    start(s)
    for path in LAB.glob('target-*.json'):
        target = json.loads(path.read_text())
        target.update(baseURL=s['baseURL'], containerId=s['coreId'], imageDigest=s['coreImage'], sources=s['sources'])
        private(path, target)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def api(s, method, path, body=None, token=None):
    request = urllib.request.Request(s['baseURL'] + '/api/v1' + path, method=method,
              data=json.dumps(body).encode() if body is not None else None,
              headers={'Content-Type': 'application/json', **({'Authorization': 'Bearer ' + token} if token else {})})
    try:
        with urllib.request.build_opener(NoRedirect).open(request, timeout=30) as response:
            result = json.load(response)
            if 'items' in result:
                return result['items']
            if 'data' not in result:
                private(LAB / 'api-error.json', result)
                raise RuntimeError(f'API {method} {path} returned no data envelope; private api-error.json')
            return result['data']
    except urllib.error.HTTPError as error:
        with error:
            private(LAB / 'api-error.json', error.read().decode())
        raise RuntimeError(f'API {method} {path} failed ({error.code}); private api-error.json') from None


def provision(s):
    if len(s.get('accounts', {})) == 4 and all(x.get('policyGranted') for x in s['accounts'].values()):
        return
    inspect(s, 'coreId')
    if not s.get('tunnelPorts'):
        remote = inspect(s, 'k3sId', remote=True)
        # Agent runs on middleware amd64, from the same packed SDK and snapshot as Core.
        image = s['name'] + '-agent-amd64'
        builddir = LAB / 'build'
        with (LAB / 'agent-amd64-build.log').open('w') as log:
            result = subprocess.run(['docker', 'build', '--platform', 'linux/amd64', '--build-context',
                     f'contracts={builddir / "package"}', '--target', 'agent-runtime', '-f',
                     str(builddir / 'agent/deploy/Dockerfile'), '-t', image, str(builddir / 'agent')],
                     stdout=log, stderr=subprocess.STDOUT, env=ENV, timeout=1200)
        if result.returncode:
            raise RuntimeError('Agent amd64 build failed; retain exact production Dockerfile and retry after registry recovery')
        s['agentMiddlewareImage'] = json.loads(run('docker', 'image', 'inspect', image))[0]['Id']
        archive = LAB / 'agent-image.tar'
        run('docker', 'save', '-o', str(archive), image)
        with archive.open('rb') as source, (LAB / 'agent-import.log').open('w') as log:
            result = subprocess.run(['ssh', 'middleware', 'docker exec -i ' + s['k3sId'] + ' ctr -n k8s.io images import -'],
                                    stdin=source, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        if result.returncode:
            raise RuntimeError('Agent image import failed')
        save(s)
        config = {'http': {'addr': '0.0.0.0:18080'}, 'auth': {'bearer_token': s['secrets']['agent']},
                  'security': {'allowed_actions': ['platform.crds.delete']},
                  'control_plane': {'enabled': False}, 'audit': {'file_path': '/tmp/actions.jsonl'},
                  'kubernetes': {'enabled': True, 'id': 'quality-agent', 'name': 'Quality Agent', 'kubeconfig': ''}}
        ns = 'quality-lab'
        labels = {'soha.test.run': s['runId']}
        resources = [
            {'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': ns, 'labels': labels}},
            {'apiVersion': 'v1', 'kind': 'ServiceAccount', 'metadata': {'name': 'quality-agent', 'namespace': ns}},
            {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRole', 'metadata': {'name': 'quality-agent'},
             'rules': [{'apiGroups': ['apiextensions.k8s.io'], 'resources': ['customresourcedefinitions'],
                        'verbs': ['get', 'list', 'watch', 'delete']},
                       {'apiGroups': [''], 'resources': ['namespaces', 'nodes'], 'verbs': ['get', 'list']}]},
            {'apiVersion': 'rbac.authorization.k8s.io/v1', 'kind': 'ClusterRoleBinding', 'metadata': {'name': 'quality-agent'},
             'roleRef': {'apiGroup': 'rbac.authorization.k8s.io', 'kind': 'ClusterRole', 'name': 'quality-agent'},
             'subjects': [{'kind': 'ServiceAccount', 'name': 'quality-agent', 'namespace': ns}]},
            {'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': 'quality-agent', 'namespace': ns},
             'stringData': {'config.yaml': json.dumps(config)}},
            {'apiVersion': 'apps/v1', 'kind': 'Deployment', 'metadata': {'name': 'quality-agent', 'namespace': ns},
             'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': 'quality-agent'}}, 'template': {
                 'metadata': {'labels': {'app': 'quality-agent', **labels}}, 'spec': {
                     'serviceAccountName': 'quality-agent', 'containers': [{'name': 'agent', 'image': image,
                     'imagePullPolicy': 'Never', 'env': [{'name': 'SOHA_AGENT_CONFIG_FILE', 'value': '/config/config.yaml'}],
                     'volumeMounts': [{'name': 'config', 'mountPath': '/config', 'readOnly': True}],
                     'resources': {'requests': {'cpu': '50m', 'memory': '64Mi'}, 'limits': {'cpu': '500m', 'memory': '256Mi'}},
                     'readinessProbe': {'httpGet': {'path': '/healthz', 'port': 18080}, 'initialDelaySeconds': 2}}],
                     'volumes': [{'name': 'config', 'secret': {'secretName': 'quality-agent'}}]}}}},
            {'apiVersion': 'v1', 'kind': 'Service', 'metadata': {'name': 'quality-agent', 'namespace': ns},
             'spec': {'type': 'NodePort', 'selector': {'app': 'quality-agent'},
                      'ports': [{'port': 18080, 'targetPort': 18080, 'nodePort': 30080}]}}
        ]
        kubectl(s, 'apply', '-f', '-', data=json.dumps({'apiVersion': 'v1', 'kind': 'List', 'items': resources}).encode())
        kubectl(s, '-n', ns, 'rollout', 'status', 'deployment/quality-agent', '--timeout=120s')
        ports = []
        for _ in range(2):
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', 0)); ports.append(sock.getsockname()[1])
        remote_ip = next(iter(remote['NetworkSettings']['Networks'].values()))['IPAddress']
        log = (LAB / 'tunnel.log').open('w')
        tunnel = subprocess.Popen(['ssh', '-N', '-o', 'ExitOnForwardFailure=yes', '-o', 'ServerAliveInterval=15',
                  '-L', f'127.0.0.1:{ports[0]}:127.0.0.1:{s["k3sPort"]}',
                  '-L', f'127.0.0.1:{ports[1]}:{remote_ip}:30080', 'middleware'],
                  stdout=log, stderr=log, start_new_session=True)
        s['tunnelPid'] = tunnel.pid
        s['tunnelPorts'] = ports
        save(s)
        time.sleep(2)
        if tunnel.poll() is not None:
            raise RuntimeError('Private SSH tunnel failed')
    ports = s['tunnelPorts']
    token = api(s, 'POST', '/auth/login', {'login': 'opensoha', 'password': s['secrets']['admin']})['tokens']['accessToken']
    kubeconfig = (LAB / 'kubeconfig').read_text().replace('https://127.0.0.1:6443', f'https://host.docker.internal:{ports[0]}')
    s.setdefault('clusters', {})
    for mode in ['direct', 'agent', 'denied']:
        if mode in s['clusters']:
            continue
        cluster_id = s['name'] + '-' + mode
        body = {'id': cluster_id, 'name': 'Quality ' + mode, 'environment': 'quality', 'labels': labels,
                'connectionMode': 'agent' if mode == 'agent' else 'direct_kubeconfig'}
        if mode == 'agent':
            body.update(agentEndpoint=f'http://host.docker.internal:{ports[1]}', agentToken=s['secrets']['agent'])
        else:
            body['kubeconfig'] = kubeconfig
        api(s, 'POST', '/clusters', body, token)
        s['clusters'][mode] = cluster_id
        save(s)
    permissions = api(s, 'GET', '/access/permissions', token=token)
    private(LAB / 'permissions.json', permissions)
    read_keys = ['workbench.platform.view', 'workspace.resource.view', 'platform.clusters.view',
                 'platform.namespaces.view', 'platform.extensions.view']
    s.setdefault('accounts', {})
    existing_roles = {x['id'] for x in api(s, 'GET', '/access/roles', token=token)}
    for mode in ['direct', 'agent']:
        for role in ['readonly', 'writer']:
            account_key = mode + '-' + role
            if s['accounts'].get(account_key, {}).get('policyGranted'):
                continue
            role_id = f'quality-{mode}-{role}'
            keys = read_keys + (['platform.extensions.crds.delete'] if role == 'writer' else [])
            if role_id not in existing_roles:
                api(s, 'POST', '/access/roles', {'id': role_id, 'name': role_id, 'scope': 'custom',
                     'permissionKeys': keys, 'capabilities': []}, token)
            if account_key not in s['accounts']:
                account_id = str(uuid.uuid4())
                api(s, 'POST', '/access/users', {'id': account_id, 'username': role_id, 'displayName': role_id,
                     'email': role_id + '@quality.invalid', 'status': 'active', 'roleIds': [role_id],
                     'password': s['secrets'][role]}, token)
                s['accounts'][account_key] = {'id': account_id, 'username': role_id, 'role': role}
                save(s)
            account_id = s['accounts'][account_key]['id']
            if not s['accounts'][account_key].get('scopeGranted'):
                api(s, 'POST', f'/access/users/{account_id}/scope-grants', {'subjectType': 'user', 'subjectId': account_id,
                     'scopeType': 'platform', 'clusterIds': [s['clusters'][mode]], 'role': role_id,
                     'resourceKinds': ['CustomResourceDefinition', 'Namespace', 'Cluster'], 'effect': 'allow', 'enabled': True}, token)
                s['accounts'][account_key]['scopeGranted'] = True
                save(s)
            api(s, 'POST', '/access/policies', {'id': 'quality-' + account_key, 'name': 'Quality ' + account_key,
                 'effect': 'allow', 'priority': 150, 'subjects': {'users': [account_id]},
                 'clusters': {'ids': [s['clusters'][mode]]},
                 'resources': {'kinds': ['CustomResourceDefinition', 'Namespace', 'Cluster']},
                 'actions': ['view', 'list', 'watch'] + (['delete'] if role == 'writer' else []),
                 'reason': 'Dedicated quality-lab account and exact cluster only'}, token)
            s['accounts'][account_key]['policyGranted'] = True
            save(s)
    print('Provisioned four scoped accounts and direct/Agent/denied connections')


def seed(s, modes=None):
    s.setdefault('leases', {})
    for mode in modes or ['direct', 'agent']:
        group = 'quality-' + s['runId'][:8] + '.soha.test'
        plural = 'widgets' + mode
        name = plural + '.' + group
        crd = {'apiVersion': 'apiextensions.k8s.io/v1', 'kind': 'CustomResourceDefinition',
               'metadata': {'name': name, 'labels': {'soha.test.run': s['runId']}}, 'spec': {
                   'group': group, 'scope': 'Namespaced', 'names': {'plural': plural, 'singular': 'widget' + mode,
                   'kind': 'Widget' + mode.title()}, 'versions': [{'name': 'v1', 'served': True, 'storage': True,
                   'schema': {'openAPIV3Schema': {'type': 'object', 'properties': {'spec': {'type': 'object',
                   'properties': {'value': {'type': 'string'}}}}}}}]}}
        if mode not in s['leases']:
            kubectl(s, 'create', '-f', '-', data=json.dumps(crd).encode())
            kubectl(s, 'wait', '--for=condition=Established', '--timeout=30s', 'crd/' + name)
            observed = json.loads(kubectl(s, 'get', 'crd', name, '-o', 'json'))
            s['leases'][mode] = {'id': name, 'uid': observed['metadata']['uid'], 'runId': s['runId'], 'definition': crd}
            save(s)
        lease = s['leases'][mode]
        observed = json.loads(kubectl(s, 'get', 'crd', name, '-o', 'json'))
        if observed['metadata']['uid'] != lease['uid'] or observed['metadata']['labels'].get('soha.test.run') != s['runId']:
            raise RuntimeError('CRD lease mismatch; refusing test target')
        if not lease.get('instanceUid'):
            instance = {'apiVersion': group + '/v1', 'kind': 'Widget' + mode.title(),
                        'metadata': {'name': 'test-widget', 'namespace': 'quality-lab',
                                     'labels': {'soha.test.run': s['runId']}}, 'spec': {'value': 'quality'}}
            created = json.loads(kubectl(s, 'create', '-f', '-', '-o', 'json', data=json.dumps(instance).encode()))
            lease['instanceUid'] = created['metadata']['uid']
            save(s)
        for role in ['readonly', 'writer']:
            target = {'runId': s['runId'], 'mode': 'e2e-' + mode, 'disposable': True,
                      'baseURL': s['baseURL'], 'containerId': s['coreId'], 'imageDigest': s['coreImage'],
                      'sources': s['sources'], 'approvalId': 'user-20260930-dedicated-lab',
                      'role': 'readonly' if role == 'readonly' else 'test-writer',
                      'clusterId': s['clusters'][mode], 'deniedClusterId': s['clusters']['denied'],
                      'namespace': 'quality-lab', 'loginEnv': 'SOHA_E2E_LOGIN', 'passwordEnv': 'SOHA_E2E_PASSWORD',
                      'lease': {key: lease[key] for key in ['id', 'uid', 'runId']}}
            private(LAB / f'target-{mode}-{role}.json', target)


def kube_api(s, method, path, body=None):
    config = json.loads(run('node', '--input-type=module', '-e',
              "import {parse} from 'yaml'; let s='';for await(const c of process.stdin)s+=c;process.stdout.write(JSON.stringify(parse(s)))",
              data=(LAB / 'kubeconfig').read_bytes(), cwd=ROOT / 'soha-web'))
    ca = config['clusters'][0]['cluster']['certificate-authority-data']
    user = config['users'][0]['user']
    for filename, encoded in [('client.crt', user['client-certificate-data']), ('client.key', user['client-key-data'])]:
        private(LAB / filename, base64.b64decode(encoded).decode())
    context = ssl.create_default_context(cadata=base64.b64decode(ca).decode())
    context.load_cert_chain(LAB / 'client.crt', LAB / 'client.key')
    request = urllib.request.Request(f'https://127.0.0.1:{s["tunnelPorts"][0]}' + path, method=method,
              data=json.dumps(body).encode() if body is not None else None, headers={'Content-Type': 'application/json'})
    with urllib.request.build_opener(NoRedirect, urllib.request.HTTPSHandler(context=context)).open(request, timeout=30) as response:
        return json.load(response)


def recreate(s, mode, expected_run, expected_id, expected_uid):
    lease = s['leases'][mode]
    if (s['runId'], lease['id'], lease['uid']) != (expected_run, expected_id, expected_uid):
        raise RuntimeError('Provider does not match requested run/id/UID; refusing replacement')
    path = '/apis/apiextensions.k8s.io/v1/customresourcedefinitions/' + lease['id']
    observed = kube_api(s, 'GET', path)
    if observed['metadata']['uid'] != lease['uid'] or observed['metadata']['labels'].get('soha.test.run') != s['runId']:
        raise RuntimeError('UID/run mismatch; refusing replacement')
    kube_api(s, 'DELETE', path, {'apiVersion': 'v1', 'kind': 'DeleteOptions', 'preconditions': {'uid': lease['uid']}})
    for attempt in range(60):
        try:
            kube_api(s, 'GET', path)
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
            break
        time.sleep(0.5)
    else:
        raise RuntimeError('CRD deletion not finalized')
    created = kube_api(s, 'POST', '/apis/apiextensions.k8s.io/v1/customresourcedefinitions', lease['definition'])
    if created['metadata']['uid'] == lease['uid']:
        raise RuntimeError('CRD replacement retained unexpected UID')
    lease['uid'] = created['metadata']['uid']
    lease.pop('instanceUid', None)
    kubectl(s, 'wait', '--for=condition=Established', '--timeout=30s', 'crd/' + lease['id'])
    save(s)
    seed(s, [mode])
    for role in ['readonly', 'writer']:
        target_path = LAB / f'target-{mode}-{role}.json'
        target = json.loads(target_path.read_text())
        target['lease']['uid'] = lease['uid']
        private(target_path, target)
    print(json.dumps({'id': lease['id'], 'uid': lease['uid'], 'runId': s['runId']}))


def test(s, mode, role, flow):
    target = LAB / f'target-{mode}-{role}.json'
    env = dict(ENV, SOHA_E2E_TARGET_FILE=str(target), SOHA_E2E_LOGIN=s['accounts'][mode + '-' + role]['username'],
               SOHA_E2E_PASSWORD=s['secrets'][role], SOHA_E2E_MUTATIONS='1' if role == 'writer' else '0')
    args = ['npm', 'run', 'test:flow:real' if flow else 'test:api:real']
    output = ROOT / 'soha-web/test-results/run-manifest.json'
    output.unlink(missing_ok=True)
    result = subprocess.run(args, cwd=ROOT / 'soha-web', env=env, timeout=300)
    if result.returncode:
        raise RuntimeError(f'Real {mode}/{role} test failed ({result.returncode})')
    if not output.exists():
        raise RuntimeError('Real test produced no manifest')
    manifest = json.loads(output.read_text())
    private(LAB / f'result-{mode}-{role}-{"flow" if flow else "api"}.json', manifest)
    expected = {'disposable': True, 'containerId': s['coreId'], 'imageDigest': s['coreImage']}
    if (manifest.get('result') != 'PASS' or manifest.get('runId') != s['runId'] or
            manifest.get('mode') != 'e2e-' + mode or manifest.get('environment') != expected or
            manifest.get('sources') != s['sources'] or len(manifest.get('tests', [])) != (2 if flow else 4) or
            any(item.get('result') != 'passed' for item in manifest.get('tests', []))):
        raise RuntimeError('Real manifest failed, skipped, incomplete, or has another target identity')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['build', 'cluster', 'start', 'refresh', 'provision', 'seed', 'recreate', 'test', 'stop'])
    parser.add_argument('--mode', choices=['direct', 'agent'], default='direct')
    parser.add_argument('--role', choices=['readonly', 'writer'], default='readonly')
    parser.add_argument('--flow', action='store_true')
    parser.add_argument('--expected-run')
    parser.add_argument('--expected-id')
    parser.add_argument('--expected-uid')
    args = parser.parse_args()
    s = state()
    if s.get('stopped'):
        raise RuntimeError('Stopped lab state cannot be reused; archive sanitized evidence and prepare a fresh private directory')
    if s.get('cleanupStarted') and args.action != 'stop':
        raise RuntimeError('Cleanup incomplete; only stop may resume this state')
    if args.action == 'test':
        test(s, args.mode, args.role, args.flow)
    elif args.action == 'recreate':
        recreate(s, args.mode, args.expected_run, args.expected_id, args.expected_uid)
        return
    else:
        globals()[args.action](s)
    print(json.dumps({'action': args.action, 'runId': s['runId'], 'result': 'PASS'}))


if __name__ == '__main__':
    main()
