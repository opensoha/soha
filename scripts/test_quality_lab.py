import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('lab', Path(__file__).with_name('quality-lab.py'))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


class QualityLabGuards(unittest.TestCase):
    def test_cleanup_can_resume_after_remote_failure_and_rejects_replaced_resources(self):
        state = {'runId': 'run', 'name': 'lab', 'coreId': 'core', 'pgId': 'pg', 'k3sId': 'cluster',
                 'coreImage': 'core-image', 'k3sImage': 'cluster-image'}
        present = {'core', 'pg', 'cluster', 'network', 'volume'}
        volume_created = 'original-date'
        fail_remote = True

        def command(*args, **kwargs):
            nonlocal fail_remote
            if args[:2] == ('ssh', 'middleware'):
                value = args[2]
                if value.startswith('docker ps -aq --filter id='):
                    return 'cluster' if 'cluster' in present else ''
                if value.startswith('docker volume ls'):
                    return 'lab-k3s' if 'volume' in present else ''
                if value.startswith('docker volume inspect'):
                    return json.dumps([{'Name': 'lab-k3s', 'CreatedAt': volume_created}])
                if value.startswith('docker ps -aq --filter volume='):
                    return 'cluster' if 'cluster' in present else ''
                if value.startswith('docker stop') and fail_remote:
                    fail_remote = False
                    raise RuntimeError('synthetic SSH failure after local deletion')
                if value.startswith('docker rm'):
                    present.discard('cluster')
                if value.startswith('docker volume rm'):
                    present.discard('volume')
                return ''
            if args[:2] == ('docker', 'ps'):
                identity = args[4].split('=')[1]
                return identity if identity in present else ''
            if args[:3] == ('docker', 'network', 'ls'):
                return 'network' if 'network' in present else ''
            if args[:3] == ('docker', 'network', 'inspect'):
                return json.dumps([{'Id': 'network', 'Labels': {'soha.test.run': 'run'}, 'Containers': {'core': {}, 'pg': {}}}])
            if args[:2] == ('docker', 'rm'):
                present.difference_update({'core', 'pg'})
            if args[:3] == ('docker', 'network', 'rm'):
                present.discard('network')
            return ''

        def inspect(value, key, remote=False):
            return {'Id': value[key], 'Image': value.get(key.replace('Id', 'Image')),
                    'Mounts': [{'Name': 'lab-k3s', 'Destination': '/var/lib/rancher/k3s'}]}

        with patch.object(lab, 'run', side_effect=command), patch.object(lab, 'inspect', side_effect=inspect), patch.object(lab, 'save'):
            with self.assertRaisesRegex(RuntimeError, 'synthetic SSH failure'):
                lab.stop(state)
            self.assertEqual(present, {'cluster', 'volume'})
            volume_created = 'replacement-date'
            with self.assertRaisesRegex(RuntimeError, 'volume identity mismatch'):
                lab.stop(state)
            self.assertEqual(present, {'cluster', 'volume'})
            volume_created = 'original-date'
            lab.stop(state)
            self.assertEqual(present, set())
            self.assertTrue(state['stopped'])

    def test_authenticated_api_refuses_redirect_without_forwarding_token(self):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                self.send_response(302)
                self.send_header('Location', '/sink')
                self.end_headers()

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory, patch.object(lab, 'LAB', Path(directory)):
                with self.assertRaisesRegex(RuntimeError, 'failed \\(302\\)'):
                    lab.api({'baseURL': f'http://127.0.0.1:{server.server_port}'}, 'GET', '/probe', token='synthetic-canary')
            self.assertEqual(requests, ['/api/v1/probe'])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_zero_exit_cannot_hide_missing_stale_skipped_or_wrong_target_results(self):
        state = {'runId': 'run', 'coreId': 'core', 'coreImage': 'image', 'sources': {},
                 'accounts': {'direct-readonly': {'username': 'test'}}, 'secrets': {'readonly': 'synthetic'}}
        good = {'result': 'PASS', 'runId': 'run', 'mode': 'e2e-direct', 'sources': {},
                'environment': {'disposable': True, 'containerId': 'core', 'imageDigest': 'image'},
                'tests': [{'result': 'passed'}] * 4}
        cases = [None, {'result': 'FAIL'}, {**good, 'tests': []},
                 {**good, 'tests': [{'result': 'skipped'}] * 4}, {**good, 'runId': 'other'}]
        with tempfile.TemporaryDirectory() as directory, patch.object(lab, 'ROOT', Path(directory)), patch.object(lab, 'LAB', Path(directory) / 'private'):
            output = Path(directory) / 'soha-web/test-results/run-manifest.json'
            output.parent.mkdir(parents=True)
            for manifest in cases:
                output.write_text(json.dumps(good))  # stale PASS must be discarded
                def execute(*args, **kwargs):
                    if manifest is not None:
                        output.write_text(json.dumps(manifest))
                    return SimpleNamespace(returncode=0)
                with patch.object(lab.subprocess, 'run', side_effect=execute), self.assertRaises(RuntimeError):
                    lab.test(state, 'direct', 'readonly', False)

    def test_private_runtime_files_never_enter_build_snapshot_even_if_unignored(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / 'repo'
            (repo / '.tmp/quality-lab').mkdir(parents=True)
            (repo / '.tmp/quality-lab/private.json').write_text('private-canary')
            (repo / 'main.go').write_text('package main')
            with patch.object(lab, 'run', side_effect=['.tmp/quality-lab/private.json\0main.go\0', 'a' * 40]):
                lab.snapshot(repo, root / 'build')
            self.assertTrue((root / 'build/main.go').exists())
            self.assertFalse((root / 'build/.tmp').exists())

    def test_sequential_stage_saves_retain_independent_resource_identity(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(lab, 'LAB', Path(directory)), patch.object(lab, 'STATE', Path(directory) / 'private.json'):
            lab.save({'runId': 'run', 'coreId': 'core'})
            lab.save({'runId': 'run', 'k3sId': 'cluster'})
            self.assertEqual(lab.state(), {'runId': 'run', 'coreId': 'core', 'k3sId': 'cluster'})
            self.assertEqual(lab.STATE.stat().st_mode & 0o777, 0o600)

    def test_replacement_rejects_any_other_run_id_or_uid_before_kubernetes_access(self):
        state = {'runId': 'run', 'leases': {'direct': {'id': 'owned', 'uid': 'original'}}}
        with patch.object(lab, 'kube_api') as kube:
            for arguments in [('other', 'owned', 'original'), ('run', 'other', 'original'), ('run', 'owned', 'other')]:
                with self.assertRaisesRegex(RuntimeError, 'refusing replacement'):
                    lab.recreate(state, 'direct', *arguments)
            kube.assert_not_called()

    def test_container_identity_and_run_label_are_both_required(self):
        import json
        state = {'runId': 'run', 'coreId': 'original'}
        for item in [{'Id': 'other', 'Config': {'Labels': {'soha.test.run': 'run'}}},
                     {'Id': 'original', 'Config': {'Labels': {'soha.test.run': 'other'}}}]:
            with patch.object(lab, 'run', return_value=json.dumps([item])), self.assertRaisesRegex(RuntimeError, 'Ownership mismatch'):
                lab.inspect(state, 'coreId')


if __name__ == '__main__':
    unittest.main()
