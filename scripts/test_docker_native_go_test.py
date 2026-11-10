import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('native_pg', Path(__file__).with_name('docker-native-go-test.py'))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)
spec = importlib.util.spec_from_file_location('pg_owner', Path(__file__).with_name('run-deletion-integration.py'))
pg = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pg)


class SourceArchiveTests(unittest.TestCase):
    def repository(self, directory):
        repo = Path(directory) / 'repo'
        repo.mkdir()
        subprocess.run(['git', 'init', '-q', str(repo)], check=True)
        return repo

    def test_current_bytes_and_untracked_sources_exclude_private_config_and_deleted(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = self.repository(directory)
            (repo / '.gitignore').write_text('config.yaml\n')
            (repo / 'internal').mkdir()
            (repo / 'internal/tracked.go').write_text('old')
            (repo / 'internal/deleted.go').write_text('deleted')
            subprocess.run(['git', '-C', str(repo), 'add', '.'], check=True)
            (repo / 'internal/tracked.go').write_text('current')
            (repo / 'internal/deleted.go').unlink()
            (repo / 'internal/new.go').write_text('new')
            (repo / 'config.yaml').write_text('PRIVATE_CONFIG_MUST_NOT_TRANSFER')
            (repo / 'private.json').write_text('UNIGNORED_PRIVATE_MUST_NOT_TRANSFER')
            data = native.source_archive(repo, 'src/core')
            self.assertNotIn(b'PRIVATE_CONFIG_MUST_NOT_TRANSFER', data)
            self.assertNotIn(b'UNIGNORED_PRIVATE_MUST_NOT_TRANSFER', data)
            with tarfile.open(fileobj=io.BytesIO(data)) as archive:
                self.assertEqual(set(archive.getnames()), {'src/core/internal/tracked.go', 'src/core/internal/new.go'})
                self.assertEqual(archive.extractfile('src/core/internal/tracked.go').read(), b'current')

    def test_external_symlink_is_rejected_before_reading_target(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = self.repository(directory)
            outside = Path(directory) / 'private'
            outside.write_text('outside')
            (repo / 'internal').mkdir()
            (repo / 'internal/outside.go').symlink_to(outside)
            with self.assertRaises(ValueError):
                native.source_archive(repo, 'src/core')

    def test_wrong_repository_module_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = self.repository(directory)
            (repo / 'go.mod').write_text('module example.test/private\n')
            with self.assertRaises(ValueError):
                native.validate_repository(repo, 'github.com/opensoha/soha-contracts')

    def test_contracts_handwritten_go_and_private_non_source_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = self.repository(directory)
            (repo / 'agent/environment').mkdir(parents=True)
            (repo / 'agent/environment/export.go').write_text('package environment')
            (repo / 'auth').mkdir()
            (repo / 'auth/permission-catalog.json').write_text('{"permissions":[]}')
            (repo / 'openapi').mkdir()
            (repo / 'openapi/soha-api.json').write_text('{}')
            (repo / 'private.json').write_text('CONTRACT_PRIVATE_MUST_NOT_TRANSFER')
            data = native.source_archive(repo, 'src/contracts')
            self.assertNotIn(b'CONTRACT_PRIVATE_MUST_NOT_TRANSFER', data)
            with tarfile.open(fileobj=io.BytesIO(data)) as archive:
                self.assertEqual(archive.getnames(), ['src/contracts/agent/environment/export.go', 'src/contracts/auth/permission-catalog.json', 'src/contracts/openapi/soha-api.json'])

    def test_unknown_create_result_recovers_only_owned_name(self):
        value = {'Id': 'owned-id', 'Name': '/owned-name', 'Image': 'expected-image',
                 'Config': {'Labels': {'soha.test.run': 'nonce', 'soha.test.purpose': 'native-postgres-race'}},
                 'HostConfig': {}}
        replies = [subprocess.CompletedProcess([], 0, json.dumps([value]).encode(), b''),
                   subprocess.CompletedProcess([], 0),
                   subprocess.CompletedProcess([], 1, b'', b'No such object: owned-name')]
        report = {}
        with patch.object(native.subprocess, 'run', side_effect=replies) as run:
            native.cleanup(['docker'], 'owned-name', None, 'expected-image', 'nonce', {}, report)
            self.assertIn('owned-id', run.call_args_list[1].args[0])
        self.assertEqual(report['cleanup'], 'PASS')

    def test_unknown_create_result_refuses_foreign_label(self):
        value = {'Id': 'foreign-id', 'Name': '/owned-name', 'Image': 'expected-image',
                 'Config': {'Labels': {'soha.test.run': 'other'}}, 'HostConfig': {}}
        with patch.object(native.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, json.dumps([value]).encode(), b'')) as run:
            with self.assertRaises(ValueError):
                native.cleanup(['docker'], 'owned-name', None, 'expected-image', 'nonce', {}, {})
            self.assertEqual(run.call_count, 1)

    def test_native_inspect_timeout_is_cleanup_failure(self):
        report = {'cleanup': 'NOT_CREATED'}
        with patch.object(native.subprocess, 'run', side_effect=subprocess.TimeoutExpired(['docker'], 20)):
            with self.assertRaises(subprocess.TimeoutExpired):
                native.cleanup(['docker'], 'owned-name', None, 'image', 'nonce', {}, report)
        self.assertEqual(report['cleanup'], 'FAIL')

    def test_postgres_unknown_create_result_recovers_owned_name(self):
        value = {'Id': 'pg-id', 'Name': '/pg-name', 'Config': {'Image': pg.IMAGE, 'Labels': {'soha.test.run': 'nonce'}}}
        replies = [subprocess.CompletedProcess([], 0, json.dumps([value]), ''),
                   subprocess.CompletedProcess([], 0),
                   subprocess.CompletedProcess([], 1, '', 'No such object: pg-name')]
        with patch.object(pg.subprocess, 'run', side_effect=replies) as run:
            self.assertEqual(pg.cleanup_postgres(['docker'], None, 'pg-name', 'nonce', True), 'PASS')
            self.assertIn('pg-id', run.call_args_list[1].args[0])

    def test_postgres_unknown_create_result_refuses_foreign_run(self):
        value = {'Id': 'other', 'Name': '/pg-name', 'Config': {'Image': pg.IMAGE, 'Labels': {'soha.test.run': 'other'}}}
        with patch.object(pg.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, json.dumps([value]), '')) as run:
            with self.assertRaises(ValueError):
                pg.cleanup_postgres(['docker'], None, 'pg-name', 'nonce', True)
            self.assertEqual(run.call_count, 1)

    def test_postgres_absence_after_unknown_create_is_not_cleanup_pass(self):
        with patch.object(pg.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'No such object: pg-name')):
            self.assertEqual(pg.cleanup_postgres(['docker'], None, 'pg-name', 'nonce', True), 'ABSENT_AFTER_UNKNOWN_CREATE')

    def test_source_staging_does_not_shadow_the_owned_container_name(self):
        image = {'Os': 'linux', 'Architecture': 'amd64', 'Id': 'image', 'RepoDigests': ['golang@sha256:' + 'a' * 64]}
        def output(args, **kwargs):
            if args[1:3] == ['image', 'inspect']:
                return json.dumps([image]).encode()
            if args[1] == 'run':
                return 'container-id\n'
            if args[-2:] == ['go', 'version']:
                return 'go version go1.27.3 linux/amd64\n'
            return b''
        with patch.object(native, 'validate_repository'), patch.object(native, 'source_archive', return_value=b'owned source archive'), patch.object(native.subprocess, 'check_output', side_effect=output), patch.object(native.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)), patch.object(native, 'cleanup') as cleanup:
            native.run(['docker'], 'nonce', Path('/core'), Path('/contracts'), ['go', 'version'], {}, io.StringIO(), io.StringIO(), {})
        self.assertEqual(cleanup.call_args.args[1:3], ('soha-native-pg-nonce', 'container-id'))


if __name__ == '__main__':
    unittest.main()
