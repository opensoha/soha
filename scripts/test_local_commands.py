import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


SCRIPTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('local_command', SCRIPTS / 'local-command.py')
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)


class CommandPlans(unittest.TestCase):
    def test_ci_internal_release_and_mcp_cache_do_not_enter_guard(self):
        for key in ('CI', 'GITHUB_ACTIONS', 'OPENSOHA_VERIFY_RELEASE', 'OPENSOHA_VERIFY_INTERNAL'):
            self.assertFalse(cli.enabled({key: 'true'}, '/tmp/project'))
        for cwd in (str(Path.home() / '.npm/_npx/tool'), '/private/tmp/opensoha-cache/npm/_npx/tool',
                    str(Path.home() / '.cache/uv/archive-v0/tool'), '/tmp/project/node_modules/native'):
            self.assertFalse(cli.enabled({}, cwd))
        self.assertTrue(cli.enabled({}, '/tmp/project'))

    def test_go_direct_implicit_outputs_are_temporary_explicit_and_nested_outputs_keep_semantics(self):
        for args in (['build'], ['-C', 'project', 'build', './...'], ['test', '-c'], ['build', '-overlay=map.json']):
            self.assertEqual(cli.go_plan(args, {}), 'go-build')
        for args in (['build', '-o', 'bin/app'], ['build', '-o=bin/app'], ['test', '-c', '-o=test.bin']):
            self.assertEqual(cli.go_plan(args, {}), 'run')
        self.assertEqual(cli.go_plan(['build'], {'GOFLAGS': '-o=bin/app'}), 'run')
        self.assertEqual(cli.go_plan(['build'], {}, nested=True), 'run')
        self.assertIsNone(cli.go_plan(['run', './cmd/server'], {}))
        self.assertIsNone(cli.go_plan(['run', './scripts/mcp-server.go'], {}))
        for action in ('test', 'vet', 'install', 'list', 'mod'):
            self.assertEqual(cli.go_plan([action], {}), 'run')

    def test_npm_installs_and_verification_enter_guard_release_and_daemons_keep_semantics(self):
        for args in (['ci'], ['install'], ['--prefix', 'app', 'run', 'build'], ['run', 'check'], ['test']):
            self.assertEqual(cli.npm_plan(args), 'run')
        for args in (['publish'], ['pack'], ['run', 'release:artifacts']):
            self.assertEqual(cli.npm_plan(args), 'release')
        for args in (['run', 'dev'], ['run', 'mcp'], ['run', 'test', '--', '--watch'],
                     ['exec', '--yes', '@example/mcp-server'], ['start']):
            self.assertIsNone(cli.npm_plan(args))

    def test_docker_ownership_and_service_release_boundaries(self):
        for args in (['build', '.'], ['image', 'build', '.'], ['buildx', 'build', '.'], ['buildx', 'build', '--push=false', '.']):
            self.assertEqual(cli.docker_plan(args)[0], 'docker-image')
        for args in (['build', '-t', 'deploy:local', '.'], ['build', '--iidfile=receipt', '.'],
                     ['compose', '-f', 'local.yaml', 'build'], ['buildx', 'bake'], ['pull', 'busybox']):
            self.assertEqual(cli.docker_plan(args)[0], 'docker-command')
        for args in (['buildx', 'build', '--push', '.'], ['push', 'release:v1'],
                     ['buildx', 'build', '-o', 'type=registry', '.']):
            self.assertEqual(cli.docker_plan(args)[0], 'release')
        for args in (['run', 'server'], ['compose', 'up', '--build'], ['volume', 'ls'], ['ps']):
            self.assertIsNone(cli.docker_plan(args)[0])
        mode, env = cli.docker_plan(['--context', 'local', 'build', '.'])
        self.assertEqual((mode, env), ('docker-image', {'DOCKER_CONTEXT': 'local'}))
        self.assertEqual(cli.docker_plan(['-clocal', 'build', '.']), (mode, env))

    def test_npm_cache_is_unified_without_rewriting_child_script_arguments(self):
        args = cli.npm_cache(['--cache=/old', 'run', 'test', '--cache', '/also-old', '--', '--cache=/child'])
        self.assertEqual(args, ['--cache', str(cli.verification.CACHE_ROOT / 'npm'), 'run', 'test', '--', '--cache=/child'])

    def test_real_executable_resolution_excludes_our_symlink_and_keeps_native_invocation_name(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shim, native = root / 'shim', root / 'native'
            shim.mkdir(); native.mkdir()
            (shim / 'docker').symlink_to(cli.SOURCE)
            executable = native / 'docker-tools'
            executable.write_text('#!/bin/sh\nexit 0\n'); executable.chmod(0o755)
            (native / 'docker').symlink_to(executable)
            self.assertEqual(cli.real_tool('docker', str(shim) + os.pathsep + str(native)), str(native / 'docker'))

    def test_go_output_keeps_C_first_and_never_overwrites_existing_artifact(self):
        with tempfile.TemporaryDirectory() as directory:
            for args in (['go', '-C', 'build', 'build'], ['go', 'build', '-C', 'project']):
                with patch.object(cli.verification, 'call', return_value='{"Name":"main"}'):
                    result = cli.verification.go_output(args, directory)
                self.assertEqual(result[-2], '-o')
                self.assertTrue(Path(result[-1]).is_dir())
                self.assertTrue(result[-1].startswith(directory))

    def test_library_builds_keep_native_discard_behavior(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(cli.verification, 'call', return_value='{"Name":"library"}'):
            self.assertEqual(cli.verification.go_output(['go', 'build', './...'], directory), ['go', 'build', './...'])


class NativeCommands(unittest.TestCase):
    def test_real_direct_go_npm_nested_failure_ci_and_artifact_cleanup(self):
        with tempfile.TemporaryDirectory(prefix='direct-cli-') as directory:
            root = Path(directory)
            shim = root / 'bin'; shim.mkdir()
            for name in ('go', 'npm', 'docker'):
                (shim / name).symlink_to(cli.SOURCE)
            env = dict(os.environ, PATH=str(shim) + os.pathsep + os.environ['PATH'],
                       CI='false', GITHUB_ACTIONS='false', GOWORK='off', GOFLAGS='')
            for key in ('OPENSOHA_VERIFY_ACTIVE', 'OPENSOHA_VERIFY_DIR', 'OPENSOHA_VERIFY_PASSTHROUGH',
                        'OPENSOHA_VERIFY_INTERNAL', 'OPENSOHA_VERIFY_RELEASE'):
                env.pop(key, None)
            project = root / 'project with spaces'; project.mkdir()
            (project / 'go.mod').write_text('module example.test/probe\n\ngo 1.25\n')
            (project / 'main.go').write_text('package main\nfunc main() {}\n')

            def run(args, extra=None):
                return subprocess.run(args, cwd=project, env=dict(env, **(extra or {})),
                                      capture_output=True, text=True, timeout=90)

            result = run(['go', 'build'])
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((project / 'probe').exists())
            result = run(['go', '-C', str(project), 'build', '-o', 'kept'])
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((project / 'kept').is_file())
            result = run(['go', 'build'], {'CI': 'true'})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((project / 'probe').is_file())
            library = project / 'library'; library.mkdir()
            (library / 'value.go').write_text('package library\nfunc Value() int { return 1 }\n')
            result = run(['go', 'build', './library'])
            self.assertEqual(result.returncode, 0, result.stderr)

            (project / 'package.json').write_text(json.dumps({'scripts': {
                'test': 'npm run check:probe -- "a b"', 'check:probe': 'node probe.cjs',
                'test:fail': 'node probe.cjs fail', 'release:probe': 'node probe.cjs'}}))
            (project / 'probe.cjs').write_text('''const fs = require('fs');
const dir = process.env.OPENSOHA_VERIFY_DIR;
fs.writeFileSync('receipt.json', JSON.stringify({dir, args: process.argv.slice(2)}));
if (dir) fs.writeFileSync(dir + '/own-output', 'owned');
if (process.argv[2] === 'fail') process.exit(7);
''')
            for args, code, expected in [(['npm', 'test', '--silent'], 0, ['a b']),
                                         (['npm', 'run', 'test:fail', '--silent'], 7, ['fail'])]:
                result = run(args)
                self.assertEqual(result.returncode, code, result.stderr)
                receipt = json.loads((project / 'receipt.json').read_text())
                self.assertEqual(receipt['args'], expected)
                self.assertFalse(Path(receipt['dir']).exists())
            for extra, args in [({'CI': 'true'}, ['npm', 'test', '--silent']),
                                ({}, ['npm', 'run', 'release:probe', '--silent'])]:
                result = run(args, extra)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn('dir', json.loads((project / 'receipt.json').read_text()))


if __name__ == '__main__':
    unittest.main()
