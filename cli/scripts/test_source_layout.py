"""Offline source-layout contracts; these checks never build or publish images."""
import json
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SourceLayoutTest(unittest.TestCase):
    def test_go_images_use_same_root_workspace_and_preserve_binary_identities(self):
        paths = ("cli/Dockerfile", "agent/deploy/Dockerfile", "agent/deploy/Dockerfile.hermes-agent-runner")
        for name in paths:
            with self.subTest(dockerfile=name):
                text = (ROOT / name).read_text()
                for source in ("COPY go.mod go.sum go.work go.work.sum ./",
                               "COPY contracts/go.mod contracts/go.sum ./contracts/",
                               "COPY cli/go.mod cli/go.sum ./cli/", "COPY agent/go.mod agent/go.sum ./agent/",
                               "COPY contracts/ /src/contracts/", "GOWORK=/src/go.work"):
                    self.assertIn(source, text)
                self.assertNotIn("GOWORK=off", text)
                self.assertNotIn("COPY --from=contracts", text)
                self.assertIn("golang:1.27.4", text)
                owner = "soha-cli/internal/sohacli.commit" if name.startswith("cli/") else "soha-agent/internal/agent/buildinfo.Commit"
                self.assertIn("github.com/opensoha/" + owner + "=${COMMIT}", text)
        agent = (ROOT / "agent/deploy/Dockerfile").read_text()
        for target in ("volume-reader-runtime", "buildpacks-runtime", "buildpacks-daemonless-runtime", "agent-runtime"):
            self.assertIn(" AS " + target, agent)
        self.assertIn('io.opensoha.volume-reader-protocol="docker-runtime-read-v1"', agent)

    def test_make_dry_run_uses_root_context_real_root_commit_and_explicit_declared_mode(self):
        head = subprocess.check_output(["git", "-C", str(ROOT), "rev-parse", "HEAD"], text=True).strip()
        for component in ("cli", "agent"):
            output = subprocess.check_output(["make", "--no-print-directory", "-n", "-C", str(ROOT / component),
                                              "build", "test", "test-declared", "verify-image" if component == "cli" else "verify-images"], text=True)
            with self.subTest(component=component):
                self.assertIn("GOWORK=" + str(ROOT / "go.work"), output)
                self.assertIn("GOWORK=off", output)
                self.assertIn("--build-arg COMMIT=" + head, output)
                self.assertIn("--build-arg DATE=", output)
                self.assertIn(str(ROOT / component / ("Dockerfile" if component == "cli" else "deploy/Dockerfile")), output)
                self.assertNotIn("--build-context", output)
                self.assertNotIn("../soha-contracts", output)
                self.assertNotIn(".Version=dev", output)
                self.assertNotIn(".version=dev", output)

    def test_dsh_keeps_node24_private_lock_and_same_root_schema(self):
        docker = (ROOT / "agent/deploy/Dockerfile.dsh-runtime").read_text()
        self.assertEqual(docker.count("FROM node:24.14.0-bookworm-slim"), 2)
        self.assertIn("agent/runtimes/dsh/package-lock.json", docker)
        self.assertIn("COPY contracts/agent/dsh-runtime.schema.json /app/contracts/agent/dsh-runtime.schema.json", docker)
        self.assertNotIn("--from=contracts", docker)
        package = json.loads((ROOT / "agent/runtimes/dsh/package.json").read_text())
        self.assertEqual(package["engines"]["node"], ">=24.9.0")
        self.assertTrue((ROOT / "agent/runtimes/dsh/package-lock.json").is_file())
        service = (ROOT / "agent/runtimes/dsh/service.mjs").read_text()
        self.assertIn("../../../contracts/agent/dsh-runtime.schema.json", service)
        self.assertNotIn("../../../soha-contracts", service)

    def test_launcher_package_remains_dependency_free_without_install_hooks(self):
        package = json.loads((ROOT / "cli/npm/cli/package.json").read_text())
        self.assertEqual(package["name"], "@opensoha/cli")
        self.assertFalse(package.get("dependencies"))
        self.assertFalse({"preinstall", "install", "postinstall"} & set(package.get("scripts", {})))
        self.assertEqual(set(package["files"]), {"bin", "lib", "README.md"})


if __name__ == "__main__":
    unittest.main()
