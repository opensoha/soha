"""Small offline checks for reproducible native release retries."""
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("native_package", Path(__file__).with_name("package-native-release.py"))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class NativeReleaseRecoveryTest(unittest.TestCase):
    def test_commit_date_is_utc_reproducible_and_invalid_sha_fails(self):
        sha = subprocess.check_output(["git", "-C", str(ROOT), "rev-parse", "HEAD"], text=True).strip()
        epoch = int(subprocess.check_output(["git", "-C", str(ROOT), "show", "-s", "--format=%ct", sha], text=True))
        expected = datetime.fromtimestamp(epoch, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        for component in ("cli", "agent"):
            workflow = (ROOT / f".github/workflows/{component}-release.yml").read_text()
            lines = [line.strip() for line in workflow.splitlines() if line.strip().startswith(("source_epoch=", "source_date=", "printf 'epoch="))]
            self.assertEqual(len(lines), 3)
            script = "set -euo pipefail\n" + "\n".join(lines)
            for tz in ("Pacific/Honolulu", "Asia/Shanghai"):
                with tempfile.TemporaryDirectory() as directory:
                    output = Path(directory) / "output"
                    env = {**os.environ, "TZ": tz, "RELEASE_SHA": sha, "GITHUB_OUTPUT": str(output)}
                    subprocess.run(["bash", "-c", script], env=env, cwd=ROOT, check=True)
                    self.assertEqual(output.read_text(), f"epoch={epoch}\ndate={expected}\n")
                    env["RELEASE_SHA"] = "0" * 40
                    failed = subprocess.run(["bash", "-c", script], env=env, cwd=ROOT, capture_output=True)
                    self.assertNotEqual(failed.returncode, 0)
            self.assertNotIn("date -u", workflow)
            self.assertNotIn("github.event.head_commit.timestamp", workflow)
            self.assertIn("DATE=${{ needs.verify.outputs.date }}", workflow)

    def test_component_tags_remain_disjoint(self):
        for component in ("cli", "agent"):
            command = ["python3", str(ROOT / "scripts/release-control.py"), "identity", "--component", component, "--sha", "a" * 40, "--tag"]
            good = subprocess.check_output([*command, f"{component}/v0.2.0"], text=True)
            self.assertEqual(json.loads(good)["version"], "0.2.0")
            for invalid in ("v0.2.0", "contracts/v0.2.0", f"{component}/v0.2.0-rc.1"):
                self.assertNotEqual(subprocess.run([*command, invalid], capture_output=True).returncode, 0)

    def test_tar_and_zip_retries_ignore_build_file_metadata(self):
        epoch = 1794614401
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for suffix, name in ((".tar.gz", "soha-agent"), (".zip", "soha.exe")):
                binary = root / name
                binary.write_bytes(b"native fixture\x00\xff")
                first, second = root / ("first" + suffix), root / ("second" + suffix)
                os.utime(binary, (epoch, epoch))
                binary.chmod(0o755)
                packager.package(binary, first, epoch)
                os.utime(binary, (epoch + 86400, epoch + 86400))
                binary.chmod(0o600)
                packager.package(binary, second, epoch)
                self.assertEqual(hashlib.sha256(first.read_bytes()).hexdigest(), hashlib.sha256(second.read_bytes()).hexdigest())
                if suffix == ".tar.gz":
                    self.assertEqual(first.read_bytes()[4:8], bytes(4))
                    with tarfile.open(first) as archive:
                        self.assertEqual(archive.getnames(), [name])
                        member = archive.getmember(name)
                        self.assertEqual((member.uid, member.gid, member.mode, member.mtime), (0, 0, 0o755, epoch))
                        self.assertEqual(archive.extractfile(member).read(), binary.read_bytes())
                else:
                    with zipfile.ZipFile(first) as archive:
                        self.assertEqual(archive.namelist(), [name])
                        self.assertEqual(archive.read(name), binary.read_bytes())
                        self.assertEqual(archive.getinfo(name).external_attr >> 16 & 0o777, 0o755)

    def test_packaging_rejects_invalid_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "soha"
            binary.write_bytes(b"fixture")
            link = root / "link"
            link.symlink_to(binary)
            for source, destination, epoch in ((link, root / "a.tar.gz", 1), (binary, root / "a.tar.gz", -1), (binary, root / "a.txt", 1), (binary, link, 1)):
                with self.assertRaises(ValueError):
                    packager.package(source, destination, epoch)
                if destination != link:
                    self.assertFalse(destination.exists())
                self.assertEqual(binary.read_bytes(), b"fixture")

    def test_workflows_use_only_shared_non_overwriting_publisher(self):
        for component, directory in (("cli", "release"), ("agent", "artifacts")):
            workflow = (ROOT / f".github/workflows/{component}-release.yml").read_text()
            self.assertIn(f'publish-release-assets.py --component {component} --tag "$RELEASE_TAG" --sha "$RELEASE_SHA" --directory {directory}', workflow)
            self.assertNotIn("gh release upload", workflow)
            self.assertNotIn("softprops/action-gh-release@", workflow)
            self.assertIn(f"RELEASE_{component.upper()}_ENABLED: ${{{{ vars.RELEASE_{component.upper()}_ENABLED }}}}", workflow)
            self.assertIn("actions: read", workflow)
            self.assertIn("package-native-release.py", workflow)


if __name__ == "__main__":
    unittest.main()
