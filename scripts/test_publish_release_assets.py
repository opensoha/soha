import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlparse

SPEC = importlib.util.spec_from_file_location("publisher", Path(__file__).with_name("publish-release-assets.py"))
publisher = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(publisher)


class GitHubFixture:
    def __init__(self, tag, assets):
        self.releases = [{"id": 42, "tag_name": tag, "draft": True, "prerelease": False, "assets": assets}]
        self.payloads = {asset["id"]: b"\x00accepted\xff" for asset in assets}
        self.calls, self.writes, self.downloads = [], [], []
        self.before_request = lambda method, endpoint: None
        self.after_upload = lambda: None
        self.latest = 7

    def request(self, args):
        assert args[:3] == ["gh", "api", "--method"], args
        method, endpoint = args[3:5]
        if endpoint == "--paginate":
            endpoint = args[-1]
        self.before_request(method, endpoint)
        self.calls.append((method, endpoint))
        fields = {args[i + 1].split("=", 1)[0]: args[i + 1].split("=", 1)[1]
                  for i, arg in enumerate(args) if arg in ("--field", "--raw-field")}
        if method != "GET":
            self.writes.append((method, endpoint, tuple(args)))
        if method == "GET" and endpoint.endswith("?per_page=100"):
            assert "--paginate" in args and "--slurp" in args
            return json.dumps([[], self.releases])
        if method == "POST" and endpoint == "repos/opensoha/soha/releases":
            assert fields["draft"] == "true" and fields["make_latest"] == "false"
            created = {"id": 42, "tag_name": fields["tag_name"], "draft": True, "prerelease": False, "assets": []}
            self.releases.append(created)
            return json.dumps(created)
        if method == "GET" and "/releases/assets/" in endpoint:
            assert "Accept:application/octet-stream" in args
            asset_id = int(endpoint.rsplit("/", 1)[1])
            self.downloads.append(asset_id)
            return self.payloads[asset_id]
        if method == "POST" and endpoint.startswith("https://uploads.github.com/"):
            url = urlparse(endpoint)
            release_id = int(url.path.split("/")[-2])
            selected = next(row for row in self.releases if row["id"] == release_id)
            file = Path(args[args.index("--input") + 1])
            assert "Content-Type:application/octet-stream" in args
            assert f"Content-Length:{file.stat().st_size}" in args
            name = parse_qs(url.query)["name"][0]
            assert not any(asset["name"] == name for asset in selected["assets"])
            asset = {"id": 100 + len(self.payloads), "name": name}
            selected["assets"].append(asset)
            self.payloads[asset["id"]] = file.read_bytes()
            self.after_upload()
            return json.dumps(asset)
        selected_id = int(endpoint.rsplit("/", 1)[1])
        selected = next((row for row in self.releases if row["id"] == selected_id), None)
        if selected is None:
            raise subprocess.CalledProcessError(1, args, stderr="gh: Not Found (HTTP 404)")
        if method == "PATCH":
            assert fields["draft"] == "false" and fields["prerelease"] == "false"
            assert args[args.index("--raw-field") + 1].startswith("make_latest=")
            selected["draft"] = selected["prerelease"] = False
            if fields["make_latest"] == "true":
                self.latest = selected_id
        else:
            assert method == "GET", args
        return json.dumps(selected)

    def run(self, args, **kwargs):
        try:
            body = self.request(args)
        except subprocess.CalledProcessError as error:
            if kwargs.get("check"):
                raise
            return subprocess.CompletedProcess(args, 1, "", error.stderr)
        if "stdout" in kwargs:
            assert kwargs.get("check") is True
            assert "text" not in kwargs
            kwargs["stdout"].write(body)
            return subprocess.CompletedProcess(args, 0)
        return subprocess.CompletedProcess(args, 0, body, "")

    def check_output(self, args, **kwargs):
        return self.request(args)


class PublisherTestCase(unittest.TestCase):
    def setUp(self):
        self.directory = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.file = self.directory / "archive.tgz"
        self.file.write_bytes(b"\x00accepted\xff")
        self.release = {"component": "skills", "repository": "opensoha/soha", "tag": "skills/v0.1.4", "sha": "a" * 40}
        self.api = GitHubFixture(self.release["tag"], [{"id": 101, "name": self.file.name}])
        self.guard = self.enterContext(patch.object(publisher.control, "verify"))
        self.enterContext(patch.object(publisher.subprocess, "run", side_effect=self.api.run))
        self.enterContext(patch.object(publisher.subprocess, "check_output", side_effect=self.api.check_output))


class ImmutableAssetsTest(PublisherTestCase):
    def test_final_publish_never_reselects_another_release_by_tag(self):
        self.release["component"] = "core"
        replacement = {**copy.deepcopy(self.api.releases[0]), "id": 43}
        def introduce_replacement(method, endpoint):
            if method == "PATCH":
                self.api.releases.append(replacement)
        self.api.before_request = introduce_replacement
        publisher.publish(self.release, [self.file], "Soha", latest=True)
        self.assertEqual(self.api.writes[-1][:2], ("PATCH", "repos/opensoha/soha/releases/42"))
        self.assertEqual(self.api.latest, 42)
        self.assertTrue(replacement["draft"])
        self.assertEqual(self.api.downloads, [101, 101])

    def test_new_release_is_published_only_after_verified_upload(self):
        self.api.releases.clear()
        publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual([row[0] for row in self.api.writes], ["POST", "POST", "PATCH"])
        self.assertIn("/releases/42/assets?name=archive.tgz", self.api.writes[1][1])
        self.assertNotIn("--clobber", self.api.writes[1][2])
        self.assertEqual(self.api.latest, 7)
        self.assertEqual(self.guard.call_count, 3)

    def test_existing_identical_assets_are_skipped_and_missing_are_selected(self):
        second = self.directory / "second"
        second.write_bytes(b"second")
        missing = publisher.compare_existing([self.file, second], [{"id": 101, "name": self.file.name}],
                                             lambda asset, out: out.write_bytes(self.file.read_bytes()))
        self.assertEqual(missing, [second])

    def test_any_conflict_blocks_before_create_or_upload(self):
        self.api.payloads[101] = b"different"
        with self.assertRaisesRegex(ValueError, "immutable asset differs"):
            publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual(self.api.writes, [])

    def test_unknown_remote_state_never_creates_release(self):
        with patch.object(publisher, "release_metadata", side_effect=ValueError("unknown")):
            with self.assertRaisesRegex(ValueError, "unknown"):
                publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual(self.api.writes, [])

    def test_component_cannot_replace_root_latest(self):
        with self.assertRaisesRegex(ValueError, "Only Core"):
            publisher.publish(self.release, [self.file], "Skills", latest=True)
        self.assertEqual(self.api.writes, [])


class DraftReleaseTest(PublisherTestCase):
    def test_slash_tag_draft_is_found_on_later_page(self):
        self.assertEqual(publisher.release_metadata("opensoha/soha", self.release["tag"]), self.api.releases[0])
        self.assertEqual(self.api.calls[-1], ("GET", "repos/opensoha/soha/releases/42"))

    def test_complete_and_partial_drafts_reuse_original_assets(self):
        for partial in (False, True):
            with self.subTest(partial=partial):
                self.api.writes.clear(); self.api.downloads.clear()
                second = self.directory / "second"
                second.write_bytes(b"second")
                self.api.releases[0]["assets"] = [{"id": 101, "name": self.file.name}]
                if not partial:
                    self.api.releases[0]["assets"].append({"id": 102, "name": second.name})
                    self.api.payloads[102] = second.read_bytes()
                publisher.publish(self.release, [self.file, second], "Skills")
                self.assertEqual([row[0] for row in self.api.writes], ["POST", "PATCH"] if partial else ["PATCH"])
                self.assertEqual(self.api.writes[-1][1], "repos/opensoha/soha/releases/42")
                self.assertEqual(self.api.latest, 7)
                self.assertEqual(len(self.api.downloads), 3 if partial else 4)

    def test_auth_unknown_and_ambiguous_metadata_never_write(self):
        draft = copy.deepcopy(self.api.releases[0])
        responses = [subprocess.CompletedProcess([], 1, "", f"gh: failed (HTTP {status})") for status in (401, 403, 404, 503)]
        responses += [subprocess.CompletedProcess([], 0, text, "") for text in (
            "not JSON", json.dumps({"unexpected": []}), json.dumps([[{}]]),
            json.dumps([[draft], [{**draft, "id": 43}]]),
        )]
        for response in responses:
            with self.subTest(response=response), patch.object(publisher.subprocess, "run", return_value=response):
                with self.assertRaises(ValueError):
                    publisher.publish(self.release, [self.file], "Skills")
                self.assertEqual(self.api.writes, [])

    def test_selected_release_id_must_keep_exact_identity(self):
        draft = copy.deepcopy(self.api.releases[0])
        variants = [{**draft, "id": 43}, {**draft, "tag_name": "skills/v9.9.9"},
                    {**draft, "prerelease": True}, {**draft, "assets": None}]
        for current in variants:
            responses = [subprocess.CompletedProcess([], 0, json.dumps([[draft]]), ""),
                         subprocess.CompletedProcess([], 0, json.dumps(current), "")]
            with self.subTest(current=current), patch.object(publisher.subprocess, "run", side_effect=responses):
                with self.assertRaises(ValueError):
                    publisher.release_metadata("opensoha/soha", draft["tag_name"])

    def test_selected_release_404_is_unknown_and_never_recreated(self):
        def disappear(method, endpoint):
            if endpoint == "repos/opensoha/soha/releases/42":
                self.api.releases.clear()
        self.api.before_request = disappear
        with self.assertRaises(ValueError):
            publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual(self.api.writes, [])

    def test_newly_uploaded_assets_cannot_add_extras_or_duplicate_names(self):
        for assets in ([], [{"id": 100, "name": self.file.name}, {"id": 102, "name": "extra"}],
                       [{"id": 100, "name": self.file.name}, {"id": 102, "name": self.file.name}]):
            with self.subTest(assets=assets):
                self.api.releases[0]["assets"] = []
                self.api.writes.clear()
                self.api.after_upload = lambda: self.api.releases[0].update(assets=assets)
                with self.assertRaises(ValueError):
                    publisher.publish(self.release, [self.file], "Skills")
                self.assertEqual([row[0] for row in self.api.writes], ["POST"])

    def test_new_core_release_binds_created_id_and_sets_latest_last(self):
        self.release["component"] = "core"
        self.api.releases.clear()
        publisher.publish(self.release, [self.file], "Soha", latest=True)
        self.assertEqual([row[0] for row in self.api.writes], ["POST", "POST", "PATCH"])
        self.assertIn("make_latest=false", self.api.writes[0][2])
        self.assertIn("make_latest=true", self.api.writes[-1][2])
        self.assertEqual(self.api.latest, 42)
        self.assertEqual(self.guard.call_count, 3)

    def test_drift_before_upload_or_publication_never_switches_objects(self):
        original = copy.deepcopy(self.api.releases[0])
        for stage in ("upload", "publish"):
            for drift in ("deleted", "retagged", "replaced", "ambiguous", "id-changed"):
                with self.subTest(stage=stage, drift=drift):
                    self.api.releases[:] = [copy.deepcopy(original)]
                    if stage == "upload":
                        self.api.releases[0]["assets"] = []
                    self.api.writes.clear()
                    self.guard.reset_mock()
                    def guard(_):
                        if self.guard.call_count != (2 if stage == "upload" else 3):
                            return
                        if drift == "deleted":
                            self.api.releases.clear()
                        elif drift == "retagged":
                            self.api.releases[0]["tag_name"] = "skills/v9.9.9"
                        elif drift == "replaced":
                            self.api.releases[0]["tag_name"] = "skills/v9.9.9"
                            self.api.releases.append({**copy.deepcopy(original), "id": 43})
                        elif drift == "ambiguous":
                            self.api.releases.append({**copy.deepcopy(original), "id": 43})
                        else:
                            self.api.releases[0]["id"] = 43
                    self.guard.side_effect = guard
                    with self.assertRaises(ValueError):
                        publisher.publish(self.release, [self.file], "Skills")
                    self.assertEqual(self.api.writes, [])

    def test_upload_id_cannot_be_redirected_by_concurrent_tag_replacement(self):
        self.api.releases[0]["assets"] = []
        replacement = {**copy.deepcopy(self.api.releases[0]), "id": 43}
        def replace_before_upload(method, endpoint):
            if endpoint.startswith("https://uploads.github.com/"):
                self.api.releases[0]["tag_name"] = "skills/v9.9.9"
                self.api.releases.append(replacement)
        self.api.before_request = replace_before_upload
        with self.assertRaisesRegex(ValueError, "replaced"):
            publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual([row[0] for row in self.api.writes], ["POST"])
        self.assertIn("/releases/42/assets?", self.api.writes[0][1])
        self.assertEqual(replacement["assets"], [])

    def test_asset_replacement_after_byte_verification_blocks_publication(self):
        def replace_asset(_):
            if self.guard.call_count == 3:
                self.api.releases[0]["assets"][0]["id"] = 999
        self.guard.side_effect = replace_asset
        with self.assertRaisesRegex(ValueError, "assets changed"):
            publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual(self.api.writes, [])

    def test_creation_response_id_cannot_be_replaced_by_discovered_tag(self):
        self.api.releases.clear()
        def replace_after_create(method, endpoint):
            if method == "GET" and self.api.releases:
                self.api.releases[0]["id"] = 43
        self.api.before_request = replace_after_create
        with self.assertRaisesRegex(ValueError, "replaced"):
            publisher.publish(self.release, [self.file], "Skills")
        self.assertEqual([(row[0], row[1]) for row in self.api.writes], [("POST", "repos/opensoha/soha/releases")])


if __name__ == "__main__":
    unittest.main()
