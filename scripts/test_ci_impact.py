import importlib.util
import json
import os
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory
import unittest

spec = importlib.util.spec_from_file_location("ci_impact", Path(__file__).with_name("ci-impact.py"))
impact = importlib.util.module_from_spec(spec)
spec.loader.exec_module(impact)


class ImpactTests(unittest.TestCase):
    def test_first_import_baseline_requires_exact_reviewed_tree_and_ancestry(self):
        with TemporaryDirectory(prefix="soha-web-import-base-") as directory:
            root = Path(directory)
            def git(*args, data=None):
                return subprocess.check_output(["git", "-C", directory, *args], input=data, text=True).strip()
            git("init", "--quiet", "-b", "main")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            (root / "README.md").write_text("Core")
            git("add", ".")
            git("commit", "--quiet", "-m", "Core baseline")
            core = git("rev-parse", "HEAD")
            git("checkout", "--quiet", "--orphan", "raw-web")
            git("rm", "--quiet", "-rf", ".")
            (root / "package.json").write_text('{"name":"soha-web"}')
            (root / "existing-debt.ts").write_text("reviewed existing debt")
            git("add", ".")
            git("commit", "--quiet", "-m", "Reviewed Web")
            web = git("rev-parse", "HEAD")
            git("checkout", "--quiet", "main")
            git("read-tree", "--prefix=web/", "-u", web)
            imported = git("commit-tree", git("write-tree"), "-p", core, "-p", web, data="Import Web\n")
            git("update-ref", "HEAD", imported, core)
            manifest = root / "contracts/compat/consumer-revisions.json"
            manifest.parent.mkdir(parents=True)
            manifest.write_text(json.dumps({"soha-web": web}))
            git("add", ".")
            git("commit", "--quiet", "-m", "Source workspace")
            self.assertEqual(impact.web_baseline(core, root), imported)
            self.assertEqual(impact.web_baseline(imported, root), imported)
            with self.assertRaises(ValueError):
                impact.web_baseline("main", root)
            manifest.write_text(json.dumps({"soha-web": core}))
            with self.assertRaisesRegex(ValueError, "differs"):
                impact.web_baseline(core, root)

    def test_cli_reads_actual_git_rename_and_delete_and_falls_back_on_bad_event(self):
        with TemporaryDirectory(prefix="soha-ci-impact-") as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(["git", "-C", directory, *args], text=True).strip()
            git("init", "--quiet")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.invalid")
            (root / "contracts").mkdir()
            (root / "contracts/old.json").write_text("old")
            (root / "contracts/deleted.json").write_text("delete")
            git("add", ".")
            git("commit", "--quiet", "-m", "base")
            base = git("rev-parse", "HEAD")
            (root / "contracts/old.json").rename(root / "renamed.md")
            (root / "contracts/deleted.json").unlink()
            git("add", "-A")
            git("commit", "--quiet", "-m", "rename and delete")
            event = root / "event.json"
            event.write_text(json.dumps({"before": base}))
            output = root / "output"
            env = {**os.environ, "GITHUB_EVENT_PATH": str(event), "GITHUB_EVENT_NAME": "push", "GITHUB_SHA": git("rev-parse", "HEAD"), "GITHUB_OUTPUT": str(output)}
            for data in [json.dumps({"before": base}), "broken json"]:
                event.write_text(data)
                output.write_text("")
                result = subprocess.run([os.sys.executable, str(Path(impact.__file__).resolve()), "route"], cwd=root, env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(output.read_text().splitlines(), [f"{job}=true" for job in impact.JOBS])

    def test_component_and_documentation_paths(self):
        self.assertFalse(any(impact.select(b"M\0docs/guide.md\0").values()))
        self.assertEqual(impact.select(b"M\0web/src/example.test.ts\0"), dict(core=True, web=True, contracts=False, deletion=False))
        self.assertEqual(impact.select(b"M\0internal/example_test.go\0"), dict(core=True, web=False, contracts=False, deletion=True))

    def test_rename_delete_shared_generated_and_unknown(self):
        for diff in [b"R100\0contracts/gen/sdk.ts\0docs/sdk.md\0", b"D\0contracts/fixtures/a.json\0", b"M\0package-lock.json\0", b"M\0web/.github/workflows/ci.yml\0", b"M\0new-tool.conf\0", b"M\0.github/workflows/ci.yml\0"]:
            with self.subTest(diff=diff):
                self.assertTrue(all(impact.select(diff).values()))
        self.assertTrue(impact.select(b"R100\0web/src/a.ts\0docs/a.md\0")["web"])
        self.assertTrue(impact.select(b"D\0internal/old.go\0")["core"])

    def test_malformed_diff_fails_closed(self):
        for diff in [b"M\0a", b"R100\0a\0", b"WHAT\0a\0", b"M\0../a\0", b"M\0\xff\0"]:
            self.assertTrue(all(impact.select(diff).values()))

    def test_required_results_cannot_be_hidden(self):
        needs = {job: {"result": "success"} for job in impact.JOBS}
        needs["route"] = {"result": "success", "outputs": dict.fromkeys(impact.JOBS, "true")}
        impact.check_summary(needs)
        for state in ["skipped", "cancelled", "failure", None]:
            broken = {**needs, "web": {"result": state}}
            with self.subTest(state=state), self.assertRaises(ValueError):
                impact.check_summary(broken)
        for broken in [{**needs, "route": {"result": "failure"}}, {**needs, "route": {"result": "success", "outputs": {}}}, {key: value for key, value in needs.items() if key != "web"}]:
            with self.assertRaises(ValueError):
                impact.check_summary(broken)
        needs["route"]["outputs"]["web"] = "false"
        needs["web"]["result"] = "skipped"
        impact.check_summary(needs)
        needs["web"]["result"] = "cancelled"
        with self.assertRaises(ValueError):
            impact.check_summary(needs)


if __name__ == "__main__":
    unittest.main()
