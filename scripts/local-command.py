#!/usr/bin/env python3
"""Local PATH entry for go/npm/docker; dispatch only build and verification work."""

import importlib.util
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys


SOURCE = Path(__file__).resolve()
spec = importlib.util.spec_from_file_location('local_verify', SOURCE.with_name('local-verify.py'))
verification = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verification)


def enabled(env, cwd):
    if any(env.get(key, '').lower() in ('1', 'true', 'yes') for key in
           ('CI', 'GITHUB_ACTIONS', 'OPENSOHA_VERIFY_RELEASE', 'OPENSOHA_VERIFY_INTERNAL',
            'OPENSOHA_VERIFY_PASSTHROUGH')):
        return False
    path = Path(cwd).resolve()
    protected = [Path.home() / '.npm', Path.home() / '.cache/uv',
                 Path.home() / '.codex/plugins/cache', verification.CACHE_ROOT / 'npm/_npx']
    return 'node_modules' not in path.parts and not any(path.is_relative_to(p.resolve()) for p in protected)


def real_tool(name, search_path):
    for entry in search_path.split(os.pathsep):
        candidate = Path(entry or '.') / name
        if (candidate.is_file() and os.access(candidate, os.X_OK)
                and candidate.resolve().name != 'local-command.py'):
            # Docker and npm are executable symlinks; keep their invocation names.
            return str(candidate.absolute())
    raise RuntimeError(f'Original {name} executable not found after the local PATH entry')


def option(arguments, names):
    return any(a.split('=', 1)[0] in names or any(
        len(n) == 2 and a.startswith(n) and not a.startswith('--') for n in names)
        for a in arguments)


def go_plan(arguments, env, nested=False):
    index = 0
    if arguments[:1] == ['-C']:
        index = 2
    elif arguments and arguments[0].startswith('-C='):
        index = 1
    if index >= len(arguments):
        return None
    action = arguments[index]
    flags = arguments[index + 1:] + shlex.split(env.get('GOFLAGS', ''))
    if action == 'build' or action == 'test' and '-c' in flags:
        return 'run' if nested or any(a == '-o' or a.startswith('-o=') for a in flags) else 'go-build'
    if action in ('test', 'vet', 'install', 'get', 'generate', 'list', 'mod', 'work', 'fix'):
        return 'run'
    if action == 'run' and any('gocyclo' in a for a in flags):
        return 'run'
    if action == 'tool' and arguments[index + 1:index + 2] in (['compile'], ['link'], ['vet'], ['cover']):
        return 'run'
    return None


def npm_plan(arguments):
    index = 0
    valued = ('--prefix', '--cache', '--userconfig', '--registry', '--workspace', '-w', '--script-shell')
    while index < len(arguments) and arguments[index].startswith('-'):
        index += 2 if arguments[index] in valued else 1
    if index >= len(arguments):
        return None
    action = arguments[index]
    rest = arguments[index + 1:]
    if action in ('publish', 'unpublish', 'pack', 'version'):
        return 'release'
    if action in ('run', 'run-script'):
        event = next((a for a in rest if not a.startswith('-')), '')
        if event.split(':')[0] in ('release', 'publish', 'deploy'):
            return 'release'
        if '--watch' in rest or event.split(':')[0] in ('dev', 'start', 'serve', 'preview', 'watch', 'mcp'):
            return None
        if (event.split(':')[0] in ('build', 'test', 'lint', 'typecheck', 'check', 'verify', 'validate', 'generate', 'smoke')
                or event in ('format:check', 'browser:baseline')
                or event.startswith(('api:reference:', 'screenshots:', 'package:'))):
            return 'run'
    if action in ('test', 't', 'it', 'install-test', 'ci', 'install', 'i', 'add', 'update', 'up',
                  'dedupe', 'rebuild', 'prune', 'uninstall', 'remove', 'audit', 'outdated'):
        return None if '--watch' in rest else 'run'
    if action in ('exec', 'x'):
        # Unknown exec packages may be MCP daemons; only known finite tools enter the guard.
        known = ('tsc', 'eslint', 'vitest', 'playwright', 'prettier')
        tools = [a.split('@', 1)[0] for a in rest if not a.startswith('-')]
        finite = any(a in known for a in tools)
        if 'vitest' in tools and 'run' not in rest or 'playwright' in tools and not any(a in rest for a in ('test', 'install')):
            finite = False
        if finite and not any('mcp' in a.lower() for a in rest) and not any(a in rest for a in ('--watch', '-w')):
            return 'run'
    return None


def npm_cache(arguments):
    normalized = ['--cache', str(verification.CACHE_ROOT / 'npm')]
    index = 0
    while index < len(arguments):
        argument = arguments[index]
        if argument == '--':
            normalized.extend(arguments[index:])
            break
        if argument == '--cache':
            index += 2
            continue
        if not argument.startswith('--cache='):
            normalized.append(argument)
        index += 1
    return normalized


def docker_plan(arguments):
    index = 0
    overrides = {}
    valued = verification.DOCKER_OPTIONS
    while index < len(arguments) and arguments[index].startswith('-'):
        flag, separator, value = arguments[index].partition('=')
        if not separator and len(flag) > 2 and flag[:2] in ('-c', '-H', '-l'):
            flag, value, separator = flag[:2], flag[2:], '='
        if flag in valued:
            if not separator:
                index += 1
                if index >= len(arguments):
                    return None, overrides
                value = arguments[index]
            if valued[flag]:
                overrides[valued[flag]] = value
        elif flag not in ('--debug', '-D', '--tls', '--tlsverify'):
            return None, overrides
        index += 1
    command = arguments[index:]
    if not command:
        return None, overrides
    outputs = [command[i + 1] for i, a in enumerate(command[:-1]) if a in ('-o', '--output')]
    outputs += [a.split('=', 1)[1] for a in command if a.startswith('--output=')]
    outputs += [a[2:] for a in command if a.startswith('-o') and not a.startswith('--') and len(a) > 2]
    if command[0] == 'push' or any(a == '--push' or a.startswith('--push=') and a != '--push=false'
                                   for a in command) or any('type=registry' in a or 'push=true' in a for a in outputs):
        return 'release', overrides
    if command[0] == 'pull':
        return 'docker-command', overrides
    if command[0] == 'buildx':
        command = command[1:]
        if command[:1] == ['bake']:
            return 'docker-command', overrides
    elif command[0] == 'image':
        command = command[1:]
    elif command[0] == 'compose':
        valued_compose = ('-f', '--file', '-p', '--project-name', '--project-directory', '--env-file', '--profile')
        i = 1
        while i < len(command) and command[i].startswith('-'):
            i += 2 if command[i] in valued_compose else 1
        return ('docker-command' if command[i:i + 1] == ['build'] else None), overrides
    if command[0] != 'build':
        return None, overrides
    explicit = ('-t', '--tag', '--iidfile', '-o', '--output', '--load', '--cache-to', '-q', '--quiet')
    return ('docker-command' if option(command[1:], explicit) else 'docker-image'), overrides


def main():
    name = Path(sys.argv[0]).name
    if name not in ('go', 'npm', 'docker'):
        raise RuntimeError('Install this entry under the go, npm and docker command names')
    arguments = sys.argv[1:]
    native = real_tool(name, os.environ.get('PATH', ''))
    env = dict(os.environ)
    mode = None
    if enabled(env, os.getcwd()):
        if name == 'go':
            mode = go_plan(arguments, env, verification.nested_run())
        elif name == 'npm':
            mode = npm_plan(arguments)
            if mode == 'run':
                arguments = npm_cache(arguments)
        else:
            mode, overrides = docker_plan(arguments)
            env.update(overrides)
            if mode and mode != 'release':
                host = env.get('DOCKER_HOST', '')
                context = env.get('DOCKER_CONTEXT')
                if 'DOCKER_CONTEXT' in overrides:
                    env.pop('DOCKER_HOST', None)
                    host = ''
                if not context and not host:
                    selected = subprocess.run([native, 'context', 'show'], capture_output=True, text=True)
                    if selected.returncode == 0:
                        context = selected.stdout.strip()
                if context and not host:
                    info = subprocess.run([native, 'context', 'inspect', context], capture_output=True, text=True)
                    if info.returncode == 0:
                        host = json.loads(info.stdout)[0]['Endpoints']['docker']['Host']
                if host and not host.startswith('unix://'):
                    mode = None
    command = [native, *arguments]
    if mode == 'release':
        env['OPENSOHA_VERIFY_RELEASE'] = '1'
    elif mode:
        command = [sys.executable, str(SOURCE.with_name('local-verify.py')), mode, '--', *command]
    else:
        env['OPENSOHA_VERIFY_PASSTHROUGH'] = '1'
    os.execvpe(command[0], command, env)


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
