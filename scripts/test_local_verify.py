import importlib.util
import json
from pathlib import Path
import signal
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location('local_verify', Path(__file__).with_name('local-verify.py'))
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class LocalVerificationGuards(unittest.TestCase):
    def test_environment_overrides_inherited_cache_paths(self):
        with patch.dict(verify.os.environ, {'GOCACHE': '/other', 'npm_config_cache': '/other'}):
            env = verify.environment()
        self.assertEqual(env['GOCACHE'], str(verify.CACHE_ROOT / 'go-build'))
        self.assertEqual(env['GOMODCACHE'], str(verify.CACHE_ROOT / 'go-mod'))
        self.assertEqual(env['SOHA_NPM_CACHE'], env['npm_config_cache'])

    def test_process_guard_handles_command_titles_containing_paths(self):
        with patch.object(verify, 'call', return_value='COMM\nnpm exec tool /some/cache/path\n'), \
                patch.object(verify.subprocess, 'run') as opened:
            self.assertTrue(verify.busy(verify.CACHE_ROOT / 'npm', ('npm',)))
        opened.assert_not_called()

    def test_direct_node_process_using_cache_is_retained(self):
        with patch.object(verify, 'call', side_effect=['COMM\nnode\n', f'ARGS\nnode {verify.CACHE_ROOT}/npm/_npx/tool.js\n']), \
                patch.object(verify.subprocess, 'run') as opened:
            self.assertTrue(verify.busy(verify.CACHE_ROOT / 'npm', ('npm',)))
        opened.assert_not_called()

    def test_docker_process_guard_ignores_own_workspace_lock(self):
        with patch.object(verify, 'call', return_value='COMM\npython3\n'), \
                patch.object(verify.subprocess, 'run') as opened:
            self.assertFalse(verify.busy(None, ('docker', 'buildkitd')))
        opened.assert_not_called()

    def test_active_over_budget_cache_is_retained(self):
        with patch.object(verify, 'size', side_effect=lambda p: 9 * verify.GIB if p.name == 'go-build' else 0), \
                patch.object(verify, 'busy', return_value=True), patch.object(verify, 'call') as command:
            self.assertEqual(verify.maintain(), ['go-build'])
        command.assert_not_called()

    def test_idle_compiler_cache_uses_go_clean_and_keeps_modules(self):
        with patch.object(verify, 'size', side_effect=[9 * verify.GIB, 0, 0, 0, 0]), \
                patch.object(verify, 'busy', return_value=False), patch.object(verify, 'call') as command:
            self.assertEqual(verify.maintain(), [])
        self.assertEqual(command.call_args.args[0], ['go', 'clean', '-cache'])
        self.assertEqual(command.call_args.args[1]['GOCACHE'], str(verify.CACHE_ROOT / 'go-build'))

    def test_npm_download_budget_does_not_delete_npx_environments(self):
        measured = []

        def usage(path):
            measured.append(path)
            return 3 * verify.GIB if path.name == '_cacache' and measured.count(path) == 1 else 0

        with patch.object(verify, 'size', side_effect=usage), patch.object(verify, 'busy', return_value=False), \
                patch.object(verify, 'call') as command:
            self.assertEqual(verify.maintain(), [])
        self.assertEqual(command.call_args.args[0][:4], ['npm', 'cache', 'clean', '--force'])
        self.assertFalse(any(p.name == '_npx' for p in measured))

    def test_cache_symlink_is_refused_and_concurrent_entries_wait(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve() / 'cache'
            with patch.object(verify, 'CACHE_ROOT', root):
                with patch.object(verify.fcntl, 'flock', side_effect=[BlockingIOError(), None]) as lock:
                    with verify.locked():
                        pass
                    self.assertEqual(lock.call_args_list[1].args[1], verify.fcntl.LOCK_EX)
                (root / 'go-build').rmdir()
                (root / 'go-build').symlink_to(root / 'go-mod')
                with self.assertRaisesRegex(RuntimeError, 'Symlink cache'):
                    with verify.locked():
                        pass

    def test_low_space_and_active_over_budget_cache_prevent_execution(self):
        for blocked, free, message in [([], 10 * verify.GIB - 1, '10 GiB'), (['go-build'], 100 * verify.GIB, 'over budget')]:
            with patch.object(verify, 'maintain', return_value=blocked), \
                    patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=free)), \
                    patch.object(verify, 'execute') as execute:
                with self.assertRaisesRegex(RuntimeError, message):
                    verify.verify(['python3', '-V'])
                execute.assert_not_called()

    def test_ten_gib_free_allows_execution(self):
        with patch.object(verify, 'maintain', return_value=[]), \
                patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=10 * verify.GIB)), \
                patch.object(verify, 'execute', return_value=0) as execute:
            self.assertEqual(verify.MIN_FREE, 10 * verify.GIB)
            self.assertEqual(verify.verify(['synthetic']), 0)
        execute.assert_called_once()

    def test_failed_command_removes_only_its_temp_directory(self):
        created = []

        def execute(arguments, env):
            path = Path(env['OPENSOHA_VERIFY_DIR'])
            (path / 'output').write_text('disposable')
            created.append(path)
            return 7

        with patch.object(verify, 'maintain', return_value=[]), \
                patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=100 * verify.GIB)), \
                patch.object(verify, 'execute', side_effect=execute):
            self.assertEqual(verify.verify(['synthetic']), 7)
        self.assertFalse(created[0].exists())

    def test_nested_guard_requires_owned_token_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(verify.os.environ, {'OPENSOHA_VERIFY_DIR': directory,
                                               'OPENSOHA_VERIFY_ACTIVE': 'run'}):
                self.assertFalse(verify.nested_run())
                (Path(directory) / '.verification-token').write_text('other')
                self.assertFalse(verify.nested_run())
                (Path(directory) / '.verification-token').write_text('run')
                self.assertTrue(verify.nested_run())

    def test_nested_status_borrows_lock_but_maintenance_is_refused(self):
        with patch.object(verify, 'nested_run', return_value=True), \
                patch.object(verify, 'locked') as lock, patch.object(verify, 'size', return_value=0), \
                patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=100 * verify.GIB)), \
                patch('sys.stdout'), patch('sys.argv', ['local-verify', 'status']):
            self.assertEqual(verify.main(), 0)
            lock.assert_not_called()
            with patch('sys.argv', ['local-verify', 'maintain']):
                with self.assertRaisesRegex(RuntimeError, 'outside an active'):
                    verify.main()

    def test_retained_image_build_still_sweeps_cache_without_removing_tag(self):
        with patch.object(verify, 'maintain', return_value=[]), \
                patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=100 * verify.GIB)), \
                patch.object(verify, 'local_docker', return_value=(['docker'], 'local')), \
                patch.object(verify, 'prune_docker') as prune, \
                patch.object(verify, 'cleanup_image') as remove, \
                patch.object(verify, 'execute', return_value=0) as execute:
            self.assertEqual(verify.verify(['-t', 'deploy:local', '.'], keep_image=True), 0)
        remove.assert_not_called()
        self.assertEqual(prune.call_count, 2)
        self.assertEqual(execute.call_args.args[0], ['docker', 'buildx', 'build', '--builder', 'local', '--load', '-t', 'deploy:local', '.'])

    def test_retained_image_mode_refuses_remote_output(self):
        for argument in ('--push', '--output=type=registry', '--builder=remote', '--context=remote'):
            with self.assertRaises(RuntimeError):
                verify.verify([argument, '.'], keep_image=True)

    def test_original_docker_command_keeps_explicit_outputs_and_disposable_command_adds_receipt(self):
        for disposable in (False, True):
            with patch.object(verify, 'maintain', return_value=[]), \
                    patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=100 * verify.GIB)), \
                    patch.object(verify, 'local_docker', return_value=(['docker'], 'local')), \
                    patch.object(verify, 'prune_docker'), patch.object(verify, 'cleanup_image') as cleanup, \
                    patch.object(verify, 'execute', return_value=0) as execute:
                command = ['/native/docker', '--config', 'build', '--context', 'local', 'build', '.']
                self.assertEqual(verify.verify(command, docker_command=not disposable, docker_image=disposable), 0)
                if disposable:
                    self.assertEqual(execute.call_args.args[0][:6], command[:6])
                    self.assertEqual(execute.call_args.args[0][-1], '.')
                    self.assertIn('--iidfile', execute.call_args.args[0])
                    self.assertIn('--tag', execute.call_args.args[0])
                    cleanup.assert_called_once()
                else:
                    self.assertEqual(execute.call_args.args[0], command)
                    cleanup.assert_not_called()

    def test_explicit_ci_and_release_bypass_before_any_maintenance(self):
        for key in ('CI', 'OPENSOHA_VERIFY_RELEASE'):
            with patch.dict(verify.os.environ, {key: 'true'}), \
                    patch('sys.argv', ['local-verify', 'run', '--', 'go', 'test']), \
                    patch.object(verify.os, 'execvpe', side_effect=SystemExit(0)) as native, \
                    patch.object(verify, 'maintain') as maintain:
                with self.assertRaises(SystemExit):
                    verify.main()
                self.assertEqual(native.call_args.args[:2], ('go', ['go', 'test']))
                maintain.assert_not_called()

    def test_cache_diagnostics_do_not_pollute_stdout(self):
        with patch.object(verify, 'size', side_effect=[9 * verify.GIB, 0, 0, 0, 0]), \
                patch.object(verify, 'busy', return_value=False), patch.object(verify, 'call'), \
                patch('sys.stdout') as stdout:
            verify.maintain()
            stdout.write.assert_not_called()

    def test_maintenance_error_does_not_hide_command_failure(self):
        with patch.object(verify, 'maintain', side_effect=[[], RuntimeError('cleanup failed')]), \
                patch.object(verify.shutil, 'disk_usage', return_value=SimpleNamespace(free=100 * verify.GIB)), \
                patch.object(verify, 'execute', return_value=7):
            self.assertEqual(verify.verify(['synthetic']), 7)

    def test_signal_is_forwarded_to_child_group_and_preserves_status(self):
        child = Mock(pid=43210)
        child.wait.side_effect = [InterruptedError(signal.SIGTERM), 0]
        with patch.object(verify.subprocess, 'Popen', return_value=child), patch.object(verify.os, 'killpg') as kill:
            self.assertEqual(verify.execute(['synthetic'], {}), 143)
        kill.assert_called_once_with(43210, signal.SIGTERM)

    def test_image_cleanup_requires_identity_label_and_no_container(self):
        with tempfile.TemporaryDirectory() as directory:
            iid = Path(directory) / 'image.id'
            iid.write_text('sha256:original')
            cases = [({'Id': 'sha256:other', 'Config': {'Labels': {verify.LABEL: 'run'}}}, False),
                     ({'Id': 'sha256:original', 'Config': {'Labels': {verify.LABEL: 'other'}}}, False),
                     ({'Id': 'sha256:original', 'Config': {'Labels': None}}, False),
                     ({'Id': 'sha256:original', 'Config': {'Labels': {verify.LABEL: 'run'}}}, True)]
            for image, referenced in cases:
                inspect = SimpleNamespace(returncode=0, stdout=json.dumps([image]))
                with patch.object(verify.subprocess, 'run', return_value=inspect), \
                        patch.object(verify, 'call', return_value='container' if referenced else '') as command:
                    with self.assertRaises(RuntimeError):
                        verify.cleanup_image(['docker'], 'owned:test', iid, 'run')
                    self.assertFalse(any(c.args[0][1:3] == ['image', 'rm'] for c in command.call_args_list))

    def test_owned_image_cleanup_removes_tag_without_force(self):
        with tempfile.TemporaryDirectory() as directory:
            iid = Path(directory) / 'image.id'
            iid.write_text('sha256:original')
            image = {'Id': 'sha256:original', 'Config': {'Labels': {verify.LABEL: 'run'}}}
            with patch.object(verify.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps([image]))), \
                    patch.object(verify, 'call', return_value='') as command:
                verify.cleanup_image(['docker'], 'owned:test', iid, 'run')
            self.assertEqual(command.call_args.args[0], ['docker', 'image', 'rm', 'owned:test'])

    def test_image_mode_rejects_export_and_ownership_overrides(self):
        for argument in ['--push', '--output=type=registry', '-tother', '--tag=other', '--label=other', '--cache-to=type=local', '--builder=other']:
            with self.assertRaises(RuntimeError):
                verify.image_arguments([argument, '.'])

    def test_remote_docker_context_is_refused(self):
        with patch.object(verify, 'call', side_effect=['remote', json.dumps([{'Endpoints': {'docker': {'Host': 'ssh://example'}}}])]):
            with self.assertRaisesRegex(RuntimeError, 'remote Docker'):
                verify.local_docker()

    def test_engine_builder_is_resolved_by_driver_and_endpoint(self):
        context = json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///socket'}}}])
        for driver in ['docker', 'docker-container']:
            builder = json.dumps({'Name': 'engine-builder', 'Driver': driver, 'Nodes': [{'Endpoint': 'local'}]})
            with patch.object(verify, 'call', side_effect=['local', context, builder]):
                if driver == 'docker':
                    self.assertEqual(verify.local_docker(), (['docker', '--context', 'local'], 'engine-builder'))
                else:
                    with self.assertRaisesRegex(RuntimeError, 'identify'):
                        verify.local_docker()

    def test_active_buildkit_records_prevent_cache_pruning(self):
        with patch.object(verify, 'busy', return_value=False), \
                patch.object(verify, 'call', return_value=json.dumps({'Reclaimable': False})) as command:
            with self.assertRaisesRegex(RuntimeError, 'records in use'):
                verify.prune_docker(['docker', '--context', 'local'], 'engine-builder')
        self.assertEqual(command.call_count, 1)

    def test_missing_iid_never_silently_leaves_a_generated_image(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(verify.subprocess, 'run', return_value=SimpleNamespace(returncode=0)):
            with self.assertRaisesRegex(RuntimeError, 'no IID receipt'):
                verify.cleanup_image(['docker'], 'owned:test', Path(directory) / 'missing', 'run')


if __name__ == '__main__':
    unittest.main()
