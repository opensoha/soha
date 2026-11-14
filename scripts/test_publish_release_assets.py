import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("publisher", Path(__file__).with_name("publish-release-assets.py"))
publisher = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(publisher)


class ImmutableAssetsTest(unittest.TestCase):
    def test_new_release_is_published_only_after_verified_upload(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "archive.tgz"; file.write_bytes(b"accepted")
            release = {"component": "contracts", "repository": "opensoha/soha", "tag": "contracts/v0.2.0", "sha": "a" * 40}
            calls = []
            def gh(*args):
                calls.append(args)
                if args[:2] == ("release", "download"):
                    Path(args[-1]).write_bytes(b"accepted")
            with patch.object(publisher.control, "verify") as guard, patch.object(publisher, "gh", side_effect=gh), patch.object(publisher, "release_metadata", side_effect=[None, {"assets": [{"name": file.name}]}]):
                publisher.publish(release, [file], "SDK")
            self.assertEqual([args[1] for args in calls], ["create", "upload", "download", "edit"])
            self.assertIn("--draft", calls[0])
            self.assertIn("--latest=false", calls[0])
            self.assertNotIn("--clobber", calls[1])
            self.assertIn("--draft=false", calls[-1])
            self.assertIn("--prerelease=false", calls[-1])
            self.assertEqual(guard.call_count, 3)

    def test_existing_identical_assets_are_skipped_and_missing_are_selected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first, second = root / "first", root / "second"
            first.write_bytes(b"first"); second.write_bytes(b"second")
            missing = publisher.compare_existing([first, second], [{"name": "first"}], lambda name, out: out.write_bytes(b"first"))
            self.assertEqual(missing, [second])

    def test_any_conflict_blocks_before_create_or_upload(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "archive.tgz"
            file.write_bytes(b"accepted")
            release = {"component": "contracts", "repository": "opensoha/soha", "tag": "contracts/v0.2.0", "sha": "a" * 40}
            writes = []
            def gh(*args):
                if args[:2] == ("release", "download"):
                    Path(args[-1]).write_bytes(b"different")
                else:
                    writes.append(args)
            with patch.object(publisher.control, "verify"), patch.object(publisher, "gh", side_effect=gh), patch.object(publisher, "release_metadata", return_value={"tag_name": release["tag"], "prerelease": False, "assets": [{"name": file.name}]}):
                with self.assertRaisesRegex(ValueError, "immutable asset differs"):
                    publisher.publish(release, [file], "SDK")
            self.assertEqual(writes, [])

    def test_unknown_remote_state_never_creates_release(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "archive.tgz"; file.write_bytes(b"ok")
            release = {"component": "contracts", "repository": "opensoha/soha", "tag": "contracts/v0.2.0", "sha": "a" * 40}
            with patch.object(publisher.control, "verify"), patch.object(publisher, "gh") as writes, patch.object(publisher, "release_metadata", side_effect=ValueError("unknown")):
                with self.assertRaisesRegex(ValueError, "unknown"):
                    publisher.publish(release, [file], "SDK")
                writes.assert_not_called()

    def test_component_cannot_replace_root_latest(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "archive.tgz"; file.write_bytes(b"ok")
            with self.assertRaisesRegex(ValueError, "Only Core"):
                publisher.publish({"component": "skills"}, [file], "Skills", latest=True)


if __name__ == "__main__":
    unittest.main()
