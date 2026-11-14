import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("release_control", Path(__file__).with_name("release-control.py"))
control = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(control)
SHA = "a" * 40


class ReleaseControlTest(unittest.TestCase):
    def setUp(self):
        self.config = json.loads((control.ROOT / ".github/release-components.json").read_text())
        self.release = {"component": "contracts", "repository": "opensoha/soha", "sha": SHA, "tag": "contracts/v0.2.0"}
        self.owner = self.config["components"]["contracts"]
        self.ci = {"id": 1, "head_sha": SHA, "head_branch": "main", "event": "push", "status": "completed", "conclusion": "success"}
        self.ci_runs = [self.ci]
        self.enabled = "true"
        self.environment = patch.dict("os.environ", {self.owner["enableVariable"]: "true"})
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.policy = {"total_count": 1, "branch_policies": [{"name": "contracts/v*", "type": "tag"}]}
        self.legacy_state = "disabled_manually"
        self.active_status = None
        self.tag_sha = SHA

    def api(self, endpoint, paginated=False):
        if "/actions/variables/" in endpoint:
            return {"value": self.enabled}
        if "/git/ref/tags/" in endpoint:
            return {"object": {"type": "commit", "sha": self.tag_sha}}
        if endpoint.endswith("/deployment-branch-policies"):
            return self.policy
        if "/environments/" in endpoint:
            return {"deployment_branch_policy": {"custom_branch_policies": True}}
        if "workflows/ci.yml" in endpoint:
            return {"id": 10}
        if "head_sha=" in endpoint:
            return [{"workflow_runs": self.ci_runs}]
        if "?status=" in endpoint:
            return [{"workflow_runs": [self.ci] if f"status={self.active_status}&" in endpoint else []}]
        return {"id": 20, "state": self.legacy_state}

    def test_exclusive_owner_with_exact_main_ci_passes(self):
        control.check_remote(self.release, self.config, self.api)

    def test_disabled_switch_or_legacy_publisher_fails_closed(self):
        self.enabled = "false"
        with patch.dict("os.environ", {self.owner["enableVariable"]: "false"}), self.assertRaisesRegex(ValueError, "enabled"):
            control.check_remote(self.release, self.config, self.api)
        self.enabled = "true"
        for state in ["active", "disabled_inactivity", None]:
            self.legacy_state = state
            with self.assertRaisesRegex(ValueError, "Legacy publisher"):
                control.check_remote(self.release, self.config, self.api)

    def test_every_nonterminal_legacy_state_blocks(self):
        for status in control.ACTIVE:
            with self.subTest(status=status):
                self.active_status = status
                with self.assertRaisesRegex(ValueError, "still has"):
                    control.check_remote(self.release, self.config, self.api)

    def test_moved_remote_tag_blocks(self):
        self.tag_sha = "b" * 40
        with self.assertRaisesRegex(ValueError, "Remote immutable tag"):
            control.check_remote(self.release, self.config, self.api)

    def test_image_preflight_allows_only_authenticated_missing_manifests(self):
        release = {"component": "agent", "version": "0.2.0"}
        calls = []
        def fetch(url, authorization):
            calls.append(url)
            if "/token?" in url:
                self.assertTrue(authorization.startswith("Basic "))
                return 200, {"token": "fixture"}
            self.assertEqual(authorization, "Bearer fixture")
            return 404, {"errors": [{"code": "MANIFEST_UNKNOWN"}]}
        with patch.dict(os.environ, {"GITHUB_ACTOR": "fixture", "GH_TOKEN": "not-a-secret"}):
            control.check_images_unused(release, self.config, fetch)
        manifests = [url for url in calls if "/manifests/" in url]
        self.assertEqual(len(manifests), 6)
        self.assertIn("https://ghcr.io/v2/opensoha/soha-buildpacks-agent/manifests/daemonless-v0.2.0", manifests)

    def test_occupied_unauthorized_or_unknown_images_block(self):
        release = {"component": "core", "version": "0.1.13"}
        for status, body in [(200, {"schemaVersion": 2}), (401, {}), (404, {}), (404, {"errors": [{"code": "NAME_UNKNOWN"}]})]:
            with self.subTest(status=status, body=body), patch.dict(os.environ, {"GITHUB_ACTOR": "fixture", "GH_TOKEN": "not-a-secret"}):
                def fetch(url, authorization):
                    return (200, {"token": "fixture"}) if "/token?" in url else (status, body)
                with self.assertRaisesRegex(ValueError, "occupied or unverified"):
                    control.check_images_unused(release, self.config, fetch)

    def test_cli_image_has_the_same_immutable_version_preflight(self):
        def fetch(url, authorization):
            if "/token?" in url:
                return 200, {"token": "fixture"}
            self.assertEqual(url, "https://ghcr.io/v2/opensoha/soha-cli/manifests/v0.2.0")
            return 200, {"schemaVersion": 2}
        with patch.dict(os.environ, {"GITHUB_ACTOR": "fixture", "GH_TOKEN": "not-a-secret"}):
            with self.assertRaisesRegex(ValueError, "occupied or unverified"):
                control.check_images_unused({"component": "cli", "version": "0.2.0"}, self.config, fetch)

    def test_old_or_pr_or_failed_ci_cannot_certify_source(self):
        for change in [{"head_sha": "b" * 40}, {"head_branch": "feature"}, {"event": "pull_request"},
                       {"event": "workflow_dispatch"}, {"status": "in_progress"}, {"conclusion": "failure"}]:
            with self.subTest(change=change):
                self.ci_runs = [{**self.ci, **change}]
                with self.assertRaisesRegex(ValueError, "source CI"):
                    control.check_remote(self.release, self.config, self.api)
        self.ci_runs = [self.ci, {**self.ci, "id": 2, "conclusion": "failure"}]
        with self.assertRaisesRegex(ValueError, "source CI"):
            control.check_remote(self.release, self.config, self.api)

    def test_environment_cannot_allow_other_branches_or_tags(self):
        for row in [{"name": "*", "type": "branch"}, {"name": "v*", "type": "tag"}]:
            self.policy = {"total_count": 2, "branch_policies": [row, {"name": "contracts/v*", "type": "tag"}]}
            with self.assertRaisesRegex(ValueError, "outside main"):
                control.check_remote(self.release, self.config, self.api)
        self.policy = {"total_count": 100, "branch_policies": [{"name": "contracts/v*", "type": "tag"}]}
        with self.assertRaisesRegex(ValueError, "outside main"):
            control.check_remote(self.release, self.config, self.api)

    def test_network_state_missing_never_becomes_approval(self):
        with self.assertRaisesRegex(ValueError, "unavailable"):
            control.check_remote(self.release, self.config, lambda *_: (_ for _ in ()).throw(ValueError("unavailable")))

    def test_tag_namespace_sha_and_package_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".github").mkdir()
            (root / ".github/release-components.json").write_text(json.dumps(self.config))
            (root / "contracts").mkdir()
            (root / "contracts/package.json").write_text('{"version":"0.2.0"}')
            result = control.identity("contracts", "contracts/v0.2.0", SHA, root)
            self.assertEqual(result["version"], "0.2.0")
            for tag, sha in [("v0.2.0", SHA), ("cli/v0.2.0", SHA), ("contracts/v0.2.1", SHA),
                             ("contracts/v0.2.0;echo bad", SHA), ("contracts/v0.2.0", "main")]:
                with self.subTest(tag=tag, sha=sha), self.assertRaises(ValueError):
                    control.identity("contracts", tag, sha, root)

    def test_verify_rejects_checkout_or_tag_drift(self):
        release = {**self.release, "tag": "contracts/v0.2.0"}
        with patch.dict("os.environ", {"GITHUB_REPOSITORY": "opensoha/soha"}), patch.object(control, "git", return_value="b" * 40):
            with self.assertRaisesRegex(ValueError, "Checkout differs"):
                control.verify(release)
        with patch.dict("os.environ", {"GITHUB_REPOSITORY": "opensoha/soha"}), patch.object(control, "git", side_effect=[SHA, "b" * 40]):
            with self.assertRaisesRegex(ValueError, "tag differs"):
                control.verify(release)


if __name__ == "__main__":
    unittest.main()
