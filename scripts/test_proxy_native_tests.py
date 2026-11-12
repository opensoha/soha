import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
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
        root = Path(directory)
        (root / 'contracts').mkdir()
        (root / 'go.mod').write_text('module github.com/opensoha/soha\n\ngo 1.27.4\nrequire ' + runner.SDK + ' v1.2.3\n')
        (root / 'go.sum').write_text(runner.SDK + ' v1.2.3 h1:distribution\n')
        (root / 'go.work').write_text('go 1.27.4\n\nuse (\n\t.\n\t./contracts\n)\n')
        (root / 'contracts/go.mod').write_text('module ' + runner.SDK + '\ngo 1.27.4\n')
        return root

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
                archive, digest = runner.source_archive(root, 'source')
                again, other_digest = runner.source_archive(root, 'source')
                declared, _ = runner.source_archive(root, 'declared')
            self.assertEqual(archive, again)
            self.assertEqual(digest, other_digest)
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
                    archive, _ = runner.source_archive(root, 'source')
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
