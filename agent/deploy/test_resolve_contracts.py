import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import resolve_contracts as resolver


class ResolveContractsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = self.temporary.name
        self.module = {"Path": resolver.MODULE, "Version": "v0.1.30-0.20261110173546-4aef3ea31f6a", "Sum": "h1:fixture", "Dir": self.directory}

    def call(self, declared=None, downloaded=None, resolved=None, requested="", verify_error=None):
        values = [self.module if declared is None else declared, self.module if downloaded is None else downloaded, self.module if resolved is None else resolved]
        with patch.object(subprocess, "check_output", side_effect=[json.dumps(value) for value in values]) as query:
            with patch.object(subprocess, "run", side_effect=verify_error) as verify:
                result = resolver.resolve_contracts(requested)
        for call in [*query.call_args_list, *verify.call_args_list]:
            self.assertEqual(call.kwargs["env"]["GOWORK"], "off")
            self.assertEqual(call.kwargs["env"]["GOFLAGS"], "-mod=readonly")
        self.assertEqual(verify.call_args.args[0], ["go", "mod", "verify"])
        return result

    def test_pseudo_version_needs_no_origin_and_preserves_manifests(self):
        files = [Path(self.directory) / name for name in ["go.mod", "go.sum"]]
        for file in files:
            file.write_bytes(b"unchanged fixture\r\n")
        result = self.call(requested=self.module["Version"])
        self.assertEqual(result, {"ref": self.module["Version"], "dir": self.directory, "sum": "h1:fixture"})
        for file in files:
            self.assertEqual(file.read_bytes(), b"unchanged fixture\r\n")

    def test_release_tag_remains_supported(self):
        self.module["Version"] = "v0.1.29"
        self.assertEqual(self.call()["ref"], "v0.1.29")

    def test_rejects_replacement_empty_version_and_requested_mismatch(self):
        for declared in [{**self.module, "Replace": {"Dir": self.directory}}, {**self.module, "Version": ""}]:
            with self.subTest(declared=declared), self.assertRaises(ValueError):
                self.call(declared=declared)
        with self.assertRaises(ValueError):
            self.call(requested="v0.1.27")

    def test_rejects_download_version_and_checksum_mismatch(self):
        for change in [{"Version": "v0.1.27"}, {"Sum": ""}, {"Sum": "h1:wrong"}, {"Error": "download failed"}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.call(downloaded={**self.module, **change})

    def test_rejects_changed_or_missing_directory(self):
        for change in [{"Version": "v0.1.27"}, {"Dir": ""}, {"Dir": "/does-not-exist"}, {"Replace": {"Dir": self.directory}}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.call(resolved={**self.module, **change})

    def test_verification_failure_cannot_output_a_success_directory(self):
        output = Path(self.directory) / "output"
        with patch.dict(os.environ, {"GITHUB_OUTPUT": str(output)}):
            with patch.object(subprocess, "check_output", side_effect=[json.dumps(self.module), json.dumps(self.module)]):
                with patch.object(subprocess, "run", side_effect=subprocess.CalledProcessError(1, ["go", "mod", "verify"])):
                    with self.assertRaises(subprocess.CalledProcessError):
                        resolver.main()
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
