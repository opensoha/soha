import importlib.util
import json
import os
import shutil
import subprocess
from pathlib import Path
import tempfile
import unittest
import uuid
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


class GoSourceTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="soha-go-source-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.addCleanup(patch.stopall)
        patch.object(workspace, "ROOT", self.root).start()
        patch.object(workspace, "SDK", self.root / "contracts").start()
        self.modules = workspace.go_modules()
        for name, directory in self.modules:
            directory.mkdir(parents=True, exist_ok=True)
            (directory / "go.mod").write_text(f"module {name}\n\ngo 1.27.4\n")
            (directory / "go.sum").write_text("")
        self.versions = ["v0.0.0-20261113000000-" + uuid.uuid4().hex[:12] for _ in range(3)]
        for version, (_, directory) in zip(self.versions, [m for m in self.modules if m[1] != workspace.SDK]):
            with (directory / "go.mod").open("a") as stream:
                stream.write(f"require {workspace.SDK_MODULE} {version}\n")
        self.aliases = [{"Old": {"Path": workspace.SDK_MODULE, "Version": v},
                         "New": {"Path": "./contracts"}} for v in self.versions]
        (self.root / "go.work").write_text("go 1.27.4\n\nuse (\n .\n ./contracts\n ./cli\n ./agent\n)\n" +
            "".join(f"replace {workspace.SDK_MODULE} {v} => ./contracts\n" for v in self.versions))

    def fake_go(self, args, **kwargs):
        if args == ["go", "env", "GOWORK"]:
            return str(self.root / "go.work")
        if args == ["go", "work", "edit", "-json"]:
            return json.dumps({"Use": [{"DiskPath": str(directory)} for _, directory in self.modules], "Replace": self.aliases})
        if args[:2] == ["go", "run"]:
            return ""
        name = args[-1]
        directory = dict(self.modules)[name]
        return json.dumps({"Path": name, "Dir": str(directory), "Main": True})

    def test_exact_four_module_identity(self):
        with patch.object(workspace, "run", side_effect=self.fake_go):
            workspace.check_go()

    def test_import_boundary_guard_failure_is_not_ignored(self):
        def run(args, **kwargs):
            if args[:2] == ["go", "run"]:
                raise subprocess.CalledProcessError(1, args)
            return self.fake_go(args, **kwargs)
        with patch.object(workspace, "run", side_effect=run), self.assertRaises(subprocess.CalledProcessError):
            workspace.check_go()

    def test_missing_component_manifest_fails(self):
        (self.root / "cli/go.mod").unlink()
        with self.assertRaisesRegex(ValueError, "same-checkout"):
            workspace.check_go()

    def test_component_symlink_fails(self):
        shutil.rmtree(self.root / "agent")
        (self.root / "agent").symlink_to(self.root / "cli", target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "confined"):
            workspace.check_go()

    def test_wrong_workspace_and_replacement_fail(self):
        for config in ({"Use": [{"DiskPath": str(self.root)}]},
                       {"Use": [{"DiskPath": str(d)} for _, d in self.modules], "Replace": [{"Old": "sdk"}]}):
            def run(args, **kwargs):
                return json.dumps(config) if args[1:3] == ["work", "edit"] else self.fake_go(args, **kwargs)
            with self.subTest(config=config), patch.object(workspace, "run", side_effect=run):
                with self.assertRaisesRegex(ValueError, "exactly"):
                    workspace.check_go()

    def test_each_module_rejects_non_sdk_replacements(self):
        for _, directory in self.modules:
            manifest = directory / "go.mod"
            before = manifest.read_text()
            with self.subTest(module=directory):
                manifest.write_text(before + "replace github.com/google/uuid v1.6.0 => /outside\n")
                with self.assertRaisesRegex(ValueError, "must not contain replacements"):
                    workspace.check_go()
            manifest.write_text(before)

    def test_wrong_module_resolution_fails(self):
        def run(args, **kwargs):
            if args[-1] == "github.com/opensoha/soha/cli":
                return json.dumps({"Path": args[-1], "Dir": str(self.root / "cli"), "Main": False})
            return self.fake_go(args, **kwargs)
        with patch.object(workspace, "run", side_effect=run):
            with self.assertRaisesRegex(ValueError, "same-checkout source"):
                workspace.check_go()

    def test_sdk_aliases_reject_missing_extra_unversioned_foreign_and_redirects(self):
        cases = [[], self.aliases + [self.aliases[0]], self.aliases[:-1]]
        for section, key, value in [("Old", "Version", ""), ("Old", "Version", "v9.0.0"),
                                     ("Old", "Path", "example.invalid/foreign"),
                                     ("New", "Path", "../contracts"), ("New", "Path", str(workspace.SDK)),
                                     ("New", "Version", "v1.0.0")]:
            aliases = json.loads(json.dumps(self.aliases))
            aliases[0][section][key] = value
            cases.append(aliases)
        for aliases in cases:
            with self.subTest(aliases=aliases), self.assertRaisesRegex(ValueError, "exactly"):
                workspace.validate_sdk_aliases({"Replace": aliases}, self.root)

    def test_sdk_alias_versions_come_only_from_require_declarations(self):
        manifest = self.root / "cli/go.mod"
        manifest.write_text(f"module github.com/opensoha/soha/cli\nrequire (\n {workspace.SDK_MODULE} {self.versions[1]} // SDK\n)\n"
                            f"exclude {workspace.SDK_MODULE} v9.0.0\n")
        self.assertEqual(workspace.validate_sdk_aliases({"Replace": self.aliases}, self.root), set(self.versions))
        with manifest.open("a") as stream:
            stream.write(f"require {workspace.SDK_MODULE} {self.versions[1]}\n")
        with self.assertRaisesRegex(ValueError, "exactly one"):
            workspace.validate_sdk_aliases({"Replace": self.aliases}, self.root)

    def test_unpublished_sdk_graph_with_external_dependency_builds_from_source_workspace(self):
        sdk = self.root / "contracts/unpublished"
        sdk.mkdir()
        (sdk / "value.go").write_text('package unpublished\nimport "github.com/google/uuid"\nvar Value = uuid.Nil.String()\n')
        sums = "".join(line + "\n" for line in (Path(spec.origin).parents[1] / "go.sum").read_text().splitlines()
                       if line.startswith("github.com/google/uuid v1.6.0 ") or line.startswith("github.com/google/uuid v1.6.0/go.mod "))
        self.assertEqual(len(sums.splitlines()), 2)
        for _, directory in self.modules:
            with (directory / "go.mod").open("a") as stream:
                stream.write("require github.com/google/uuid v1.6.0\n")
            (directory / "go.sum").write_text(sums)
            if directory != workspace.SDK:
                (directory / "source.go").write_text('package source\nimport "github.com/opensoha/soha/contracts/unpublished"\nvar Value = unpublished.Value\n')
        env = {**os.environ, "GOWORK": str(self.root / "go.work"), "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local"}
        manifests = {p: p.read_bytes() for p in self.root.rglob("go.mod")}
        with patch.dict(os.environ, env):
            workspace.check_go()
            for _, directory in self.modules:
                subprocess.run(["go", "test", "./..."], cwd=directory, env=env, check=True, capture_output=True)
            for args in (["list", "-m", "all"], ["mod", "download"], ["mod", "download", "all"]):
                subprocess.run(["go", *args], cwd=self.root, env=env, check=True, capture_output=True)
            standalone = subprocess.run(["go", "test", "./..."], cwd=self.root, env={**env, "GOWORK": "off"}, capture_output=True)
            self.assertNotEqual(standalone.returncode, 0)
        self.assertEqual(manifests, {p: p.read_bytes() for p in manifests})


class NpmSourceTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="soha-npm-source-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.addCleanup(patch.stopall)
        patch.object(workspace, "ROOT", self.root).start()
        patch.object(workspace, "SDK", self.root / "contracts").start()
        self.names = {"contracts": "@opensoha/contracts", "web": "soha-web", "connectors": "@opensoha/connectors", "cli/npm/cli": "@opensoha/cli"}
        self.package = {"private": True, "workspaces": list(self.names)}
        self.lock = {"packages": {"": {"workspaces": list(self.names)}}}
        for part, name in self.names.items():
            directory = self.root / part
            directory.mkdir(parents=True)
            package = {"name": name, "version": "1.0.0"}
            if part == "web":
                package["dependencies"] = {"@opensoha/contracts": "file:../contracts"}
            (directory / "package.json").write_text(json.dumps(package))
            installed = self.root / "node_modules" / name
            installed.parent.mkdir(parents=True, exist_ok=True)
            installed.symlink_to(directory, target_is_directory=True)
            self.lock["packages"][part] = {"version": "1.0.0"}
            self.lock["packages"]["node_modules/" + name] = {"resolved": part, "link": True}
        sdk = workspace.SDK / "gen/ts/sohaapi/index.d.ts"
        sdk.parent.mkdir(parents=True)
        sdk.write_text("export {}")
        patch.object(workspace, "run", return_value=str(sdk)).start()
        self.write()

    def write(self):
        (self.root / "package.json").write_text(json.dumps(self.package))
        (self.root / "package-lock.json").write_text(json.dumps(self.lock))

    def test_root_four_workspaces_and_separate_dsh_lock(self):
        dsh = self.root / "agent/runtimes/dsh"
        dsh.mkdir(parents=True)
        (dsh / "package-lock.json").write_text("{}")
        workspace.check_npm()

    def test_missing_new_workspace_link_fails(self):
        del self.lock["packages"]["node_modules/@opensoha/cli"]
        self.write()
        with self.assertRaisesRegex(ValueError, "same-checkout"):
            workspace.check_npm()

    def test_dsh_in_root_workspace_fails(self):
        self.package["workspaces"].append("agent/runtimes/dsh")
        self.write()
        with self.assertRaisesRegex(ValueError, "private root npm"):
            workspace.check_npm()

    def test_other_install_lock_fails(self):
        (self.root / "connectors/package-lock.json").write_text("{}")
        with self.assertRaisesRegex(ValueError, "only the root"):
            workspace.check_npm()

    def test_sdk_redirect_and_bad_locked_identity_fail(self):
        link = self.root / "node_modules/@opensoha/contracts"
        link.unlink()
        link.symlink_to(self.root / "web", target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "same-checkout"):
            workspace.check_npm()
        link.unlink()
        link.symlink_to(workspace.SDK, target_is_directory=True)
        self.lock["packages"]["cli/npm/cli"]["version"] = "2.0.0"
        self.write()
        with self.assertRaisesRegex(ValueError, "identity"):
            workspace.check_npm()


if __name__ == "__main__":
    unittest.main()
