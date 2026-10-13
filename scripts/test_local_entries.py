import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
WORKSPACE = SCRIPTS.parents[1]
spec = importlib.util.spec_from_file_location('npm_shell', SCRIPTS / 'local-npm-shell.py')
npm_shell = importlib.util.module_from_spec(spec)
spec.loader.exec_module(npm_shell)


class LocalEntries(unittest.TestCase):
    def test_npm_build_and_nested_test_are_guarded_but_services_and_mcp_are_not(self):
        for event in ('test', 'build:dev', 'typecheck:e2e', 'check:consumers', 'verify',
                      'api:reference:check', 'package:check', 'release:verify', 'smoke:compiled'):
            self.assertTrue(npm_shell.guarded(WORKSPACE / 'soha-web', event, 'command'))
        for event in ('dev', 'start', 'start:feishu', 'preview', 'test:watch', 'postinstall'):
            self.assertFalse(npm_shell.guarded(WORKSPACE / 'soha-web', event, 'command'))
        self.assertFalse(npm_shell.guarded(WORKSPACE / 'soha-web/node_modules/tool', 'build', 'command'))
        self.assertFalse(npm_shell.guarded('/private/tmp/opensoha-cache/npm/_npx/tool', 'test', 'command'))

    def test_real_npm_nested_exit_arguments_and_temporary_output_cleanup(self):
        with tempfile.TemporaryDirectory(prefix='entry-probe-', dir=WORKSPACE) as directory:
            root = Path(directory)
            (root / 'package.json').write_text(json.dumps({'scripts': {
                'test': 'npm run check:probe -- "a b"',
                'check:probe': 'node probe.cjs',
                'test:failure': 'node probe.cjs fail'}}))
            (root / 'probe.cjs').write_text('''const fs = require('fs');
fs.writeFileSync('receipt.json', JSON.stringify({dir: process.env.OPENSOHA_VERIFY_DIR, args: process.argv.slice(2)}));
fs.writeFileSync(process.env.OPENSOHA_VERIFY_DIR + '/output', 'owned');
if (process.argv[2] === 'fail') process.exit(7);
''')
            shell = root / 'npm-shell'
            shell.symlink_to(SCRIPTS / 'local-npm-shell.py')
            env = dict(os.environ, GITHUB_ACTIONS='false', npm_config_script_shell=str(shell))
            for event, expected, arguments in [('test', 0, ['a b']), ('test:failure', 7, ['fail'])]:
                result = subprocess.run(['npm', 'run', event, '--silent'], cwd=root, env=env,
                                        capture_output=True, text=True, timeout=60)
                self.assertEqual(result.returncode, expected, result.stderr)
                receipt = json.loads((root / 'receipt.json').read_text())
                self.assertEqual(receipt['args'], arguments)
                self.assertFalse(Path(receipt['dir']).exists())

    def test_go_make_entry_plans_use_guard_and_ci_keeps_native_commands(self):
        matrix = {'soha': ['test', 'lint', 'complexity-check'],
                  'soha-agent': ['build', 'test', 'vet', 'deploy-images', 'verify-images'],
                  'soha-cli': ['build', 'test', 'vet', 'deploy-image', 'verify-image'],
                  'soha-operator': ['verify', 'test-race', 'docker-build', 'verify-image']}
        for repo, targets in matrix.items():
            for target in targets:
                with self.subTest(repo=repo, target=target):
                    result = subprocess.run(['make', '-n', target], cwd=WORKSPACE / repo,
                                            env=dict(os.environ, GITHUB_ACTIONS='false', OPENSOHA_LOCAL_VERIFY_REQUIRED='1'),
                                            capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn('opensoha-verify ', result.stdout)
            result = subprocess.run(['make', '-n', 'build' if repo != 'soha' else 'test'],
                                    cwd=WORKSPACE / repo, env=dict(os.environ, GITHUB_ACTIONS='true'),
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn('opensoha-verify ', result.stdout)


if __name__ == '__main__':
    unittest.main()
