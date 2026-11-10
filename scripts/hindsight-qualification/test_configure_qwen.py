"""Offline narrow ownership, configuration preservation and bounded probe checks."""
from copy import deepcopy
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

from configure_qwen import ACTION, EMBED, MODEL, UPSTREAM, cleanup_configuration, credential, fingerprint, format_unsupported, main, probe, route_fingerprint, run, upstream_input, validate_completion


class QwenConfigurationTests(unittest.TestCase):
    def test_route_receipt_exact_pgx_microseconds_preserves_policy_checks(self):
        receipt = {'id': 'owned', 'createdAt': '2026-11-08T17:24:46.123456789Z', 'updatedAt': '2026-11-08T17:24:46.123456789Z', 'cachePolicy': {}}
        stored = dict(receipt, createdAt='2026-11-08T17:24:46.123456Z', updatedAt='2026-11-08T17:24:46.123456Z')
        before = deepcopy(receipt)
        self.assertEqual(route_fingerprint(receipt), route_fingerprint(stored))
        self.assertNotEqual(route_fingerprint(receipt), route_fingerprint(dict(stored, updatedAt='2026-11-08T17:24:46.123457Z')))
        self.assertNotEqual(route_fingerprint(receipt), route_fingerprint(dict(stored, cachePolicy={'enabled': True})))
        self.assertEqual(receipt, before)

    def test_recorded_unknown_resume_cannot_create_or_refresh_again(self):
        history = {'configuration_probes': [{'action_key': ACTION, 'upstream': {'update_http': 200}},
            {'action_key': ACTION, 'resume_from': {'route_id': 'same-owned-id'}, 'configuration_cleanup': {'unknown_route_create': True, 'requires_followup': True}}]}
        with tempfile.TemporaryDirectory(prefix='task123-a08-history-') as root:
            path = Path(root)/'evidence.json'
            path.write_text(json.dumps(history))
            with patch.object(sys, 'argv', ['configure_qwen.py', '--execute', '--resume-known-append', '--evidence', str(path)]), patch('configure_qwen.api') as api, patch('configure_qwen.subprocess.run') as refresh, redirect_stdout(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    main()
                self.assertEqual(error.exception.code, 1)
                api.assert_not_called()
                refresh.assert_not_called()
            self.assertEqual(json.loads(path.read_text())['configuration_probes'][-1]['status'], 'FAIL')

    def original(self):
        return {'id': UPSTREAM, 'name': 'existing', 'providerKind': 'qwen', 'baseUrl': 'https://synthetic.invalid/v1',
            'apiKeyPrefix': 'synthetic-prefix', 'status': 'active', 'supportedModels': [EMBED], 'weight': 1,
            'priority': 0, 'timeoutSeconds': 120, 'streamTimeoutSeconds': 300, 'maxConcurrency': 2,
            'health': {'consecutiveFailures': 0}, 'createdBy': 'owner', 'createdAt': 'before', 'updatedAt': 'before'}

    def test_full_put_omits_key_and_preserves_fields(self):
        original = self.original()
        calls = []
        state = {'upstream': deepcopy(original), 'routes': []}
        def api(g, method, path, payload=None, **kwargs):
            calls.append((method, path, deepcopy(payload)))
            if method == 'GET':
                return 200, {'items': [deepcopy(state['upstream'])] if path == '/relay/upstreams' else deepcopy(state['routes'])}
            if method == 'PUT':
                state['upstream'].update(dict(payload, supportedModels=sorted(set(payload['supportedModels']))))
                state['upstream']['updatedAt'] = 'after'
                return 200, {'data': deepcopy(state['upstream'])}
            if method == 'POST' and path == '/relay/model-routes':
                state['routes'].append(deepcopy(payload))
                return 201, {'data': deepcopy(payload)}
            self.fail('unexpected write')
        report = {'status': 'FAIL'}
        with patch('configure_qwen.api', side_effect=api), patch('configure_qwen.probe', side_effect=lambda g, r: r.update(capability='PASS')):
            run(object(), report)
        self.assertEqual(report['status'], 'PASS')
        writes = [c for c in calls if c[0] != 'GET']
        self.assertEqual(len(writes), 2)
        self.assertEqual(writes[0][2], dict(upstream_input(original), supportedModels=sorted([EMBED, MODEL])))
        self.assertNotIn('apiKey', writes[0][2])
        self.assertEqual(writes[1][2]['metadata']['action_key'], ACTION)
        self.assertTrue(writes[1][2]['metadata']['nonDefault'])

    def test_concurrent_snapshot_or_existing_route_never_writes(self):
        original = self.original()
        changed = dict(original, maxConcurrency=3)
        with patch('configure_qwen.inventory', side_effect=[(original, []), (changed, [])]), patch('configure_qwen.api') as api:
            with self.assertRaises(AssertionError):
                run(object(), {'status': 'FAIL'})
            api.assert_not_called()

    def test_resume_known_append_skips_put_and_reuses_original_identity(self):
        original = self.original()
        current = dict(original, supportedModels=sorted([EMBED, MODEL]), updatedAt='2026-11-08T17:24:46Z')
        route = {'id': 'original-allocated-route', 'publicModel': MODEL, 'upstreamModel': MODEL, 'upstreamId': UPSTREAM,
            'providerKind': 'qwen', 'enabled': True, 'metadata': {'action_key': ACTION, 'owner_nonce': 'original-nonce', 'nonDefault': True}}
        previous = {'action_key': ACTION, 'status': 'FAIL', 'started_utc': '2026-11-08T17:24:45+00:00', 'finished_utc': '2026-11-08T17:24:47+00:00',
            'upstream': {'update_http': 200, 'original_models': [EMBED], 'new_models': [EMBED, MODEL],
                'original_field_hashes': {k: fingerprint(v) for k, v in upstream_input(original).items()}}, 'route': route}
        state = {'routes': []}
        writes = []
        def api(g, method, path, payload=None):
            writes.append((method, path, deepcopy(payload)))
            self.assertEqual((method, path), ('POST', '/relay/model-routes'))
            state['routes'] = [deepcopy(payload)]
            return 201, {'data': deepcopy(payload)}
        report = {'status': 'FAIL'}
        with patch('configure_qwen.inventory', side_effect=lambda g: (deepcopy(current), deepcopy(state['routes']))), patch('configure_qwen.api', side_effect=api), patch('configure_qwen.probe'):
            run(object(), report, previous=previous)
        self.assertEqual(report['status'], 'PASS')
        self.assertEqual(len(writes), 1)
        self.assertEqual(writes[0][2], route)
        self.assertTrue(report['resume_from']['skip_upstream_put'])
        for changed in (dict(current, health={'consecutiveFailures': 1}), dict(current, updatedAt='2026-11-08T17:25:00Z')):
            report = {'status': 'FAIL'}
            with patch('configure_qwen.inventory', return_value=(changed, [])), patch('configure_qwen.api') as api:
                with self.assertRaises(AssertionError):
                    run(object(), report, previous=previous)
                api.assert_not_called()
            self.assertTrue(report['configuration_cleanup']['requires_followup'])
        for changed in (dict(original, metadata={'masked': '[REDACTED]'}), dict(original, health={'lastErrorMessage': '[REDACTED]'}), dict(original, proxyUrl='http://synthetic.invalid')):
            with patch('configure_qwen.inventory', return_value=(changed, [])), patch('configure_qwen.api') as api:
                with self.assertRaises(AssertionError):
                    run(object(), {'status': 'FAIL'})
                api.assert_not_called()
        with patch('configure_qwen.inventory', return_value=(original, [{'publicModel': MODEL}])), patch('configure_qwen.api') as api:
            with self.assertRaises(AssertionError):
                run(object(), {'status': 'FAIL'})
            api.assert_not_called()

    def test_cleanup_does_not_delete_changed_owner_or_other_references(self):
        original = self.original()
        wanted = dict(original, supportedModels=[EMBED, MODEL])
        route = {'id': 'owned', 'publicModel': MODEL, 'metadata': {'action_key': ACTION}}
        for routes in ([dict(route, metadata={'action_key': 'other'})], [{'id': 'other', 'publicModel': MODEL}]):
            report = {'upstream': {'update_http': 200}}
            with patch('configure_qwen.inventory', return_value=(wanted, routes)), patch('configure_qwen.api') as api:
                cleanup_configuration(object(), report, original, wanted, route, True, True, deepcopy(route))
                api.assert_not_called()
            self.assertTrue(report['configuration_cleanup']['requires_followup'])
        with patch('configure_qwen.inventory', side_effect=[(wanted, []), (wanted, [{'id': 'new-other', 'upstreamModel': MODEL}])]), patch('configure_qwen.api') as api:
            cleanup_configuration(object(), {'upstream': {'update_http': 200}}, original, wanted, route, True, True, deepcopy(route))
            api.assert_not_called()

    def test_exact_owned_failure_cleans_route_then_only_appended_model(self):
        original = self.original()
        current = dict(original, supportedModels=[EMBED, MODEL])
        route = {'id': 'owned', 'publicModel': MODEL, 'metadata': {'action_key': ACTION}}
        state = {'upstream': current, 'routes': [route]}
        writes = []
        def api(g, method, path, payload=None):
            writes.append((method, path, payload))
            if method == 'DELETE':
                state['routes'] = []
                return 200, {}
            self.assertEqual(method, 'PUT')
            state['upstream'] = dict(current, **payload)
            return 200, {}
        report = {'upstream': {'update_http': 200}}
        with patch('configure_qwen.inventory', side_effect=lambda g: (deepcopy(state['upstream']), deepcopy(state['routes']))), patch('configure_qwen.api', side_effect=api):
            cleanup_configuration(object(), report, original, current, route, True, True, deepcopy(route))
        self.assertEqual([m for m, _, _ in writes], ['DELETE', 'PUT'])
        self.assertEqual(writes[-1][2]['supportedModels'], [EMBED])
        self.assertFalse(report['configuration_cleanup']['requires_followup'])

    def test_unknown_writes_never_replay_or_claim_absent_means_settled(self):
        original = self.original()
        wanted = dict(original, supportedModels=[EMBED, MODEL])
        route = {'id': 'owned', 'publicModel': MODEL, 'metadata': {'action_key': ACTION}}
        report = {'upstream': {'update_http': 200}}
        with patch('configure_qwen.inventory', return_value=(wanted, [])), patch('configure_qwen.api') as api:
            cleanup_configuration(object(), report, original, wanted, route, True, False, None)
            api.assert_not_called()
        self.assertTrue(report['configuration_cleanup']['unknown_route_create'])
        changed = dict(route, cachePolicy={'enabled': True})
        for receipt in (None, deepcopy(route)):
            report = {'upstream': {'update_http': 200}}
            with patch('configure_qwen.inventory', return_value=(wanted, [changed])), patch('configure_qwen.api') as api:
                cleanup_configuration(object(), report, original, wanted, route, True, False, receipt)
                api.assert_not_called()
            self.assertTrue(report['configuration_cleanup']['requires_followup'])
        for status in (None, 500, 408):
            report = {'upstream': {'update_http': status}}
            with patch('configure_qwen.inventory', return_value=(original, [])), patch('configure_qwen.api') as api:
                cleanup_configuration(object(), report, original, wanted, route, False, False, None)
                api.assert_not_called()
            self.assertTrue(report['configuration_cleanup']['requires_followup'])

    def test_probe_requires_exact_fact_and_only_explicit_format_fallback(self):
        good = {'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps({'facts': [{'subject': 'HQ_CANARY_A08', 'preference': 'cobalt tea'}]})}}]}
        self.assertEqual(validate_completion(good)['facts'][0]['preference'], 'cobalt tea')
        bad = deepcopy(good)
        bad['choices'][0]['message']['content'] = '{"facts":[{"subject":"HQ_CANARY_A08","preference":"jasmine tea"}]}'
        with self.assertRaises(AssertionError):
            validate_completion(bad)
        self.assertTrue(format_unsupported(400, {'error': {'message': 'response_format json_schema is not supported'}}))
        for status in (401, 403, 429, 500):
            self.assertFalse(format_unsupported(status, {'error': {'message': 'json_schema unsupported'}}))
        self.assertFalse(format_unsupported(400, {'error': {'message': 'invalid model'}}))
        from contextlib import nullcontext
        gateway = type('Gateway', (), {'base': 'http://127.0.0.1:1/api/v1/ai-gateway'})()
        with patch('configure_qwen.credential', return_value=nullcontext('synthetic')), patch('configure_qwen.api', side_effect=[(400, {'error': {'message': 'json_schema not supported'}}), (200, good)]) as api:
            report = {}
            probe(gateway, report)
            self.assertEqual(api.call_count, 2)
            self.assertEqual(report['capability'], 'PASS')
            self.assertTrue(all(c.args[3]['max_tokens'] == 256 and 'reasoning_effort' not in c.args[3] for c in api.call_args_list))

    def test_known_pat_revokes_even_if_probe_fails_without_secret_log(self):
        calls = []
        def api(g, method, path, payload=None):
            calls.append((method, path, payload))
            if method == 'POST' and path == '/personal-access-tokens':
                self.assertEqual(payload['metadata']['allowedModels'], [MODEL, EMBED])
                self.assertEqual(payload['metadata']['allowedProviderKinds'], ['qwen'])
                return 201, {'data': {'token': {'id': 'owned-token', 'permissionKeys': ['ai.gateway.relay.invoke']}, 'value': 'synthetic-secret'}}
            if method == 'POST':
                return 200, {}
            return 200, {'items': [{'id': 'owned-token', 'revokedAt': 'now'}]}
        report = {}
        with patch('configure_qwen.api', side_effect=api):
            with self.assertRaises(RuntimeError):
                with credential(object(), report):
                    raise RuntimeError('synthetic probe failure')
        self.assertEqual([p for _, p, _ in calls][-2:], ['/personal-access-tokens/owned-token/revoke', '/personal-access-tokens'])
        self.assertTrue(report['credential']['revoked_confirmed'])
        self.assertNotIn('synthetic-secret', json.dumps(report))


if __name__ == '__main__':
    unittest.main()
