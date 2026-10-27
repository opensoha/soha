"""Exercise development setup without starting Docker or application processes."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class DevelopmentStartupTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        repo = Path(__file__).resolve().parents[1]
        shutil.copyfile(repo / "Makefile", self.root / "Makefile")
        (self.root / "configs").mkdir()
        (self.root / "configs/config.example.yaml").write_text("database:\n  host: localhost\n")
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        docker = bin_dir / "docker"
        docker.write_text(
            '#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_CALL_LOG"\n'
            'case "$*" in *"up -d"*) exit "${DOCKER_FAIL_UP:-0}" ;; esac\n'
        )
        docker.chmod(0o755)
        self.env = dict(os.environ, PATH=str(bin_dir) + os.pathsep + os.environ["PATH"],
                        DOCKER_CALL_LOG=str(self.root / "docker-calls"))
        self.env.pop("SOHA_CONFIG_FILE", None)
        self.env.pop("DEV_DB_MODE", None)

    def make(self, *args):
        return subprocess.run(["make", "--no-print-directory", *args], cwd=self.root,
                              env=self.env, capture_output=True, text=True, timeout=10)

    def test_external_is_default_and_does_not_call_docker(self):
        result = self.make("init-db")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "docker-calls").exists())
        self.assertEqual((self.root / "configs/config.yaml").read_bytes(),
                         (self.root / "configs/config.example.yaml").read_bytes())

    def test_existing_local_config_is_preserved(self):
        config = self.root / "configs/config.yaml"
        config.write_text("private local settings\n")
        result = self.make("init-config")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config.read_text(), "private local settings\n")

    def test_local_mode_starts_and_checks_postgres(self):
        result = self.make("init-db", "DEV_DB_MODE=local")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = (self.root / "docker-calls").read_text()
        self.assertIn("up -d postgres", calls)
        self.assertIn("exec -T postgres pg_isready", calls)

    def test_local_start_failure_is_not_hidden(self):
        self.env["DOCKER_FAIL_UP"] = "3"
        result = self.make("init-db", "DEV_DB_MODE=local")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("pg_isready", (self.root / "docker-calls").read_text())

    def test_invalid_mode_fails_without_docker(self):
        result = self.make("init-db", "DEV_DB_MODE=invalid")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid DEV_DB_MODE", result.stderr)
        self.assertFalse((self.root / "docker-calls").exists())

    def test_missing_explicit_config_is_not_replaced(self):
        result = self.make("init-config", "SOHA_CONFIG_FILE=custom.yaml")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "custom.yaml").exists())


if __name__ == "__main__":
    unittest.main()
