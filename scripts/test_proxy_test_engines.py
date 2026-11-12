import gzip
import hashlib
import json
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

spec = importlib.util.spec_from_file_location('engines', Path(__file__).with_name('prepare-proxy-test-engines.py'))
e = importlib.util.module_from_spec(spec)
spec.loader.exec_module(e)

class Tests(unittest.TestCase):
    def test_formats_and_unsafe_paths(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d)
            with gzip.open(p / 'a.gz', 'wb') as f: f.write(b'binary')
            with tarfile.open(p / 'a.tar.gz', 'w:gz') as f:
                info = tarfile.TarInfo('dir/bin'); info.size = 6
                f.addfile(info, io.BytesIO(b'binary'))
            with zipfile.ZipFile(p / 'a.zip', 'w') as f: f.writestr('bin', b'binary')
            for i, (archive, member) in enumerate([('a.gz', ''), ('a.tar.gz', 'dir/bin'), ('a.zip', 'bin')]):
                e.extract(p / archive, p / str(i), member)
                self.assertEqual((p / str(i)).read_bytes(), b'binary')
            with zipfile.ZipFile(p / 'bad.zip', 'w') as f:
                f.writestr('../escape', 'x'); f.writestr('bin', 'x')
            with self.assertRaises(ValueError): e.extract(p / 'bad.zip', p / 'bad', 'bin')
            with tarfile.open(p / 'link.tar.gz', 'w:gz') as f:
                info = tarfile.TarInfo('link'); info.type = tarfile.SYMTYPE; info.linkname = 'bin'; f.addfile(info)
            with self.assertRaises(ValueError): e.extract(p / 'link.tar.gz', p / 'link', 'bin')

    def test_prepare_and_guards(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d); archive = p / 'input.gz'
            with gzip.open(archive, 'wb') as f: f.write(b'binary')
            data = archive.read_bytes()
            metadata = {'engine': {'version': '1', 'platforms': {'linux-amd64': {'url': 'https://example.test/input.gz', 'sha256': e.digest(archive), 'binary_sha256': hashlib.sha256(b'binary').hexdigest(), 'member': ''}}}}
            class Response(io.BytesIO):
                def geturl(self): return 'https://example.test/input.gz'
            with mock.patch.object(e.urllib.request, 'urlopen', side_effect=lambda *a, **k: Response(data)):
                result = e.prepare(p / 'out', 'linux-amd64', metadata)
                self.assertEqual(result, e.prepare(p / 'out', 'linux-amd64', metadata))
                (p / 'out' / 'engine').write_bytes(b'changed')
                with self.assertRaises(ValueError): e.prepare(p / 'out', 'linux-amd64', metadata)
                manifest_path = p / 'out' / 'manifest.json'
                manifest = json.loads(manifest_path.read_text())
                manifest['engines']['engine']['binary_sha256'] = e.digest(p / 'out' / 'engine')
                manifest_path.write_text(json.dumps(manifest))
                with self.assertRaises(ValueError): e.prepare(p / 'out', 'linux-amd64', metadata)
                with self.assertRaises(ValueError): e.prepare(p / 'other', 'bad', metadata)
                (p / 'unknown').mkdir()
                with self.assertRaises(ValueError): e.prepare(p / 'unknown', 'linux-amd64', metadata)
                metadata['engine']['platforms']['linux-amd64']['binary_sha256'] = '0' * 64
                with self.assertRaises(ValueError): e.prepare(p / 'binary-hash', 'linux-amd64', metadata)
                self.assertFalse((p / 'binary-hash').exists())
                metadata['engine']['platforms']['linux-amd64']['sha256'] = '0' * 64
                with self.assertRaises(ValueError): e.prepare(p / 'hash', 'linux-amd64', metadata)
                self.assertFalse((p / 'hash').exists())
            with mock.patch.object(e, 'LIMIT', 2):
                with self.assertRaises(ValueError): e.copy_bounded(io.BytesIO(b'123'), io.BytesIO())

if __name__ == '__main__': unittest.main()
