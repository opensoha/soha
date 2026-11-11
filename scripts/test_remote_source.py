from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest


class RemoteSourceFingerprintTest(unittest.TestCase):
    def test_workspace_files_reload_and_generated_outputs_do_not(self):
        script = (Path(__file__).resolve().parents[1] / "deploy/remote-dev/backend.sh").read_text()
        body = script.split("source_fingerprint() {", 1)[1].split("\n}\n", 1)[0]
        with tempfile.TemporaryDirectory(prefix="soha-remote-source-") as directory:
            root = Path(directory).resolve()
            for name in ["go.mod", "go.sum", "go.work", "go.work.sum", "cmd/server/main.go",
                         "contracts/go.mod", "contracts/gen/go/sohaapi/client.go", "contracts/gen/go/sohaapi/types.go"]:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("initial")
            command = "source_fingerprint() {" + body.replace("/workspace/soha", shlex.quote(str(root))) + "\n}\nsource_fingerprint\n"
            def fingerprint():
                return subprocess.check_output(["sh", "-eu", "-c", command], text=True).strip()
            initial = fingerprint()
            (root / "go.work").write_text("changed source workspace")
            changed_workspace = fingerprint()
            self.assertNotEqual(initial, changed_workspace)
            (root / "go.work.sum").write_text("changed workspace checksums")
            changed_sums = fingerprint()
            self.assertNotEqual(changed_workspace, changed_sums)
            (root / ".tmp").mkdir()
            (root / ".tmp/build.go").write_text("ignored generated file")
            self.assertEqual(changed_sums, fingerprint())


if __name__ == "__main__":
    unittest.main()
