"""Native race tests near a disposable remote PG, without mounting host files."""
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile

IMAGE = 'golang:1.27.3'
CONTRACT_EMBEDS = {
    'auth/permission-catalog.json',
    'network/network-runtime-protocol.schema.json',
    'network/network-ingest-event.schema.json',
    'network/network-radius-accounting.schema.json',
    'network/proxy-runtime.schema.json',
    'openapi/soha-api.json',
    'openapi/soha-api.yaml',
    'delivery/document.schema.json',
}


def validate_repository(repository, module):
    supplied = Path(repository)
    repository = supplied.resolve(strict=True)
    top = Path(subprocess.check_output(['git', '-C', str(repository), 'rev-parse', '--show-toplevel'], text=True).strip()).resolve(strict=True)
    relative = {'github.com/opensoha/soha': '.', 'github.com/opensoha/soha/contracts': 'contracts'}.get(module)
    manifest = repository / 'go.mod'
    if (relative is None or supplied.is_symlink() or repository != top / relative
            or (top / relative).is_symlink() or manifest.is_symlink() or not manifest.is_file()
            or not re.search(r'^module\s+' + re.escape(module) + r'\s*$', manifest.read_text(), re.M)):
        raise ValueError('Native test source is not the declared owning Go repository')


def source_file(name, prefix):
    if name in ('go.mod', 'go.sum'):
        return True
    if prefix == 'src/contracts':
        return name.endswith('.go') or name in CONTRACT_EMBEDS
    if name.startswith('migrations/') and name.endswith('.sql'):
        return True
    if name in ('internal/infrastructure/virtualization/personal_guest_installer.py',
                'internal/infrastructure/virtualization/personal_guest_tools_installer.py'):
        return True
    return name.startswith(('internal/', 'pkg/')) and (name.endswith('.go') or
           '/testdata/' in name and Path(name).suffix in ('.sql', '.json', '.yaml', '.yml', '.txt', '.golden'))


def source_archive(repository, prefix):
    repository = Path(repository).resolve()
    names = subprocess.check_output(['git', '-C', str(repository), 'ls-files', '--full-name', '--cached', '--others', '--exclude-standard', '-z', '--', '.']).decode().split('\0')
    top = Path(subprocess.check_output(['git', '-C', str(repository), 'rev-parse', '--show-toplevel'], text=True).strip()).resolve()
    relative = repository.relative_to(top)
    names = [str(Path(name).relative_to(relative)) for name in names if name]
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode='w') as archive:
        for name in sorted(set(names) - {''}):
            if not source_file(name, prefix):
                continue
            path = repository / name
            if not path.exists() and not path.is_symlink():
                continue
            # Ignored private config and .git are absent from git ls-files.
            # Never dereference a source link outside its owning repository.
            path.resolve().relative_to(repository)
            if path.is_dir():
                continue
            if not path.is_file() and not path.is_symlink():
                raise ValueError('Unsupported test source entry')
            archive.add(path, arcname=prefix + '/' + name, recursive=False)
    return stream.getvalue()


def cleanup(docker, name, identifier, image_id, run_id, env, report):
    report['cleanup'] = 'FAIL'
    def inspect():
        return subprocess.run([*docker, 'inspect', identifier or name], env=env, capture_output=True, timeout=20)

    absent = (b'no such object:', b'no such container:')
    result = inspect()
    if result.returncode and any(marker in result.stderr.lower() for marker in absent):
        report['cleanup'] = 'NOT_CREATED' if identifier is None else 'ALREADY_REMOVED'
        return
    report['cleanup'] = 'FAIL'
    if result.returncode:
        raise ValueError('Cannot verify native Go container cleanup state')
    value = json.loads(result.stdout)[0]
    labels, host = value['Config'].get('Labels', {}), value['HostConfig']
    if (value['Name'] != '/' + name or identifier is not None and value['Id'] != identifier or
            value['Image'] != image_id or labels.get('soha.test.run') != run_id or
            labels.get('soha.test.purpose') != 'native-postgres-race' or host.get('Binds') or
            host.get('Privileged') or host.get('PortBindings') or
            any(mount.get('Type') == 'bind' for mount in value.get('Mounts', []))):
        raise ValueError('Native Go container ownership changed; cleanup refused')
    report['containerId'] = value['Id']
    subprocess.run([*docker, 'rm', '--force', '--volumes', value['Id']], env=env, check=True, stdout=subprocess.DEVNULL, timeout=30)
    result = inspect()
    if result.returncode == 0 or not any(marker in result.stderr.lower() for marker in absent):
        raise ValueError('Native Go fixture cleanup could not be verified')
    report['cleanup'] = 'PASS'


def run(docker, run_id, repository, contracts, test_command, environment, stdout, stderr, report):
    validate_repository(repository, 'github.com/opensoha/soha')
    if contracts is not None:
        validate_repository(contracts, 'github.com/opensoha/soha/contracts')
        if Path(contracts).resolve() != Path(repository).resolve() / 'contracts':
            raise ValueError('Native SDK source must be this Root contracts subdirectory')
    env = dict(environment, OPENSOHA_VERIFY_INTERNAL='1')

    def command(args, **kwargs):
        return subprocess.check_output([*docker, *args], env=env, timeout=60, **kwargs)

    found = subprocess.run([*docker, 'image', 'inspect', IMAGE], env=env, capture_output=True, timeout=30)
    if found.returncode:
        subprocess.run([*docker, 'pull', IMAGE], env=env, check=True, stdout=stderr, stderr=stderr, timeout=600)
    image = json.loads(command(['image', 'inspect', IMAGE]))[0]
    if image['Os'] != 'linux' or image['Architecture'] != 'amd64' or not any(ref.startswith('golang@sha256:') for ref in image.get('RepoDigests', [])):
        raise ValueError('Native PG race fixture requires the official Linux amd64 Go image')
    report.update(mode='native-docker-race' if '-race' in test_command else 'native-docker-toolchain-probe', imageId=image['Id'], repoDigests=image['RepoDigests'],
                  cpuLimit=1, memoryLimitBytes=2 * 1024**3, hostMounts=False, cleanup='NOT_CREATED')
    container = None
    container_name = 'soha-native-pg-' + run_id
    try:
        container = command(['run', '--detach', '--rm', '--name', container_name, '--label', 'soha.test.run=' + run_id,
                             '--label', 'soha.test.purpose=native-postgres-race', '--cpus', '1', '--memory', '2g',
                             '--pids-limit', '256', '--workdir', '/src/core', image['Id'], 'sleep', '2400'], text=True).strip()
        report['containerId'] = container
        version = command(['exec', container, 'go', 'version'], text=True).strip()
        if version != 'go version go1.27.3 linux/amd64':
            raise ValueError('Native Go toolchain differs from the owner version')
        report['toolchain'] = version
        command(['exec', container, 'mkdir', '-p', '/src/core', '/src/contracts'])
        report['sourceArchives'] = {}
        for repo_name, repo in [('core', repository), ('contracts', contracts)]:
            if repo is None:
                continue
            archive = source_archive(repo, 'src/' + repo_name)
            report['sourceArchives'][repo_name] = hashlib.sha256(archive).hexdigest()
            subprocess.run([*docker, 'cp', '-', container + ':/'], input=archive, env=env, check=True, timeout=90)
        if contracts is not None:
            command(['exec', container, 'go', 'mod', 'edit', '-replace=github.com/opensoha/soha/contracts=/src/contracts'])
        variables = ['SOHA_TEST_POSTGRES_HOST', 'SOHA_TEST_POSTGRES_PASSWORD',
                     'SOHA_APPLICATION_TEST_POSTGRES_PORT', 'SOHA_CATALOG_TEST_POSTGRES_PORT']
        flags = [value for name in variables for value in ('--env', name)]
        invocation = [*docker, 'exec', *flags, '--env', 'GOWORK=off', '--env', 'GOTOOLCHAIN=local',
                      '--env', 'CGO_ENABLED=1', '--env', 'GOMAXPROCS=2', '--env', 'GOFLAGS=-mod=mod -p=2',
                      container, *test_command]
        report['command'] = test_command
        report['exitCode'] = subprocess.run(invocation, env=env, stdout=stdout, stderr=stderr, timeout=1800).returncode
        return report['exitCode']
    finally:
        cleanup(docker, container_name, container, image['Id'], run_id, env, report)
