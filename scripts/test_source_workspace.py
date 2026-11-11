import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("source_workspace", Path(__file__).with_name("source-workspace.py"))
workspace = importlib.util.module_from_spec(spec)
spec.loader.exec_module(workspace)


class WebStageTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="soha-stage-test-")
        self.addCleanup(self.directory.cleanup)
        self.base = Path(self.directory.name).resolve()
        self.root = self.base / "checkout"
        self.root.mkdir()
        self.source = self.root / "web/dist"
        self.destination = self.root / "internal/staticassets/web/dist"
        self.source.mkdir(parents=True)
        self.destination.mkdir(parents=True)
        (self.source / "index.html").write_text("same-source web")
        (self.source / ".vite").mkdir()
        (self.source / ".vite/manifest.json").write_text('{"index.html":{}}')
        (self.destination / "stale.txt").write_text("old generated output")
        self.protected = self.base / "protected.txt"
        self.protected.write_text("keep")
        for patcher in [patch.object(workspace, "ROOT", self.root),
                        patch.object(workspace, "check_npm", return_value=None),
                        patch.object(workspace, "run", return_value="a" * 40)]:
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_stages_all_bytes_and_removes_only_stale_generated_output(self):
        workspace.stage_web()
        self.assertFalse((self.destination / "stale.txt").exists())
        for relative in ["index.html", ".vite/manifest.json"]:
            self.assertEqual((self.source / relative).read_bytes(), (self.destination / relative).read_bytes())
        self.assertEqual(self.protected.read_text(), "keep")
        self.assertTrue((self.root / ".tmp/embedded-web.json").is_file())

    def test_rejects_source_symlink_before_touching_previous_artifact(self):
        (self.source / "external").symlink_to(self.protected)
        with self.assertRaisesRegex(ValueError, "symlinks"):
            workspace.stage_web()
        self.assertTrue((self.destination / "stale.txt").is_file())
        self.assertEqual(self.protected.read_text(), "keep")

    def test_rejects_output_redirection_without_touching_target(self):
        (self.destination / "stale.txt").unlink()
        self.destination.rmdir()
        self.destination.symlink_to(self.root / "web")
        with self.assertRaisesRegex(ValueError, "inside this checkout"):
            workspace.stage_web()
        self.assertEqual((self.source / "index.html").read_text(), "same-source web")

    def test_missing_entrypoint_preserves_previous_artifact(self):
        (self.source / "index.html").unlink()
        with self.assertRaisesRegex(ValueError, "index.html"):
            workspace.stage_web()
        self.assertTrue((self.destination / "stale.txt").exists())

    def test_receipt_directory_symlink_preserves_artifact_and_external_file(self):
        outside = self.base / "outside"
        outside.mkdir()
        protected = outside / "embedded-web.json"
        protected.write_text("keep")
        (self.root / ".tmp").symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "receipt"):
            workspace.stage_web()
        self.assertEqual(protected.read_text(), "keep")
        self.assertTrue((self.destination / "stale.txt").exists())

    def test_receipt_file_symlink_preserves_artifact_and_external_file(self):
        (self.root / ".tmp").mkdir()
        (self.root / ".tmp/embedded-web.json").symlink_to(self.protected)
        with self.assertRaisesRegex(ValueError, "receipt"):
            workspace.stage_web()
        self.assertEqual(self.protected.read_text(), "keep")
        self.assertTrue((self.destination / "stale.txt").exists())

    def test_atomic_receipt_replacement_preserves_other_hardlink(self):
        (self.root / ".tmp").mkdir()
        (self.root / ".tmp/embedded-web.json").hardlink_to(self.protected)
        workspace.stage_web()
        self.assertEqual(self.protected.read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
