"""Opt-in remote image verification on an attested, disposable Buildx builder.

No default context changes, shared builder pruning, host mounts or retained tags.
Only the explicitly authorized database SSH alias is accepted.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import uuid


ENDPOINT = 'ssh://database'
GIB = 1024 ** 3


def command(arguments):
    result = subprocess.run(arguments, env=dict(os.environ, OPENSOHA_VERIFY_INTERNAL='1'),
                            capture_output=True, text=True, timeout=180)
    if result.returncode:
        raise RuntimeError('Remote image verification command failed: ' + arguments[0])
    return result.stdout


def validate_endpoint(endpoint):
    if endpoint != ENDPOINT or os.environ.get('DOCKER_HOST', endpoint) != endpoint:
        raise RuntimeError('Remote verification requires the explicit ssh://database endpoint')
    if os.environ.get('DOCKER_CONTEXT') or os.environ.get('BUILDX_BUILDER'):
        raise RuntimeError('Remote verification refuses context/builder environment overrides')
    if any(os.environ.get(key, '').lower() in ('1', 'true', 'yes')
           for key in ('CI', 'GITHUB_ACTIONS', 'OPENSOHA_VERIFY_RELEASE')):
        raise RuntimeError('Remote verification cannot bypass its guard in CI/release mode')


def remote_capacity(docker, minimum):
    info = json.loads(command(docker + ['info', '--format', '{{json .}}']))
    root = info.get('DockerRootDir', '')
    if not info.get('ID') or not re.fullmatch(r'/[a-zA-Z0-9_./-]+', root) or '..' in root.split('/'):
        raise RuntimeError('Cannot attest remote Docker identity/root')
    # SSH is the already authorized Docker transport. This read checks the
    # actual Docker-root filesystem; builder du is not free-space evidence.
    fields = command(['ssh', '-o', 'BatchMode=yes', 'database', 'df', '-Pk', '--', root]).splitlines()
    if len(fields) != 2 or len(fields[1].split()) < 6:
        raise RuntimeError('Cannot establish remote Docker-root free space')
    free = int(fields[1].split()[3]) * 1024
    if free < minimum:
        raise RuntimeError('Remote Docker-root free space is below the unchanged verification floor')
    return {'engineID': info['ID'], 'engineName': info.get('Name'), 'root': root, 'freeBytes': free}


def inspect(docker, kind, name):
    listing = [kind, 'ls', *(['--all'] if kind == 'container' else []), '--format', '{{json .}}']
    rows = [json.loads(line) for line in command(docker + listing).splitlines()]
    key = 'Names' if kind == 'container' else 'Name'
    # An empty successful listing proves absence. A failed inspect/transport
    # does not, even though Docker also uses exit 1 for both of those cases.
    if not any(row.get(key, '').lstrip('/') == name for row in rows):
        return None
    raw = command(docker + [kind, 'inspect', name])
    values = json.loads(raw)
    if len(values) != 1:
        raise RuntimeError('Ambiguous remote resource identity')
    return values[0]


def validate_build_inputs(build, private):
    private = Path(private).resolve(strict=True)

    def owned(path, directory=False):
        resolved = Path(path).resolve(strict=True)
        if not resolved.is_relative_to(private) or not (resolved.is_dir() if directory else resolved.is_file()):
            raise RuntimeError('Remote build inputs must stay inside the owned source snapshots')

    owned(Path.cwd(), True)
    owned(build[-1], True)
    for index, argument in enumerate(build):
        # Pflag accepts attached and clustered short options, including -fFILE
        # and -qfFILE. Refuse ambiguity before Docker can reinterpret a path or
        # an output flag that was not checked against this owned snapshot.
        if argument.startswith('-') and not argument.startswith('--') and argument != '-f' and not argument.startswith('-f='):
            raise RuntimeError('Remote verification requires long options or explicit -f <file> / -f=<file>')
        option, equal, inline = argument.partition('=')
        if option not in ('--build-context', '--file', '-f'):
            continue
        value = inline if equal else build[index + 1] if index + 1 < len(build) else ''
        if option == '--build-context':
            name, delimiter, path = value.partition('=')
            if name != 'contracts' or not delimiter:
                raise RuntimeError('Only the owned contracts named context is supported')
            owned(path, True)
        else:
            owned(value)


def cleanup_image(docker, tag, iidfile, label, run_id):
    rows = command(docker + ['image', 'ls', '--no-trunc', '--format', '{{json .}}', '--filter', 'reference=' + tag]).splitlines()
    if not iidfile.is_file():
        if rows:
            raise RuntimeError('Remote image without an IID receipt is retained')
        return
    iid = iidfile.read_text().strip()
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', iid) or not rows:
        raise RuntimeError('Remote image identity cannot be established')
    values = json.loads(command(docker + ['image', 'inspect', tag]))
    if (len(values) != 1 or values[0].get('Id') != iid or
            (values[0].get('Config', {}).get('Labels') or {}).get(label) != run_id):
        raise RuntimeError('Remote image identity changed; retained')
    if command(docker + ['ps', '-aq', '--filter', 'ancestor=' + iid]).strip():
        raise RuntimeError('Remote image has container references; retained')
    command(docker + ['image', 'rm', tag])
    if command(docker + ['image', 'ls', '--format', '{{json .}}', '--filter', 'reference=' + tag]).strip():
        raise RuntimeError('Remote verification tag removal is incomplete')


class RemoteBuilder:
    def __init__(self, endpoint, run_id, directory, report):
        self.docker = ['docker', '--host', endpoint]
        self.name = 'opensoha-verify-' + run_id
        self.node = self.name + '0'
        self.container = 'buildx_buildkit_' + self.node
        self.volume = self.container + '_state'
        self.directory, self.report = directory, report
        self.attempted, self.created, self.bootstrap_attempted, self.identity = False, False, False, None

    def builder(self):
        # Installed Buildx exposes structured ls, but inspect has no --format.
        rows = [json.loads(line) for line in command(self.docker + ['buildx', 'ls', '--format', 'json']).splitlines()]
        matches = [value for value in rows if value.get('Name') == self.name]
        if len(matches) != 1:
            raise RuntimeError('Original remote builder metadata is missing or ambiguous')
        value = matches[0]
        nodes = value.get('Nodes', [])
        if (value.get('Name') != self.name or value.get('Driver') != 'docker-container' or
                len(nodes) != 1 or nodes[0].get('Name') != self.node or nodes[0].get('Endpoint') != ENDPOINT):
            raise RuntimeError('Remote builder identity changed; cleanup refused')
        return value

    def capture(self):
        self.builder()
        container = inspect(self.docker, 'container', self.container)
        volume = inspect(self.docker, 'volume', self.volume)
        if not container or not volume:
            raise RuntimeError('Remote builder resource receipts are incomplete')
        mounts = container.get('Mounts', [])
        if (container.get('Name') != '/' + self.container or not container.get('Id') or
                not container.get('Image', '').startswith('sha256:') or len(mounts) != 1 or
                mounts[0].get('Type') != 'volume' or mounts[0].get('Name') != self.volume or
                mounts[0].get('Destination') != '/var/lib/buildkit' or not mounts[0].get('RW') or
                volume.get('Name') != self.volume or volume.get('Driver') != 'local'):
            raise RuntimeError('Remote builder container/volume shape changed; cleanup refused')
        identity = {'containerID': container['Id'], 'containerName': container['Name'],
                    'imageID': container['Image'], 'volume': {key: volume.get(key) for key in
                    ('Name', 'Driver', 'Mountpoint', 'CreatedAt', 'Labels', 'Options')}}
        if self.identity is not None and identity != self.identity:
            raise RuntimeError('Remote builder resource identity changed; retained')
        self.identity = identity
        self.report['builderResources'] = identity

    def start(self):
        existing = command(self.docker + ['buildx', 'ls', '--format', 'json'])
        if any(json.loads(line).get('Name') == self.name for line in existing.splitlines()):
            raise RuntimeError('Generated builder name already exists')
        if inspect(self.docker, 'container', self.container) or inspect(self.docker, 'volume', self.volume):
            raise RuntimeError('Generated builder resources already exist')
        config = Path(self.directory) / 'buildkitd.toml'
        config.write_text('[worker.oci]\n  gc = true\n  reservedSpace = "0B"\n  maxUsedSpace = "4GB"\n  minFreeSpace = "10GB"\n')
        self.report['builderConfigSHA256'] = hashlib.sha256(config.read_bytes()).hexdigest()
        self.attempted = True
        self.report['createAttempted'] = True
        command(self.docker + ['buildx', 'create', '--name', self.name, '--node', self.node,
                '--driver', 'docker-container', '--driver-opt', 'memory=4g,cpu-period=100000,cpu-quota=200000',
                '--buildkitd-config', str(config), ENDPOINT])
        self.created = True
        self.builder()
        self.bootstrap_attempted = True
        self.report['bootstrapAttempted'] = True
        command(self.docker + ['buildx', 'inspect', self.name, '--bootstrap'])
        self.capture()

    def cache(self):
        self.capture()
        records = [json.loads(line) for line in command(self.docker + [
            'buildx', 'du', '--builder', self.name, '--format', 'json']).splitlines()]
        if any(record.get('Reclaimable') is not True for record in records):
            raise RuntimeError('Owned BuildKit records are still in use; retained')
        command(self.docker + ['buildx', 'prune', '--builder', self.name, '--all',
                '--max-used-space', '4GB', '--reserved-space', '0', '--force'])
        after = [json.loads(line) for line in command(self.docker + [
            'buildx', 'du', '--builder', self.name, '--format', 'json']).splitlines()]
        if any(record.get('Reclaimable') is not True for record in after):
            raise RuntimeError('Owned BuildKit records became active during cleanup; retained')
        # Buildx versions render JSON Size as either bytes or human units.
        # Preserve the vendor values. Final zero usage is proven by removing
        # this disposable state volume, not by rounding formatted sizes.
        self.report['cacheBeforeRemovalReportedSizes'] = [record['Size'] for record in after]
        self.report['cacheLimitArgument'] = '4GB'

    def close(self):
        if not self.attempted:
            return
        # Create is local Buildx metadata, but its acknowledgement can be lost.
        # Reconcile that original random identity rather than silently skipping.
        rows = [json.loads(line) for line in command(self.docker + ['buildx', 'ls', '--format', 'json']).splitlines()]
        container = inspect(self.docker, 'container', self.container)
        volume = inspect(self.docker, 'volume', self.volume)
        self.report['cleanupInventory'] = {'builder': self.name,
            'builderPresent': any(row.get('Name') == self.name for row in rows),
            'containerID': container.get('Id') if container else None,
            'volumeName': volume.get('Name') if volume else None}
        if not self.report['cleanupInventory']['builderPresent']:
            if container or volume:
                raise RuntimeError('Remote resources without original builder metadata are retained')
            self.report['builderCleanup'] = 'PASS'
            self.report['cacheAfterBytes'] = 0
            return
        self.builder()
        if not container and not volume and not self.bootstrap_attempted:
            command(self.docker + ['buildx', 'rm', self.name])
            rows = [json.loads(line) for line in command(self.docker + ['buildx', 'ls', '--format', 'json']).splitlines()]
            if any(row.get('Name') == self.name for row in rows):
                raise RuntimeError('Original builder metadata removal is incomplete')
            self.report['builderCleanup'] = 'PASS'
            self.report['cacheAfterBytes'] = 0
            return
        if not container or not volume:
            # A timed-out bootstrap may still create its resources later. Keep
            # the original metadata/identities for recovery, never guess/prune.
            raise RuntimeError('Partial bootstrap resources retained for original-identity recovery')
        self.capture()
        self.cache()
        command(self.docker + ['buildx', 'rm', self.name])
        builders = command(self.docker + ['buildx', 'ls', '--format', 'json'])
        if (any(json.loads(line).get('Name') == self.name for line in builders.splitlines()) or
                inspect(self.docker, 'container', self.container) or inspect(self.docker, 'volume', self.volume)):
            raise RuntimeError('Owned builder removal is incomplete')
        self.report['builderCleanup'] = 'PASS'
        self.report['cacheAfterBytes'] = 0


def run(arguments, owner, nested):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--endpoint', required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    parser.add_argument('--server-token-fetch', action='store_true', help='Use BuildKit server-side registry token fetching for this process only')
    parser.add_argument('build', nargs=argparse.REMAINDER)
    args = parser.parse_args(arguments)
    validate_endpoint(args.endpoint)
    if not nested:
        raise RuntimeError('Remote image verification requires an owned outer verification snapshot')
    build = args.build[1:] if args.build[:1] == ['--'] else args.build
    owner.image_arguments(build)
    validate_build_inputs(build, os.environ['OPENSOHA_VERIFY_DIR'])
    for argument in build:
        if argument.split('=')[0] in ('--context', '--host', '-H', '--cache-from', '--allow', '--secret', '--ssh', '--metadata-file'):
            raise RuntimeError('Unsupported remote verification build option')
    if args.receipt.exists() or args.receipt.is_symlink():
        raise RuntimeError('Remote image receipt already exists')
    if not nested and owner.maintain():
        raise RuntimeError('Local cache over budget and in use')
    if shutil.disk_usage(owner.CACHE_ROOT).free < owner.MIN_FREE:
        raise RuntimeError('Local free space is below the unchanged verification floor')
    run_id = uuid.uuid4().hex
    report = {'result': 'FAIL', 'endpoint': args.endpoint, 'runID': run_id,
              'scope': 'Actual Dockerfile build on a disposable remote builder; no publication',
              'builderCleanup': 'NOT_RUN', 'imageCleanup': 'NOT_RUN', 'minimumFreeBytes': owner.MIN_FREE}
    report['clientSideTokenFetchDisabled'] = args.server_token_fetch
    args.receipt.parent.mkdir(parents=True, exist_ok=True)
    code, failures = 1, []
    try:
        with tempfile.TemporaryDirectory(prefix='opensoha-remote-image-') as directory, owner.forwarding_signals():
            builder = RemoteBuilder(args.endpoint, run_id, directory, report)
            tag, iid = 'opensoha-verify/' + run_id + ':test', Path(directory) / 'image.id'
            report['capacityBefore'] = remote_capacity(builder.docker, owner.MIN_FREE)
            try:
                builder.start()
                env = dict(owner.environment(), TMPDIR=directory, OPENSOHA_VERIFY_INTERNAL='1',
                           BUILDKIT_NO_CLIENT_TOKEN='1' if args.server_token_fetch else '0')
                code = owner.execute(builder.docker + ['buildx', 'build', '--builder', builder.name,
                    '--load', '--tag', tag, '--iidfile', str(iid), '--label', owner.LABEL + '=' + run_id] + build, env)
                report['buildExitCode'] = code
                if iid.is_file():
                    report['imageID'] = iid.read_text().strip()
            finally:
                try:
                    if builder.created:
                        cleanup_image(builder.docker, tag, iid, owner.LABEL, run_id)
                    report['imageCleanup'] = 'PASS'
                except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
                    report['imageCleanup'] = 'FAIL'
                    failures.append(str(error))
                try:
                    builder.close()
                except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
                    report['builderCleanup'] = 'FAIL'
                    failures.append(str(error))
                report['capacityAfter'] = remote_capacity(builder.docker, owner.MIN_FREE)
                if report['capacityAfter']['engineID'] != report['capacityBefore']['engineID']:
                    failures.append('Remote Docker engine changed')
        if not nested:
            failures.extend('Local cache over budget: ' + name for name in owner.maintain())
        if code == 0 and not failures:
            report['result'] = 'PASS'
    except InterruptedError as error:
        code = 128 + int(error.args[0])
        failures.append('Remote image verification interrupted')
    except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
        failures.append(type(error).__name__ + ': ' + str(error))
    finally:
        if failures:
            report['failures'] = failures
        args.receipt.write_text(json.dumps(report, indent=2) + '\n')
    return code if code else (1 if failures else 0)
