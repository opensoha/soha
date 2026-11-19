#!/usr/bin/env python3
"""Run the mandatory pinned proxy native race gate in disposable Linux containers."""
import argparse
import hashlib
import importlib.util
import io
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import tarfile
import tempfile
import uuid
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
IMAGE = 'golang:1.27.4'
SDK = 'github.com/opensoha/soha/contracts'
PACKAGE = 'github.com/opensoha/soha/internal/networkproxyruntime'
NATIVE_PACKAGE = './internal/networkproxyruntime'
NATIVE_FLAGS = ('-race', '-tags', 'soha_native_integration')
COMPILER_SUFFIXES = {'.go', '.c', '.cc', '.cpp', '.cxx', '.m', '.h', '.hh', '.hpp', '.hxx',
                     '.f', '.F', '.for', '.f90', '.s', '.S', '.sx', '.swig', '.swigcxx', '.syso'}
PURPOSE = 'proxy-native-race'
LABEL = 'soha.test.run'
SOURCE_MODULES = {'.': 'github.com/opensoha/soha', 'contracts': SDK,
                  'cli': 'github.com/opensoha/soha/cli', 'agent': 'github.com/opensoha/soha/agent'}
SOURCE_WORKSPACE = 'go 1.27.4\n\nuse (\n\t.\n\t./contracts\n\t./cli\n\t./agent\n)\n'


def error_message(error):
    return str(error) if isinstance(error, (ValueError, RuntimeError, InterruptedError)) else type(error).__name__


def reserve_receipt(path, report):
    report['receiptPath'] = str(path)
    with path.open('x') as output:
        json.dump(report, output, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())


def persist(report):
    path = Path(report['receiptPath'])
    if path.is_symlink() or not path.is_file() or json.loads(path.read_text()).get('runId') != report['runId']:
        raise ValueError('Receipt ownership changed; write refused')
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', prefix='.' + path.name + '.', dir=path.parent, delete=False) as output:
            temporary = Path(output.name)
            json.dump(report, output, indent=2)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def load(name):
    spec = importlib.util.spec_from_file_location(name.replace('-', '_'), ROOT / 'scripts' / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def command(args, error_log=None, **kwargs):
    result = subprocess.run(args, capture_output=True, timeout=kwargs.pop('timeout', 120), **kwargs)
    if result.returncode:
        if error_log is not None:
            error_log.write(result.stderr.encode() if isinstance(result.stderr, str) else result.stderr)
        raise RuntimeError('Command failed: ' + args[0] + ' ' + args[1])
    return result.stdout


def docker_endpoint(environment, allow_remote):
    if environment.get('DOCKER_CONTEXT'):
        raise ValueError('Explicit Docker context overrides are unsupported')
    endpoint = environment.get('DOCKER_HOST')
    if not endpoint:
        value = json.loads(command(['docker', 'context', 'inspect'], text=True))
        endpoint = value[0]['Endpoints']['docker']['Host']
    parsed = urlsplit(endpoint)
    if parsed.password is not None or parsed.query or parsed.fragment:
        raise ValueError('Unsafe Docker endpoint')
    if parsed.scheme == 'unix' and not parsed.netloc and Path(parsed.path).is_socket():
        return endpoint
    if allow_remote and endpoint == 'ssh://database':
        return endpoint
    raise ValueError('Use a local Docker socket or explicitly allow ssh://database')


def source_workspace_config(workspace):
    if not workspace.startswith(SOURCE_WORKSPACE):
        raise ValueError('Native source gate requires the exact root workspace')
    lines = [line.strip() for line in workspace[len(SOURCE_WORKSPACE):].splitlines() if line.strip()]
    if lines and lines[0] == 'replace (' and lines[-1] == ')':
        aliases = lines[1:-1]
    elif all(line.startswith('replace ') for line in lines):
        aliases = [line.removeprefix('replace ') for line in lines]
    else:
        raise ValueError('Invalid native source workspace aliases')
    replacements = []
    for line in aliases:
        match = re.fullmatch(r'(\S+)(?:\s+(\S+))?\s+=>\s+(\S+)(?:\s+(\S+))?', line)
        if not match:
            raise ValueError('Invalid native source workspace alias')
        replacements.append({'Old': {'Path': match[1], 'Version': match[2] or ''},
                             'New': {'Path': match[3], 'Version': match[4] or ''}})
    return {'Use': [{'DiskPath': '.' if folder == '.' else './' + folder} for folder in SOURCE_MODULES],
            'Replace': replacements}


def declared_sdk_version(text):
    # Native source preflight runs before the Go test container starts.
    required = []
    in_require = False
    for original in text.splitlines():
        line = original.split("//", 1)[0].strip()
        if line == "require (":
            in_require = True
            continue
        if line == ")":
            in_require = False
            continue
        fields = line.split()
        if fields[:1] == ["require"]:
            fields = fields[1:]
        elif not in_require:
            continue
        if fields[:1] == [SDK] and len(fields) == 2:
            required.append(fields[1])
    if len(required) != 1:
        raise ValueError("each consumer must require exactly one SDK version")
    return required[0]


def sdk_input(root, mode):
    root = root.resolve(strict=True)

    def read(name):
        path = root / name
        try:
            path.resolve(strict=True).relative_to(root)
            return path.read_text()
        except (OSError, ValueError):
            raise ValueError('Unavailable or unconfined native source input: ' + name) from None

    core = read('go.mod')
    if not re.search(r'^module github.com/opensoha/soha\s*$', core, re.M):
        raise ValueError('Incorrect Core module identity')
    if re.search(r'^\s*replace\b', core, re.M):
        raise ValueError('Native SDK proof forbids module replacements')
    version = re.search(r'^\s*(?:require\s+)?' + re.escape(SDK) + r'\s+(v\S+)', core, re.M)
    if not version:
        raise ValueError('Missing declared SDK version')
    identity = {'mode': mode, 'declaredVersion': version[1],
                'goModSHA256': hashlib.sha256(core.encode()).hexdigest()}
    if mode == 'source':
        workspace = read('go.work')
        config = source_workspace_config(workspace)
        modules = identity['sourceModules'] = {}
        versions = set()
        for folder, module in SOURCE_MODULES.items():
            content = core if folder == '.' else read(folder + '/go.mod')
            manifest = root / folder / 'go.mod'
            if (root / folder).resolve() != root / folder or (module != SDK and manifest.is_symlink()):
                raise ValueError('Native source SDK aliases require confined manifests')
            if not re.search(r'^module ' + re.escape(module) + r'\s*$', content, re.M):
                raise ValueError('Incorrect native source module identity: ' + module)
            if re.search(r'^\s*replace\b', content, re.M):
                raise ValueError('Native source gate forbids module replacements')
            if module != SDK:
                versions.add(declared_sdk_version(content))
            modules[module] = {'Dir': '/src' if folder == '.' else '/src/' + folder,
                               'goModSHA256': hashlib.sha256(content.encode()).hexdigest()}
        policy = load('source-workspace')
        identity['sourceSDKAliasVersions'] = sorted(policy.validate_sdk_aliases(config, versions))
        identity['sourcePolicySHA256'] = hashlib.sha256(Path(policy.__file__).read_bytes()).hexdigest()
        identity['contractsGoModSHA256'] = modules[SDK]['goModSHA256']
        identity['goWorkSHA256'] = hashlib.sha256(workspace.encode()).hexdigest()
    else:
        sums = read('go.sum').splitlines()
        matches = [line.split()[2] for line in sums if line.startswith(SDK + ' ' + version[1] + ' ')]
        if len(matches) != 1 or not matches[0].startswith('h1:'):
            raise ValueError('Declared SDK lacks a unique archive checksum')
        identity['expectedSum'] = matches[0]
    return identity


def source_file(name, mode):
    parts = Path(name).parts
    if Path(name).is_absolute() or '..' in parts:
        return False
    forbidden = {'.git', '.tmp', 'node_modules', 'dist', '.next', 'coverage', 'bin',
                 'graphify-out', '__pycache__', 'private', '.ssh'}
    if any(part in forbidden or part.startswith('.env') for part in parts):
        return False
    if Path(name).suffix in ('.key', '.pem', '.p12', '.pfx'):
        return False
    if name in ('go.mod', 'go.sum', 'go.work', 'go.work.sum'):
        return mode == 'source' or not name.startswith('go.work')
    if parts and parts[0] in ('contracts', 'cli', 'agent'):
        return mode == 'source'
    return bool(parts) and parts[0] in ('cmd', 'internal', 'pkg', 'migrations')


def add_source_file(archive, name, data, mode):
    info = tarfile.TarInfo('src/' + name)
    info.size, info.mode = len(data), mode & 0o777
    archive.addfile(info, io.BytesIO(data))


def source_archive(root, mode):
    root = root.resolve(strict=True)
    names = command(['git', '-C', str(root), 'ls-files', '--cached', '--others', '--exclude-standard', '-z']).decode().split('\0')
    stream, hashes = io.BytesIO(), {}
    with tarfile.open(fileobj=stream, mode='w') as archive:
        for name in sorted(set(names) - {''}):
            if not source_file(name, mode):
                continue
            path = root / name
            if (path.suffix != '.go' and path.name not in ('go.mod', 'go.sum', 'go.work', 'go.work.sum')
                    and '/testdata/' not in name and not name.startswith('migrations/')):
                continue
            if not path.exists() and not path.is_symlink():
                continue
            if path.resolve(strict=True) != path or not stat.S_ISREG(path.lstat().st_mode):
                raise ValueError('Native source entry must be a confined regular file: ' + name)
            data = path.read_bytes()
            hashes[name] = hashlib.sha256(data).hexdigest()
            add_source_file(archive, name, data, path.stat().st_mode)
    return stream.getvalue(), source_digest(hashes)


def source_digest(hashes):
    return hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()


def embed_catalog(root, mode, base_archive, package_paths):
    """Present the Go tool with names and file types without copying resource bytes."""
    with tarfile.open(fileobj=io.BytesIO(base_archive)) as base:
        existing = {entry.name.removeprefix('src/') for entry in base}
    package_dirs = {root / name for name in package_paths}
    pending = sorted(package_dirs)
    visited = set()
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        while pending:
            directory = pending.pop()
            if directory in visited:
                continue
            visited.add(directory)
            if directory.resolve(strict=True) != directory:
                raise ValueError('Native embed directory must not redirect')
            for path in sorted(directory.iterdir()):
                name = path.relative_to(root).as_posix()
                if name in existing or not source_file(name, mode):
                    continue
                kind = path.lstat().st_mode
                # Resource subdirectories can contain Go templates. At a package root,
                # keep unselected source and object files out of the compiler's view.
                if (directory in package_dirs and stat.S_ISREG(kind)
                        and path.suffix in COMPILER_SUFFIXES and not path.name.startswith(('.', '_'))):
                    continue
                info = tarfile.TarInfo('src/' + name)
                if stat.S_ISDIR(kind):
                    info.type, info.mode = tarfile.DIRTYPE, 0o755
                    pending.append(path)
                elif not stat.S_ISREG(kind):
                    # A self-link exposes a nonregular leaf to Go without exposing its target.
                    info.type, info.linkname = tarfile.SYMTYPE, path.name
                archive.addfile(info)
    return stream.getvalue()


def local_packages(output, source='/src'):
    root = PurePosixPath(source)
    decoder = json.JSONDecoder()
    nonspace = re.compile(r'\S')
    position = 0
    found_local = False
    while (start := nonspace.search(output, position)) is not None:
        package, position = decoder.raw_decode(output, start.start())
        directory = PurePosixPath(package.get('Dir') or '.')
        if not directory.is_relative_to(root):
            continue
        relative = directory.relative_to(root)
        if '..' in relative.parts:
            raise ValueError('Go package directory escapes the native archive')
        found_local = True
        yield relative, package
    if not found_local:
        raise ValueError('Go returned no local native package metadata')


def local_embed_files(output, mode, source='/src'):
    files = set()
    for directory, package in local_packages(output, source):
        for key in ('EmbedFiles', 'TestEmbedFiles', 'XTestEmbedFiles'):
            for value in package.get(key) or []:
                filename = PurePosixPath(value)
                name = (directory / filename).as_posix()
                if filename.is_absolute() or not source_file(name, mode):
                    raise ValueError('Go selected a file outside the native archive policy: ' + name)
                files.add(name)
    return files


def add_embeds(root, mode, base_archive, names):
    stream, hashes = io.BytesIO(), {}
    with tarfile.open(fileobj=io.BytesIO(base_archive)) as base, tarfile.open(fileobj=stream, mode='w') as archive:
        for entry in base:
            data = base.extractfile(entry).read()
            name = entry.name.removeprefix('src/')
            hashes[name] = hashlib.sha256(data).hexdigest()
            archive.addfile(entry, io.BytesIO(data))
        for name in sorted(names):
            path = root / name
            if (not source_file(name, mode) or path.resolve(strict=True) != path
                    or not stat.S_ISREG(path.lstat().st_mode)):
                raise ValueError('Go embed must be a confined regular file: ' + name)
            if name in hashes:
                continue
            data = path.read_bytes()
            hashes[name] = hashlib.sha256(data).hexdigest()
            add_source_file(archive, name, data, path.stat().st_mode)
    return stream.getvalue(), source_digest(hashes)


def install_native_source(resources, prep, root, mode, base_archive, error_log=None):
    list_command = ['exec', prep, 'go', 'list', '-deps', '-test', *NATIVE_FLAGS]
    # Before assets exist, allow incomplete packages only to discover their directories.
    directories = resources.call([*list_command, '-e', '-json=Dir', NATIVE_PACKAGE],
                                 text=True, timeout=600, error_log=error_log)
    package_paths = {directory.as_posix() for directory, _ in local_packages(directories)}
    resources.call(['cp', '-', prep + ':/'], input=embed_catalog(root, mode, base_archive, package_paths))
    metadata = resources.call([*list_command, '-json=Dir,EmbedFiles,TestEmbedFiles,XTestEmbedFiles', NATIVE_PACKAGE],
                              text=True, timeout=600, error_log=error_log)
    names = local_embed_files(metadata, mode)
    archive, tree_hash = add_embeds(root, mode, base_archive, names)
    resources.call(['exec', prep, 'rm', '-rf', '/src'])
    resources.call(['cp', '-', prep + ':/'], input=archive)
    return archive, tree_hash


def write_input_identity(report):
    report['gateSources'] = {name: hashlib.sha256((ROOT / 'scripts' / name).read_bytes()).hexdigest()
        for name in ('run-proxy-native-tests.py', 'check-go-test-evidence.py',
                     'prepare-proxy-test-engines.py', 'proxy-test-engines.json')}


def verify_sdk(value, identity):
    if value.get('Path') != SDK or value.get('Replace') or value.get('Error'):
        raise ValueError('SDK module identity/replacement mismatch')
    if identity['mode'] == 'source':
        verify_source_module(value, identity)
    elif (value.get('Version') != identity['declaredVersion'] or value.get('Sum') != identity['expectedSum'] or value.get('Main')):
        raise ValueError('SDK did not resolve to the declared checksummed distribution')


def verify_source_module(value, identity):
    expected = identity['sourceModules'].get(value.get('Path'))
    if (expected is None or value.get('Dir') != expected['Dir'] or value.get('Main') is not True
            or value.get('Replace') or value.get('Error')):
        raise ValueError('Module did not resolve to this source workspace')


def labels(run):
    return ['--label', LABEL + '=' + run, '--label', 'soha.test.purpose=' + PURPOSE]


def owned(value, kind, name, identifier, run, image=None, network=None):
    config = value.get('Config', {}) if kind != 'network' else value
    if (value.get('Id') != identifier or value.get('Name', '').lstrip('/') != name and kind != 'image'
            or config.get('Labels', {}).get(LABEL) != run
            or config.get('Labels', {}).get('soha.test.purpose') != PURPOSE):
        raise ValueError('Resource identity/ownership changed; cleanup refused')
    if kind == 'image' and name not in value.get('RepoTags', []):
        raise ValueError('Owned image tag changed; cleanup refused')
    if kind == 'container':
        host = value.get('HostConfig', {})
        if (value.get('Image') != image or host.get('Binds') or value.get('Mounts')
                or host.get('Privileged') or host.get('PortBindings') or host.get('PublishAllPorts')
                or host.get('NetworkMode') != network or host.get('ExtraHosts')
                or set(value.get('NetworkSettings', {}).get('Networks', {})) != {network}
                or host.get('PidMode') or host.get('IpcMode') == 'host' or host.get('Devices') or host.get('CapAdd')
                or host.get('NanoCpus') != 2000000000 or host.get('Memory') != 2 * 1024**3
                or host.get('PidsLimit') != 256):
            raise ValueError('Container isolation changed; cleanup refused')
    if kind == 'network' and (not value.get('Internal') or value.get('Driver') != 'bridge'
            or value.get('Containers')):
        raise ValueError('Network isolation/attachment changed; cleanup refused')
    if kind == 'network' and network is not None:
        subnets = [entry.get('Subnet') for entry in value.get('IPAM', {}).get('Config', [])]
        if subnets != [network] or value.get('EnableIPv6'):
            raise ValueError('Network IPAM changed; cleanup refused')


class Resources:
    def __init__(self, docker, run, report):
        self.docker, self.run, self.report = docker, run, report
        self.items = report.setdefault('resources', [])

    def checkpoint(self, operation, identifier=None):
        self.report['resources'] = self.items
        self.report['lastAction'] = {'operation': operation, 'resourceId': identifier}
        persist(self.report)

    def call(self, args, **kwargs):
        mutation = args[0] in ('start', 'stop', 'exec', 'cp') or args[0] in ('container', 'network', 'image') and args[1] == 'rm'
        if mutation:
            self.checkpoint('docker-' + args[0], args[-1] if args[0] in ('stop', 'cp') or args[1] == 'rm' else args[1])
        result = command([*self.docker, *args], **kwargs)
        if mutation:
            self.report['lastAction']['completed'] = True
            persist(self.report)
        return result

    def inspect(self, kind, identifier):
        return json.loads(self.call([kind, 'inspect', identifier], text=True))[0]

    def create(self, kind, name, args, image=None, network=None):
        # Register before mutation: a lost response must still leave an owned cleanup target.
        item = {'kind': kind, 'name': name, 'id': None, 'image': image, 'network': network}
        self.items.append(item)
        self.checkpoint('create-' + kind, name)
        identifier = self.call(args, text=True).strip()
        if not re.fullmatch(r'(sha256:)?[0-9a-f]{64}', identifier):
            raise ValueError('Invalid Docker resource ID')
        item['id'] = identifier
        persist(self.report)
        value = self.inspect(kind, identifier)
        owned(value, kind, name, identifier, self.run, image, network)
        return identifier

    def cleanup(self):
        errors = []
        for item in reversed(self.items):
            entry = dict(item, result='FAIL')
            self.report.setdefault('cleanup', []).append(entry)
            kind, name = item['kind'], item['name']
            try:
                self.report['resources'] = self.items
                persist(self.report)
                listing = self.call([kind, 'ls', *(['--all'] if kind in ('container', 'image') else []),
                                     '--no-trunc', '--format', '{{json .}}'], text=True)
                rows = [json.loads(line) for line in listing.splitlines() if line]
                key = {'container': 'Names', 'network': 'Name', 'image': 'Repository'}[kind]
                named = [row for row in rows if (row.get('Repository', '') + ':' + row.get('Tag', '') == name if kind == 'image'
                         else row.get(key, '').lstrip('/') == name)]
                present = any(row.get('ID', row.get('Id')) == item['id'] for row in rows) if item['id'] else bool(named)
                if not present:
                    if item['id'] and named:
                        raise ValueError('Resource name was reused by another ID; cleanup refused')
                    entry['result'] = 'ALREADY_ABSENT'
                    persist(self.report)
                    continue
                value = self.inspect(kind, item['id'] or name)
                identifier = item['id'] or value['Id']
                owned(value, kind, name, identifier, self.run, item['image'], item['network'])
                item['id'] = identifier
                entry['id'] = identifier
                persist(self.report)
                if kind == 'image':
                    if self.call(['ps', '-aq', '--filter', 'ancestor=' + identifier], text=True).strip():
                        raise ValueError('Owned image remains in use')
                    self.call(['image', 'rm', name])
                else:
                    self.call([kind, 'rm', *(['--force', '--volumes'] if kind == 'container' else []), identifier])
                # Successful listing, rather than failed inspect, proves exact removal.
                remaining = self.call([kind, 'ls', *(['--all'] if kind in ('container', 'image') else []), '--no-trunc', '--format', '{{json .}}'], text=True)
                if any(identifier == row.get('ID', row.get('Id')) for row in map(json.loads, remaining.splitlines())):
                    raise ValueError('Resource removal not confirmed')
                entry['result'] = 'PASS'
                persist(self.report)
            except Exception as exc:
                entry['result'] = 'FAIL'
                entry['error'] = type(exc).__name__
                errors.append(name)
                try:
                    persist(self.report)
                except Exception as write_error:
                    self.report.setdefault('receiptWriteErrors', []).append(error_message(write_error))
        if errors:
            raise RuntimeError('Owned cleanup incomplete: ' + ', '.join(errors))


def install_kernel_metadata(resources, container, engines):
    names = ('mihomo', 'sing-box', 'v2ray')
    if set(engines) != set(names) or any(not re.fullmatch(r'[0-9a-f]{64}',
            engines[name].get('binary_sha256', '')) for name in names):
        raise ValueError('Invalid pinned kernel installation inputs')
    matches = [item for item in resources.items if item['kind'] == 'container'
               and item['id'] == container and item['name'] == 'soha-proxy-prep-' + resources.run]
    if len(matches) != 1:
        raise ValueError('Kernel installation requires the owned preparation container')
    item = matches[0]
    paths = ['/gate/engines', *('/gate/engines/' + name for name in names)]

    def execute(*args):
        owned(resources.inspect('container', container), 'container', item['name'], container,
              resources.run, item['image'], item['network'])
        return resources.call(['exec', container, *args], text=True)

    if execute('id', '-u').strip() != '0':
        raise ValueError('Kernel installation requires preparation runtime UID 0')
    copied_owner = (os.geteuid(), os.getegid())

    def inspect_files(installed):
        rows = execute('stat', '-c', '%n %u %g %f %s', '--', *paths).splitlines()
        if len(rows) != len(paths):
            raise ValueError('Invalid kernel installation metadata')
        result = {}
        for expected, row in zip(paths, rows):
            fields = row.split()
            if len(fields) != 5 or fields[0] != expected:
                raise ValueError('Invalid kernel installation metadata')
            uid, gid, mode, size = int(fields[1]), int(fields[2]), int(fields[3], 16), int(fields[4])
            regular = stat.S_ISDIR(mode) if expected == paths[0] else stat.S_ISREG(mode)
            owners = {(0, 0)} if installed else {(0, 0), copied_owner}
            if (not regular or stat.S_IMODE(mode) != 0o700 or (uid, gid) not in owners
                    or size < 0 or expected != paths[0] and not 0 < size <= 128 << 20):
                raise ValueError('Unsafe kernel installation metadata: ' + expected)
            result[expected] = {'uid': uid, 'gid': gid, 'mode': '0700',
                                'kind': 'directory' if expected == paths[0] else 'regular', 'size': size}
        hashes = execute('sha256sum', '--', *paths[1:]).splitlines()
        if len(hashes) != len(names):
            raise ValueError('Invalid installed kernel checksums')
        for name, path, row in zip(names, paths[1:], hashes):
            fields = row.split()
            if len(fields) != 2 or fields != [engines[name]['binary_sha256'], path]:
                raise ValueError('Copied kernel digest mismatch: ' + name)
            result[path]['sha256'] = fields[0]
        return result

    installation = {'result': 'FAIL', 'runtimeUID': 0, 'copiedOwnerUID': copied_owner[0],
                    'copiedOwnerGID': copied_owner[1], 'before': inspect_files(False)}
    resources.report['kernelInstallation'] = installation
    persist(resources.report)
    # Plain docker cp can preserve host ownership; normalize only these verified private fixtures.
    execute('chown', '--no-dereference', '0:0', '--', *paths)
    installation['after'] = inspect_files(True)
    installation['result'] = 'PASS'
    persist(resources.report)


def available_subnet(resources):
    ids = resources.call(['network', 'ls', '-q'], text=True).split()
    networks = json.loads(resources.call(['network', 'inspect', *ids], text=True)) if ids else []
    if not isinstance(networks, list):
        raise ValueError('Invalid Docker network inspection shape')
    used = []
    for network in networks:
        ipam = network.get('IPAM') if isinstance(network, dict) else None
        if not isinstance(ipam, dict) or 'Config' not in ipam:
            raise ValueError('Invalid Docker network IPAM shape')
        configs = ipam['Config']
        # Docker's built-in host and none networks have no address pools.
        if configs is None:
            continue
        if not isinstance(configs, list):
            raise ValueError('Invalid Docker network IPAM.Config shape')
        for config in configs:
            subnet = config.get('Subnet') if isinstance(config, dict) else None
            if not isinstance(subnet, str) or not subnet:
                raise ValueError('Invalid Docker IPAM subnet shape')
            try:
                used.append(ipaddress.ip_network(subnet))
            except ValueError:
                raise ValueError('Invalid Docker IPAM subnet CIDR') from None
    candidates = list(ipaddress.ip_network('172.16.0.0/12').subnets(new_prefix=24))
    offset = int(resources.run[:8], 16) % len(candidates)
    for n in candidates[offset:] + candidates[:offset]:
        if not any(n.overlaps(other) for other in used if n.version == other.version):
            return str(n)
    raise ValueError('No unused internal fixture subnet in 172.16/12')


def execute(args, report):
    write_input_identity(report)
    top = Path(command(['git', '-C', str(ROOT), 'rev-parse', '--show-toplevel'], text=True).strip()).resolve()
    if top != ROOT:
        raise ValueError('Native source must be the owning Core Git checkout')
    identity = sdk_input(ROOT, args.sdk_mode)
    base_archive, _ = source_archive(ROOT, args.sdk_mode)
    report.update(sdk=identity, head=command(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip(),
                  sourceBaseArchiveSHA256=hashlib.sha256(base_archive).hexdigest())
    endpoint = docker_endpoint(os.environ, args.allow_remote_docker)
    docker = ['docker', '--host', endpoint]
    report['dockerEndpoint'] = endpoint
    persist(report)
    if endpoint == 'ssh://database':
        remote = load('local_verify_remote')
        remote.validate_endpoint(endpoint)
        report['capacity'] = remote.remote_capacity(docker, int(5.5 * 1024**3))
    image = subprocess.run([*docker, 'image', 'inspect', IMAGE], capture_output=True, timeout=30)
    if image.returncode:
        report['baseImagePreparation'] = {'operation': 'pull', 'reference': IMAGE, 'platform': 'linux/amd64', 'retained': True}
        persist(report)
        command([*docker, 'pull', '--platform', 'linux/amd64', IMAGE], timeout=600)
    base = json.loads(command([*docker, 'image', 'inspect', IMAGE], text=True))[0]
    if (base.get('Os') != 'linux' or base.get('Architecture') != 'amd64'
            or not re.fullmatch(r'sha256:[0-9a-f]{64}', base.get('Id', ''))
            or not any(re.fullmatch(r'(docker.io/library/)?golang@sha256:[0-9a-f]{64}', d) for d in base.get('RepoDigests', []))):
        raise ValueError('Official Linux amd64 Go image identity is required')
    report['baseImage'] = {k: base[k] for k in ('Id', 'RepoDigests', 'Os', 'Architecture')}
    report['baseImage'].update(alreadyPresent=image.returncode == 0, retained=True)
    persist(report)
    run = report['runId']
    resources = Resources(docker, run, report)
    budget = ['--cpus', '2', '--memory', '2g', '--pids-limit', '256']
    execution_error = None
    try:
        prep_name = 'soha-proxy-prep-' + run
        prep = resources.create('container', prep_name, ['create', '--name', prep_name, *labels(run),
            *budget, '--platform', 'linux/amd64', '--network', 'bridge', '--workdir', '/src',
            '--env', 'GOTOOLCHAIN=local', '--env', 'CGO_ENABLED=1', '--env', 'GOMAXPROCS=2',
            '--env', 'GODEBUG=embedfollowsymlinks=0',
            '--env', 'GOFLAGS=-mod=readonly -p=2', '--env', 'GOWORK=' + ('/src/go.work' if args.sdk_mode == 'source' else 'off'),
            base['Id'], 'sleep', '3600'], base['Id'], 'bridge')
        resources.call(['start', prep])
        version = resources.call(['exec', prep, 'go', 'version'], text=True).strip()
        if version != 'go version go1.27.4 linux/amd64':
            raise ValueError('Native Go toolchain mismatch')
        report['toolchain'] = version
        resources.call(['cp', '-', prep + ':/'], input=base_archive)
        with tempfile.TemporaryDirectory(prefix='proxy-native-', dir=os.environ.get('OPENSOHA_VERIFY_DIR')) as tmp:
            engines = Path(tmp) / 'engines'
            report['kernels'] = load('prepare-proxy-test-engines').prepare(engines, 'linux-amd64', json.loads((ROOT / 'scripts/proxy-test-engines.json').read_text()))
            resources.call(['exec', prep, 'mkdir', '-p', '/gate'])
            resources.call(['cp', str(engines), prep + ':/gate/engines'])
        if args.sdk_mode == 'declared':
            download = json.loads(resources.call(['exec', prep, 'go', 'mod', 'download', '-json', SDK], text=True))
            verify_sdk(download, identity)
            identity['download'] = {k: download.get(k) for k in ('Path', 'Version', 'Sum', 'GoModSum', 'Origin')}
        actual = json.loads(resources.call(['exec', prep, 'go', 'list', '-m', '-json', SDK], text=True))
        verify_sdk(actual, identity)
        identity['resolved'] = {k: actual.get(k) for k in ('Path', 'Version', 'Main', 'Dir', 'Sum', 'GoModSum')}
        if args.sdk_mode == 'source':
            identity['resolvedSourceModules'] = {SDK: identity['resolved']}
            for module in identity['sourceModules']:
                if module == SDK:
                    continue
                resolved = json.loads(resources.call(['exec', prep, 'go', 'list', '-m', '-json', module], text=True))
                verify_source_module(resolved, identity)
                identity['resolvedSourceModules'][module] = {k: resolved.get(k)
                    for k in ('Path', 'Version', 'Main', 'Dir', 'Sum', 'GoModSum')}
        if args.sdk_mode == 'declared':
            ziphash = '/go/pkg/mod/cache/download/' + SDK + '/@v/' + identity['declaredVersion'] + '.ziphash'
            identity['downloadDirHash'] = resources.call(['exec', prep, 'cat', ziphash], text=True).strip()
            if identity['downloadDirHash'] != identity['expectedSum']:
                raise ValueError('Downloaded SDK directory checksum mismatch')
        install_kernel_metadata(resources, prep, report['kernels']['engines'])
        # Compilation/dependency network access ends before either acceptance container starts.
        preparation = args.out.with_name(args.out.stem + '.preparation.log')
        report['preparationLog'] = str(preparation)
        with preparation.open('xb') as log:
            archive, tree_hash = install_native_source(resources, prep, ROOT, args.sdk_mode, base_archive, log)
            report.update(workingTreeSHA256=tree_hash, sourceArchiveSHA256=hashlib.sha256(archive).hexdigest())
            persist(report)
            identity['actualGoSumSHA256'] = resources.call(['exec', prep, 'sha256sum', '/src/go.sum'], text=True).split()[0]
            for invocation in (['go', 'mod', 'verify'], ['go', 'test', *NATIVE_FLAGS, '-c',
                    '-o', '/gate/native.test', NATIVE_PACKAGE], ['go', 'build', '-o', '/gate/test2json', 'cmd/test2json']):
                resources.checkpoint('native-preparation', prep)
                result = subprocess.run([*docker, 'exec', prep, *invocation], stdout=log, stderr=log, timeout=1800)
                report['lastAction']['exitCode'] = result.returncode
                persist(report)
                if result.returncode:
                    raise ValueError('Native preparation failed; see preparation log')
        report['testBinarySHA256'] = resources.call(['exec', prep, 'sha256sum', '/gate/native.test'], text=True).split()[0]
        resources.call(['exec', prep, 'rm', '-rf', '/src', '/go/pkg', '/root/.cache/go-build'])
        resources.call(['stop', '--time', '10', prep])
        tag = 'soha-proxy-native:' + run
        candidate = resources.create('image', tag, ['commit', prep, tag])
        report['candidateImageId'] = candidate
        network_name = 'soha-proxy-net-' + run
        subnet = available_subnet(resources)
        network = resources.create('network', network_name, ['network', 'create', '--internal', '--driver', 'bridge',
            '--subnet', subnet, *labels(run), network_name], network=subnet)
        report['network'] = {'id': network, 'subnet': subnet, 'internal': True}
        evidence = load('check-go-test-evidence')
        failed_profiles = []
        for profile, network_mode, pattern in (
                ('dataplane', network_name, '^(TestRealEngineIntegration|TestRealUDPIntegration)$'),
                ('parser', 'none', '^TestRealKernelPreflightIntegration$')):
            name = 'soha-proxy-' + profile + '-' + run
            container = resources.create('container', name, ['create', '--name', name, *labels(run), *budget,
                '--network', network_mode, '--workdir', '/gate', '--env', 'SOHA_REAL_ENGINE_DIR=/gate/engines',
                '--env', 'GOMAXPROCS=2', '--env', 'GOPROXY=off', '--env', 'GOSUMDB=off',
                candidate, '/gate/test2json', '-t', '-p', PACKAGE, '/gate/native.test',
                '-test.v=test2json', '-test.count=1', '-test.timeout=8m', '-test.run', pattern], candidate, network_mode)
            log = args.out.with_name(args.out.stem + '.' + profile + '.go.json')
            err = args.out.with_name(args.out.stem + '.' + profile + '.stderr.log')
            report.setdefault('profiles', {})[profile] = {'containerId': container, 'log': str(log), 'stderr': str(err)}
            resources.checkpoint('native-' + profile, container)
            with log.open('xb') as stdout, err.open('xb') as stderr:
                process = subprocess.run([*docker, 'start', '--attach', container], stdout=stdout, stderr=stderr, timeout=540)
            state = resources.inspect('container', container)['State']
            report.setdefault('profiles', {})[profile] = {'containerId': container, 'log': str(log),
                'stderr': str(err), 'exitCode': state.get('ExitCode'), 'attachExitCode': process.returncode}
            try:
                if state.get('Running') or state.get('OOMKilled') or process.returncode:
                    raise ValueError('Native container did not complete normally')
                report['profiles'][profile]['evidence'] = evidence.validate_proxy_native(log.read_text(), state.get('ExitCode'), profile == 'parser')
            except ValueError as exc:
                report['profiles'][profile]['error'] = str(exc)
                failed_profiles.append(profile)
        if failed_profiles:
            raise ValueError('Native profiles failed: ' + ', '.join(failed_profiles))
        report['terminalTests'] = 26
    except Exception as exc:
        execution_error = exc
        report['executionError'] = error_message(exc)
        raise
    finally:
        try:
            resources.cleanup()
        except Exception as exc:
            report['cleanupError'] = error_message(exc)
            if execution_error is None:
                raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--sdk-mode', required=True, choices=('source', 'declared'))
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--allow-remote-docker', action='store_true')
    args = parser.parse_args()
    artifacts = [args.out, *[args.out.with_name(args.out.stem + suffix) for suffix in (
        '.preparation.log', '.dataplane.go.json', '.dataplane.stderr.log', '.parser.go.json', '.parser.stderr.log')]]
    if any(p.exists() or p.is_symlink() for p in artifacts) or not args.out.parent.is_dir():
        parser.error('--out must be a new receipt in an existing directory')
    args.out = args.out.parent.resolve() / args.out.name
    report = {'schemaVersion': 'proxy-native-gate/v1', 'runId': uuid.uuid4().hex, 'result': 'FAIL', 'resources': [], 'cleanup': []}
    def interrupted(signum, frame):
        raise InterruptedError('Native gate interrupted')
    previous = {s: signal.signal(s, interrupted) for s in (signal.SIGTERM, signal.SIGINT)}
    try:
        reserve_receipt(args.out, report)
        execute(args, report)
        report['result'] = 'PASS'
    except Exception as exc:
        report['error'] = error_message(exc)
    finally:
        for s, handler in previous.items():
            signal.signal(s, handler)
        try:
            persist(report)
        except Exception as exc:
            report['result'] = 'FAIL'
            report.setdefault('receiptWriteErrors', []).append(error_message(exc))
    print(json.dumps({'receipt': str(args.out), **{key: report[key] for key in ('result', 'receiptPath', 'error', 'executionError', 'cleanupError', 'receiptWriteErrors') if key in report}}))
    return 0 if report['result'] == 'PASS' else 1


if __name__ == '__main__':
    raise SystemExit(main())
