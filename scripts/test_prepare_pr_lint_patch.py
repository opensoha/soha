from pathlib import Path
import os
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("prepare-pr-lint-patch.sh").resolve()


class PreparePRLintPatchTest(unittest.TestCase):
    def setUp(self):
        self.repo = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.patch = self.repo / "lint.patch"
        self.source = self.repo / "agent" / "fixture.go"
        self.source.parent.mkdir()
        self.env = {
            **os.environ,
            "GIT_AUTHOR_NAME": "CI fixture",
            "GIT_AUTHOR_EMAIL": "ci@example.invalid",
            "GIT_COMMITTER_NAME": "CI fixture",
            "GIT_COMMITTER_EMAIL": "ci@example.invalid",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
        }
        self.git("init", "-q", "-b", "base")
        self.body = "package fixture\n" + "\n".join(f"// context {i}" for i in range(10)) + "\n"
        self.commit(self.body + "var result = 0\n")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, env=self.env)

    def commit(self, source):
        self.source.write_text(source, encoding="utf-8")
        self.git("add", "agent/fixture.go")
        self.git("commit", "-qm", "fixture")
        return self.git("rev-parse", "HEAD").decode().strip()

    def prepare(self, base, cwd=None):
        return subprocess.run(["bash", SCRIPT, base, self.patch], cwd=cwd or self.repo, env=self.env, capture_output=True)

    def test_patch_uses_merge_candidate_line_numbers_and_module_relative_paths(self):
        self.git("switch", "-qc", "topic")
        self.commit(self.body + "var result = 1\n")
        self.git("switch", "-q", "base")
        base = self.commit("// new base lines\n// shift\n" + self.body + "var result = 0\n")
        pr_diff = self.git("diff", "base...topic")
        self.git("merge", "--no-ff", "--no-edit", "topic")

        result = self.prepare(base)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        patch = self.patch.read_bytes()
        self.assertEqual(patch, self.git("diff", "--binary", base, "HEAD"))
        self.assertIn(b"+++ b/agent/fixture.go", patch)
        self.assertNotEqual(patch, pr_diff)

        result = self.prepare(base, self.source.parent)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        patch = self.patch.read_bytes()
        self.assertEqual(patch, self.git("diff", "--binary", "--relative=agent", base, "HEAD"))
        self.assertIn(b"+++ b/fixture.go", patch)

    def test_rejects_invalid_missing_and_empty_baselines(self):
        head = self.commit(self.body + "var result = 2\n")
        for base in ("HEAD^", "0" * 40, head):
            with self.subTest(base=base):
                self.assertNotEqual(self.prepare(base).returncode, 0)

    def test_allows_empty_module_patch_when_changes_are_outside_module(self):
        base = self.git("rev-parse", "HEAD").decode().strip()
        (self.repo / "readme.md").write_text("Changes outside the module.\n", encoding="utf-8")
        self.git("add", "readme.md")
        self.git("commit", "-qm", "doc change")
        result = self.prepare(base, self.source.parent)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertEqual(self.patch.read_bytes(), b"")

    def test_rejects_unmerged_commit(self):
        self.git("switch", "-qc", "topic")
        topic = self.commit(self.body + "var result = 2\n")
        self.git("switch", "-q", "base")
        self.assertNotEqual(self.prepare(topic).returncode, 0)


if __name__ == "__main__":
    unittest.main()
