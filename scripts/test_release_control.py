import importlib.util
import hashlib
import json
import os
from pathlib import Path
import subprocess
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
        self.config.pop("legacyRetirement", None)
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
        self.dependency = {"draft": False, "prerelease": False, "tag_name": "skills/v0.1.4"}

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
        if "/releases/tags/" in endpoint:
            return self.dependency
        if "?status=" in endpoint:
            return [{"workflow_runs": [self.ci] if f"status={self.active_status}&" in endpoint else []}]
        return {"id": 20, "state": self.legacy_state}

    def test_exclusive_owner_with_exact_main_ci_passes(self):
        calls = []
        def api(endpoint, paginated=False):
            calls.append(endpoint)
            return self.api(endpoint, paginated)
        control.check_remote(self.release, self.config, api)
        self.assertTrue(any("repos/opensoha/soha-contracts/" in endpoint for endpoint in calls))

    def test_missing_legacy_repository_is_not_retirement_approval(self):
        def api(endpoint, paginated=False):
            if endpoint.startswith("repos/opensoha/soha-contracts/"):
                raise ValueError("Legacy repository unavailable: 404")
            return self.api(endpoint, paginated)
        with self.assertRaisesRegex(ValueError, "404"):
            control.check_remote(self.release, self.config, api)

    def retirement_fixture(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / ".github").mkdir()
        self.manifest_path = self.root / ".github/legacy-retirement.json"
        self.manifest = {"schemaVersion": 1, "repository": "opensoha/soha",
            "archive": {"repository": "opensoha/soha", "releaseId": 1, "draft": True, "verified": True},
            "publishers": [{"repository": repository, "workflow": "release.yml", "finalHead": SHA,
                "archived": True, "workflowState": "disabled_manually", "activeRunCount": 0,
                "bundle": {"assetId": 2, "size": 100, "sha256": "b" * 64, "path": "history.bundle"},
                "metadata": {"assetId": 2, "size": 100, "sha256": "c" * 64, "path": "metadata.json"},
                "releaseAssets": {"count": 0, "bytes": 0, "manifestSha256": "d" * 64, "verified": True}}
                for repository in control.LEGACY_REPOSITORIES.values()]}
        self.write_retirement()

    def write_retirement(self):
        raw = json.dumps(self.manifest).encode()
        self.manifest_path.write_bytes(raw)
        self.config["legacyRetirement"] = {"manifest": ".github/legacy-retirement.json",
            "sha256": hashlib.sha256(raw).hexdigest()}

    def test_retired_publishers_pass_without_any_legacy_api_reads(self):
        self.retirement_fixture()
        calls = []
        def api(endpoint, paginated=False):
            self.assertTrue(endpoint.startswith("repos/opensoha/soha/"), endpoint)
            calls.append(endpoint)
            return self.api(endpoint, paginated)
        control.check_remote(self.release, self.config, api, root=self.root)
        self.assertTrue(any("head_sha=" in endpoint for endpoint in calls))
        self.assertTrue(any("/environments/" in endpoint for endpoint in calls))

    def test_retirement_does_not_bypass_root_guards(self):
        self.retirement_fixture()
        with patch.dict(os.environ, {self.owner["enableVariable"]: "false"}):
            with self.assertRaisesRegex(ValueError, "enabled"):
                control.check_remote(self.release, self.config, self.api, self.root)
        self.tag_sha = "b" * 40
        with self.assertRaisesRegex(ValueError, "Remote immutable tag"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.tag_sha = SHA
        self.ci_runs = [{**self.ci, "conclusion": "failure"}]
        with self.assertRaisesRegex(ValueError, "source CI"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.ci_runs = [self.ci]
        policy = self.policy
        self.policy = {"total_count": 1, "branch_policies": [{"name": "*", "type": "branch"}]}
        with self.assertRaisesRegex(ValueError, "outside main"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.policy = policy
        self.owner["requiredReleaseTags"] = ["skills/v0.1.4"]
        self.dependency["draft"] = True
        with self.assertRaisesRegex(ValueError, "not public/stable"):
            control.check_remote(self.release, self.config, self.api, self.root)

    def test_missing_tampered_or_invalid_json_retirement_blocks(self):
        self.retirement_fixture()
        self.manifest_path.unlink()
        with self.assertRaisesRegex(ValueError, "regular Root file"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.write_retirement()
        self.manifest_path.write_bytes(self.manifest_path.read_bytes() + b"\n")
        with self.assertRaisesRegex(ValueError, "SHA256 differs"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.manifest_path.write_bytes(b"{invalid")
        self.config["legacyRetirement"]["sha256"] = hashlib.sha256(b"{invalid").hexdigest()
        with self.assertRaisesRegex(ValueError, "Cannot read"):
            control.check_remote(self.release, self.config, self.api, self.root)

    def test_retirement_reference_cannot_escape_root_or_use_symlinks(self):
        self.retirement_fixture()
        for reference in [None, {}, {"manifest": "/tmp/retirement.json", "sha256": "a" * 64},
                          {"manifest": "../.github/legacy-retirement.json", "sha256": "a" * 64},
                          {"manifest": ".github/legacy-retirement.json", "sha256": True},
                          {"manifest": ".github/legacy-retirement.json", "sha256": "bad"}]:
            with self.subTest(reference=reference):
                self.config["legacyRetirement"] = reference
                with self.assertRaisesRegex(ValueError, "manifest reference"):
                    control.check_remote(self.release, self.config, self.api, self.root)
        self.write_retirement()
        target = self.root / "manifest.json"
        self.manifest_path.rename(target)
        self.manifest_path.symlink_to(target)
        with self.assertRaisesRegex(ValueError, "symlink"):
            control.check_remote(self.release, self.config, self.api, self.root)
        self.manifest_path.unlink()
        target.rename(self.manifest_path)
        (self.root / ".github").rename(self.root / "elsewhere")
        (self.root / ".github").symlink_to(self.root / "elsewhere", target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symlink"):
            control.check_remote(self.release, self.config, self.api, self.root)

    def test_incomplete_retirement_records_fail_closed(self):
        self.retirement_fixture()
        original = json.dumps(self.manifest)
        changes = [(("schemaVersion",), True), (("schemaVersion",), 2), (("repository",), "other/soha"),
            (("archive",), None), (("archive", "repository"), "other/soha"),
            (("archive", "releaseId"), True), (("archive", "releaseId"), 0),
            (("archive", "draft"), False), (("archive", "verified"), 1),
            (("publishers",), []), (("publishers",), {}),
            (("publishers", 0), None), (("publishers", 0, "repository"), "other/soha"),
            (("publishers", 0, "workflow"), "other.yml"), (("publishers", 0, "finalHead"), "0" * 40),
            (("publishers", 0, "finalHead"), "main"), (("publishers", 0, "archived"), 1),
            (("publishers", 0, "workflowState"), "active"), (("publishers", 0, "activeRunCount"), 1),
            (("publishers", 0, "activeRunCount"), False), (("publishers", 0, "bundle"), None),
            (("publishers", 0, "metadata"), None), (("publishers", 0, "releaseAssets"), None)]
        for name in ("bundle", "metadata"):
            for field, value in [("assetId", True), ("assetId", 0), ("size", False), ("size", 0),
                                 ("size", -1), ("sha256", "bad"), ("sha256", 123)]:
                changes.append((("publishers", 0, name, field), value))
        for field, value in [("count", True), ("count", -1), ("bytes", False), ("bytes", -1),
                             ("manifestSha256", "bad"), ("verified", 1)]:
            changes.append((("publishers", 0, "releaseAssets", field), value))
        for path, value in changes:
            with self.subTest(path=path, value=value):
                self.manifest = json.loads(original)
                parent = self.manifest
                for key in path[:-1]:
                    parent = parent[key]
                parent[path[-1]] = value
                self.write_retirement()
                with self.assertRaisesRegex(ValueError, "[Ll]egacy retirement"):
                    control.check_remote(self.release, self.config, self.api, self.root)
        self.manifest = json.loads(original)
        self.manifest["publishers"][0] = self.manifest["publishers"][1]
        self.write_retirement()
        with self.assertRaisesRegex(ValueError, "duplicate"):
            control.check_remote(self.release, self.config, self.api, self.root)
        for path in [("archive",), ("publishers", 0, "finalHead"),
                     ("publishers", 0, "metadata"), ("publishers", 0, "releaseAssets", "verified")]:
            with self.subTest(missing=path):
                self.manifest = json.loads(original)
                parent = self.manifest
                for key in path[:-1]:
                    parent = parent[key]
                del parent[path[-1]]
                self.write_retirement()
                with self.assertRaisesRegex(ValueError, "[Ll]egacy retirement"):
                    control.check_remote(self.release, self.config, self.api, self.root)

    def test_retirement_cannot_remove_or_reassign_configured_publishers(self):
        self.retirement_fixture()
        original = json.dumps(self.config)
        for change in ("remove_component", "empty", "duplicate", "wrong_repository", "wrong_workflow"):
            with self.subTest(change=change):
                self.config = json.loads(original)
                owner = self.config["components"]["core"]
                if change == "remove_component":
                    del self.config["components"]["core"]
                elif change == "empty":
                    owner["legacy"] = []
                elif change == "duplicate":
                    owner["legacy"] *= 2
                elif change == "wrong_repository":
                    owner["legacy"][0]["repository"] = "opensoha/soha-contracts"
                else:
                    owner["legacy"][0]["workflow"] = "other.yml"
                with self.assertRaisesRegex(ValueError, "[Ll]egacy"):
                    control.check_remote(self.release, self.config, self.api, self.root)

    def recovery_fixture(self):
        historical = json.dumps(self.config)
        self.retirement_fixture()
        self.source_root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        (self.source_root / ".github").mkdir()
        (self.source_root / ".github/release-components.json").write_text(historical)
        (self.source_root / "contracts").mkdir()
        (self.source_root / "contracts/package.json").write_text('{"version":"0.2.0"}')
        (self.root / ".github/release-components.json").write_text(json.dumps(self.config))
        for root in (self.source_root, self.root):
            subprocess.run(["git", "init", "--quiet", "--initial-branch=main", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), "add", "."], check=True)
            subprocess.run(["git", "-C", str(root), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                            "-c", "commit.gpgSign=false", "commit", "--quiet", "-m", "fixture"], check=True)
            subprocess.run(["git", "-C", str(root), "update-ref", "refs/remotes/origin/main", "HEAD"], check=True)
        self.control_sha = control.git("rev-parse", "HEAD", root=self.root)
        self.tag_sha = control.git("rev-parse", "HEAD", root=self.source_root)
        self.release["sha"] = self.tag_sha
        self.ci["head_sha"] = self.tag_sha
        subprocess.run(["git", "-C", str(self.source_root), "tag", self.release["tag"]], check=True)
        self.enterContext(patch.dict(os.environ, {"GITHUB_REPOSITORY": "opensoha/soha", "GITHUB_EVENT_NAME": "workflow_dispatch",
            "GITHUB_REF": "refs/heads/main", "GITHUB_SHA": self.control_sha, "NPM_PUBLISH_MODE": "recover",
            "GITHUB_WORKFLOW_REF": "opensoha/soha/.github/workflows/contracts-release.yml@refs/heads/main"}))
        self.control_ci = {**self.ci, "head_sha": self.control_sha}
        self.api_calls = []
        original_run = subprocess.run
        def run(args, **kwargs):
            if args[0] != "gh":
                return original_run(args, **kwargs)
            endpoint = args[-1]
            self.assertTrue(endpoint.startswith("repos/opensoha/soha/"), endpoint)
            self.api_calls.append(endpoint)
            body = ([{"workflow_runs": [self.control_ci]}] if f"head_sha={self.control_sha}" in endpoint
                    else self.api(endpoint, "--paginate" in args))
            return subprocess.CompletedProcess(args, 0, json.dumps(body), "")
        self.enterContext(patch.object(control.subprocess, "run", side_effect=run))

    def verify_recovery(self):
        control.verify(self.release, root=self.source_root, policy_root=self.root, control_sha=self.control_sha)

    def test_recovery_uses_current_policy_and_unchanged_historical_source(self):
        self.recovery_fixture()
        self.assertNotIn("legacyRetirement", json.loads((self.source_root / ".github/release-components.json").read_text()))
        release = control.identity("contracts", self.release["tag"], self.tag_sha, self.source_root, self.root)
        self.assertEqual(release["version"], "0.2.0")
        self.verify_recovery()
        for sha in (self.tag_sha, self.control_sha):
            self.assertTrue(any(f"head_sha={sha}" in endpoint for endpoint in self.api_calls))
        self.assertEqual(control.git("diff", "--name-only", root=self.source_root), "")

    def test_recovery_rejects_untrusted_context_or_control_identity(self):
        self.recovery_fixture()
        for key, value in [("GITHUB_REPOSITORY", "other/soha"), ("GITHUB_REF", "refs/tags/contracts/v0.2.0"),
                           ("GITHUB_EVENT_NAME", "push"), ("NPM_PUBLISH_MODE", "stage"),
                           ("GITHUB_WORKFLOW_REF", "opensoha/soha/.github/workflows/other.yml@refs/heads/main"),
                           ("GITHUB_SHA", "b" * 40)]:
            with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ValueError):
                self.verify_recovery()
        with self.assertRaisesRegex(ValueError, "exact Root main"):
            control.verify(self.release, root=self.source_root, policy_root=self.root)
        actual = control.git
        def drift(*args, root):
            if root == self.root and args == ("rev-parse", "HEAD"):
                return "b" * 40
            return actual(*args, root=root)
        with patch.object(control, "git", side_effect=drift), self.assertRaisesRegex(ValueError, "Control checkout differs"):
            self.verify_recovery()

    def test_recovery_checks_both_source_and_control_ci(self):
        self.recovery_fixture()
        for target in (self.ci, self.control_ci):
            for field, value in [("head_branch", "feature"), ("event", "pull_request"), ("conclusion", "failure")]:
                with self.subTest(control=target is self.control_ci, field=field):
                    previous = target[field]
                    target[field] = value
                    with self.assertRaisesRegex(ValueError, "main source CI"):
                        self.verify_recovery()
                    target[field] = previous

    def test_recovery_rejects_source_sha_tag_or_dirty_control_drift(self):
        self.recovery_fixture()
        self.release["sha"] = "b" * 40
        with self.assertRaisesRegex(ValueError, "Checkout differs"):
            self.verify_recovery()
        self.release["sha"] = self.tag_sha
        actual = control.git
        def tag_drift(*args, root):
            if root == self.source_root and args == ("rev-parse", "refs/tags/contracts/v0.2.0^{commit}"):
                return "b" * 40
            return actual(*args, root=root)
        with patch.object(control, "git", side_effect=tag_drift), self.assertRaisesRegex(ValueError, "tag differs"):
            self.verify_recovery()
        source = self.source_root / "contracts/package.json"
        original = source.read_bytes()
        source.write_bytes(original + b"\n")
        with self.assertRaisesRegex(ValueError, "Tracked source changed"):
            self.verify_recovery()
        source.write_bytes(original)
        (self.root / ".github/legacy-retirement.json").write_text("changed")
        with self.assertRaisesRegex(ValueError, "Tracked release controls changed"):
            self.verify_recovery()

    def test_recovery_requires_completed_retirement_without_live_fallback(self):
        self.recovery_fixture()
        for missing in (False, True):
            config = json.loads(json.dumps(self.config))
            if missing:
                del config["legacyRetirement"]
            else:
                self.manifest["publishers"][0]["archived"] = False
                self.write_retirement()
                config = self.config
            (self.root / ".github/release-components.json").write_text(json.dumps(config))
            subprocess.run(["git", "-C", str(self.root), "add", ".github"], check=True)
            subprocess.run(["git", "-C", str(self.root), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
                            "-c", "commit.gpgSign=false", "commit", "--quiet", "-m", "incomplete retirement"], check=True)
            self.control_sha = control.git("rev-parse", "HEAD", root=self.root)
            self.control_ci["head_sha"] = self.control_sha
            os.environ["GITHUB_SHA"] = self.control_sha
            subprocess.run(["git", "-C", str(self.root), "update-ref", "refs/remotes/origin/main", "HEAD"], check=True)
            with self.subTest(missing=missing), self.assertRaisesRegex(ValueError, "[Ll]egacy retirement"):
                self.verify_recovery()

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
        self.assertEqual(len(manifests), 5)
        self.assertFalse(any("soha-hermes-agent" in url for url in manifests))
        self.assertIn("https://ghcr.io/v2/opensoha/soha-buildpacks-agent/manifests/v0.2.0", manifests)
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

    def test_only_explicit_first_release_can_create_a_new_image_repository(self):
        config = json.loads(json.dumps(self.config))
        config["components"]["agent"]["images"] = [
            image for image in config["components"]["agent"]["images"] if image.get("firstRelease")
        ]
        self.assertEqual(len(config["components"]["agent"]["images"]), 2)
        def fetch(url, authorization):
            return (200, {"token": "fixture"}) if "/token?" in url else (404, {"errors": [{"code": "NAME_UNKNOWN"}]})
        with patch.dict(os.environ, {"GITHUB_ACTOR": "fixture", "GH_TOKEN": "not-a-secret"}):
            control.check_images_unused({"component": "agent", "version": "0.2.0"}, config, fetch)
            with self.assertRaisesRegex(ValueError, "occupied or unverified"):
                control.check_images_unused({"component": "agent", "version": "0.2.1"}, config, fetch)

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
