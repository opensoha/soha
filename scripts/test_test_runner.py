import importlib.util
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, call, patch


SOURCE = Path(__file__).with_name("test-runner.py")
sys.path.insert(0, str(SOURCE.parent))
SPEC = importlib.util.spec_from_file_location("test_runner", SOURCE)
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)


def repository():
    directory = tempfile.TemporaryDirectory()
    root = Path(directory.name)
    subprocess.run(["git", "init", "-q"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=root, check=True)
    subprocess.run(["git", "config", "user.name", "Test"], cwd=root, check=True)
    (root / "internal/domain/delivery").mkdir(parents=True)
    (root / "internal/domain/delivery/tracked.go").write_text("package delivery\n")
    subprocess.run(["git", "add", "internal/domain/delivery/tracked.go"], cwd=root, check=True)
    subprocess.run(["git", "commit", "-qm", "base"], cwd=root, check=True)
    return directory, root


class RunnerTest(unittest.TestCase):
    def test_exact_job_arguments(self):
        base = "1" * 40
        jobs = runner.build_jobs(
            {"core": True, "web": True},
            ["./internal/domain/delivery"], "src/services/api-error.test.ts", base,
        )
        self.assertEqual(jobs["core"], [
            ["bash", "scripts/run-pr-go-lint.sh", base],
            ["go", "test", "-count=1", "./internal/domain/delivery"],
        ])
        self.assertEqual(jobs["web"], [
            ["npm", "run", "lint", "--workspace", "web"],
            ["npm", "run", "typecheck", "--workspace", "web"],
            ["npm", "test", "--workspace", "web", "--", "src/services/api-error.test.ts"],
        ])

    def test_plan_counts_staged_worktree_and_untracked_changes(self):
        directory, root = repository()
        self.addCleanup(directory.cleanup)
        (root / "internal/domain/delivery/untracked.go").write_text("package delivery\n")
        tracked = root / "internal/domain/delivery/tracked.go"
        tracked.write_text("package delivery\n// staged\n")
        subprocess.run(["git", "add", str(tracked)], cwd=root, check=True)
        tracked.write_text("package delivery\n// worktree\n")
        plan = runner.make_plan("HEAD", root=root)
        self.assertEqual(plan["changed"], [
            "internal/domain/delivery/tracked.go",
            "internal/domain/delivery/untracked.go",
        ])
        self.assertEqual(plan["chosen"], ["core"])
        self.assertEqual(plan["subset"]["go_packages"], ["./internal/domain/delivery"])
        self.assertEqual(plan["jobs"]["core"][1],
                         ["go", "test", "-count=1", "./internal/domain/delivery"])

    def test_jobs_overlap_with_two_workers(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        code = (
            "import pathlib,sys,time\n"
            "start,end,other=map(pathlib.Path,sys.argv[1:])\n"
            "start.write_text(str(time.monotonic()))\n"
            "deadline=time.monotonic()+1\n"
            "while not other.exists() and time.monotonic()<deadline: time.sleep(0.005)\n"
            "end.write_text(str(time.monotonic()))\n"
            "raise SystemExit(0 if other.exists() else 3)\n"
        )
        a_start, a_end = root / "a.start", root / "a.end"
        b_start, b_end = root / "b.start", root / "b.end"
        jobs = {
            "a": [[sys.executable, "-c", code, str(a_start), str(a_end), str(b_start)]],
            "b": [[sys.executable, "-c", code, str(b_start), str(b_end), str(a_start)]],
        }
        results = runner.execute(jobs, 2, root=root, json_output=True)
        self.assertEqual({name: result["status"] for name, result in results.items()},
                         {"a": "success", "b": "success"})
        a0, a1 = float(a_start.read_text()), float(a_end.read_text())
        b0, b1 = float(b_start.read_text()), float(b_end.read_text())
        self.assertLess(max(a0, b0), min(a1, b1))

    def test_web_change_selects_core_without_a_go_change(self):
        directory, root = repository()
        self.addCleanup(directory.cleanup)
        (root / "web").mkdir()
        (root / "web/feature.ts").write_text("export const feature = true\n")
        plan = runner.make_plan("HEAD", root=root)
        self.assertEqual(plan["chosen"], ["core", "web"])
        self.assertEqual(plan["subset"]["go_packages"], ["./..."])

    def test_default_base_prefers_upstream_main_over_personal_fork(self):
        directory, root = repository()
        self.addCleanup(directory.cleanup)
        old = runner.git("rev-parse", "HEAD", root=root).strip()
        runner.git("update-ref", "refs/remotes/origin/main", old, root=root)
        runner.git("commit", "--allow-empty", "-m", "upstream update", root=root)
        new = runner.git("rev-parse", "HEAD", root=root).strip()
        runner.git("update-ref", "refs/remotes/opensoha/main", new, root=root)
        self.assertEqual(runner.resolve_base(None, root), new)
        self.assertEqual(runner.resolve_base("origin/main", root), old)

    def test_shared_change_keeps_full_go_scope(self):
        directory, root = repository()
        self.addCleanup(directory.cleanup)
        (root / "Makefile").write_text("# shared change\n")
        (root / "internal/domain/delivery/local.go").write_text("package delivery\n")
        self.assertEqual(runner.make_plan("HEAD", root=root)["subset"]["go_packages"], ["./..."])

    def test_guard_does_not_reexec_in_cache_bypass_environments(self):
        for key in ("CI", "GITHUB_ACTIONS", "OPENSOHA_VERIFY_RELEASE"):
            with self.subTest(key=key), patch.dict(os.environ, {key: "yes"}, clear=True):
                with patch.object(runner.os, "execv") as execv:
                    runner.ensure_guard(["run"])
                execv.assert_not_called()

    def test_failure_makes_job_and_run_nonzero(self):
        results = runner.execute({"core": [[sys.executable, "-c", "raise SystemExit(3)"]]},
                                 1, json_output=True)
        self.assertEqual(results["core"]["status"], "failure")
        self.assertEqual(results["core"]["commands"][0]["returncode"], 3)

    def test_cancellation_escalates_when_group_leader_has_already_exited(self):
        child = Mock(pid=123, **{"poll.return_value": 0})
        self.addCleanup(runner.CANCELLED.clear)
        with (patch.object(runner, "PROCESSES", {child}),
              patch.object(runner.os, "killpg") as killpg,
              patch.object(runner.time, "monotonic", side_effect=[0, 1, 6]),
              patch.object(runner.time, "sleep"),
              self.assertRaises(SystemExit) as interrupted):
            runner.terminate(signal.SIGTERM, None)
        self.assertEqual(interrupted.exception.code, 143)
        self.assertEqual(killpg.call_args_list, [call(123, signal.SIGTERM), call(123, 0),
                                                call(123, signal.SIGKILL)])

    def test_unsafe_web_test_is_rejected(self):
        with self.assertRaises(ValueError):
            runner.make_plan("HEAD", web_test="../outside.test.ts")


if __name__ == "__main__":
    unittest.main()
