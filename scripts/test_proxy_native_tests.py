import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('proxy_native', Path(__file__).with_name('run-proxy-native-tests.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class ProxyNativeTests(unittest.TestCase):
    def setUp(self):
        self.receipt_directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.receipt_directory.cleanup)
        self.receipt_count = 0

    def receipt(self, run='run'):
        self.receipt_count += 1
        report = {'schemaVersion': 'proxy-native-gate/v1', 'runId': run, 'result': 'FAIL', 'resources': [], 'cleanup': []}
        path = Path(self.receipt_directory.name).resolve() / (str(self.receipt_count) + '.json')
        runner.reserve_receipt(path, report)
        return report

    def resources(self, docker=None, run='run', report=None):
        return runner.Resources(docker or [], run, report if report is not None else self.receipt(run))

    def test_endpoint_requires_explicit_remote_and_rejects_other_transports(self):
        for endpoint in ('ssh://database', 'ssh://other', 'ssh://user:secret@database', 'tcp://localhost:2375'):
            with self.subTest(endpoint=endpoint), self.assertRaises(ValueError):
                runner.docker_endpoint({'DOCKER_HOST': endpoint}, False)
        self.assertEqual(runner.docker_endpoint({'DOCKER_HOST': 'ssh://database'}, True), 'ssh://database')
        for env in ({'DOCKER_CONTEXT': 'other'}, {'DOCKER_HOST': 'ssh://other'}, {'DOCKER_HOST': 'ssh://database?x=1'}):
            with self.assertRaises(ValueError):
                runner.docker_endpoint(env, True)
        with tempfile.TemporaryDirectory() as tmp, socket.socket(socket.AF_UNIX) as sock:
            path = str(Path(tmp) / 'docker.sock')
            sock.bind(path)
            self.assertEqual(runner.docker_endpoint({'DOCKER_HOST': 'unix://' + path}, False), 'unix://' + path)

    def source(self, directory):
        root = Path(directory).resolve()
        (root / 'go.mod').write_text('module github.com/opensoha/soha\n\ngo 1.27.4\nrequire ' + runner.SDK + ' v1.2.3\n')
        (root / 'go.sum').write_text(runner.SDK + ' v1.2.3 h1:distribution\n')
        (root / 'go.work').write_text(runner.SOURCE_WORKSPACE + '\nreplace (\n\t' + runner.SDK
            + ' v1.2.3 => ./contracts\n\t' + runner.SDK + ' v1.2.4 => ./contracts\n)\n')
        for folder, module in (('contracts', runner.SDK), ('cli', 'github.com/opensoha/soha/cli'),
                               ('agent', 'github.com/opensoha/soha/agent')):
            (root / folder).mkdir()
            requirement = '' if folder == 'contracts' else 'require ' + runner.SDK + (' v1.2.3\n' if folder == 'cli' else ' v1.2.4\n')
            (root / folder / 'go.mod').write_text('module ' + module + '\ngo 1.27.4\n' + requirement)
        return root

    def test_source_aliases_record_policy_and_keep_selected_sdk_main_without_go(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            with patch.object(runner, 'command', side_effect=AssertionError('No Go invocation')):
                identity = runner.sdk_input(root, 'source')
            self.assertEqual(identity['sourceSDKAliasVersions'], ['v1.2.3', 'v1.2.4'])
            self.assertEqual(identity['sourcePolicySHA256'], hashlib.sha256(
                (runner.ROOT / 'scripts/source-workspace.py').read_bytes()).hexdigest())
            runner.verify_sdk({'Path': runner.SDK, 'Main': True, 'Dir': '/src/contracts'}, identity)
            with self.assertRaises(ValueError):
                runner.verify_sdk({'Path': runner.SDK, 'Main': True, 'Dir': '/src/contracts',
                    'Replace': {'Path': './contracts'}}, identity)

    def test_source_rejects_missing_extra_duplicate_and_unscoped_aliases(self):
        sdk = runner.SDK
        valid = [sdk + ' v1.2.3 => ./contracts', sdk + ' v1.2.4 => ./contracts']
        invalid = [[], valid[:1], valid + [valid[0]], valid + [sdk + ' v1.2.5 => ./contracts'],
            [sdk + ' => ./contracts', valid[1]], [valid[0], 'example.invalid/sdk v1.2.4 => ./contracts'],
            [valid[0], sdk + ' v1.2.4 => ./contracts v1.0.0'],
            [valid[0], sdk + ' v1.2.4 => ../contracts'],
            [valid[0], sdk + ' v1.2.4 => /src/contracts'],
            [valid[0], sdk + ' v1.2.4 => ./contracts/../contracts']]
        for aliases in invalid:
            with self.subTest(aliases=aliases), tempfile.TemporaryDirectory() as tmp:
                root = self.source(tmp)
                (root / 'go.work').write_text(runner.SOURCE_WORKSPACE + '\nreplace (\n' + '\n'.join(aliases) + '\n)\n')
                with self.assertRaises(ValueError):
                    runner.sdk_input(root, 'source')

    def test_source_alias_versions_follow_all_three_declared_requirements(self):
        for folder in ('.', 'cli', 'agent'):
            with self.subTest(folder=folder), tempfile.TemporaryDirectory() as tmp:
                root = self.source(tmp)
                manifest = root / folder / 'go.mod'
                original = manifest.read_text()
                old = 'v1.2.4' if folder == 'agent' else 'v1.2.3'
                manifest.write_text(original.replace(old, 'v1.2.5'))
                with self.assertRaises(ValueError):
                    runner.sdk_input(root, 'source')
                versions = {'v1.2.3', 'v1.2.4', 'v1.2.5'} - ({'v1.2.4'} if folder == 'agent' else set())
                (root / 'go.work').write_text(runner.SOURCE_WORKSPACE + '\n' + ''.join(
                    'replace ' + runner.SDK + ' ' + version + ' => ./contracts\n' for version in sorted(versions)))
                self.assertEqual(runner.sdk_input(root, 'source')['sourceSDKAliasVersions'], sorted(versions))
                for malformed in (original.replace('require ' + runner.SDK + ' ' + old + '\n', ''),
                        original + 'require ' + runner.SDK + ' ' + old + '\n'):
                    manifest.write_text(malformed)
                    with self.assertRaises(ValueError):
                        runner.sdk_input(root, 'source')

    def test_source_alias_target_rejects_internal_and_external_directory_symlinks(self):
        for external in (False, True):
            with self.subTest(external=external), tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as outside:
                root = self.source(tmp)
                target = Path(outside) / 'contracts' if external else root / 'other-contracts'
                (root / 'contracts').rename(target)
                (root / 'contracts').symlink_to(target, target_is_directory=True)
                with self.assertRaises(ValueError):
                    runner.sdk_input(root, 'source')

    def test_source_alias_bytes_are_bound_in_identity_and_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            first = runner.sdk_input(root, 'source')
            work = root / 'go.work'
            work.write_text(runner.SOURCE_WORKSPACE + '\nreplace ' + runner.SDK + ' v1.2.3 => ./contracts\nreplace '
                + runner.SDK + ' v1.2.4 => ./contracts\n')
            second = runner.sdk_input(root, 'source')
            self.assertNotEqual(first['goWorkSHA256'], second['goWorkSHA256'])
            self.assertEqual(first['sourceModules'], second['sourceModules'])
            with patch.object(runner, 'command', return_value=b'go.work\0'):
                archive, _ = runner.source_archive(root, 'source')
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                self.assertEqual(tar.extractfile('src/go.work').read(), work.read_bytes())

    def test_declared_ignores_workspace_aliases_and_never_loads_source_policy(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            (root / 'go.work').write_text('replace foreign => /outside\n')
            with patch.object(runner, 'load', side_effect=AssertionError('No source helper in declared mode')):
                identity = runner.sdk_input(root, 'declared')
            self.assertNotIn('sourceSDKAliasVersions', identity)
            self.assertNotIn('sourcePolicySHA256', identity)
            runner.verify_sdk({'Path': runner.SDK, 'Version': 'v1.2.3', 'Sum': 'h1:distribution'}, identity)

    def test_source_and_declared_sdk_identities_never_substitute(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            source, declared = [runner.sdk_input(root, mode) for mode in ('source', 'declared')]
            runner.verify_sdk({'Path': runner.SDK, 'Main': True, 'Dir': '/src/contracts'}, source)
            runner.verify_sdk({'Path': runner.SDK, 'Version': 'v1.2.3', 'Sum': 'h1:distribution'}, declared)
            for actual, identity in (({'Path': runner.SDK, 'Main': True, 'Dir': '/src/contracts'}, declared),
                    ({'Path': runner.SDK, 'Dir': '/other', 'Main': True}, source),
                    ({'Path': runner.SDK, 'Version': 'v1.2.3', 'Sum': 'h1:wrong'}, declared),
                    ({'Path': runner.SDK, 'Replace': {'Dir': '/src/contracts'}}, declared)):
                with self.assertRaises(ValueError):
                    runner.verify_sdk(actual, identity)
            (root / 'go.mod').write_text((root / 'go.mod').read_text() + 'replace other => ../other\n')
            for mode in ('source', 'declared'):
                with self.assertRaises(ValueError):
                    runner.sdk_input(root, mode)

    def test_source_requires_exact_workspace_and_declared_checksum(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            (root / 'go.work').write_text('go 1.27.4\nuse ../contracts\n')
            with self.assertRaises(ValueError):
                runner.sdk_input(root, 'source')
            (root / 'go.sum').write_text('')
            with self.assertRaises(ValueError):
                runner.sdk_input(root, 'declared')

    def test_source_binds_and_resolves_all_four_module_identities(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            identity = runner.sdk_input(root, 'source')
            self.assertEqual(set(identity['sourceModules']), {'github.com/opensoha/soha', runner.SDK,
                'github.com/opensoha/soha/cli', 'github.com/opensoha/soha/agent'})
            for folder, module in runner.SOURCE_MODULES.items():
                with self.subTest(module=module):
                    expected = identity['sourceModules'][module]
                    self.assertEqual(expected['goModSHA256'], hashlib.sha256((root / folder / 'go.mod').read_bytes()).hexdigest())
                    actual = {'Path': module, 'Dir': '/src' if folder == '.' else '/src/' + folder, 'Main': True}
                    runner.verify_source_module(actual, identity)
                    for changed in ({'Dir': '/outside'}, {'Main': False}, {'Main': 'true'},
                                    {'Replace': {'Dir': actual['Dir']}}, {'Error': 'unavailable'}):
                        with self.assertRaises(ValueError):
                            runner.verify_source_module(dict(actual, **changed), identity)

    def test_source_rejects_missing_cli_and_agent_manifests_without_affecting_declared(self):
        for folder in ('cli', 'agent'):
            with self.subTest(folder=folder), tempfile.TemporaryDirectory() as tmp:
                root = self.source(tmp)
                (root / folder / 'go.mod').unlink()
                with self.assertRaisesRegex(ValueError, 'Unavailable or unconfined'):
                    runner.sdk_input(root, 'source')
                declared = runner.sdk_input(root, 'declared')
                self.assertNotIn('sourceModules', declared)
                self.assertEqual(declared['expectedSum'], 'h1:distribution')

    def test_source_rejects_wrong_module_ids_and_replacements_in_every_module(self):
        for folder in ('contracts', 'cli', 'agent'):
            for change in ('module other\n', '\n  replace example.invalid/old => ../other\n'):
                with self.subTest(folder=folder, change=change), tempfile.TemporaryDirectory() as tmp:
                    root = self.source(tmp)
                    path = root / folder / 'go.mod'
                    path.write_text(change if change.startswith('module') else path.read_text() + change)
                    with self.assertRaises(ValueError):
                        runner.sdk_input(root, 'source')

    def test_source_rejects_external_manifest_symlinks(self):
        for folder in ('.', 'contracts', 'cli', 'agent'):
            with self.subTest(folder=folder), tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as outside:
                root = self.source(tmp)
                path = root / folder / 'go.mod'
                target = Path(outside) / 'go.mod'
                target.write_bytes(path.read_bytes())
                path.unlink()
                path.symlink_to(target)
                with self.assertRaisesRegex(ValueError, 'Unavailable or unconfined'):
                    runner.sdk_input(root, 'source')

    def test_archive_includes_four_module_inputs_and_binds_cli_agent_content(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            names = ['go.mod', 'go.sum', 'go.work', 'contracts/go.mod', 'cli/go.mod', 'agent/go.mod']
            files = {'cli/cmd/main.go': 'package main\n//go:embed message.txt\nvar message string\n',
                'cli/cmd/message.txt': 'public embedded fixture',
                'agent/internal/runtime/runtime_test.go': 'package runtime\n',
                'agent/internal/runtime/testdata/reply.json': '{"ok":true}',
                'cli/private/token.go': 'private fixture', 'agent/.env': 'private fixture',
                'cli/bin/generated.go': 'generated fixture', 'agent/node_modules/private.go': 'generated fixture'}
            for name, content in files.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
            names.extend(files)
            with patch.object(runner, 'command', return_value=('\0'.join(names) + '\0').encode()):
                base, digest = runner.source_archive(root, 'source')
                archive, _ = runner.add_embeds(root, 'source', base, {'cli/cmd/message.txt'})
                declared, _ = runner.source_archive(root, 'declared')
                (root / 'cli/cmd/main.go').write_text(files['cli/cmd/main.go'] + '// changed source\n')
                _, changed_cli = runner.source_archive(root, 'source')
                (root / 'agent/internal/runtime/runtime_test.go').write_text('package runtime\n// changed source\n')
                _, changed_agent = runner.source_archive(root, 'source')
            self.assertNotEqual(digest, changed_cli)
            self.assertNotEqual(changed_cli, changed_agent)
            with tarfile.open(fileobj=io.BytesIO(base)) as tar:
                self.assertNotIn('src/cli/cmd/message.txt', tar.getnames())
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                members = tar.getnames()
                for name in names[:6] + list(files)[:4]:
                    self.assertIn('src/' + name, members)
                for name in list(files)[4:]:
                    self.assertNotIn('src/' + name, members)
                self.assertEqual(tar.extractfile('src/cli/cmd/message.txt').read(), b'public embedded fixture')
            with tarfile.open(fileobj=io.BytesIO(declared)) as tar:
                self.assertFalse(any(name.startswith(('src/contracts/', 'src/cli/', 'src/agent/'))
                                     or name.startswith('src/go.work') for name in tar.getnames()))

    def test_archive_rejects_cli_and_agent_symlink_escape_and_unsafe_paths(self):
        for folder in ('cli', 'agent'):
            with self.subTest(folder=folder), tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as outside:
                root = self.source(tmp)
                target = Path(outside) / 'source.go'
                target.write_text('package fixture\n')
                (root / folder / 'escape.go').symlink_to(target)
                with patch.object(runner, 'command', return_value=(folder + '/escape.go\0').encode()):
                    with self.assertRaises(ValueError):
                        runner.source_archive(root, 'source')
            self.assertFalse(runner.source_file('/' + folder + '/source.go', 'source'))
            self.assertFalse(runner.source_file(folder + '/../internal/source.go', 'source'))

    def test_archive_includes_untracked_go_testdata_and_embeds_without_private_artifacts(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            names = ['go.mod', 'go.sum', 'go.work', 'contracts/go.mod',
                     'internal/test/new_test.go', 'internal/test/testdata/response.json',
                     'internal/test/embedded.txt', 'internal/test/.env', 'internal/test/private/token.go',
                     'internal/staticassets/web/dist/index.html', 'node_modules/token.go']
            for name in names[4:]:
                p = root / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_text('//go:embed embedded.txt\n' if name.endswith('new_test.go') else 'fixture')
            with patch.object(runner, 'command', return_value=('\0'.join(names) + '\0').encode()):
                base, digest = runner.source_archive(root, 'source')
                archive, _ = runner.add_embeds(root, 'source', base, {'internal/test/embedded.txt'})
                again, other_digest = runner.source_archive(root, 'source')
                declared, _ = runner.source_archive(root, 'declared')
            self.assertEqual(base, again)
            self.assertEqual(digest, other_digest)
            with tarfile.open(fileobj=io.BytesIO(base)) as tar:
                self.assertNotIn('src/internal/test/embedded.txt', tar.getnames())
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                members = tar.getnames()
                self.assertIn('src/internal/test/new_test.go', members)
                self.assertIn('src/internal/test/testdata/response.json', members)
                self.assertIn('src/internal/test/embedded.txt', members)
                self.assertFalse(any('private' in m or '/dist/' in m or '.env' in m or 'node_modules' in m for m in members))
            with tarfile.open(fileobj=io.BytesIO(declared)) as tar:
                self.assertFalse(any(m.startswith('src/contracts/') or m == 'src/go.work' for m in tar.getnames()))

    def test_archive_includes_indented_contract_schema_embeds(self):
        for indent in ('\t', '    '):
            with self.subTest(indent=repr(indent)), tempfile.TemporaryDirectory() as tmp:
                root = self.source(tmp)
                folder = root / 'contracts/network'
                folder.mkdir()
                (folder / 'schema.go').write_text('package network\nimport _ "embed"\nvar (\n' + indent + '//go:embed event.schema.json\n' + indent + 'schema []byte\n)\n')
                schema = b'{"type":"object"}'
                (folder / 'event.schema.json').write_bytes(schema)
                names = ['go.mod', 'go.sum', 'go.work', 'contracts/go.mod',
                         'contracts/network/schema.go', 'contracts/network/event.schema.json']
                with patch.object(runner, 'command', return_value=('\0'.join(names) + '\0').encode()):
                    base, _ = runner.source_archive(root, 'source')
                with tarfile.open(fileobj=io.BytesIO(base)) as tar:
                    self.assertNotIn('src/contracts/network/event.schema.json', tar.getnames())
                archive, _ = runner.add_embeds(root, 'source', base, {'contracts/network/event.schema.json'})
                with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                    self.assertEqual(tar.extractfile('src/contracts/network/event.schema.json').read(), schema)

    def test_escaping_source_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as outside:
            root = Path(tmp)
            (root / 'internal').mkdir()
            target = Path(outside) / 'secret.go'
            target.write_text('secret')
            (root / 'internal/link.go').symlink_to(target)
            with patch.object(runner, 'command', return_value=b'internal/link.go\0'), self.assertRaises(ValueError):
                runner.source_archive(root, 'source')

    def go_oracle(self, root, flags=(), discover=False):
        go = shutil.which('go')
        if go is None:
            self.skipTest('Go is not installed')
        env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOOS='linux', GOARCH='amd64',
                   GOPROXY='off', GOSUMDB='off',
                   CGO_ENABLED='1', GOFLAGS='-mod=readonly', GODEBUG='embedfollowsymlinks=0')
        version = subprocess.run([go, 'env', 'GOVERSION'], env=env, capture_output=True, text=True, check=True)
        self.assertEqual(version.stdout.strip(), 'go1.27.4', 'The embed oracle must use the native gate Go version')
        query = ['-e', '-json=Dir'] if discover else ['-json=Dir,EmbedFiles,TestEmbedFiles,XTestEmbedFiles']
        return subprocess.run([go, 'list', '-deps', '-test', *flags, *query, runner.NATIVE_PACKAGE],
            cwd=root, env=env, capture_output=True, text=True, timeout=120)

    def git_snapshot(self, root, files):
        for name, content in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        subprocess.run(['git', 'init', '-q', str(root)], check=True, capture_output=True)
        subprocess.run(['git', '-C', str(root), 'add', '.'], check=True, capture_output=True)

    def staged_catalog(self, root, stage, flags=()):
        base, digest = runner.source_archive(root, 'source')
        with tarfile.open(fileobj=io.BytesIO(base)) as tar:
            tar.extractall(stage, filter='data')
        source = stage / 'src'
        directories = self.go_oracle(source, flags, discover=True)
        self.assertEqual(directories.returncode, 0, directories.stderr)
        package_paths = {p.as_posix() for p, _ in runner.local_packages(directories.stdout, str(source))}
        catalog = runner.embed_catalog(root, 'source', base, package_paths)
        with tarfile.open(fileobj=io.BytesIO(catalog)) as tar:
            tar.extractall(stage, filter='data')
        return base, digest, catalog

    def test_go_oracle_selects_exact_embeds_from_filtered_catalog_and_preserves_base(self):
        package = 'internal/networkproxyruntime/'
        files = {
            'go.mod': 'module example.invalid/native\n\ngo 1.27.4\n',
            '.gitignore': ''.join(package + name + '\n' for name in
                                  ('resources/ignored.txt', 'resources/template.go', 'ignored.go', 'ignored.c')),
            package + 'runtime.go': r'''package networkproxyruntime
import (
    "embed"
    _ "example.invalid/native/pkg/dependency"
)
//go:embed "quoted space.txt"
var quoted string
//go:embed "escaped\x20name.txt"
var escaped string
//go:embed `backtick space.txt`
var backtick string
//go:embed resources
var resources embed.FS
var decoy = `
//go:embed raw-decoy.txt
`
''',
            package + 'runtime_test.go': 'package networkproxyruntime\nimport _ "embed"\n//go:embed inline.txt\nvar inline string\n',
            package + 'external_test.go': 'package networkproxyruntime_test\nimport _ "embed"\n//go:embed external.txt\nvar external string\n',
            package + 'default.go': '//go:build !soha_native_integration\n\npackage networkproxyruntime\nimport _ "embed"\n//go:embed default.txt\nvar selected string\n',
            package + 'active.go': '//go:build soha_native_integration\n\npackage networkproxyruntime\nimport _ "embed"\n//go:embed active.txt\nvar selected string\n',
            package + 'platform_linux_amd64.go': 'package networkproxyruntime\nimport _ "embed"\n//go:embed linux.txt\nvar platform string\n',
            'pkg/dependency/dependency.go': 'package dependency\nimport _ "embed"\n//go:embed dep.txt\nvar data string\n',
            package + 'resources/nested/go.mod': 'module example.invalid/nested\n\ngo 1.27.4\n',
            package + 'resources/tracked.go': 'package template\n',
            package + 'testdata/reply.json': '{"base":"preserved"}',
        }
        for name in ('quoted space.txt', 'escaped name.txt', 'backtick space.txt', 'inline.txt',
                     'external.txt', 'default.txt', 'active.txt', 'linux.txt', 'raw-decoy.txt',
                     'resources/visible.txt', 'resources/ignored.txt', 'resources/template.go',
                     'ignored.go', 'ignored.c', 'resources/.hidden.txt',
                     'resources/_hidden.txt', 'resources/nested/child.txt', 'resources/private/token.txt',
                     'resources/.env', 'resources/key.pem', 'resources/dist/bundle.txt'):
            files[package + name] = 'concealed-resource:' + name
        files['pkg/dependency/dep.txt'] = 'concealed-dependency'
        for name, prefix in (('.hidden.go', ''), ('_hidden.go', ''),
                             ('inactive.go', '//go:build never_enabled\n\n'), ('platform_windows.go', '')):
            files[package + name] = prefix + 'package networkproxyruntime\nimport _ "embed"\n//go:embed missing-negative.txt\nvar inactive string\n'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve() / 'checkout'
            root.mkdir()
            self.git_snapshot(root, files)
            # The Git snapshot includes unstaged source and ignored resource files on disk.
            (root / package / 'runtime.go').write_text(files[package + 'runtime.go'] + '// unstaged source\n')
            expected = {package + name for name in ('quoted space.txt', 'escaped name.txt', 'backtick space.txt',
                'inline.txt', 'external.txt', 'linux.txt', 'resources/visible.txt', 'resources/ignored.txt',
                'resources/template.go', 'resources/tracked.go')}
            expected.add('pkg/dependency/dep.txt')
            for flags, selected in (((), 'default.txt'), (runner.NATIVE_FLAGS, 'active.txt')):
                with self.subTest(flags=flags), tempfile.TemporaryDirectory() as staged:
                    stage = Path(staged).resolve()
                    base, base_digest, catalog = self.staged_catalog(root, stage, flags)
                    with tarfile.open(fileobj=io.BytesIO(catalog)) as tar:
                        members = tar.getmembers()
                        self.assertTrue(members)
                        self.assertTrue(all(entry.size == 0 for entry in members))
                        self.assertTrue(all(tar.extractfile(entry).read() == b'' for entry in members if entry.isfile()))
                        self.assertIn('src/' + package + 'resources/ignored.txt', tar.getnames())
                        self.assertFalse(any('/private/' in entry.name or '/dist/' in entry.name
                            or entry.name.endswith(('/.env', '.pem')) for entry in members))
                    result = self.go_oracle(stage / 'src', flags)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    names = runner.local_embed_files(result.stdout, 'source', str(stage / 'src'))
                    self.assertEqual(names, expected | {package + selected})
                    archive, digest = runner.add_embeds(root, 'source', base, names)
                    self.assertNotEqual(digest, base_digest)
                    with tarfile.open(fileobj=io.BytesIO(base)) as before, tarfile.open(fileobj=io.BytesIO(archive)) as after:
                        self.assertEqual(set(after.getnames()), set(before.getnames()) | {'src/' + name for name in names})
                        for entry in before:
                            self.assertEqual(after.extractfile(entry.name).read(), before.extractfile(entry).read())
                            self.assertEqual(after.getmember(entry.name).mode, entry.mode)
                        for name in names:
                            self.assertEqual(after.extractfile('src/' + name).read(), (root / name).read_bytes())
                    self.assertEqual(runner.add_embeds(root, 'source', base, names), (archive, digest))

    def test_go_oracle_and_native_archive_refuse_symlink_and_fifo_embeds(self):
        package = 'internal/networkproxyruntime/'
        for kind in ('symlink', 'fifo'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryDirectory() as staged:
                root = Path(tmp).resolve()
                self.git_snapshot(root, {
                    'go.mod': 'module example.invalid/native\n\ngo 1.27.4\n',
                    package + 'runtime.go': 'package networkproxyruntime\nimport _ "embed"\n//go:embed unsafe.txt\nvar data string\n',
                })
                path = root / package / 'unsafe.txt'
                if kind == 'symlink':
                    target = root / 'outside.txt'
                    target.write_text('never copy target bytes')
                    path.symlink_to(target)
                else:
                    os.mkfifo(path)
                stage = Path(staged).resolve()
                base, _, catalog = self.staged_catalog(root, stage)
                with tarfile.open(fileobj=io.BytesIO(catalog)) as tar:
                    entry = tar.getmember('src/' + package + 'unsafe.txt')
                    self.assertTrue(entry.issym())
                    self.assertEqual(entry.linkname, 'unsafe.txt')
                result = self.go_oracle(stage / 'src', runner.NATIVE_FLAGS)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('unsafe.txt', result.stderr)
                with self.assertRaises(ValueError):
                    runner.add_embeds(root, 'source', base, {package + 'unsafe.txt'})

    def test_install_native_source_uses_shared_flags_and_replaces_catalog_with_selected_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.source(tmp)
            folder = root / 'internal/networkproxyruntime'
            folder.mkdir(parents=True)
            (folder / 'runtime.go').write_text('package networkproxyruntime\n')
            (folder / 'selected.txt').write_text('selected bytes')
            (folder / 'unselected.txt').write_text('unselected bytes')
            with patch.object(runner, 'command', return_value=b'internal/networkproxyruntime/runtime.go\0'):
                base, _ = runner.source_archive(root, 'source')
            metadata = json.dumps({'Dir': '/src/internal/networkproxyruntime', 'EmbedFiles': ['selected.txt']})
            resources = self.resources()
            with patch.object(resources, 'call', side_effect=[metadata, b'', metadata, b'', b'']) as call:
                archive, digest = runner.install_native_source(resources, 'prep', root, 'source', base)
            self.assertEqual(call.call_args_list[0].args[0], ['exec', 'prep', 'go', 'list', '-deps', '-test',
                '-race', '-tags', 'soha_native_integration', '-e', '-json=Dir', runner.NATIVE_PACKAGE])
            self.assertEqual(call.call_args_list[2].args[0], ['exec', 'prep', 'go', 'list', '-deps', '-test',
                '-race', '-tags', 'soha_native_integration', '-json=Dir,EmbedFiles,TestEmbedFiles,XTestEmbedFiles', runner.NATIVE_PACKAGE])
            self.assertEqual(call.call_args_list[3].args[0], ['exec', 'prep', 'rm', '-rf', '/src'])
            self.assertEqual(call.call_args_list[4].kwargs['input'], archive)
            self.assertEqual((archive, digest), runner.add_embeds(root, 'source', base, {'internal/networkproxyruntime/selected.txt'}))
            with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
                self.assertNotIn('src/internal/networkproxyruntime/unselected.txt', tar.getnames())
            with patch.object(resources, 'call', side_effect=[metadata, b'', RuntimeError('go list failed')]) as call:
                with self.assertRaisesRegex(RuntimeError, 'go list failed'):
                    runner.install_native_source(resources, 'prep', root, 'source', base)
            self.assertEqual(call.call_count, 3)
            self.assertEqual(call.call_args_list[1].args[0], ['cp', '-', 'prep:/'])
            self.assertEqual(call.call_args_list[2].args[0][:4], ['exec', 'prep', 'go', 'list'])

    def test_private_config_is_excluded_even_inside_source_directories(self):
        for name in ('internal/secret.key', 'internal/private/token.go', 'internal/.env.local',
                     'contracts/node_modules/token.go', 'internal/.tmp/token.go'):
            self.assertFalse(runner.source_file(name, 'source'))

    def test_cli_rejects_arbitrary_modes_commands_and_existing_outputs_without_docker(self):
        for arguments in (['--sdk-mode', 'latest', '--out', 'x'],
                          ['--sdk-mode', 'source', '--out', 'x', '--test-command', 'anything']):
            with patch.object(sys, 'argv', ['runner', *arguments]), patch.object(runner, 'execute') as execute:
                with self.assertRaises(SystemExit):
                    runner.main()
                execute.assert_not_called()
        with tempfile.TemporaryDirectory() as tmp:
            receipt = Path(tmp) / 'receipt.json'
            receipt.write_text('original')
            with patch.object(sys, 'argv', ['runner', '--sdk-mode', 'source', '--out', str(receipt)]), patch.object(runner, 'execute') as execute:
                with self.assertRaises(SystemExit):
                    runner.main()
                execute.assert_not_called()
            self.assertEqual(receipt.read_text(), 'original')

    def test_cli_records_timeout_and_signal_failure_without_claiming_success(self):
        for error in (subprocess.TimeoutExpired(['docker'], 1), InterruptedError('interrupted')):
            with tempfile.TemporaryDirectory() as tmp:
                receipt = Path(tmp) / 'receipt.json'
                with patch.object(sys, 'argv', ['runner', '--sdk-mode', 'source', '--out', str(receipt)]), patch.object(runner, 'execute', side_effect=error):
                    self.assertEqual(runner.main(), 1)
                self.assertEqual(json.loads(receipt.read_text())['result'], 'FAIL')

    def test_execute_always_cleans_after_timeout_or_signal_during_creation(self):
        base = {'Id': 'sha256:' + 'a' * 64, 'Os': 'linux', 'Architecture': 'amd64',
                'RepoDigests': ['golang@sha256:' + 'b' * 64]}
        responses = [str(runner.ROOT), 'head', json.dumps([base])]
        for error in (subprocess.TimeoutExpired(['docker'], 1), InterruptedError('signal')):
            with patch.object(runner, 'write_input_identity'), patch.object(runner, 'sdk_input', return_value={}), \
                    patch.object(runner, 'source_archive', return_value=(b'archive', 'hash')), \
                    patch.object(runner, 'docker_endpoint', return_value='unix:///fixture'), \
                    patch.object(runner, 'command', side_effect=responses), \
                    patch.object(runner.subprocess, 'run', return_value=SimpleNamespace(returncode=0)), \
                    patch.object(runner.Resources, 'create', side_effect=error), \
                    patch.object(runner.Resources, 'cleanup') as cleanup:
                with self.assertRaises(type(error)):
                    runner.execute(SimpleNamespace(sdk_mode='source', allow_remote_docker=False), self.receipt())
                cleanup.assert_called_once()

    def test_creation_registers_cleanup_target_before_uncertain_response(self):
        resources = self.resources()
        with patch.object(resources, 'call', side_effect=subprocess.TimeoutExpired(['docker'], 1)):
            with self.assertRaises(subprocess.TimeoutExpired):
                resources.create('container', 'fixture', ['create'], 'sha256:base', 'none')
        self.assertEqual(resources.items[0]['name'], 'fixture')
        self.assertIsNone(resources.items[0]['id'])
        disk = json.loads(Path(resources.report['receiptPath']).read_text())
        self.assertEqual(disk['resources'][0]['name'], 'fixture')
        self.assertIsNone(disk['resources'][0]['id'])

    def test_create_persists_full_intent_before_docker_and_acknowledged_id_before_inspection(self):
        resources = self.resources()
        path = Path(resources.report['receiptPath'])
        def create(args, **kwargs):
            disk = json.loads(path.read_text())
            self.assertEqual(disk['runId'], 'run')
            self.assertEqual(disk['resources'], [dict(kind='container', name='fixture', id=None,
                image='sha256:base', network='none')])
            return 'a' * 64
        def inspect(kind, identifier):
            disk = json.loads(path.read_text())
            self.assertEqual(disk['resources'][0]['id'], identifier)
            return self.container()
        with patch.object(resources, 'call', side_effect=create), patch.object(resources, 'inspect', side_effect=inspect):
            self.assertEqual(resources.create('container', 'fixture', ['create'], 'sha256:base', 'none'), 'a' * 64)

    def test_failed_intent_write_prevents_docker_mutation(self):
        resources = self.resources()
        with patch.object(runner, 'persist', side_effect=OSError('disk full')), patch.object(resources, 'call') as call:
            with self.assertRaises(OSError):
                resources.create('container', 'fixture', ['create'], 'sha256:base', 'none')
        call.assert_not_called()
        self.assertEqual(json.loads(Path(resources.report['receiptPath']).read_text())['resources'], [])

    def test_cli_reserves_durable_run_identity_before_execute(self):
        with tempfile.TemporaryDirectory() as tmp:
            receipt = Path(tmp) / 'receipt.json'
            def execute(args, report):
                disk = json.loads(receipt.read_text())
                self.assertEqual(disk['runId'], report['runId'])
                self.assertEqual(disk['result'], 'FAIL')
                self.assertEqual(disk['resources'], [])
                raise InterruptedError('simulated disconnect')
            with patch.object(sys, 'argv', ['runner', '--sdk-mode', 'source', '--out', str(receipt)]), patch.object(runner, 'execute', side_effect=execute):
                self.assertEqual(runner.main(), 1)

    def test_endpoint_and_pull_intent_are_durable_before_base_image_pull(self):
        report = self.receipt()
        def command(args, **kwargs):
            if args[-1] == '--show-toplevel':
                return str(runner.ROOT)
            if args[-1] == 'HEAD':
                return 'head'
            if 'pull' in args:
                disk = json.loads(Path(report['receiptPath']).read_text())
                self.assertEqual(disk['dockerEndpoint'], 'unix:///fixture')
                self.assertEqual(disk['baseImagePreparation']['reference'], runner.IMAGE)
                raise InterruptedError('stop before fixture mutation')
            self.fail('unexpected command')
        with patch.object(runner, 'write_input_identity'), patch.object(runner, 'sdk_input', return_value={}), \
                patch.object(runner, 'source_archive', return_value=(b'archive', 'hash')), \
                patch.object(runner, 'docker_endpoint', return_value='unix:///fixture'), \
                patch.object(runner, 'command', side_effect=command), \
                patch.object(runner.subprocess, 'run', return_value=SimpleNamespace(returncode=1)):
            with self.assertRaises(InterruptedError):
                runner.execute(SimpleNamespace(sdk_mode='source', allow_remote_docker=False), report)

    def test_primary_cleanup_and_receipt_write_errors_remain_distinct(self):
        report = self.receipt()
        base = {'Id': 'sha256:' + 'a' * 64, 'Os': 'linux', 'Architecture': 'amd64',
                'RepoDigests': ['golang@sha256:' + 'b' * 64]}
        with patch.object(runner, 'write_input_identity'), patch.object(runner, 'sdk_input', return_value={}), \
                patch.object(runner, 'source_archive', return_value=(b'archive', 'hash')), \
                patch.object(runner, 'docker_endpoint', return_value='unix:///fixture'), \
                patch.object(runner, 'command', side_effect=[str(runner.ROOT), 'head', json.dumps([base])]), \
                patch.object(runner.subprocess, 'run', return_value=SimpleNamespace(returncode=0)), \
                patch.object(runner.Resources, 'create', side_effect=InterruptedError('primary disconnect')), \
                patch.object(runner.Resources, 'cleanup', side_effect=RuntimeError('cleanup unavailable')):
            with self.assertRaisesRegex(InterruptedError, 'primary disconnect'):
                runner.execute(SimpleNamespace(sdk_mode='source', allow_remote_docker=False), report)
        self.assertEqual(report['executionError'], 'primary disconnect')
        self.assertEqual(report['cleanupError'], 'cleanup unavailable')
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'receipt.json'
            def failure(args, current):
                current.update(executionError=report['executionError'], cleanupError=report['cleanupError'])
                raise InterruptedError('primary disconnect')
            with patch.object(sys, 'argv', ['runner', '--sdk-mode', 'source', '--out', str(path)]), \
                    patch.object(runner, 'execute', side_effect=failure), \
                    patch.object(runner, 'persist', side_effect=OSError('receipt unavailable')), \
                    patch('builtins.print') as output:
                self.assertEqual(runner.main(), 1)
            final = json.loads(output.call_args.args[0])
            self.assertEqual(final['executionError'], 'primary disconnect')
            self.assertEqual(final['cleanupError'], 'cleanup unavailable')
            self.assertEqual(final['receiptWriteErrors'], ['OSError'])

    def container(self):
        return {'Id': 'a' * 64, 'Name': '/fixture', 'Image': 'sha256:base',
            'Config': {'Labels': {runner.LABEL: 'run', 'soha.test.purpose': runner.PURPOSE}},
            'NetworkSettings': {'Networks': {'none': {}}},
            'Mounts': [], 'HostConfig': {'NetworkMode': 'none', 'NanoCpus': 2000000000,
                'Memory': 2 * 1024**3, 'PidsLimit': 256}}

    def kernel_installation_fixture(self):
        resources = self.resources(['docker'])
        item = dict(kind='container', name='soha-proxy-prep-run', id='a' * 64,
                    image='sha256:base', network='bridge')
        resources.items.append(item)
        container = self.container()
        container.update(Name='/' + item['name'], NetworkSettings={'Networks': {'bridge': {}}})
        container['HostConfig']['NetworkMode'] = 'bridge'
        content = {name: ('public fixture ' + name).encode() for name in ('mihomo', 'sing-box', 'v2ray')}
        engines = {name: {'binary_sha256': hashlib.sha256(raw).hexdigest()} for name, raw in content.items()}
        metadata = {'/gate/engines': [501, 20, stat.S_IFDIR | 0o700, 4096]}
        metadata.update(('/gate/engines/' + name, [501, 20, stat.S_IFREG | 0o700, len(raw)])
                        for name, raw in content.items())
        fixture = SimpleNamespace(resources=resources, container=container, content=content,
            engines=engines, metadata=metadata, commands=[], mutations=[], drift_after=False, normalize=True)

        def command(args, **kwargs):
            self.assertEqual(args[:3], ['docker', 'exec', item['id']])
            invocation = args[3:]
            fixture.commands.append(invocation)
            if invocation == ['id', '-u']:
                return '0\n'
            paths = list(metadata)
            if invocation == ['stat', '-c', '%n %u %g %f %s', '--', *paths]:
                return ''.join(f'{path} {uid} {gid} {mode:x} {size}\n'
                    for path, (uid, gid, mode, size) in metadata.items())
            if invocation == ['sha256sum', '--', *paths[1:]]:
                return ''.join(hashlib.sha256(raw).hexdigest() + '  /gate/engines/' + name + '\n'
                               for name, raw in content.items())
            self.assertEqual(invocation, ['chown', '--no-dereference', '0:0', '--', *paths])
            receipt = json.loads(Path(resources.report['receiptPath']).read_text())
            self.assertEqual(receipt['kernelInstallation']['result'], 'FAIL')
            self.assertEqual(receipt['lastAction'], {'operation': 'docker-exec', 'resourceId': item['id']})
            fixture.mutations.append(invocation)
            if fixture.normalize:
                for values in metadata.values():
                    values[:2] = [0, 0]
            if fixture.drift_after:
                content['mihomo'] += b'changed'
            return ''

        fixture.command = command
        return fixture

    def install_fixture_kernels(self, fixture):
        with patch.object(runner, 'command', side_effect=fixture.command), \
                patch.object(fixture.resources, 'inspect', return_value=fixture.container), \
                patch.object(runner.os, 'geteuid', return_value=501), \
                patch.object(runner.os, 'getegid', return_value=20):
            runner.install_kernel_metadata(fixture.resources, 'a' * 64, fixture.engines)

    def test_kernel_installation_corrects_copied_owner_without_changing_bytes(self):
        fixture = self.kernel_installation_fixture()
        original = dict(fixture.content)
        self.install_fixture_kernels(fixture)
        installation = fixture.resources.report['kernelInstallation']
        self.assertEqual(installation['result'], 'PASS')
        self.assertEqual(len(fixture.mutations), 1)
        self.assertEqual(fixture.content, original)
        for path, before in installation['before'].items():
            after = installation['after'][path]
            self.assertEqual((before['uid'], before['gid']), (501, 20))
            self.assertEqual((after['uid'], after['gid'], after['mode']), (0, 0, '0700'))
            self.assertEqual(before.get('sha256'), after.get('sha256'))
        disk = json.loads(Path(fixture.resources.report['receiptPath']).read_text())
        self.assertEqual(disk['kernelInstallation'], installation)

    def test_kernel_installation_rejects_symlinks_nonregular_unknown_owner_and_unsafe_mode(self):
        changes = [('directory symlink', '/gate/engines', 2, stat.S_IFLNK | 0o700),
            ('binary symlink', '/gate/engines/mihomo', 2, stat.S_IFLNK | 0o700),
            ('nonregular', '/gate/engines/mihomo', 2, stat.S_IFIFO | 0o700),
            ('unknown owner', '/gate/engines/mihomo', 0, 1001),
            ('unknown group', '/gate/engines/mihomo', 1, 1001),
            ('unsafe mode', '/gate/engines/mihomo', 2, stat.S_IFREG | 0o720),
            ('oversized', '/gate/engines/mihomo', 3, (128 << 20) + 1)]
        for name, path, field, value in changes:
            with self.subTest(name=name):
                fixture = self.kernel_installation_fixture()
                fixture.metadata[path][field] = value
                with self.assertRaises(ValueError):
                    self.install_fixture_kernels(fixture)
                self.assertEqual(fixture.mutations, [])

    def test_kernel_installation_rejects_checksum_drift_before_and_after_mutation(self):
        for after in (False, True):
            with self.subTest(after=after):
                fixture = self.kernel_installation_fixture()
                if after:
                    fixture.drift_after = True
                else:
                    fixture.content['mihomo'] += b'changed'
                with self.assertRaisesRegex(ValueError, 'Copied kernel digest mismatch'):
                    self.install_fixture_kernels(fixture)
                self.assertEqual(len(fixture.mutations), int(after))
                self.assertNotEqual(fixture.resources.report.get('kernelInstallation', {}).get('result'), 'PASS')

    def test_kernel_installation_refuses_foreign_preparation_container(self):
        fixture = self.kernel_installation_fixture()
        fixture.container['Config']['Labels'][runner.LABEL] = 'foreign'
        with self.assertRaises(ValueError):
            self.install_fixture_kernels(fixture)
        self.assertEqual(fixture.commands, [])

    def test_kernel_installation_rejects_unpinned_inventory_before_container_calls(self):
        fixture = self.kernel_installation_fixture()
        fixture.engines['other'] = fixture.engines.pop('mihomo')
        with self.assertRaises(ValueError):
            self.install_fixture_kernels(fixture)
        self.assertEqual(fixture.commands, [])

    def test_kernel_installation_requires_final_root_metadata(self):
        fixture = self.kernel_installation_fixture()
        fixture.normalize = False
        with self.assertRaises(ValueError):
            self.install_fixture_kernels(fixture)
        self.assertEqual(len(fixture.mutations), 1)
        self.assertEqual(fixture.resources.report['kernelInstallation']['result'], 'FAIL')

    def test_kernel_installation_receipt_failure_prevents_ownership_mutation(self):
        fixture = self.kernel_installation_fixture()
        persist = runner.persist
        def fail_installation_receipt(report):
            if 'kernelInstallation' in report:
                raise OSError('receipt full')
            persist(report)
        with patch.object(runner, 'persist', side_effect=fail_installation_receipt):
            with self.assertRaises(OSError):
                self.install_fixture_kernels(fixture)
        self.assertEqual(fixture.mutations, [])

    def test_ownership_requires_exact_identity_and_runtime_isolation(self):
        original = self.container()
        runner.owned(original, 'container', 'fixture', 'a' * 64, 'run', 'sha256:base', 'none')
        for key, value in (('Binds', ['/host:/gate']), ('Privileged', True), ('PortBindings', {'80/tcp': []}),
                ('NetworkMode', 'host'), ('ExtraHosts', ['alias:1.2.3.4']), ('NanoCpus', 0),
                ('Memory', 0), ('PidsLimit', 0), ('PidMode', 'host')):
            candidate = copy.deepcopy(original)
            candidate['HostConfig'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                runner.owned(candidate, 'container', 'fixture', 'a' * 64, 'run', 'sha256:base', 'none')
        for key, value in (('Id', 'other'), ('Image', 'shared'), ('Name', '/other'), ('Mounts', [{'Type': 'bind'}])):
            candidate = dict(original, **{key: value})
            with self.assertRaises(ValueError):
                runner.owned(candidate, 'container', 'fixture', 'a' * 64, 'run', 'sha256:base', 'none')
        with self.assertRaises(ValueError):
            runner.owned(original, 'container', 'fixture', 'a' * 64, 'other-run', 'sha256:base', 'none')
        with self.assertRaises(ValueError):
            runner.owned(dict(original, NetworkSettings={'Networks': {'none': {}, 'bridge': {}}}),
                         'container', 'fixture', 'a' * 64, 'run', 'sha256:base', 'none')

    def test_cleanup_refuses_replaced_resource_and_transport_failure(self):
        for value in (dict(self.container(), Image='shared-image'), None):
            report = self.receipt()
            resources = self.resources(['docker'], report=report)
            resources.items = [{'kind': 'container', 'name': 'fixture', 'id': 'a' * 64,
                                'image': 'sha256:base', 'network': 'none'}]
            calls = []
            def fake(args, **kwargs):
                calls.append(args)
                if value is None:
                    raise RuntimeError('transport failed')
                return json.dumps({'Names': 'fixture', 'ID': 'a' * 64}) + '\n'
            with patch.object(resources, 'call', side_effect=fake), patch.object(resources, 'inspect', return_value=value):
                with self.assertRaises(RuntimeError):
                    resources.cleanup()
            self.assertFalse(any('rm' in call for call in calls))
            self.assertEqual(report['cleanup'][0]['result'], 'FAIL')

    def test_cleanup_recovers_lost_create_response_by_owned_name_and_verifies_absence(self):
        resources = self.resources(['docker'])
        resources.items = [{'kind': 'container', 'name': 'fixture', 'id': None, 'image': 'sha256:base', 'network': 'none'}]
        responses = [json.dumps({'Names': 'fixture', 'ID': 'a' * 64}), b'', '']
        with patch.object(resources, 'call', side_effect=responses) as call, patch.object(resources, 'inspect', return_value=self.container()):
            resources.cleanup()
        self.assertEqual(call.call_args_list[1].args[0], ['container', 'rm', '--force', '--volumes', 'a' * 64])
        self.assertEqual(resources.report['cleanup'][0]['result'], 'PASS')

    def test_cleanup_continues_after_mismatch_without_deleting_foreign_resource(self):
        resources = self.resources()
        resources.items = [dict(kind='container', name=name, id='a' * 64,
                               image='sha256:base', network='none') for name in ('fixture', 'foreign')]
        responses = [json.dumps({'Names': 'foreign', 'ID': 'a' * 64}),
                     json.dumps({'Names': 'fixture', 'ID': 'a' * 64}), b'', '']
        with patch.object(resources, 'call', side_effect=responses) as call, patch.object(resources, 'inspect', return_value=self.container()):
            with self.assertRaises(RuntimeError):
                resources.cleanup()
        removals = [args.args[0] for args in call.call_args_list if 'rm' in args.args[0]]
        self.assertEqual(len(removals), 1)
        self.assertEqual([entry['result'] for entry in resources.report['cleanup']], ['FAIL', 'PASS'])

    def test_cleanup_does_not_accept_unconfirmed_removal(self):
        resources = self.resources()
        resources.items = [dict(kind='container', name='fixture', id='a' * 64,
                               image='sha256:base', network='none')]
        listing = json.dumps({'Names': 'fixture', 'ID': 'a' * 64})
        with patch.object(resources, 'call', side_effect=[listing, b'', listing]), patch.object(resources, 'inspect', return_value=self.container()):
            with self.assertRaises(RuntimeError):
                resources.cleanup()

    def test_cleanup_finds_known_renamed_container_by_id_and_refuses_false_absence(self):
        resources = self.resources()
        resources.items = [dict(kind='container', name='fixture', id='a' * 64,
                               image='sha256:base', network='none')]
        value = dict(self.container(), Name='/renamed')
        with patch.object(resources, 'call', return_value=json.dumps({'Names': 'renamed', 'ID': 'a' * 64})) as call, \
                patch.object(resources, 'inspect', return_value=value) as inspect:
            with self.assertRaises(RuntimeError):
                resources.cleanup()
        inspect.assert_called_once_with('container', 'a' * 64)
        self.assertFalse(any('rm' in args.args[0] for args in call.call_args_list))
        self.assertEqual(resources.report['cleanup'][0]['result'], 'FAIL')

    def test_cleanup_finds_known_untagged_image_by_id_and_preserves_unknown_refs(self):
        identifier = 'sha256:' + 'a' * 64
        for tags, repository, tag in (([], '<none>', '<none>'), (['other:tag'], 'other', 'tag')):
            resources = self.resources()
            resources.items = [dict(kind='image', name='fixture:run', id=identifier, image=None, network=None)]
            value = {'Id': identifier, 'RepoTags': tags,
                     'Config': {'Labels': {runner.LABEL: 'run', 'soha.test.purpose': runner.PURPOSE}}}
            with patch.object(resources, 'call', return_value=json.dumps({'Repository': repository, 'Tag': tag, 'ID': identifier})) as call, \
                    patch.object(resources, 'inspect', return_value=value) as inspect:
                with self.assertRaises(RuntimeError):
                    resources.cleanup()
            inspect.assert_called_once_with('image', identifier)
            self.assertFalse(any('rm' in args.args[0] for args in call.call_args_list))
            self.assertEqual(resources.report['cleanup'][0]['result'], 'FAIL')

    def test_image_cleanup_lists_all_images_before_and_after_removal(self):
        resources = self.resources()
        identifier = 'sha256:' + 'a' * 64
        resources.items = [dict(kind='image', name='fixture:run', id=identifier, image=None, network=None)]
        image = {'Id': identifier, 'RepoTags': ['fixture:run'],
                 'Config': {'Labels': {runner.LABEL: 'run', 'soha.test.purpose': runner.PURPOSE}}}
        listings = []
        def docker(args, **kwargs):
            if args[:2] == ['image', 'ls']:
                self.assertIn('--all', args)
                self.assertIn('--no-trunc', args)
                listings.append(args)
                if len(listings) == 1:
                    return json.dumps({'Repository': 'fixture', 'Tag': 'run', 'ID': identifier})
                # A now-untagged intermediate image still exists as another image's parent.
                return json.dumps({'Repository': '<none>', 'Tag': '<none>', 'ID': identifier})
            if args[:2] == ['ps', '-aq']:
                return ''
            self.assertEqual(args, ['image', 'rm', 'fixture:run'])
            return b''
        with patch.object(resources, 'call', side_effect=docker), patch.object(resources, 'inspect', return_value=image):
            with self.assertRaises(RuntimeError):
                resources.cleanup()
        self.assertEqual(len(listings), 2)
        self.assertEqual(resources.report['cleanup'][0]['result'], 'FAIL')

    def test_cleanup_preserves_same_name_replacement_when_known_id_is_absent(self):
        resources = self.resources()
        resources.items = [dict(kind='container', name='fixture', id='a' * 64,
                               image='sha256:base', network='none')]
        with patch.object(resources, 'call', return_value=json.dumps({'Names': 'fixture', 'ID': 'b' * 64})) as call, \
                patch.object(resources, 'inspect') as inspect:
            with self.assertRaises(RuntimeError):
                resources.cleanup()
        inspect.assert_not_called()
        self.assertFalse(any('rm' in args.args[0] for args in call.call_args_list))

    def test_network_cleanup_rejects_external_or_unexpected_attachments(self):
        value = {'Id': 'a' * 64, 'Name': 'fixture', 'Driver': 'bridge', 'Internal': True,
                 'Labels': {runner.LABEL: 'run', 'soha.test.purpose': runner.PURPOSE}, 'Containers': {}}
        runner.owned(value, 'network', 'fixture', 'a' * 64, 'run')
        for changed in ({'Internal': False}, {'Driver': 'host'}, {'Containers': {'foreign': {}}}):
            with self.assertRaises(ValueError):
                runner.owned(dict(value, **changed), 'network', 'fixture', 'a' * 64, 'run')
        for configs in ([{'Subnet': '10.0.0.0/8'}], [{'Subnet': '172.16.0.0/24'}, {'Subnet': '10.0.0.0/8'}]):
            with self.assertRaises(ValueError):
                runner.owned(dict(value, IPAM={'Config': configs}), 'network', 'fixture', 'a' * 64, 'run', network='172.16.0.0/24')

    def test_subnet_avoids_existing_networks_and_protected_ten_range(self):
        resources = self.resources(run='00000000')
        with patch.object(resources, 'call', side_effect=['id', json.dumps([{'IPAM': {'Config': [{'Subnet': '172.16.0.0/16'}]}}])]):
            self.assertEqual(resources.run, '00000000')
            self.assertEqual(runner.available_subnet(resources), '172.17.0.0/24')

    def test_subnet_builtin_null_ipam_and_ipv6_preserve_existing_ipv4_avoidance(self):
        resources = self.resources(run='00000000')
        networks = [
            {'Name': 'host', 'Driver': 'host', 'IPAM': {'Driver': 'default', 'Options': None, 'Config': None}},
            {'Name': 'none', 'Driver': 'null', 'IPAM': {'Driver': 'default', 'Options': None, 'Config': None}},
            {'Name': 'bridge', 'Driver': 'bridge', 'IPAM': {'Config': [{'Subnet': '172.17.0.0/16', 'Gateway': '172.17.0.1'}]}},
            {'Name': 'existing', 'Driver': 'bridge', 'IPAM': {'Config': [{'Subnet': '172.16.0.0/16', 'Gateway': '172.16.0.1'}]}},
            {'Name': 'ipv6', 'Driver': 'bridge', 'EnableIPv6': True, 'IPAM': {'Config': [{'Subnet': 'fd00:1234::/64'}]}},
        ]
        with patch.object(resources, 'call', side_effect=['host none bridge existing ipv6', json.dumps(networks)]):
            self.assertEqual(runner.available_subnet(resources), '172.18.0.0/24')

    def test_subnet_ipv6_pools_do_not_reserve_ipv4_candidates(self):
        for subnet in ('fd00:1234::/64', '::/0', '::ffff:ac10:0/112'):
            with self.subTest(subnet=subnet):
                resources = self.resources(run='00000000')
                networks = [{'IPAM': {'Config': [{'Subnet': subnet}]}}]
                with patch.object(resources, 'call', side_effect=['ipv6', json.dumps(networks)]):
                    self.assertEqual(runner.available_subnet(resources), '172.16.0.0/24')

    def test_subnet_rejects_malformed_network_ipam_and_subnet_shapes(self):
        malformed = [
            {}, None, [None], [{}], [{'IPAM': None}], [{'IPAM': []}], [{'IPAM': {}}],
            [{'IPAM': {'Config': {}}}], [{'IPAM': {'Config': ''}}],
            [{'IPAM': {'Config': False}}], [{'IPAM': {'Config': [None]}}],
            [{'IPAM': {'Config': [[]]}}], [{'IPAM': {'Config': [{}]}}],
        ]
        malformed.extend([{'IPAM': {'Config': [{'Subnet': subnet}]}}]
                         for subnet in (None, '', 123, False, 'invalid', '172.16.0.1/24'))
        for networks in malformed:
            with self.subTest(networks=networks):
                resources = self.resources(run='00000000')
                with patch.object(resources, 'call', side_effect=['id', json.dumps(networks)]):
                    with self.assertRaisesRegex(ValueError, 'Invalid Docker'):
                        runner.available_subnet(resources)


if __name__ == '__main__':
    unittest.main()
