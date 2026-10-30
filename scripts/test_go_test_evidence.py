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

    def test_root_cr_scope_rejects_missing_and_skipped_control_fences(self):
        scope=evidence.MANIFEST_ROOT_SCOPE
        names=[scope['parent'],*[scope['parent']+'/'+child for child in scope['children']]]
        events=[dict(Action=action,Package=scope['package'],Test=name) for name in names for action in ('run','pass')]+[dict(Action='pass',Package=scope['package'])]
        self.assertEqual(evidence.validate(self.raw(events),**scope)['children'],6)
        for invalid in (events[:2]+events[4:],[{**e,'Action':'skip'} if e.get('Test')==names[-1] else e for e in events],events+[dict(Action='output',Package=scope['package'],Output='ok (cached)')]):
            with self.assertRaises(ValueError):evidence.validate(self.raw(invalid),**scope)

    def test_template_adoption_scope_requires_atomicity_and_fencing(self):
        scope=evidence.TEMPLATE_ADOPTION_SCOPE
        names=[scope["parent"],*[scope["parent"]+"/"+child for child in scope["children"]]]
        events=[dict(Action=action,Package=scope["package"],Test=name) for name in names for action in ("run","pass")]+[dict(Action="pass",Package=scope["package"])]
        self.assertEqual(evidence.validate(self.raw(events),**scope)["children"],3)
        for invalid in (events[:2]+events[4:],[{**e,"Action":"skip"} if e.get("Test")==names[-1] else e for e in events],events+[dict(Action="output",Package=scope["package"],Output="ok (cached)")]):
            with self.assertRaises(ValueError):evidence.validate(self.raw(invalid),**scope)

    def test_service_reference_candidates_require_every_cas_and_lifecycle_case(self):
        scope = evidence.SERVICE_REFERENCE_CANDIDATES_SCOPE
        names = [scope["parent"], *[scope["parent"] + "/" + child for child in scope["children"]]]
        events = [dict(Action=action, Package=scope["package"], Test=name)
                  for name in names for action in ("run", "pass")]
        events.append(dict(Action="pass", Package=scope["package"]))
        self.assertEqual(evidence.validate(self.raw(events), **scope)["children"], 18)
        for name in names:
            for invalid in ([event for event in events if event.get("Test") != name],
                            [{**event, "Action": "skip"} if event.get("Test") == name else event for event in events]):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    evidence.validate(self.raw(invalid), **scope)
        for invalid in (events + [dict(Action="output", Package=scope["package"], Output="ok (cached)")],
                        events[:-1]):
            with self.assertRaises(ValueError):
                evidence.validate(self.raw(invalid), **scope)

    def test_manifest_control_requires_every_atomic_and_compatibility_case(self):
        scope = evidence.MANIFEST_CONTROL_SCOPE
        names = [scope["parent"], *[scope["parent"] + "/" + child for child in scope["children"]]]
        events = [dict(Action=action, Package=scope["package"], Test=name)
                  for name in names for action in ("run", "pass")]
        events.append(dict(Action="pass", Package=scope["package"]))
        self.assertEqual(evidence.validate(self.raw(events), **scope)["children"], 5)
        for name in names:
            for invalid in ([event for event in events if event.get("Test") != name],
                            [{**event, "Action": "skip"} if event.get("Test") == name else event for event in events]):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    evidence.validate(self.raw(invalid), **scope)
        for invalid in (events[:-1], events + [dict(Action="output", Package=scope["package"], Output="ok (cached)")],
                        [{**event, "Package": "wrong"} for event in events]):
            with self.assertRaises(ValueError):
                evidence.validate(self.raw(invalid), **scope)

    def test_all_required_tests_pass(self):
        self.assertEqual(evidence.validate(self.raw(self.events()))["children"], 16)

    def test_migration_scopes_require_declaration_and_success_evidence(self):
        for scope in (evidence.TEMPLATE_MIGRATION_SCOPE, evidence.MANIFEST_MIGRATION_SCOPE, evidence.INITIALIZATION_SCOPE):
            names=[scope["parent"],*[scope["parent"]+"/"+child for child in scope["children"]]]
            events=[dict(Action=action,Package=scope["package"],Test=name) for name in names for action in ("run","pass")]+[dict(Action="pass",Package=scope["package"])]
            self.assertEqual(evidence.validate(self.raw(events),**scope)["result"],"PASS")
            for invalid in (events[:2]+events[4:],[{**event,"Action":"skip"} if event.get("Test")==names[-1] else event for event in events],events+[dict(Action="output",Package=scope["package"],Output="ok (cached)")]):
                with self.assertRaises(ValueError): evidence.validate(self.raw(invalid),**scope)

    def test_main_chat_scope_requires_a_real_uncached_run(self):
        package = "github.com/opensoha/soha/internal/repository/copilot"
        parent = "TestMainSessionWithPostgres"
        events = [dict(Action=action, Package=package, Test=parent) for action in ("run", "pass")] + [dict(Action="pass", Package=package)]
        scope = dict(package=package, parent=parent, children=[])
        self.assertEqual(evidence.validate(self.raw(events), **scope)["result"], "PASS")
        for invalid in (events[1:], [{**events[0], "Action": "skip"}, *events[1:]], self.events()):
            with self.assertRaises(ValueError): evidence.validate(self.raw(invalid), **scope)

    def test_identity_scope_requires_every_revocation_scenario(self):
        scope = evidence.IDENTITY_SCOPE
        names = [scope['parent'], *[scope['parent'] + '/' + child for child in scope['children']]]
        events = [dict(Action=action, Package=scope['package'], Test=name) for name in names for action in ('run', 'pass')]
        events.append(dict(Action='pass', Package=scope['package']))
        self.assertEqual(evidence.validate(self.raw(events), **scope)['children'], 6)
        for invalid in (events[:2] + events[4:], [{**e, 'Action': 'skip'} if e.get('Test') == names[-1] else e for e in events], self.events()):
            with self.assertRaises(ValueError): evidence.validate(self.raw(invalid), **scope)

    def test_missing_empty_corrupt_process_and_package_only(self):
        for raw, code in (("", 0), ("{broken", 0), ("[]", 0), (self.raw(self.events()), 1), (self.raw([self.events()[-1]]), 0)):
            with self.subTest(raw=raw[:20], code=code), self.assertRaises(ValueError):
                evidence.validate(raw, code)

    def test_kubernetes_import_scope_rejects_missing_or_skipped_identity_checks(self):
        scope = evidence.KUBERNETES_IMPORT_SCOPE
        names = [scope['parent'], *[scope['parent'] + '/' + child for child in scope['children']]]
        events = [dict(Action=action, Package=scope['package'], Test=name) for name in names for action in ('run', 'pass')]
        events.append(dict(Action='pass', Package=scope['package']))
        self.assertEqual(evidence.validate(self.raw(events), **scope)['children'], 14)
        required_new = {'explicit_service_across_environments', 'explicit_service_invalid_rollback', 'import_fences_default_edit_cas', 'helm_explicit_service_mapping', 'service_creation_identity_matches_persistence'}
        self.assertTrue(required_new.issubset(scope['children']))
        for child in required_new:
            full_name = scope['parent'] + '/' + child
            for invalid in ([e for e in events if e.get('Test') != full_name], [{**e, 'Action': 'skip'} if e.get('Test') == full_name else e for e in events]):
                with self.subTest(child=child), self.assertRaises(ValueError): evidence.validate(self.raw(invalid), **scope)
        for invalid in (events[:2] + events[4:], [{**e, 'Action': 'skip'} if e.get('Test') == names[-1] else e for e in events], self.events()):
            with self.assertRaises(ValueError): evidence.validate(self.raw(invalid), **scope)

    def test_relation_rules_scope_rejects_missing_skipped_or_cached_results(self):
        scope = evidence.RELATION_RULES_SCOPE
        names = [scope['parent'], scope['parent'] + '/' + scope['children'][0]]
        events = [dict(Action=action, Package=scope['package'], Test=name) for name in names for action in ('run', 'pass')]
        events.append(dict(Action='pass', Package=scope['package']))
        self.assertEqual(evidence.validate(self.raw(events), **scope)['children'], 1)
        invalids = [events[:2] + events[4:], [{**e, 'Action': 'skip'} if e.get('Test') == names[-1] else e for e in events], events + [dict(Action='output', Package=scope['package'], Output='ok (cached)')]]
        for invalid in invalids:
            with self.assertRaises(ValueError): evidence.validate(self.raw(invalid), **scope)

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
