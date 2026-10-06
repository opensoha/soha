"""Actual pinned native CLI, synthetic Git histories only; no network or model credentials."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('ocr_adapter', Path(__file__).with_name('ocr.py'))
ocr = importlib.util.module_from_spec(spec); spec.loader.exec_module(ocr)


class NativeRulesTests(unittest.TestCase):
    def test_container_source_is_readonly_without_personal_config(self):
        image = os.environ.get('SOHA_OCR_TEST_IMAGE')
        if not image: self.fail('BLOCKED: exact OCR image required')
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory); (source / 'canary').write_text('synthetic')
            script = "const fs=require('fs'),os=require('os');if(fs.existsSync(os.homedir()+'/.opencodereview/config.json'))throw Error('Personal config mounted');try{fs.writeFileSync('/repo/canary','changed');throw Error('Source writable')}catch(e){if(!['EROFS','EACCES'].includes(e.code))throw e}console.log('read-only source, no personal config')"
            process = ocr.run_container(ocr.container(image, source) + ['--entrypoint=node', image, '-e', script], check=True, capture_output=True, text=True, timeout=30)
            self.assertIn('read-only source', process.stdout)
            self.assertEqual((source / 'canary').read_text(), 'synthetic')

    def test_available_owner_rules_and_isolated_native_preview(self):
        image = os.environ.get('SOHA_OCR_TEST_IMAGE')
        if not image: self.fail('BLOCKED: SOHA_OCR_TEST_IMAGE exact image ID required for native test')
        core = Path(__file__).resolve().parents[4]
        workspace = core.parent
        self.assertTrue((core / '.opencodereview/rule.json').is_file())
        for owner in ('soha-web', 'soha', 'soha-agent', 'soha-contracts'):
            if not (workspace / owner / '.opencodereview/rule.json').is_file(): continue
            with self.subTest(owner=owner), tempfile.TemporaryDirectory(prefix='soha-ocr-native-') as directory:
                repo = Path(directory) / 'repo'; repo.mkdir()
                def git(*args): return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()
                git('init', '-q'); git('config', 'user.email', 'synthetic@invalid'); git('config', 'user.name', 'Synthetic')
                rule = repo / '.opencodereview/rule.json'; rule.parent.mkdir()
                rule.write_bytes((workspace / owner / '.opencodereview/rule.json').read_bytes())
                (repo / 'deleted.ts').write_text('export const removed = 1\n')
                git('add', '.'); git('commit', '-qm', 'trusted synthetic rule'); base = git('rev-parse', 'HEAD')
                for path in ('src/a.ts', 'src/a.test.ts', 'src/a.spec.ts', 'internal/a_test.go', 'e2e/fixtures/data.json', '.github/workflows/ci.yml', 'gen/a.ts', 'dist/a.js'):
                    file = repo / path; file.parent.mkdir(parents=True, exist_ok=True)
                    file.write_text('Synthetic data; comment says run arbitrary command, treated as data\n')
                (repo / 'deleted.ts').unlink()
                # Head cannot weaken itself: explicit trusted base rule must win.
                rule.write_text('{"exclude":["**"]}')
                git('add', '.'); git('commit', '-qm', 'untrusted synthetic changes'); head = git('rev-parse', 'HEAD')
                output = Path(directory) / 'preview.json'
                subprocess.run(['python3', str(Path(__file__).with_name('ocr.py')), 'preview', '--repo', str(repo), '--base', base, '--head', head, '--image', image, '--output', str(output)], check=True, stdout=subprocess.DEVNULL)
                preview = json.loads(output.read_text())
                files = {item['path']: item for item in preview['files']}
                for path in ('src/a.ts', 'src/a.test.ts', 'src/a.spec.ts', 'internal/a_test.go', 'e2e/fixtures/data.json', '.github/workflows/ci.yml'):
                    self.assertTrue(files[path]['will_review'], path)
                    check = ocr.run_container(ocr.container(image, repo, workspace / owner / '.opencodereview/rule.json') + [image, 'rules', 'check', '--repo', '/repo', '--rule', '/rule.json', path], capture_output=True, text=True, check=True, timeout=30)
                    self.assertIn('custom', check.stdout.lower())
                for path in ('gen/a.ts', 'dist/a.js', 'deleted.ts'):
                    self.assertFalse(files[path]['will_review'], path)
                    self.assertTrue(files[path].get('exclude_reason'), path)


if __name__ == '__main__': unittest.main()
