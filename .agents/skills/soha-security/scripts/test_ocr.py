import copy
import json
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('ocr_adapter', Path(__file__).with_name('ocr.py'))
ocr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ocr)


def fixture():
    preview = {'files': [{'path': 'src/a.ts', 'will_review': True}, {'path': 'gen/a.ts', 'will_review': False, 'exclude_reason': 'generated'}]}
    item = {'item_id': 'one', 'path': 'src/a.ts'}
    result = {'status': 'complete', 'tool_calls': 1, 'comments': [], 'manifest': {'schema_version': 'ocr.run-manifest/v1', 'terminal_state': 'complete', 'execution': {'ocr_version': ocr.VERSION}, 'coverage': {'selected': [item], 'completed': [item], 'reused': [], 'failed': [], 'waived': []}}}
    return preview, result


class EvidenceTests(unittest.TestCase):
    def test_broker_rejects_paths_and_credentials_without_exposing_canary(self):
        import urllib.error
        import urllib.request
        env = {'SOHA_OCR_ENABLED': '1', 'SOHA_OCR_APPROVAL_ID': 'synthetic', 'SOHA_OCR_ALLOWED_HOST': 'approved.invalid', 'OCR_LLM_URL': 'https://approved.invalid/v1', 'OCR_LLM_TOKEN': 'synthetic-key-canary', 'OCR_LLM_MODEL': 'synthetic'}
        server, capability, state = ocr.broker(env)
        try:
            url = f'http://127.0.0.1:{server.server_port}'
            for path, key in (('/v1/chat/completions', 'wrong'), ('/execute', capability)):
                with self.assertRaises(urllib.error.HTTPError) as error:
                    urllib.request.urlopen(urllib.request.Request(url + path, data=b'{}', headers={'Authorization': 'Bearer ' + key}), timeout=2)
                self.assertEqual(error.exception.code, 403)
                error.exception.close()
            self.assertNotIn('synthetic-key-canary', json.dumps(state))
            self.assertEqual(state['calls'], 0)
            self.assertEqual(state['rejected'], 2)
        finally: server.shutdown(); server.server_close()
    def test_cleanup_failure_is_visible_and_foreign_container_is_preserved(self):
        from unittest.mock import patch
        import subprocess
        command = ocr.container('sha256:' + 'a' * 64)
        completed = subprocess.CompletedProcess(command, 0)
        with patch.object(ocr.subprocess, 'run', side_effect=[completed, subprocess.CompletedProcess([], 1, '', 'daemon unavailable')]) as calls:
            with self.assertRaisesRegex(ValueError, 'cleanup state'): ocr.run_container(command)
            self.assertEqual(calls.call_count, 2)
        foreign = {'Id': 'unrelated', 'Config': {'Labels': {'soha.test.run': 'other-run'}}}
        with patch.object(ocr.subprocess, 'run', side_effect=[completed, subprocess.CompletedProcess([], 0, json.dumps([foreign]), '')]) as calls:
            with self.assertRaisesRegex(ValueError, 'ownership mismatch'): ocr.run_container(command)
            self.assertEqual(calls.call_count, 2)
        with patch.object(ocr.subprocess, 'run', side_effect=[subprocess.TimeoutExpired(command, 1), subprocess.CompletedProcess([], 1, '', 'error: no such object: synthetic')]):
            with self.assertRaises(subprocess.TimeoutExpired): ocr.run_container(command)

    def test_model_and_ci_authorization(self):
        with self.assertRaises(ValueError): ocr.approved_model({})
        env = {'SOHA_OCR_ENABLED': '1', 'SOHA_OCR_APPROVAL_ID': 'synthetic', 'SOHA_OCR_ALLOWED_HOST': 'approved.invalid', 'OCR_LLM_URL': 'https://approved.invalid/v1', 'OCR_LLM_TOKEN': 'synthetic-canary', 'OCR_LLM_MODEL': 'synthetic-model'}
        self.assertEqual(ocr.approved_model(env).hostname, 'approved.invalid')
        for url in ('https://elsewhere.invalid', 'http://approved.invalid', 'https://canary@approved.invalid', 'https://approved.invalid/?secret=canary'):
            with self.assertRaises(ValueError): ocr.approved_model({**env, 'OCR_LLM_URL': url})
        sha = 'a' * 40
        ci = {'GITHUB_REF': 'refs/heads/main', 'GITHUB_SHA': sha, 'SOHA_OCR_APPROVED_CODE_SHA': sha, 'SOHA_OCR_APPROVED_BASE': sha, 'SOHA_OCR_APPROVED_HEAD': sha}
        ocr.trusted_dispatch(ci, sha, sha)
        for values in ({**ci, 'GITHUB_REF': 'refs/pull/1/merge'}, {**ci, 'GITHUB_SHA': 'b' * 40}, {}):
            with self.assertRaises(ValueError): ocr.trusted_dispatch(values, sha, sha)

    def test_complete_and_deduplicate(self):
        preview, result = fixture()
        self.assertEqual(ocr.normalize(preview, result)['findingStatus'], 'NO_FINDINGS')
        comment = {'path': 'src/a.ts', 'start_line': 1, 'end_line': 2, 'content': 'Synthetic defect: missing exact UID check'}
        result['comments'] = [comment, comment]
        value = ocr.normalize(preview, result, line_counts={'src/a.ts': 2})
        self.assertEqual(len(value['findings']), 1)
        self.assertEqual(value['findingStatus'], 'NEEDS_TRIAGE')
        with self.assertRaises(ValueError): ocr.normalize(preview, result, expected_range=('a' * 40, 'b' * 40))
        result['manifest']['input'] = {'resolved_base': 'a' * 40, 'resolved_head': 'b' * 40}
        self.assertEqual(ocr.normalize(preview, result, line_counts={'src/a.ts': 2}, expected_range=('a' * 40, 'b' * 40))['coverageStatus'], 'COMPLETE')

    def test_partial_budget_failure_missing_and_status(self):
        preview, result = fixture()
        mutations = [lambda r: r.update(status='unknown'), lambda r: r['manifest'].update(terminal_state='failed'), lambda r: r.pop('manifest'), lambda r: r['manifest']['execution'].update(ocr_version='latest'), lambda r: r.pop('tool_calls')]
        for mutate in mutations:
            bad = copy.deepcopy(result); mutate(bad)
            with self.assertRaises(ValueError): ocr.normalize(preview, bad)
        result['manifest']['coverage']['completed'] = []
        result['manifest']['coverage']['failed'] = result['manifest']['coverage']['selected']
        result.update(status='partial', summary={'budget_exceeded': True})
        result['manifest']['terminal_state'] = 'partial'
        self.assertEqual(ocr.normalize(preview, result)['coverageStatus'], 'PARTIAL')
        self.assertEqual(ocr.normalize(preview, result)['findingStatus'], 'UNKNOWN')
        self.assertEqual(ocr.normalize(preview, result, exit_code=1)['coverageStatus'], 'ERROR')

    def test_scope_lines_and_overlap(self):
        preview, result = fixture()
        for path, start, end in [('../secret', 0, 0), ('src/b.ts', 1, 1), ('src/a.ts', 0, 1), ('src/a.ts', 1, 99), ('src/a.ts', True, 2)]:
            result['comments'] = [{'path': path, 'start_line': start, 'end_line': end, 'content': 'synthetic'}]
            with self.assertRaises(ValueError): ocr.normalize(preview, result, line_counts={'src/a.ts': 2})
        result['comments'] = []
        result['manifest']['coverage']['reused'] = result['manifest']['coverage']['completed']
        with self.assertRaises(ValueError): ocr.normalize(preview, result)

    def test_malformed_preview_and_duplicate_item_ids(self):
        preview, result = fixture()
        for files in ([{'path': 'src/a.ts'}], [{'path': 'src/a.ts', 'will_review': 'true'}], [preview['files'][0], preview['files'][0]]):
            with self.assertRaises(ValueError): ocr.normalize({'files': files}, result)
        preview['files'].append({'path': 'src/b.ts', 'will_review': True})
        duplicate = {'item_id': 'one', 'path': 'src/b.ts'}
        for kind in ('selected', 'completed'): result['manifest']['coverage'][kind].append(duplicate)
        with self.assertRaises(ValueError): ocr.normalize(preview, result)
        with tempfile.TemporaryDirectory() as directory:
            import subprocess
            root = Path(directory)
            (root / 'preview.json').write_text(json.dumps(fixture()[0]))
            (root / 'result.json').write_text('{corrupt')
            (root / 'lines.json').write_text('{}')
            process = subprocess.run(['python3', str(Path(__file__).with_name('ocr.py')), 'normalize', str(root / 'preview.json'), str(root / 'result.json'), '--exit-code', '0', '--line-counts', str(root / 'lines.json'), '--base', 'a' * 40, '--head', 'b' * 40], capture_output=True, text=True)
            self.assertNotEqual(process.returncode, 0)
            self.assertNotIn('NO_FINDINGS', process.stdout)

    def test_zero_scope_and_missing_completion(self):
        preview, result = fixture()
        preview['files'][0]['will_review'] = False
        with self.assertRaises(ValueError): ocr.normalize(preview, result)
        result['manifest']['coverage'] = {kind: [] for kind in ('selected', 'completed', 'reused', 'failed', 'waived')}
        result['status'] = result['manifest']['terminal_state'] = 'skipped'
        self.assertEqual(ocr.normalize(preview, result)['coverageStatus'], 'NOT_APPLICABLE')
        preview, result = fixture()
        result['manifest']['coverage']['completed'] = []
        self.assertEqual(ocr.normalize(preview, result)['coverageStatus'], 'PARTIAL')

    def test_ref_injection_and_wrong_git_root(self):
        with tempfile.TemporaryDirectory() as directory:
            import subprocess
            subprocess.run(['git', 'init', '-q', directory], check=True)
            with self.assertRaises(ValueError): ocr.resolve_range(directory, 'main;touch /tmp/never', 'f' * 40)
            nested = Path(directory) / 'nested'; nested.mkdir()
            with self.assertRaises(ValueError): ocr.resolve_range(nested, 'a' * 40, 'b' * 40)


if __name__ == '__main__': unittest.main()
