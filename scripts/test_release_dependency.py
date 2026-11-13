"""Exercise the release workflow's real public Go qualification without networking."""
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github/workflows/release.yml").read_text()
SDK = "example.invalid/task125-release-sdk"
VERSION = "v0.1.0"
GO = shutil.which("go")


def workflow_run(name):
    match = re.search(r"^      - name: " + re.escape(name) + r"\n(.*?)(?=^      - |\Z)", WORKFLOW, re.M | re.S)
    if not match or "        run: |\n" not in match[1]:
        raise AssertionError(f"missing executable release step: {name}")
    return textwrap.dedent(match[1].split("        run: |\n", 1)[1])


def workflow_python(marker):
    match = re.search(r"<<'" + marker + r"'\n(.*?)^          " + marker + r"$", WORKFLOW, re.M | re.S)
    if not match:
        raise AssertionError(f"missing executable release metadata guard: {marker}")
    return textwrap.dedent(match[1])


PUBLIC_QUALIFICATION = workflow_run("Qualify Core against the published Go SDK")
IDENTITY_GUARD = workflow_python("SDK_IDENTITY")
CHECKSUM_GUARD = workflow_python("SDK_CHECKSUM")


class ReleaseFixture:
    def __init__(self, directory, extra):
        directory = Path(directory).resolve()
        self.root = directory / "core"
        self.root.mkdir()
        self.runner = Path(directory) / "runner"
        self.runner.mkdir()
        (self.runner / "keep.txt").write_text("keep unrelated runner output")
        proxy = Path(directory) / "proxy"
        module = f"module {SDK}\n\ngo 1.27.4\n"
        published = {"go.mod": module, "base/base.go": 'package base\nconst Value = "published"\n'}
        version_dir = proxy / SDK / "@v"
        version_dir.mkdir(parents=True)
        (version_dir / "list").write_text(VERSION + "\n")
        (version_dir / f"{VERSION}.mod").write_text(module)
        (version_dir / f"{VERSION}.info").write_text(json.dumps({"Version": VERSION, "Time": "2026-01-01T00:00:00Z"}))
        # Stable bytes allow reuse of the shared module cache without colliding with a real SDK.
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as zipped:
            for name, content in published.items():
                entry = zipfile.ZipInfo(f"{SDK}@{VERSION}/{name}", (1980, 1, 1, 0, 0, 0))
                entry.external_attr = 0o100644 << 16
                zipped.writestr(entry, content)
        (version_dir / f"{VERSION}.zip").write_bytes(archive.getvalue())
        self.write("go.mod", f"module example.invalid/task125-release-core\n\ngo 1.27.4\n\nrequire {SDK} {VERSION}\n")
        self.write("go.work", "go 1.27.4\n\nuse (\n .\n ./contracts\n)\n")
        for name, content in published.items():
            self.write("contracts/" + name, content)
        self.write("contracts/privateextra/extra.go", 'package privateextra\nconst Value = "unpublished"\n')
        package = "privateextra" if extra == "test" else "base"
        expected = "unpublished" if extra == "test" else "published"
        self.write("probe_test.go", f'package core\nimport("testing"; "{SDK}/{package}")\nfunc TestSDK(t *testing.T) {{ if {package}.Value != "{expected}" {{ t.Fatal("wrong SDK") }} }}\n')
        self.write("cmd/server/main.go", f'package main\nimport("fmt"; "{SDK}/base")\nfunc main() {{ fmt.Println(base.Value) }}\n')
        if extra == "embed":
            self.write("cmd/server/embed.go", f'//go:build embedassets\n\npackage main\nimport _ "{SDK}/privateextra"\n')
        self.env = {**os.environ, "GOPROXY": proxy.as_uri(), "GOSUMDB": "off", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOVCS": "*:off", "GOTOOLCHAIN": "local", "GOMAXPROCS": "2", "RUNNER_TEMP": str(self.runner)}
        downloaded = self.go("mod", "download", "-json", SDK, work="off", flags="-mod=mod")
        self.require_success(downloaded)
        self.downloaded = json.loads(downloaded.stdout)
        if not self.downloaded.get("Sum") or not self.downloaded.get("GoModSum"):
            raise AssertionError("native Go did not provide published fixture checksums")
        self.require_success(self.go("mod", "verify", work="off"))
        # The published base package is usable before the negative qualification is attempted.
        self.require_success(self.go("list", SDK + "/base", work="off"))
        self.trace = Path(directory) / "go-invocations.jsonl"
        self.bin = Path(directory) / "bin"
        self.bin.mkdir()
        wrapper = self.bin / "go"
        wrapper.write_text("#!/usr/bin/env python3\nimport json,os,sys\nfrom pathlib import Path\nwith Path(os.environ['RELEASE_GO_TRACE']).open('a') as output:\n output.write(json.dumps({'argv':sys.argv[1:],'GOWORK':os.environ.get('GOWORK'),'GOFLAGS':os.environ.get('GOFLAGS'),'CGO_ENABLED':os.environ.get('CGO_ENABLED')})+'\\n')\nos.execv(os.environ['RELEASE_REAL_GO'],[os.environ['RELEASE_REAL_GO'],*sys.argv[1:]])\n")
        wrapper.chmod(0o755)

    def write(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    @staticmethod
    def require_success(result):
        if result.returncode:
            raise AssertionError(result.stdout + result.stderr)

    def go(self, *args, work=None, flags="-mod=readonly"):
        env = {**self.env, "GOWORK": work or str(self.root / "go.work"), "GOFLAGS": flags}
        return subprocess.run([GO, *args], cwd=self.root, env=env, capture_output=True, text=True)

    def qualify(self):
        env = {**self.env, "GOWORK": str(self.root / "go.work"), "GOFLAGS": "-mod=mod", "PATH": str(self.bin) + os.pathsep + self.env["PATH"], "RELEASE_REAL_GO": GO, "RELEASE_GO_TRACE": str(self.trace)}
        return subprocess.run(["bash", "-e", "-o", "pipefail", "-c", PUBLIC_QUALIFICATION], cwd=self.root, env=env, capture_output=True, text=True)

    def manifests(self):
        return {name: (self.root / name).read_bytes() if (self.root / name).exists() else None for name in ["go.mod", "go.sum", "go.work", "go.work.sum", "contracts/go.mod", "contracts/go.sum"]}

    def metadata(self):
        result = self.go("list", "-m", "-json", SDK, work="off")
        self.require_success(result)
        return json.loads(result.stdout)

    def guard(self, program, *documents):
        files = []
        for index, document in enumerate(documents):
            path = self.runner / f"metadata-{index}.json"
            path.write_text(json.dumps(document))
            files.append(str(path))
        # Bind the guard to this isolated proxy namespace, preserving its actual validation logic.
        program = program.replace("github.com/opensoha/soha/contracts", SDK)
        return subprocess.run([sys.executable, "-", *files], input=program, cwd=self.root, env=self.env, capture_output=True, text=True)


class ReleaseDependencyTest(unittest.TestCase):
    def fixture(self, extra):
        temporary = tempfile.TemporaryDirectory(prefix="soha-release-dependency-", dir=os.environ.get("OPENSOHA_VERIFY_DIR"))
        self.addCleanup(temporary.cleanup)
        return ReleaseFixture(temporary.name, extra)

    def test_workflow_qualification_uses_published_sdk_for_tests_and_embed_compile(self):
        for extra in ["none", "test", "embed"]:
            with self.subTest(unpublished_import=extra):
                fixture = self.fixture(extra)
                before = fixture.manifests()
                fixture.require_success(fixture.go("test", "./..."))
                fixture.require_success(fixture.go("build", "-tags", "embedassets", "-o", str(fixture.runner / "source-server"), "./cmd/server"))
                result = fixture.qualify()
                calls = [json.loads(line) for line in fixture.trace.read_text().splitlines()]
                self.assertTrue(calls)
                self.assertTrue(all(call["GOWORK"] == "off" and call["GOFLAGS"] == "-mod=readonly" for call in calls))
                self.assertEqual(calls[0]["argv"], ["test", "./..."])
                if extra == "none":
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(SDK + "/privateextra", result.stderr)
                    self.assertRegex(result.stderr, r"cannot find module providing package|no required module provides package")
                if extra != "test":
                    self.assertEqual(calls[-1]["argv"][0:3], ["build", "-tags", "embedassets"])
                    self.assertEqual(calls[-1]["CGO_ENABLED"], "0")
                else:
                    self.assertEqual(len(calls), 1)
                self.assertEqual(fixture.manifests(), before)
                self.assertEqual(list(fixture.runner.glob("soha-published-sdk.*")), [])
                self.assertEqual((fixture.runner / "keep.txt").read_text(), "keep unrelated runner output")
                print(json.dumps({"fixtureModule": SDK, "version": VERSION, "nativeSum": fixture.downloaded["Sum"], "unpublishedImport": extra, "sourceTestAndEmbed": "PASS", "actualWorkflowExit": result.returncode, "actualGoCalls": calls, "expectedPublicResult": "PASS" if extra == "none" else "missing unpublished package"}), flush=True)

    def test_actual_metadata_guard_rejects_a_semver_local_replacement(self):
        fixture = self.fixture("test")
        original = (fixture.root / "go.mod").read_text()
        fixture.write("go.mod", original + f"\nreplace {SDK} => ./contracts\n")
        metadata = fixture.metadata()
        self.assertEqual(metadata["Version"], VERSION)
        self.assertTrue(metadata.get("Replace"))
        result = fixture.guard(IDENTITY_GUARD, metadata)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not use a replacement", result.stderr)

    def test_actual_guards_reject_legacy_and_foreign_module_identities(self):
        self.assertIn('"github.com/opensoha/soha/contracts"', IDENTITY_GUARD)
        self.assertIn('"github.com/opensoha/soha/contracts"', CHECKSUM_GUARD)
        fixture = self.fixture("none")
        metadata = fixture.metadata()
        for path in ["github.com/opensoha/soha-contracts", "example.invalid/foreign"]:
            with self.subTest(path=path):
                result = fixture.guard(IDENTITY_GUARD, {**metadata, "Path": path})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("identity mismatch", result.stderr)
                result = fixture.guard(CHECKSUM_GUARD, metadata, {**fixture.downloaded, "Path": path})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("identity mismatch", result.stderr)

    def test_actual_checksum_guard_requires_matching_native_sums(self):
        fixture = self.fixture("none")
        metadata = fixture.metadata()
        valid = fixture.guard(CHECKSUM_GUARD, metadata, fixture.downloaded)
        self.assertEqual(valid.returncode, 0, valid.stdout + valid.stderr)
        invalid = fixture.guard(CHECKSUM_GUARD, metadata, {**fixture.downloaded, "Sum": "h1:unit-mismatch"})
        self.assertNotEqual(invalid.returncode, 0)
        self.assertIn("version/checksum mismatch", invalid.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
