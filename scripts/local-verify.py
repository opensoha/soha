#!/usr/bin/env python3
"""Bound local verification caches and remove only this run's disposable outputs."""

import argparse
from contextlib import contextmanager, nullcontext
import fcntl
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import uuid


CACHE_ROOT = Path('/private/tmp/opensoha-cache')
GIB = 1024 ** 3
MIN_FREE = 10 * GIB
LIMITS = {'go-build': 8 * GIB, 'go-mod': 6 * GIB,
          'npm': 2 * GIB, 'golangci-lint': GIB}
LABEL = 'com.opensoha.local-verification'
DOCKER_OPTIONS = {'--context': 'DOCKER_CONTEXT', '-c': 'DOCKER_CONTEXT', '--host': 'DOCKER_HOST',
                  '-H': 'DOCKER_HOST', '--config': 'DOCKER_CONFIG', '--log-level': None, '-l': None,
                  '--tlscacert': None, '--tlscert': None, '--tlskey': None}


def environment():
    return dict(os.environ, GOCACHE=str(CACHE_ROOT / 'go-build'),
                GOMODCACHE=str(CACHE_ROOT / 'go-mod'),
                npm_config_cache=str(CACHE_ROOT / 'npm'),
                SOHA_NPM_CACHE=str(CACHE_ROOT / 'npm'),
                GOLANGCI_LINT_CACHE=str(CACHE_ROOT / 'golangci-lint'))


def call(arguments, env=None):
    result = subprocess.run(arguments, env=dict(os.environ if env is None else env,
                                                OPENSOHA_VERIFY_INTERNAL='1'),
                            text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError(f'{arguments[0]} failed (exit {result.returncode})')
    return result.stdout


def size(path):
    if not path.exists():
        return 0
    return int(call(['du', '-sk', str(path)]).split()[0]) * 1024


def busy(path, programs=()):
    # Open files alone miss short-lived compiler/download steps.
    names = call(['ps', '-axo', 'comm']).splitlines()[1:]
    if any(Path(name.strip().split(' ', 1)[0]).name in programs for name in names):
        return True
    if path is None:
        return False
    titles = call(['ps', '-axo', 'args']).splitlines()[1:]
    if any(Path(title.strip().split(' ', 1)[0]).name == 'node' and str(path) in title
           for title in titles):
        return True
    opened = subprocess.run(['lsof', '-nP', '-Fn'], text=True, capture_output=True)
    if opened.returncode not in (0, 1):
        raise RuntimeError('Cannot establish cache usage; cleanup refused')
    prefix = 'n' + str(path) + '/'
    return any(line.startswith(prefix) or line == 'n' + str(path)
               for line in opened.stdout.splitlines())


@contextmanager
def locked():
    CACHE_ROOT.mkdir(parents=True, exist_ok=True)
    if CACHE_ROOT.is_symlink() or CACHE_ROOT.resolve() != CACHE_ROOT:
        raise RuntimeError('Unexpected cache root; refusing cleanup')
    for name in LIMITS:
        path = CACHE_ROOT / name
        if path.is_symlink():
            raise RuntimeError(f'Symlink cache refused: {name}')
        path.mkdir(exist_ok=True)
    descriptor = os.open(CACHE_ROOT / '.verification.lock',
                         os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'w') as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print('Waiting for another local verification to finish', file=sys.stderr, flush=True)
            fcntl.flock(handle, fcntl.LOCK_EX)
        yield


def nested_run():
    directory = Path(os.environ.get('OPENSOHA_VERIFY_DIR', '/nonexistent'))
    token = directory / '.verification-token'
    active = os.environ.get('OPENSOHA_VERIFY_ACTIVE')
    return bool(active and not directory.is_symlink() and not token.is_symlink()
                and token.is_file() and token.stat().st_uid == os.getuid()
                and token.read_text() == active)


def maintain():
    blocked = []
    env = environment()
    for name, limit in LIMITS.items():
        path = CACHE_ROOT / name
        measured = path / '_cacache' if name == 'npm' else path
        used = size(measured)
        if used <= limit:
            continue
        programs = ('npm', 'npx') if name == 'npm' else ('go', 'compile', 'link', 'golangci-lint')
        if busy(path, programs):
            print(f'Skip in-use cache: {name} ({used / GIB:.2f} GiB)', file=sys.stderr, flush=True)
            blocked.append(name)
            continue
        if name == 'go-build':
            call(['go', 'clean', '-cache'], env)
        elif name == 'go-mod':
            call(['go', 'clean', '-modcache'], env)
        elif name == 'npm':
            call(['npm', 'cache', 'clean', '--force', '--cache', str(path)], env)
        else:
            shutil.rmtree(path)
            path.mkdir()
        remaining = size(measured)
        if remaining > limit:
            blocked.append(name)
        print(f'Cache {name}: {used / GIB:.2f} -> {remaining / GIB:.2f} GiB', file=sys.stderr, flush=True)
    return blocked


def local_docker():
    context = call(['docker', 'context', 'show']).strip()
    info = json.loads(call(['docker', 'context', 'inspect', context]))[0]
    if not info['Endpoints']['docker']['Host'].startswith('unix://'):
        raise RuntimeError('Local verification refuses a remote Docker context')
    if os.environ.get('DOCKER_HOST') and os.environ['DOCKER_HOST'] != info['Endpoints']['docker']['Host']:
        raise RuntimeError('Select the matching local Docker context for capacity maintenance')
    docker = ['docker', '--context', context]
    builders = [json.loads(line) for line in call(docker + ['buildx', 'ls', '--format', 'json']).splitlines()]
    matches = [builder['Name'] for builder in builders if builder['Driver'] == 'docker'
               and len(builder['Nodes']) == 1 and builder['Nodes'][0]['Endpoint'] == context]
    if len(matches) != 1:
        raise RuntimeError('Cannot identify the local Docker engine builder')
    return docker, matches[0]


def prune_docker(docker, builder):
    if busy(None, ('docker', 'buildkitd')):
        raise RuntimeError('Docker operation active; cache maintenance skipped')
    records = call(docker + ['buildx', 'du', '--builder', builder, '--format', 'json'])
    if any(not json.loads(line)['Reclaimable'] for line in records.splitlines()):
        raise RuntimeError('BuildKit records in use; cache maintenance skipped')
    # A prune is a capacity sweep, not a persistent daemon setting or host disk quota.
    output = call(docker + ['buildx', 'prune', '--builder', builder, '--all', '--max-used-space', '4GB',
                            '--reserved-space', '0', '--force'])
    print(next((line for line in reversed(output.splitlines()) if line.startswith('Total:')),
               'Docker cache maintenance completed'), file=sys.stderr, flush=True)


def cleanup_image(docker, tag, iidfile, run_id):
    result = subprocess.run(docker + ['image', 'inspect', tag], text=True, capture_output=True,
                            env=dict(os.environ, OPENSOHA_VERIFY_INTERNAL='1'))
    if not iidfile.exists():
        if result.returncode:
            return
        raise RuntimeError(f'Generated image has no IID receipt; retained {tag}')
    iid = iidfile.read_text().strip()
    if result.returncode:
        raise RuntimeError(f'Cannot verify generated image ownership: {tag}')
    image = json.loads(result.stdout)[0]
    if image['Id'] != iid or (image['Config'].get('Labels') or {}).get(LABEL) != run_id:
        raise RuntimeError(f'Image identity changed; retained {tag}')
    if call(docker + ['ps', '-aq', '--filter', f'ancestor={iid}']).strip():
        raise RuntimeError(f'Image referenced by a container; retained {tag}')
    call(docker + ['image', 'rm', tag])
    print(f'Removed verification image: {tag}', file=sys.stderr, flush=True)


def image_arguments(arguments):
    # Output/push/tag overrides could export or retain artifacts beyond our ownership.
    forbidden = ('-t', '--tag', '--iidfile', '--label', '--output', '-o', '--push',
                 '--load', '--cache-to', '--builder')
    for argument in arguments:
        if (argument.split('=')[0] in forbidden
                or (argument.startswith('-t') and not argument.startswith('--'))
                or (argument.startswith('-o') and not argument.startswith('--'))):
            raise RuntimeError(f'Unsupported verification image option: {argument.split("=")[0]}')
    if not arguments:
        raise RuntimeError('image requires Docker build arguments and a context')
    return arguments


@contextmanager
def forwarding_signals():
    previous = {}

    def interrupted(signum, frame):
        raise InterruptedError(signum)

    for signum in (signal.SIGINT, signal.SIGTERM):
        previous[signum] = signal.signal(signum, interrupted)
    try:
        yield
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)


def execute(arguments, env):
    child = subprocess.Popen(arguments, env=env, start_new_session=True)
    try:
        code = child.wait()
        return code if code >= 0 else 128 - code
    except InterruptedError as error:
        signum = error.args[0]
        os.killpg(child.pid, signum)
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()
        return 128 + signum


def go_output(arguments, directory):
    command = list(arguments)
    index = 3 if command[1:2] == ['-C'] else 2 if command[1:2] and command[1].startswith('-C=') else 1
    if command[index:index + 1] not in (['build'], ['test']):
        raise RuntimeError('Expected a Go build/test command')
    action = index
    index += 1
    if command[index:index + 1] == ['-C']:
        index += 2
    elif command[index:index + 1] and command[index].startswith('-C='):
        index += 1
    if command[action] == 'build':
        # -o <directory> rejects library-only builds; keep Go's native discard behavior.
        probe = command[:index] + [a for a in command[index:] if a not in ('-json', '-json=true', '-json=false')]
        probe[action] = 'list'
        probe[index:index] = ['-json=Name']
        try:
            output = call(probe, dict(environment(), TMPDIR=directory, GOTMPDIR=directory)).lstrip()
            decoder = json.JSONDecoder()
            names = []
            while output:
                package, length = decoder.raw_decode(output)
                names.append(package.get('Name'))
                output = output[length:].lstrip()
            if 'main' not in names:
                return command
        except (RuntimeError, ValueError):
            return command
    output = Path(tempfile.mkdtemp(prefix='go-output-', dir=directory))
    command[index:index] = ['-o', str(output) + '/']
    return command


def verify(arguments, image=False, keep_image=False, nested=False, go_build=False,
           docker_command=False, docker_image=False):
    if not arguments:
        raise RuntimeError('A verification command is required')
    if image:
        image_arguments(arguments)
    if keep_image:
        for argument in arguments:
            if argument.split('=')[0] in ('--push', '--output', '-o', '--cache-to', '--builder', '--load', '--context', '--host', '-H'):
                raise RuntimeError('Unsupported local image output/context override')
    blocked = [] if nested else maintain()
    if blocked:
        raise RuntimeError('Cache over budget and in use: ' + ', '.join(blocked))
    docker, builder = local_docker() if image or keep_image or docker_command or docker_image else (None, None)
    if docker_command or docker_image:
        for index, argument in enumerate(arguments):
            if argument == '--builder' and arguments[index + 1:index + 2] != [builder]:
                raise RuntimeError('Local capacity guard requires the engine builder')
            if argument.startswith('--builder=') and argument != '--builder=' + builder:
                raise RuntimeError('Local capacity guard requires the engine builder')
        if os.environ.get('BUILDX_BUILDER', builder) != builder:
            raise RuntimeError('Local capacity guard requires the engine builder')
    if docker_image:
        labels = [arguments[i + 1] for i, a in enumerate(arguments[:-1]) if a == '--label']
        labels += [a.split('=', 1)[1] for a in arguments if a.startswith('--label=')]
        if any(label.split('=', 1)[0] == LABEL for label in labels):
            raise RuntimeError('Verification ownership label cannot be overridden')
    if docker:
        prune_docker(docker, builder)
    if shutil.disk_usage(CACHE_ROOT).free < MIN_FREE:
        raise RuntimeError(f'Less than {MIN_FREE // GIB} GiB free; verification refused')
    run_id = uuid.uuid4().hex
    failures = []
    code = 1
    with tempfile.TemporaryDirectory(prefix='opensoha-verify-') as directory:
        (Path(directory) / '.verification-token').write_text(run_id)
        env = dict(environment(), TMPDIR=directory, GOTMPDIR=directory, OPENSOHA_VERIFY_DIR=directory,
                   OPENSOHA_VERIFY_ACTIVE=run_id)
        tag = 'opensoha-verify/' + run_id + ':test'
        iidfile = Path(directory) / 'image.id'
        command = (docker + ['buildx', 'build', '--builder', builder, '--load', '--tag', tag,
                              '--iidfile', str(iidfile), '--label', f'{LABEL}={run_id}']
                   + arguments) if image else (docker + ['buildx', 'build', '--builder', builder, '--load']
                                              + arguments if keep_image else arguments)
        if go_build:
            command = go_output(arguments, directory)
        if docker_image:
            command = list(arguments)
            index = 1
            while index < len(command) and command[index].startswith('-'):
                index += 2 if command[index] in DOCKER_OPTIONS else 1
            if command[index:index + 1] in (['buildx'], ['image']):
                index += 1
            if command[index:index + 1] != ['build']:
                raise RuntimeError('Expected a Docker build command')
            index += 1
            command[index:index] = ['--tag', tag, '--iidfile', str(iidfile), '--label', f'{LABEL}={run_id}']
        try:
            with forwarding_signals():
                code = execute(command, env)
        finally:
            if docker:
                try:
                    if image or docker_image:
                        cleanup_image(docker, tag, iidfile, run_id)
                    prune_docker(docker, builder)
                except RuntimeError as error:
                    failures.append(str(error))
            try:
                if not nested:
                    failures.extend('Cache over budget: ' + name for name in maintain())
            except (RuntimeError, OSError) as error:
                failures.append(str(error))
    for failure in failures:
        print(failure, file=sys.stderr)
    return code if code else (1 if failures else 0)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['status', 'maintain', 'run', 'image', 'build-image',
                                       'go-build', 'docker-command', 'docker-image'])
    parser.add_argument('arguments', nargs=argparse.REMAINDER)
    options = parser.parse_args()
    arguments = options.arguments[1:] if options.arguments[:1] == ['--'] else options.arguments
    if options.mode not in ('status', 'maintain') and any(
            os.environ.get(key, '').lower() in ('1', 'true', 'yes')
            for key in ('CI', 'GITHUB_ACTIONS', 'OPENSOHA_VERIFY_RELEASE')):
        command = ['docker', 'build', *arguments] if options.mode in ('image', 'build-image') else arguments
        if not command:
            raise RuntimeError('A verification command is required')
        os.execvpe(command[0], command, os.environ)
    nested = nested_run()
    if nested and options.mode == 'maintain':
        raise RuntimeError('Run cache maintenance outside an active verification')
    if nested and options.mode in ('run', 'go-build', 'image', 'build-image', 'docker-command', 'docker-image'):
        if options.mode == 'run':
            if not arguments:
                raise RuntimeError('A verification command is required')
            os.execvpe(arguments[0], arguments, environment())
        if options.mode == 'go-build':
            os.execvpe(arguments[0], go_output(arguments, os.environ['OPENSOHA_VERIFY_DIR']), environment())
        return verify(arguments, options.mode == 'image', options.mode == 'build-image', nested=True,
                      docker_command=options.mode == 'docker-command', docker_image=options.mode == 'docker-image')
    with nullcontext() if nested else locked():
        if options.mode == 'status':
            print(json.dumps({'paths': {name: {'path': str(CACHE_ROOT / name),
                                              'bytes': size(CACHE_ROOT / name),
                                              'budgeted_bytes': size(CACHE_ROOT / name / '_cacache') if name == 'npm' else size(CACHE_ROOT / name),
                                              'threshold_scope': '_cacache only' if name == 'npm' else 'entire cache',
                                              'threshold_bytes': limit}
                                       for name, limit in LIMITS.items()},
                              'free_bytes': shutil.disk_usage(CACHE_ROOT).free,
                              'minimum_free_bytes': MIN_FREE}, indent=2))
            return 0
        if options.mode == 'maintain':
            blocked = maintain()
            if shutil.which('docker'):
                prune_docker(*local_docker())
            return 1 if blocked else 0
        return verify(arguments, options.mode == 'image', options.mode == 'build-image',
                      go_build=options.mode == 'go-build', docker_command=options.mode == 'docker-command',
                      docker_image=options.mode == 'docker-image')


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
