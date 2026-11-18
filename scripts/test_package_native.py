import hashlib
import importlib.util
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("package_native", Path(__file__).with_name("package-native.py"))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


class NativePackageTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.base = Path(directory.name)
        self.root = self.base / "source"
        for name, content in {
            "server": b"test executable",
            "LICENSE": b"source license",
            "migrations/postgres/0001_init.sql": b"CREATE TABLE example(id int);",
            "migrations/postgres/0002_next.sql": b"ALTER TABLE example ADD COLUMN name text;",
            "configs/config.example.yaml": b"public defaults",
            "configs/config.yaml": b"private credentials must not ship",
            "deploy/native/README.md": b"installation instructions",
            "data/private.txt": b"retain original data",
        }.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        self.binary = self.root / "server"
        self.output = self.base / "soha-linux-amd64.tar.gz"
        self.enterContext(patch.object(native, "ROOT", self.root))

    def test_complete_reproducible_bundle_excludes_private_files(self):
        native.package(self.binary, self.output)
        with tarfile.open(self.output) as archive:
            self.assertEqual(set(archive.getnames()), {
                "soha/soha", "soha/LICENSE", "soha/configs/config.example.yaml", "soha/README.md",
                "soha/migrations/postgres/0001_init.sql", "soha/migrations/postgres/0002_next.sql",
            })
            self.assertEqual(archive.getmember("soha/soha").mode, 0o755)
            for member in archive.getmembers():
                source = self.binary if member.name == "soha/soha" else self.root / (
                    "deploy/native/README.md" if member.name == "soha/README.md" else member.name.removeprefix("soha/"))
                self.assertEqual(archive.extractfile(member).read(), source.read_bytes())
                self.assertTrue(member.isfile())
        checksum = self.output.with_name(self.output.name + ".sha256").read_text()
        self.assertEqual(checksum, f"{hashlib.sha256(self.output.read_bytes()).hexdigest()}  {self.output.name}\n")
        other = self.base / "second.tar.gz"
        native.package(self.binary, other)
        self.assertEqual(self.output.read_bytes(), other.read_bytes())
        self.assertEqual((self.root / "data/private.txt").read_bytes(), b"retain original data")

    def test_missing_required_inputs_fail_without_artifacts(self):
        for name in ["server", "configs/config.example.yaml", "migrations/postgres/0001_init.sql"]:
            with self.subTest(name=name):
                path = self.root / name
                content = path.read_bytes()
                path.unlink()
                with self.assertRaises(ValueError):
                    native.package(self.binary, self.output)
                self.assertFalse(self.output.exists())
                path.write_bytes(content)

    def test_rejects_symlink_input_and_preserves_existing_outputs(self):
        example = self.root / "configs/config.example.yaml"
        example.unlink()
        example.symlink_to(self.root / "configs/config.yaml")
        with self.assertRaisesRegex(ValueError, "symlink"):
            native.package(self.binary, self.output)
        example.unlink()
        example.write_bytes(b"public")
        for path in [self.output, self.output.with_name(self.output.name + ".sha256")]:
            path.write_bytes(b"existing release")
            with self.assertRaisesRegex(ValueError, "overwrite"):
                native.package(self.binary, self.output)
            self.assertEqual(path.read_bytes(), b"existing release")
            path.unlink()


if __name__ == "__main__":
    unittest.main()
