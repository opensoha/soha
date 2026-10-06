import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("evidence", Path(__file__).with_name("check-go-test-evidence.py"))
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class EvidenceTests(unittest.TestCase):
    def test_cleanup_requires_exact_container_run_and_image(self):
        spec = importlib.util.spec_from_file_location("pg_runner", Path(__file__).with_name("run-deletion-integration.py"))
        runner = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(runner)
        owned = {"Id": "exact-id", "Config": {"Image": runner.IMAGE, "Labels": {"soha.test.run": "exact-run"}}}
        runner.check_owner(owned, "exact-id", "exact-run")
        for info, expected_id, expected_run in (
            (owned, "other-id", "exact-run"), (owned, "exact-id", "other-run"),
            ({**owned, "Config": {**owned["Config"], "Image": "shared-image"}}, "exact-id", "exact-run"),
        ):
            with self.assertRaises(ValueError): runner.check_owner(info, expected_id, expected_run)

    def events(self):
        names = [evidence.PARENT, *[evidence.PARENT + "/" + name for name in evidence.CHILDREN]]
        return [dict(Action=action, Package=evidence.PACKAGE, Test=name) for name in names for action in ("run", "pass")] + [dict(Action="pass", Package=evidence.PACKAGE)]

    def raw(self, events):
        return "\n".join(json.dumps(e) for e in events)

    def test_all_required_tests_pass(self):
        self.assertEqual(evidence.validate(self.raw(self.events()))["children"], 16)

    def test_missing_empty_corrupt_process_and_package_only(self):
        for raw, code in (("", 0), ("{broken", 0), ("[]", 0), (self.raw(self.events()), 1), (self.raw([self.events()[-1]]), 0)):
            with self.subTest(raw=raw[:20], code=code), self.assertRaises(ValueError):
                evidence.validate(raw, code)

    def test_child_skip_fail_missing_or_incomplete(self):
        for action in ("skip", "fail", "run"):
            events = self.events()
            events[3]["Action"] = action
            with self.subTest(action=action), self.assertRaises(ValueError):
                evidence.validate(self.raw(events))
        with self.assertRaises(ValueError):
            evidence.validate(self.raw(self.events()[2:]))

    def test_package_truncated_cached_wrong_scope_and_nested_skip(self):
        for events in (
            self.events()[:-1],
            self.events() + [dict(Action="output", Package=evidence.PACKAGE, Output="ok (cached)")],
            [{**e, "Package": "wrong"} for e in self.events()],
            self.events() + [dict(Action="skip", Package=evidence.PACKAGE, Test=evidence.PARENT + "/extra/child")],
        ):
            with self.assertRaises(ValueError):
                evidence.validate(self.raw(events))


if __name__ == "__main__":
    unittest.main()
