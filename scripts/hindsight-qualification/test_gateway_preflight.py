"""Offline guard and PAT lifecycle checks; no credentials or network."""
from argparse import Namespace
import unittest

from gateway_preflight import Gateway, run


class FakeGateway:
    def __init__(self, failure=None):
        self.calls = []
        self.failure = failure
        self.created = False
        self.revoked = False

    def call(self, path, payload=None, token=None):
        self.calls.append((path, payload, token))
        if path == '/relay/model-routes':
            return 200, {'data': {'items': [{'enabled': True, 'providerKind': 'openai', 'publicModel': 'gpt-6-luna'}]}}
        if path == '/relay/upstreams':
            return 200, {'items': [{'id': 'existing-upstream', 'status': 'active', 'providerKind': 'openai'}]}
        if path == '/relay/upstreams/existing-upstream/test':
            return 200, {'data': {'status': 'ok', 'httpStatus': 200, 'models': ['embedding-fixture'], 'modelCount': 1}}
        if path == '/personal-access-tokens' and payload:
            self.created = True
            self.name = payload['name']
            if self.failure == 'lost-create':
                raise TimeoutError('synthetic lost receipt')
            return 200, {'data': {'token': {'id': 'owned-test-id', 'permissionKeys': ['ai.gateway.relay.invoke']}, 'value': 'synthetic-secret'}}
        if path == '/personal-access-tokens':
            return 200, {'items': [{'id': 'owned-test-id', 'name': self.name, 'revokedAt': 'now' if self.revoked else None}] if self.created else []}
        if path.endswith('/revoke'):
            self.revoked = True
            return 200, {'status': 'ok'}
        if path == '/llm/openai/v1/models':
            return 200, {'data': [{'id': 'gpt-6-luna'}]}
        if path == '/llm/openai/v1/embeddings':
            assert token == 'synthetic-secret'
            return 200, {'data': [{'embedding': [0.1, 0.2]}], 'usage': {'total_tokens': 2}}
        if path == '/llm/openai/v1/chat/completions':
            assert token == 'synthetic-secret'
            assert payload['max_completion_tokens'] == 160
            if self.failure == 'relay':
                return 403, {'error_code': 'access_denied'}
            return 200, {'choices': [{'finish_reason': 'stop', 'message': {'content': '{"facts":[{"what":"喜欢茉莉茶","when":"N/A","where":"N/A","who":"N/A","why":"N/A","fact_type":"world","entities":[]}]}'}}], 'usage': {'total_tokens': 100}}
        raise AssertionError(path)


class PreflightTests(unittest.TestCase):
    def args(self, **kwargs):
        return Namespace(invoke=kwargs.get('invoke', True), chat_model='gpt-6-luna', embedding_model=kwargs.get('embedding_model'))

    def test_inventory_has_no_mutation_or_relay(self):
        gateway, report = FakeGateway(), {}
        run(gateway, self.args(invoke=False), report)
        self.assertEqual(report['status'], 'INVENTORY_ONLY')
        self.assertEqual(len(gateway.calls), 1)

    def test_missing_embedding_route_creates_nothing(self):
        gateway, report = FakeGateway(), {}
        run(gateway, self.args(embedding_model='unconfigured-model'), report)
        self.assertEqual(report['status'], 'FAIL')
        self.assertEqual(len(gateway.calls), 1)

    def test_success_and_relay_failure_both_revoke(self):
        for failure in (None, 'relay'):
            with self.subTest(failure=failure):
                gateway, report = FakeGateway(failure), {}
                run(gateway, self.args(), report)
                self.assertEqual(report['status'], 'PREFLIGHT_PASS' if failure is None else 'FAIL')
                self.assertTrue(report['revocation']['confirmed'])
                self.assertFalse(report['cleanup_requires_followup'])
                self.assertEqual(sum(p.endswith('chat/completions') for p, _, _ in gateway.calls), 1)
                self.assertNotIn('synthetic-secret', str(report))
                create = gateway.calls[1][1]
                self.assertEqual(create['metadata']['allowedModels'], ['gpt-6-luna'])
                self.assertEqual(create['permissionKeys'], ['ai.gateway.relay.invoke'])

    def test_known_id_revoke_precedes_failing_first_cleanup_list(self):
        for failure in ('http500', 'network'):
            with self.subTest(failure=failure):
                class FailingList(FakeGateway):
                    def call(self, path, payload=None, token=None):
                        if path == '/personal-access-tokens' and payload is None:
                            self.calls.append((path, payload, token))
                            if failure == 'network':
                                raise TimeoutError('synthetic list failure')
                            return 500, {}
                        return super().call(path, payload, token)
                gateway, report = FailingList(), {}
                with self.assertRaises((AssertionError, TimeoutError)):
                    run(gateway, self.args(), report)
                self.assertTrue(gateway.revoked)
                self.assertEqual(sum(path.endswith('/revoke') for path, _, _ in gateway.calls), 1)
                cleanup = [path for path, payload, _ in gateway.calls if path.endswith('/revoke') or (path == '/personal-access-tokens' and payload is None)]
                self.assertEqual(cleanup, ['/personal-access-tokens/owned-test-id/revoke', '/personal-access-tokens'])
                self.assertTrue(report['cleanup_requires_followup'])
                self.assertFalse(report['revocation']['confirmed'])

    def test_lost_revoke_receipt_uses_list_without_replaying_revoke(self):
        for applied in (True, False):
            with self.subTest(applied=applied):
                class LostRevoke(FakeGateway):
                    def call(self, path, payload=None, token=None):
                        if path.endswith('/revoke'):
                            self.calls.append((path, payload, token))
                            self.revoked = applied
                            raise TimeoutError('synthetic lost revoke receipt')
                        return super().call(path, payload, token)
                gateway, report = LostRevoke(), {}
                if applied:
                    run(gateway, self.args(), report)
                else:
                    with self.assertRaises(AssertionError):
                        run(gateway, self.args(), report)
                self.assertEqual(report['revocation']['confirmed'], applied)
                self.assertEqual(report['cleanup_requires_followup'], not applied)
                self.assertEqual(report['revocation']['error_class'], 'TimeoutError')
                self.assertEqual(sum(path.endswith('/revoke') for path, _, _ in gateway.calls), 1)
                self.assertEqual(gateway.calls[-1][0], '/personal-access-tokens')

    def test_caller_action_key_and_compatible_defaults(self):
        gateway, report = FakeGateway(), {}
        run(gateway, self.args(), report, action_key='Task123/gateway-qualification/hindsight-u2/3')
        self.assertEqual(report['action_key'], 'Task123/gateway-qualification/hindsight-u2/3')
        inventory = {}
        run(FakeGateway(), self.args(invoke=False), inventory, action_key='Task123/inventory/4')
        self.assertEqual(inventory['action_key'], 'Task123/inventory/4')
        gateway, report = FakeGateway(), {}
        run(gateway, self.args(), report)
        self.assertEqual(report['action_key'], 'Task123/gateway-qualification/hindsight-u2/1')
        gateway, report = FakeGateway(), {}
        with self.assertRaises(ValueError):
            run(gateway, self.args(), report, action_key='invalid\nkey')
        self.assertEqual(gateway.calls, [])

    def test_lost_create_is_reconciled_without_replay(self):
        gateway, report = FakeGateway('lost-create'), {}
        with self.assertRaises(TimeoutError):
            run(gateway, self.args(), report)
        self.assertTrue(report['revocation']['confirmed'])
        self.assertEqual(sum(p == '/personal-access-tokens' and bool(body) for p, body, _ in gateway.calls), 1)

    def test_create_5xx_without_observed_token_remains_unknown(self):
        class UnknownCreate(FakeGateway):
            def call(self, path, payload=None, token=None):
                if path == '/personal-access-tokens' and payload:
                    self.calls.append((path, payload, token))
                    return 500, {}
                return super().call(path, payload, token)
        gateway, report = UnknownCreate(), {}
        with self.assertRaises(AssertionError):
            run(gateway, self.args(), report)
        self.assertTrue(report['cleanup_requires_followup'])
        self.assertFalse(report['revocation']['confirmed'])
        self.assertEqual(sum(path == '/personal-access-tokens' and bool(body) for path, body, _ in gateway.calls), 1)

    def test_unknown_create_without_observed_token_requires_followup(self):
        class UnknownCreate(FakeGateway):
            def call(self, path, payload=None, token=None):
                if path == '/personal-access-tokens' and payload is None:
                    return 200, {'items': []}
                return super().call(path, payload, token)
        gateway, report = UnknownCreate('lost-create'), {}
        with self.assertRaises(AssertionError):
            run(gateway, self.args(), report)
        self.assertTrue(report['cleanup_requires_followup'])
        self.assertFalse(report['revocation']['confirmed'])

    def test_embedding_only_never_repeats_chat(self):
        gateway, report = FakeGateway(), {}
        args = self.args(embedding_model='gpt-6-luna')
        args.embedding_only = True
        run(gateway, args, report)
        self.assertEqual(report['status'], 'PREFLIGHT_PASS')
        self.assertEqual(report['embeddings']['dimensions'], 2)
        self.assertTrue(report['revocation']['confirmed'])
        self.assertFalse(any(path.endswith('chat/completions') for path, _, _ in gateway.calls))
        self.assertEqual(sum(path.endswith('/embeddings') for path, _, _ in gateway.calls), 1)

    def test_qwen_embedding_only_has_exact_provider_and_no_chat_dependency(self):
        class QwenGateway(FakeGateway):
            def call(self, path, payload=None, token=None):
                if path == '/relay/model-routes':
                    self.calls.append((path, payload, token))
                    return 200, {'data': {'items': [{'enabled': True, 'providerKind': 'qwen', 'publicModel': 'Qwen/Qwen3-Embedding-0.6B'}]}}
                if path == '/llm/qwen/v1/models':
                    self.calls.append((path, payload, token))
                    return 200, {'data': [{'id': 'Qwen/Qwen3-Embedding-0.6B'}]}
                if path == '/llm/qwen/v1/embeddings':
                    self.calls.append((path, payload, token))
                    assert token == 'synthetic-secret'
                    return 200, {'data': [{'embedding': [0.1, 0.2]}]}
                return super().call(path, payload, token)
        gateway, report = QwenGateway(), {}
        args = self.args(embedding_model='Qwen/Qwen3-Embedding-0.6B')
        args.embedding_only = True
        run(gateway, args, report)
        self.assertEqual(report['status'], 'PREFLIGHT_PASS')
        create = next(body for path, body, _ in gateway.calls if path == '/personal-access-tokens' and body)
        self.assertEqual(create['metadata']['allowedModels'], ['Qwen/Qwen3-Embedding-0.6B'])
        self.assertEqual(create['metadata']['allowedProviderKinds'], ['qwen'])
        self.assertEqual(sum(path == '/llm/qwen/v1/embeddings' for path, _, _ in gateway.calls), 1)
        self.assertFalse(any(path.startswith('/llm/openai/') for path, _, _ in gateway.calls))
        self.assertFalse(any(path.endswith('chat/completions') for path, _, _ in gateway.calls))
        self.assertTrue(report['revocation']['confirmed'])

    def test_active_upstream_discovery_without_credentials_or_inference(self):
        gateway, report = FakeGateway(), {}
        args = self.args(invoke=False)
        args.discover_upstreams = True
        run(gateway, args, report)
        self.assertEqual(report['upstream_discovery'][0]['models'], ['embedding-fixture'])
        self.assertFalse(gateway.created)
        self.assertEqual(len(gateway.calls), 3)

    def test_nonlocal_profiles_rejected(self):
        for url in ('https://example.com', 'http://user:secret@localhost', 'http://localhost?redirect=1'):
            with self.assertRaises(ValueError):
                Gateway({'serverUrl': url, 'accessToken': 'synthetic'})


if __name__ == '__main__':
    unittest.main()
