import hashlib
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
            self.assertEqual(call.kwargs["cwd"], Path(resolver.__file__).resolve().parents[1])
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
                        resolver.main(["--mode", "declared"])
        self.assertFalse(output.exists())


class SourceContractsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        identities = {".": "github.com/opensoha/soha", "contracts": resolver.MODULE,
                      "cli": "github.com/opensoha/soha-cli", "agent": "github.com/opensoha/soha-agent"}
        self.versions = {".": "v0.1.30-0.20261110173546-4aef3ea31f6a", "cli": "v0.1.30-0.20261110173546-4aef3ea31f6a",
                         "agent": "v0.1.30-0.20261112071503-e0b9d2f86a68"}
        for directory, module in identities.items():
            destination = self.root / directory
            destination.mkdir(exist_ok=True)
            (destination / "go.mod").write_text("module " + module + "\n\ngo 1.27.4\n")
            if directory in self.versions:
                with (destination / "go.mod").open("a") as manifest:
                    manifest.write("require " + resolver.MODULE + " " + self.versions[directory] + "\n")
            (destination / "go.sum").write_text("")
        self.workspace = "go 1.27.4\nuse (\n.\n./contracts\n./cli\n./agent\n)\n"
        self.aliases = [resolver.MODULE + " " + version + " => ./contracts\n" for version in sorted(set(self.versions.values()))]
        (self.root / "go.work").write_text(self.workspace + "replace (\n" + "".join(self.aliases) + ")\n")
        (self.root / "go.work.sum").write_text("")
        self.schema = self.root / "contracts/agent/dsh-runtime.schema.json"
        self.schema.parent.mkdir()
        self.schema.write_text('{"type":"object"}')

    def query(self, args):
        self.assertEqual(args[:3], ["git", "-C", str(self.root)])
        if args[3:] == ["rev-parse", "--show-toplevel"]:
            return str(self.root).encode() + b"\n"
        if args[3:] == ["rev-parse", "HEAD"]:
            return b"a" * 40 + b"\n"
        self.assertEqual(args[3:], ["ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "contracts"])
        return b"contracts/go.mod\0contracts/go.sum\0contracts/agent/dsh-runtime.schema.json\0"

    def resolve(self):
        with patch.object(subprocess, "check_output", side_effect=self.query), \
                patch.object(subprocess, "run", side_effect=AssertionError("source mode must not download SDK")):
            return resolver.resolve_source_contracts(self.root)

    def test_source_uses_exact_four_modules_and_actual_root_provenance(self):
        result = self.resolve()
        self.assertEqual(result["mode"], "source")
        self.assertEqual(result["ref"], "a" * 40)
        self.assertEqual(result["dir"], str(self.root / "contracts"))
        self.assertEqual(result["repository"], str(self.root))
        self.assertNotIn("sum", result)  # Source bytes are not a published module checksum.
        self.assertEqual(len(result["sourceSHA256"]), 64)
        self.assertEqual(result["goWorkSHA256"], hashlib.sha256((self.root / "go.work").read_bytes()).hexdigest())
        self.schema.write_text('{"type":"object","additionalProperties":false}')
        self.assertNotEqual(result["sourceSHA256"], self.resolve()["sourceSHA256"])

    def test_missing_schema_manifest_or_module_never_falls_back_to_registry(self):
        for path in (self.schema, self.root / "contracts/go.mod", self.root / "cli/go.sum", self.root / "go.work.sum"):
            with self.subTest(path=path):
                original = path.read_bytes()
                path.unlink()
                with self.assertRaises(ValueError):
                    self.resolve()
                path.write_bytes(original)

    def test_changed_workspace_or_module_identity_is_rejected(self):
        workspace = self.root / "go.work"
        for text in ("go 1.27.4\nuse .\n", "go 1.27.4\nuse (\n.\n./contracts\n./cli\n./cli\n)\n"):
            workspace.write_text(text)
            with self.assertRaises(ValueError):
                self.resolve()
        workspace.write_text("go 1.27.4\nuse (\n.\n./contracts\n./cli\n./agent\n)\n")
        (self.root / "agent/go.mod").write_text("module github.com/other/agent\n")
        with self.assertRaises(ValueError):
            self.resolve()

    def test_schema_symlink_and_old_repo_provenance_are_rejected(self):
        self.schema.unlink()
        self.schema.symlink_to(self.root / "contracts/go.mod")
        with self.assertRaises(ValueError):
            self.resolve()
        self.schema.unlink()
        self.schema.write_text('{"type":"object"}')
        with patch.object(subprocess, "check_output", return_value=str(self.root / "agent").encode()):
            with self.assertRaises(ValueError):
                resolver.resolve_source_contracts(self.root)

    def test_source_cannot_claim_an_external_requested_ref(self):
        output = self.root / "output"
        with patch.dict(os.environ, {"REQUESTED_CONTRACTS_REF": "v0.1.29", "GITHUB_OUTPUT": str(output)}):
            with self.assertRaises(ValueError):
                resolver.main([])
        self.assertFalse(output.exists())

    def test_exact_two_version_aliases_support_single_and_block_forms_and_bind_workspace(self):
        original = self.resolve()
        (self.root / "go.work").write_text(self.workspace + "".join("replace " + alias for alias in self.aliases))
        updated = self.resolve()
        self.assertNotEqual(original["goWorkSHA256"], updated["goWorkSHA256"])
        self.assertEqual(original["sourceSHA256"], updated["sourceSHA256"])

    def test_missing_extra_duplicate_unversioned_foreign_and_outside_aliases_are_rejected(self):
        variants = [self.aliases[:1], self.aliases + [resolver.MODULE + " v0.1.29 => ./contracts\n"],
                    self.aliases + self.aliases[:1], [resolver.MODULE + " => ./contracts\n"],
                    [self.aliases[0].replace(resolver.MODULE, "github.com/other/sdk"), self.aliases[1]],
                    [self.aliases[0].replace("./contracts", "../outside"), self.aliases[1]],
                    [self.aliases[0].replace("./contracts", str(self.root / "contracts")), self.aliases[1]]]
        for aliases in [[], *variants]:
            with self.subTest(aliases=aliases):
                (self.root / "go.work").write_text(self.workspace + "replace (\n" + "".join(aliases) + ")\n")
                with self.assertRaises(ValueError):
                    self.resolve()

    def test_consumer_sdk_requirement_and_any_module_replacement_are_rejected(self):
        manifest = self.root / "agent/go.mod"
        original = manifest.read_text()
        for text in [original.replace("require ", "// require "),
                     original.replace(self.versions["agent"], "v0.1.29"),
                     original + "require " + resolver.MODULE + " v0.1.29\n"]:
            with self.subTest(text=text):
                manifest.write_text(text)
                with self.assertRaises(ValueError):
                    self.resolve()
        manifest.write_text(original)
        for name in ("go.mod", "cli/go.mod", "agent/go.mod", "contracts/go.mod"):
            manifest = self.root / name
            original = manifest.read_text()
            with self.subTest(manifest=name):
                manifest.write_text(original + "replace github.com/other/sdk v1.0.0 => ../outside\n")
                with self.assertRaises(ValueError):
                    self.resolve()
                manifest.write_text(original)

    def test_sdk_directory_or_manifest_symlink_is_rejected(self):
        sdk = self.root / "contracts"
        manifest = sdk / "go.mod"
        original = manifest.read_text()
        manifest.write_text(original.replace(resolver.MODULE, "github.com/other/sdk"))
        with self.assertRaises(ValueError):
            self.resolve()
        manifest.write_text(original)
        relocated = self.root / "sdk-redirect"
        sdk.rename(relocated)
        sdk.symlink_to(relocated, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.resolve()
        sdk.unlink()
        relocated.rename(sdk)
        manifest = sdk / "go.mod"
        manifest.rename(sdk / "go.mod.saved")
        manifest.symlink_to(sdk / "go.mod.saved")
        with self.assertRaises(ValueError):
            self.resolve()


if __name__ == "__main__":
    unittest.main()
