"""Offline budget, egress, and credential safety checks for the real runner."""
import http.server
import asyncio
import os
import io
import json
import threading
import time
import unittest
import urllib.error
import urllib.request
from copy import deepcopy
from functools import lru_cache
from unittest.mock import patch
from qualify import final_refresh_proof, optional_get, request as qualification_request, wait_final_barrier, wait_operation
from types import SimpleNamespace

from run_gateway import BudgetProxy, CHAT_MODEL, EMBED_MODEL, QWEN_CHAT_MODEL, aggregate_sse, close_runtime, done_visible_tokens, final_done_payload, observation_snapshot, purge_guarded_document, relay_credential
from test_gateway_preflight import FakeGateway


class Response(io.BytesIO):
    status = 200


DONE_ARGS = {'answer_found': True, 'markdown': 'HQ_CANARY_A1 prefers cobalt tea.', 'memory_ids': ['f1'], 'observation_ids': []}


@lru_cache(maxsize=6)
def captured_tool_turn(strict=True, choice='done'):
    """Locked call_with_tools -> real OpenAICompatible -> local HTTP, no remote call."""
    from hindsight_api.engine.llm_wrapper import LLMProvider
    from hindsight_api.engine.llm_interface import LLMToolChoice, LLMToolChoiceMode
    schema = {'type': 'object', 'properties': {'answer_found': {'type': 'boolean'}, 'markdown': {'type': 'string'},
        'memory_ids': {'type': 'array', 'items': {'type': 'string'}}, 'observation_ids': {'type': 'array', 'items': {'type': 'string'}}},
        'required': list(DONE_ARGS), 'additionalProperties': False}
    tools = [{'type': 'function', 'function': {'name': 'done', 'description': 'Signal completion with your final answer, as a markdown document.', 'parameters': schema}},
        {'type': 'function', 'function': {'name': 'search', 'description': 'Search stored evidence.', 'parameters': {'type': 'object', 'properties': {'query': {'type': 'string'}}, 'required': ['query'], 'additionalProperties': False}}}]
    value = {'call': {'name': 'done', 'arguments': DONE_ARGS}} if choice == 'free' else {'query': 'tea'} if choice == 'search' else DONE_ARGS
    completion = {'id': 'actual-wire-fixture', 'object': 'chat.completion', 'model': QWEN_CHAT_MODEL,
        'choices': [{'index': 0, 'finish_reason': 'stop', 'message': {'role': 'assistant', 'content': json.dumps(value)}}],
        'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}
    captured = []
    class Endpoint(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass
        def do_POST(self):
            assert self.path == '/v1/chat/completions'
            captured.append(json.loads(self.rfile.read(int(self.headers['Content-Length']))))
            body = json.dumps(completion).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Endpoint)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    async def invoke():
        # DeepSeek is this fixed OpenAICompatible implementation's official soft-schema branch.
        provider = LLMProvider(provider='openai' if strict else 'deepseek', api_key='synthetic',
            base_url=f'http://127.0.0.1:{server.server_port}/v1', model=QWEN_CHAT_MODEL, timeout=5, max_retries=0)
        try:
            selection = LLMToolChoice(LLMToolChoiceMode.AUTO) if choice == 'free' else LLMToolChoice.named(choice)
            await provider.call_with_tools(messages=[{'role': 'system', 'content': 'Synthetic qualification.'}], tools=tools,
                tool_choice=selection, max_completion_tokens=256, max_retries=0)
            assert type(provider._provider_impl).__name__ == 'OpenAICompatibleLLM'
        finally:
            await provider.cleanup()
    try:
        with patch.dict(os.environ, {'HTTP_PROXY': '', 'HTTPS_PROXY': '', 'ALL_PROXY': '', 'NO_PROXY': '127.0.0.1,localhost'}):
            asyncio.run(invoke())
        assert len(captured) == 1
        return captured[0], completion
    finally:
        server.shutdown()
        server.server_close()


def actual_tool_wire(strict=True, choice='done'):
    return deepcopy(captured_tool_turn(strict, choice)[0])


class ActualToolWireTests(unittest.TestCase):
    def test_actual_strict_and_soft_provider_wire_match_named_done(self):
        for strict in (True, False):
            wire, completion = deepcopy(captured_tool_turn(strict))
            self.assertEqual(wire['response_format']['type'], 'json_schema' if strict else 'json_object')
            if strict:
                self.assertEqual(wire['response_format']['json_schema']['name'], 'response')
            self.assertEqual(final_done_payload(wire, completion, 'cobalt'), DONE_ARGS)

    def test_actual_free_hallucinated_done_and_named_search_never_match(self):
        for choice in ('free', 'search'):
            wire = actual_tool_wire(choice=choice)
            completion = deepcopy(captured_tool_turn()[1])
            self.assertIsNone(final_done_payload(wire, completion, 'cobalt'))


def stream_bytes(events, done=True):
    output = b': heartbeat\n\n'
    for event in events:
        if 'error' not in event:
            event = dict({'id': 'fixture-completion', 'model': CHAT_MODEL, 'created': 0}, **event)
        output += ('data: ' + json.dumps(event, ensure_ascii=False) + '\n\n').encode()
    return output + (b'data: [DONE]\n\n' if done else b'')


class SSETests(unittest.TestCase):
    def aggregate(self, events, done=True):
        return aggregate_sse(io.BytesIO(stream_bytes(events, done)), time.monotonic() + 5)

    def test_chinese_content_finish_and_exact_usage(self):
        result = self.aggregate([
            {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': '茉莉'}, 'finish_reason': None}]},
            {'choices': [{'index': 0, 'delta': {'content': '花茶'}, 'finish_reason': 'stop'}]},
            {'choices': [], 'usage': {'prompt_tokens': 7, 'completion_tokens': 3, 'total_tokens': 10}},
        ])
        self.assertEqual(result['choices'][0]['message']['content'], '茉莉花茶')
        self.assertEqual(result['choices'][0]['finish_reason'], 'stop')
        self.assertEqual(result['usage']['total_tokens'], 10)

    def test_fragmented_parallel_tool_calls_preserve_ids_names_arguments(self):
        result = self.aggregate([
            {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'tool_calls': [
                {'index': 0, 'id': 'call-a', 'type': 'function', 'function': {'name': 'search_', 'arguments': '{"q":'}},
                {'index': 1, 'id': 'call-b', 'type': 'function', 'function': {'name': 'other', 'arguments': '{}'}},
            ]}, 'finish_reason': None}]},
            {'choices': [{'index': 0, 'delta': {'tool_calls': [{'index': 0, 'function': {'name': 'memories', 'arguments': '"tea"}'}}]}, 'finish_reason': 'tool_calls'}]},
            {'choices': [], 'usage': {'prompt_tokens': 7, 'completion_tokens': 3, 'total_tokens': 10}},
        ])
        calls = result['choices'][0]['message']['tool_calls']
        self.assertEqual([(x['id'], x['function']['name'], x['function']['arguments']) for x in calls], [('call-a', 'search_memories', '{"q":"tea"}'), ('call-b', 'other', '{}')])

    def test_truncated_error_missing_terminal_usage_and_size_fail_closed(self):
        content = {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': 'partial'}, 'finish_reason': None}]}
        terminal = {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': 'text'}, 'finish_reason': 'stop'}]}
        for events, done in (([content], False), ([content], True), ([terminal], True), ([{'error': {'message': 'synthetic'}}], True)):
            with self.subTest(events=events, done=done), self.assertRaises((AssertionError, EOFError)):
                self.aggregate(events, done)
        with self.assertRaises(AssertionError):
            aggregate_sse(io.BytesIO(stream_bytes([terminal])), time.monotonic() + 5, max_bytes=2)

    def test_refusal_is_preserved(self):
        result = self.aggregate([
            {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'refusal': 'Cannot comply'}, 'finish_reason': 'stop'}]},
            {'choices': [], 'usage': {'prompt_tokens': 1, 'completion_tokens': 2, 'total_tokens': 3}},
        ])
        self.assertEqual(result['choices'][0]['message']['refusal'], 'Cannot comply')


class ProxyTests(unittest.TestCase):
    def setUp(self):
        self.forwarded = []
        def forward(req, timeout):
            payload = json.loads(req.data)
            self.forwarded.append((req.full_url, payload, req.headers))
            result = {'usage': {'total_tokens': 1}}
            if req.full_url.endswith('/embeddings'):
                result['data'] = [{'embedding': [0.1] * 1024}]
            else:
                result['choices'] = [{'finish_reason': 'stop', 'message': {'content': '{}'}}]
            if req.full_url.endswith('/chat/completions'):
                return Response(stream_bytes([
                    {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': '{}'}, 'finish_reason': None}]},
                    {'choices': [{'index': 0, 'delta': {}, 'finish_reason': 'stop'}]},
                    {'choices': [], 'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}]))
            return Response(json.dumps(result).encode())
        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), BudgetProxy)
        self.server.state = {'gateway': SimpleNamespace(base='http://gateway.test/api/v1/ai-gateway', opener=SimpleNamespace(open=forward)),
            'pat': 'synthetic-parent-token', 'local_key': 'synthetic-child-cap', 'lock': threading.Lock(),
            'counts': {'chat': 0, 'embedding': 0}, 'limits': {'chat': 1, 'embedding': 1}, 'deadline': time.monotonic() + 60, 'events': []}
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()

    def call(self, path, payload, key='synthetic-child-cap'):
        req = urllib.request.Request(f'http://127.0.0.1:{self.server.server_port}' + path, data=json.dumps(payload).encode(),
            headers={'Authorization': 'Bearer ' + key})
        try:
            with urllib.request.urlopen(req, timeout=5) as r:
                return r.status, json.load(r)
        except urllib.error.HTTPError as e:
            return e.code, json.load(e)

    def test_provider_endpoints_and_generation_cap(self):
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL, 'max_tokens': 64000})[0], 200)
        self.assertEqual(self.call('/embed/v1/embeddings', {'model': EMBED_MODEL, 'input': ['synthetic'], 'encoding_format': 'float'})[0], 200)
        self.assertEqual(self.forwarded[0][0], 'http://gateway.test/api/v1/ai-gateway/llm/openai/v1/chat/completions')
        self.assertEqual(self.forwarded[1][0], 'http://gateway.test/api/v1/ai-gateway/llm/qwen/v1/embeddings')
        self.assertEqual(self.forwarded[1][1]['input'], ['synthetic'])
        self.assertEqual(self.forwarded[1][1]['encoding_format'], 'float')
        self.assertEqual(self.forwarded[0][1]['max_completion_tokens'], 2048)
        self.assertTrue(self.forwarded[0][1]['stream'])
        self.assertEqual(self.forwarded[0][1]['stream_options'], {'include_usage': True})
        self.assertNotIn('max_tokens', self.forwarded[0][1])
        self.assertEqual(self.forwarded[0][2]['Authorization'], 'Bearer synthetic-parent-token')
        self.assertNotIn('synthetic-parent-token', json.dumps(self.server.state['events']))

    def test_budget_wrong_model_path_and_cap_reject_without_forward(self):
        self.assertEqual(self.call('/embed/v1/embeddings', {'model': EMBED_MODEL, 'input': ['synthetic'], 'encoding_format': 'float'})[0], 200)
        self.assertEqual(self.call('/embed/v1/embeddings', {'model': EMBED_MODEL, 'input': ['synthetic'], 'encoding_format': 'float'})[0], 502)
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': 'unapproved-model'})[0], 502)
        self.assertEqual(self.call('/other-egress', {'model': CHAT_MODEL})[0], 502)
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL}, key='wrong')[0], 502)
        self.assertEqual(self.call('/embed/v1/embeddings', {'model': EMBED_MODEL, 'input': [1, 2, 3]})[0], 502)
        self.assertEqual(len(self.forwarded), 1)
        self.assertEqual(self.server.state['counts'], {'chat': 0, 'embedding': 1})

    def test_additional_allowance_is_only_for_manual_model_phase(self):
        self.server.state['limits']['chat'] = 16
        self.server.state['phase_report'] = {'stage': 'core'}
        for _ in range(12):
            self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL})[0], 200)
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL})[0], 502)
        self.server.state['phase_report']['stage'] = 'manual-model-generation'
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL})[0], 200)
        self.assertEqual(self.server.state['base_counts']['chat'], 12)
        self.assertEqual(self.server.state['counts']['chat'], 13)

    def test_expired_deadline_does_not_forward(self):
        self.server.state['deadline'] = time.monotonic() - 1
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL})[0], 502)
        self.assertEqual(self.forwarded, [])


class FinalBarrierTests(ProxyTests):
    def test_only_complete_named_done_consumes_latch_after_generation(self):
        self.server.state['limits']['chat'] = 3
        entered, release = threading.Event(), threading.Event()
        self.server.state['pause_after_final_chat'] = (entered, release, 'cobalt')
        done_args = {'answer_found': True, 'markdown': 'HQ_CANARY_A1 prefers cobalt tea.', 'memory_ids': ['f1'], 'observation_ids': []}
        forwarded = []
        def forward(req, timeout):
            payload = json.loads(req.data)
            name = 'done' if payload['messages'][-1]['content'].startswith('Call `done` now') else 'next_call'
            args = done_args if name == 'done' else {'call': {'name': 'recall', 'arguments': {'query': 'tea'}}}
            forwarded.append(name)
            return Response(stream_bytes([
                {'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': json.dumps(args)}, 'finish_reason': 'stop'}]},
                {'choices': [], 'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}]))
        self.server.state['gateway'].opener.open = forward
        def payload(name):
            wire = actual_tool_wire(choice='done' if name == 'done' else 'free')
            wire['model'] = CHAT_MODEL
            return wire
        self.assertEqual(self.call('/chat/v1/chat/completions', payload('next_call'))[0], 200)
        self.assertFalse(entered.is_set())
        self.assertIn('pause_after_final_chat', self.server.state)
        responses = []
        thread = threading.Thread(target=lambda: responses.append(self.call('/chat/v1/chat/completions', payload('done'))))
        thread.start()
        try:
            self.assertTrue(entered.wait(timeout=3))
            self.assertEqual(responses, [])
            event = self.server.state['events'][-1]
            self.assertEqual(event['final_done_payload'], done_args)
            self.assertEqual(event['rendered_visible_tokens'], done_visible_tokens(done_args))
            self.assertLessEqual(event['rendered_visible_tokens'], 256)
            self.assertEqual(event['tokenizer_encoding'], 'o200k_base')
            self.assertEqual(event['barrier_kind'], 'after_final_chat')
            self.assertTrue(event['complete_gateway_response_sha256'])
            self.assertEqual(forwarded, ['next_call', 'done'])
        finally:
            release.set()
            thread.join(timeout=5)
        self.assertFalse(thread.is_alive())
        self.assertEqual(responses[0][0], 200)
        self.assertEqual(json.loads(responses[0][1]['choices'][0]['message']['content']), done_args)

    def test_done_schema_refusal_empty_uncited_and_tool_calls_do_not_latch(self):
        request = actual_tool_wire()
        args = {'answer_found': True, 'markdown': 'cobalt tea', 'memory_ids': ['f1'], 'observation_ids': []}
        for wrapped in (args, {'done': args}, {'call': {'name': 'done', 'arguments': args}}):
            result = {'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps(wrapped)}}]}
            self.assertEqual(final_done_payload(request, result, 'cobalt'), args)
        invalid = [dict(args, answer_found=False), dict(args, markdown=''), dict(args, memory_ids=[])]
        for value in invalid:
            result = {'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps(value)}}]}
            self.assertIsNone(final_done_payload(request, result, 'cobalt'))
        for finish, message in [('tool_calls', {'tool_calls': [{'id': 'call'}]}), ('stop', {'refusal': 'no', 'content': json.dumps(args)})]:
            self.assertIsNone(final_done_payload(request, {'choices': [{'finish_reason': finish, 'message': message}]}, 'cobalt'))
        self.assertIsNone(final_done_payload(actual_tool_wire(choice='free'), {'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps(args)}}]}, 'cobalt'))

    def test_overlong_done_cannot_claim_last_model_response(self):
        from hindsight_api.engine.reflect.structured_doc import split_markdown, render_document
        from hindsight_api.engine.token_encoding import count_tokens
        args = {'answer_found': True, 'markdown': '# Tea\n\ncobalt ' + 'tea ' * 400, 'memory_ids': ['f1'], 'observation_ids': []}
        rendered = render_document(split_markdown(args['markdown'])).strip()
        self.assertEqual(done_visible_tokens(args), count_tokens(rendered, encoding='o200k_base'))
        self.assertGreater(done_visible_tokens(args), 256)
        result = {'choices': [{'finish_reason': 'stop', 'message': {'content': json.dumps(args)}}]}
        self.assertIsNone(final_done_payload(actual_tool_wire(), result, 'cobalt'))

    def test_failed_refresh_never_proves_late_write_fence(self):
        self.assertEqual(final_refresh_proof({'status': 'completed'}), 'PASS')
        for error in ('budget exhausted', 'token limit', 'authentication failed', 'parse error', 'validation error', 'Mental model owned not found in bank isolated'):
            self.assertEqual(final_refresh_proof({'status': 'failed', 'error_message': error}), 'NOT_PROVEN_FAILED_REFRESH')
        for status in ('cancelled', 'not_found', 'running'):
            with self.assertRaises(AssertionError):
                final_refresh_proof({'status': status})


class QwenProfileTests(ProxyTests):
    def setUp(self):
        super().setUp()
        self.server.state.update(qualification_profile='qwen', limits={'chat': 10, 'embedding': 1})
        self.native_result = {'id': 'native-fixture', 'object': 'chat.completion', 'model': QWEN_CHAT_MODEL,
            'choices': [{'index': 0, 'finish_reason': 'stop', 'message': {'role': 'assistant', 'content': '{}'}}],
            'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}
        def forward(req, timeout):
            self.forwarded.append((req.full_url, json.loads(req.data), dict(req.header_items())))
            return Response(json.dumps(self.native_result).encode())
        self.server.state['gateway'].opener.open = forward

    def test_qwen_native_wire_keeps_schema_and_refuses_gpt(self):
        schema = {'type': 'json_schema', 'json_schema': {'name': 'fact', 'strict': True, 'schema': {'type': 'object'}}}
        payload = {'model': QWEN_CHAT_MODEL, 'max_completion_tokens': 64000, 'reasoning_effort': 'low', 'response_format': schema}
        status, result = self.call('/chat/v1/chat/completions', payload)
        self.assertEqual(status, 200)
        self.assertEqual(result, self.native_result)
        url, wire, _ = self.forwarded[0]
        self.assertTrue(url.endswith('/llm/qwen/v1/chat/completions'))
        self.assertEqual(wire['response_format'], schema)
        self.assertEqual(wire['max_tokens'], 2048)
        self.assertIs(wire['stream'], False)
        for key in ('reasoning_effort', 'stream_options', 'max_completion_tokens'):
            self.assertNotIn(key, wire)
        self.assertEqual(self.call('/chat/v1/chat/completions', {'model': CHAT_MODEL})[0], 502)
        self.assertEqual(len(self.forwarded), 1)
        self.assertEqual(self.server.state['events'][0]['transport'], 'gateway_json')

    def test_qwen_complete_native_done_latches_and_returns_exact_response(self):
        args = deepcopy(DONE_ARGS)
        self.native_result['choices'][0]['message']['content'] = json.dumps(args)
        entered, release = threading.Event(), threading.Event()
        self.server.state['pause_after_final_chat'] = (entered, release, 'cobalt')
        responses = []
        thread = threading.Thread(target=lambda: responses.append(self.call('/chat/v1/chat/completions', actual_tool_wire())))
        thread.start()
        try:
            self.assertTrue(entered.wait(timeout=3))
            self.assertEqual(responses, [])
            event = self.server.state['events'][0]
            self.assertEqual(event['final_done_payload'], args)
            self.assertLessEqual(event['rendered_visible_tokens'], 256)
            self.assertEqual(event['usage']['total_tokens'], 2)
        finally:
            release.set()
            thread.join(timeout=5)
        self.assertFalse(thread.is_alive())
        self.assertEqual(responses, [(200, self.native_result)])

    def test_qwen_incomplete_native_terminal_usage_or_json_never_latches(self):
        from copy import deepcopy
        good = deepcopy(self.native_result)
        good['choices'][0]['message']['content'] = json.dumps(DONE_ARGS)
        payload = actual_tool_wire()
        entered, release = threading.Event(), threading.Event()
        release.set()  # A regression must fail assertions immediately, not wait at a false latch.
        self.server.state['pause_after_final_chat'] = (entered, release, 'cobalt')
        invalid = []
        missing_usage = deepcopy(good)
        del missing_usage['usage']
        invalid.append(missing_usage)
        missing_terminal = deepcopy(good)
        missing_terminal['choices'][0]['finish_reason'] = None
        invalid.append(missing_terminal)
        invalid.append(dict(good, usage={'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 0}))
        for value in invalid:
            self.native_result = value
            self.assertEqual(self.call('/chat/v1/chat/completions', payload)[0], 502)
        self.server.state['gateway'].opener.open = lambda *a, **k: Response(b'{"choices":')
        self.assertEqual(self.call('/chat/v1/chat/completions', payload)[0], 502)
        self.assertFalse(entered.is_set())
        self.assertIn('pause_after_final_chat', self.server.state)
        self.assertTrue(all('usage' not in event for event in self.server.state['events']))

    def test_qwen_pat_scope_is_only_fixed_models_and_qwen_provider(self):
        gateway, report = FakeGateway(), {'qualification_profile': 'qwen'}
        with relay_credential(gateway, report):
            pass
        create = next(payload for path, payload, _ in gateway.calls if path == '/personal-access-tokens' and payload)
        self.assertEqual(create['metadata']['allowedModels'], [QWEN_CHAT_MODEL, EMBED_MODEL])
        self.assertEqual(create['metadata']['allowedProviderKinds'], ['qwen'])
        self.assertEqual(create['permissionKeys'], ['ai.gateway.relay.invoke'])
        self.assertTrue(report['credential']['revoked_confirmed'])


class OperationChecks(unittest.TestCase):
    def test_failed_operation_is_only_terminal_for_deleted_model(self):
        with patch('qualify.request', return_value={'status': 'failed'}):
            with self.assertRaises(AssertionError):
                wait_operation('http://127.0.0.1:1', 'bank', 'operation', {})
            result = wait_operation('http://127.0.0.1:1', 'bank', 'operation', {}, allow_cancelled=True)
            self.assertEqual(result['status'], 'failed')

    def test_optional_get_only_treats_404_as_absent(self):
        for code in (404, 500):
            with patch('qualify.request', side_effect=urllib.error.HTTPError('http://127.0.0.1:1', code, 'synthetic', {}, None)):
                if code == 404:
                    self.assertEqual(optional_get('http://127.0.0.1:1', '/model'), (404, None))
                else:
                    with self.assertRaises(urllib.error.HTTPError):
                        optional_get('http://127.0.0.1:1', '/model')


class BarrierWaitTests(unittest.TestCase):
    def test_live_entered_barrier_requires_processing_operation(self):
        entered = threading.Event()
        entered.set()
        report = {}
        with patch('qualify.request', return_value={'status': 'processing'}) as call:
            wait_final_barrier('http://127.0.0.1:1', 'bank', 'own-operation', entered, report, time.monotonic()+5)
        self.assertEqual(call.call_args.args[1], 'GET')
        self.assertTrue(call.call_args.args[2].endswith('/operations/own-operation'))
        with patch('qualify.request', return_value={'status': 'completed'}):
            with self.assertRaises(AssertionError):
                wait_final_barrier('http://127.0.0.1:1', 'bank', 'own-operation', entered, {}, time.monotonic()+5)

    def test_terminal_without_barrier_fails_immediately_without_model_request(self):
        for state in ('completed', 'failed', 'cancelled'):
            report = {}
            with patch('qualify.request', return_value={'status': state}) as call:
                with self.assertRaisesRegex(AssertionError, 'FAIL_QUALIFIER_BARRIER'):
                    wait_final_barrier('http://127.0.0.1:1', 'bank', 'own-operation', threading.Event(), report, time.monotonic()+600)
            self.assertEqual(call.call_count, 1)
            self.assertEqual(call.call_args.args[1], 'GET')
            self.assertEqual(report['qualifier_barrier_failure']['terminal']['status'], state)


class ObservationStorageTests(unittest.TestCase):
    def test_snapshot_reuses_exact_source_linked_ids_without_writes(self):
        class Cursor:
            def __init__(self, counts):
                self.queries = []
                self.rows = iter([[('source-uuid',)], [('observation-uuid',)]])
                self.counts = counts
            def execute(self, sql, params):
                self.queries.append((sql, params))
            def fetchall(self):
                if "content->>'previous_text'" in self.queries[-1][0]:
                    return [('observation-uuid', 'HQ_CANARY_A1 prefers cobalt tea.')] if self.counts[1] else []
                return next(self.rows)
            def fetchone(self):
                return self.counts
        initial = Cursor((1, 2, 2))
        before = observation_snapshot(initial, 'owned-bank', 'source-r1')
        self.assertEqual(before['source_unit_ids'], ['source-uuid'])
        self.assertEqual(before['observation_ids'], ['observation-uuid'])
        after_cursor = Cursor((0, 0, 0))
        after = observation_snapshot(after_cursor, 'owned-bank', 'source-r1', before)
        self.assertEqual(after['history_rows'], 0)
        self.assertEqual(len(after_cursor.queries), 2)
        self.assertEqual(after_cursor.queries[0][1][1], ['observation-uuid'])
        for sql, params in initial.queries + after_cursor.queries:
            self.assertTrue(sql.startswith('SELECT '))
            self.assertIn('bank_id=%s', sql)
            self.assertIn('owned-bank', params)
        empty_history = observation_snapshot(Cursor((1, 0, 0)), 'owned-bank', 'source-r1')
        self.assertEqual(empty_history['history_rows'], 0)
        self.assertEqual(empty_history['canary_history_rows'], 0)


class NativeEmbeddingWireTests(unittest.TestCase):
    def test_official_extra_body_produces_float_text_without_proxy_mutation(self):
        from hindsight_api.config import HindsightConfig
        from hindsight_api.engine.embeddings import OpenAIEmbeddings
        captured = []
        class Endpoint(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_POST(self):
                captured.append(json.loads(self.rfile.read(int(self.headers['Content-Length']))))
                body = json.dumps({'object': 'list', 'model': EMBED_MODEL, 'data': [{'object': 'embedding', 'index': 0, 'embedding': [0.1] * 1024}], 'usage': {'prompt_tokens': 1, 'total_tokens': 1}}).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Endpoint)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            with patch.dict(os.environ, {'HINDSIGHT_API_LLM_PROVIDER': 'openai', 'HINDSIGHT_API_LLM_API_KEY': 'synthetic',
                    'HINDSIGHT_API_EMBEDDINGS_PROVIDER': 'openai', 'HINDSIGHT_API_RERANKER_PROVIDER': 'rrf',
                    'HINDSIGHT_API_EMBEDDINGS_OPENAI_EXTRA_BODY': '{"encoding_format":"float"}'}, clear=True):
                config = HindsightConfig.from_env()
            async def encode():
                backend = OpenAIEmbeddings(api_key='synthetic', model=EMBED_MODEL, base_url=f'http://127.0.0.1:{server.server_port}/v1',
                    dimensions=1024, max_retries=0, extra_body=config.embeddings_openai_extra_body)
                await backend.initialize()
                try:
                    return await backend.encode(['synthetic tea'])
                finally:
                    await backend._loop_client().close()
            with patch.dict(os.environ, {'HTTP_PROXY': '', 'HTTPS_PROXY': '', 'ALL_PROXY': '', 'NO_PROXY': '127.0.0.1,localhost'}):
                vectors = asyncio.run(encode())
            self.assertEqual(len(vectors[0]), 1024)
            self.assertEqual(captured[0]['input'], ['synthetic tea'])
            self.assertEqual(captured[0]['encoding_format'], 'float')
            self.assertEqual(captured[0]['dimensions'], 1024)
        finally:
            server.shutdown()
            server.server_close()


class DeadlineTests(unittest.TestCase):
    def test_barrier_released_after_expired_deadline_never_forwards(self):
        entered, release = threading.Event(), threading.Event()
        forwarded = []
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), BudgetProxy)
        server.state = {'gateway': SimpleNamespace(base='http://gateway.test', opener=SimpleNamespace(open=lambda *a, **k: forwarded.append(a))),
            'pat': 'synthetic-parent', 'local_key': 'synthetic-child', 'lock': threading.Lock(),
            'counts': {'chat': 0, 'embedding': 0}, 'limits': {'chat': 1, 'embedding': 0}, 'deadline': time.monotonic()+30,
            'events': [], 'pause_next_chat': (entered, release)}
        threading.Thread(target=server.serve_forever, daemon=True).start()
        results = []
        def call():
            req = urllib.request.Request(f'http://127.0.0.1:{server.server_port}/chat/v1/chat/completions',
                data=json.dumps({'model': CHAT_MODEL}).encode(), headers={'Authorization': 'Bearer synthetic-child'})
            try:
                urllib.request.urlopen(req, timeout=5)
            except urllib.error.HTTPError as error:
                results.append(error.code)
        thread = threading.Thread(target=call)
        thread.start()
        try:
            self.assertTrue(entered.wait(timeout=3))
            server.state['deadline'] = time.monotonic()-1
            release.set()
            thread.join(timeout=5)
            self.assertFalse(thread.is_alive())
            self.assertEqual(results, [502])
            self.assertEqual(forwarded, [])
        finally:
            release.set()
            thread.join(timeout=5)
            server.shutdown()
            server.server_close()

    def test_purge_deadline_expires_between_requests_without_delete(self):
        opened = []
        def open_request(req, timeout):
            opened.append((req.method, timeout))
            raise urllib.error.HTTPError(req.full_url, 403, 'synthetic', {}, None)
        gateway = SimpleNamespace(opener=SimpleNamespace(open=open_request))
        with patch('run_gateway.time.monotonic', side_effect=[100, 125]):
            with self.assertRaises(TimeoutError):
                purge_guarded_document(gateway, 'http://127.0.0.1:1', 'synthetic', 'bank', 'doc', 125)
        self.assertEqual(opened, [('GET', 25)])
        with patch('run_gateway.time.monotonic', return_value=126):
            with self.assertRaises(TimeoutError):
                purge_guarded_document(gateway, 'http://127.0.0.1:1', 'synthetic', 'bank', 'doc', 125)
        self.assertEqual(opened, [('GET', 25)])

    def test_official_config_exposes_bounded_retry_and_reflect_timeout(self):
        from hindsight_api.config import HindsightConfig
        with patch.dict(os.environ, {'HINDSIGHT_API_LLM_PROVIDER': 'openai', 'HINDSIGHT_API_LLM_API_KEY': 'synthetic',
                'HINDSIGHT_API_LLM_TIMEOUT': '95', 'HINDSIGHT_API_REFLECT_LLM_TIMEOUT': '95',
                'HINDSIGHT_API_LLM_MAX_RETRIES': '1', 'HINDSIGHT_API_RETAIN_LLM_MAX_RETRIES': '1',
                'HINDSIGHT_API_CONSOLIDATION_LLM_MAX_RETRIES': '1'}, clear=True):
            config = HindsightConfig.from_env()
        self.assertEqual(config.llm_timeout, 95)
        self.assertEqual(config.reflect_llm_timeout, 95)
        self.assertEqual((config.llm_max_retries, config.retain_llm_max_retries, config.consolidation_llm_max_retries), (1, 1, 1))
        self.assertIsNone(config.reflect_llm_max_retries)

    def test_http_and_operation_wait_share_absolute_deadline(self):
        opened = []
        def open_request(req, timeout):
            opened.append((req.method, req.full_url, timeout))
            return Response(b'{"status":"completed"}')
        with patch('qualify.time.monotonic', return_value=100), patch('qualify.urllib.request.build_opener', return_value=SimpleNamespace(open=open_request)):
            qualification_request('http://127.0.0.1:1', 'GET', '/health', deadline=125)
            self.assertEqual(opened[-1][2], 25)
            qualification_request('http://127.0.0.1:1', 'GET', '/health', deadline=500)
            self.assertEqual(opened[-1][2], 300)
            result = wait_operation('http://127.0.0.1:1', 'bank', 'same-operation', {}, deadline=125)
            self.assertEqual(result['status'], 'completed')
            self.assertEqual(opened[-1], ('GET', 'http://127.0.0.1:1/v1/default/banks/bank/operations/same-operation', 25))
            for action in (lambda: qualification_request('http://127.0.0.1:1', 'POST', '/memories', deadline=100),
                           lambda: wait_operation('http://127.0.0.1:1', 'bank', 'same-operation', {}, deadline=100)):
                with self.assertRaises(TimeoutError):
                    action()
            self.assertEqual(len(opened), 3)  # Expired work never reaches HTTP or resubmits retain.


class NativeRetryTests(unittest.TestCase):
    async def provider_call(self, base, key='synthetic'):
        from pydantic import BaseModel
        from hindsight_api.engine.providers.openai_compatible_llm import OpenAICompatibleLLM
        class TeaFact(BaseModel):
            fact: str
        provider = OpenAICompatibleLLM(provider='openai', api_key=key, base_url=base, model=CHAT_MODEL, timeout=5)
        self.assertEqual(provider._client.max_retries, 0)  # No multiplying SDK loop.
        try:
            return await provider.call(messages=[{'role': 'user', 'content': 'Synthetic cobalt preference'}],
                response_format=TeaFact, strict_schema=True, max_completion_tokens=160,
                max_retries=1, initial_backoff=0, max_backoff=0)
        finally:
            await provider.cleanup()

    def sequence(self, replies, expected_calls, raises=False):
        captured = []
        class Endpoint(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_POST(self):
                captured.append((self.path, json.loads(self.rfile.read(int(self.headers['Content-Length'])))))
                status, content = replies[min(len(captured) - 1, len(replies) - 1)]
                result = {'error': {'message': 'synthetic provider error'}} if status != 200 else {
                    'id': 'fixture', 'object': 'chat.completion', 'model': CHAT_MODEL,
                    'choices': [{'index': 0, 'finish_reason': 'stop', 'message': {'role': 'assistant', 'content': content}}],
                    'usage': {'prompt_tokens': 1, 'completion_tokens': 1, 'total_tokens': 2}}
                body = json.dumps(result).encode()
                self.send_response(status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Endpoint)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            with patch.dict(os.environ, {'HTTP_PROXY': '', 'HTTPS_PROXY': '', 'ALL_PROXY': '', 'NO_PROXY': '127.0.0.1,localhost'}):
                if raises:
                    from openai import APIStatusError
                    with self.assertRaises(APIStatusError):
                        asyncio.run(self.provider_call(f'http://127.0.0.1:{server.server_port}/v1'))
                else:
                    result = asyncio.run(self.provider_call(f'http://127.0.0.1:{server.server_port}/v1'))
                    self.assertEqual(result.content.fact, 'cobalt')
            self.assertEqual(len(captured), expected_calls)
            self.assertTrue(all(path == '/v1/chat/completions' for path, _ in captured))
            self.assertTrue(all(body == captured[0][1] for _, body in captured))
        finally:
            server.shutdown()
            server.server_close()

    def test_single_502_recovery_and_two_attempt_failure_bound(self):
        self.sequence([(502, None), (200, '{"fact":"cobalt"}')], 2)
        self.sequence([(502, None)], 2, raises=True)

    def test_official_400_429_and_json_parse_retry_are_explicit(self):
        for status in (400, 429):
            with self.subTest(status=status):
                self.sequence([(status, None), (200, '{"fact":"cobalt"}')], 2)
        self.sequence([(200, 'not-json'), (200, '{"fact":"cobalt"}')], 2)

    def test_auth_failures_do_not_retry(self):
        for status in (401, 403):
            with self.subTest(status=status):
                self.sequence([(status, None)], 1, raises=True)

    def test_budget_rejection_cannot_forward_beyond_cap(self):
        forwarded = []
        def forward(req, timeout):
            forwarded.append(req.data)
            return Response(stream_bytes([{'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': 'partial'}, 'finish_reason': None}]}], done=False))
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), BudgetProxy)
        server.state = {'gateway': SimpleNamespace(base='http://gateway.test', opener=SimpleNamespace(open=forward)),
            'pat': 'synthetic-parent', 'local_key': 'synthetic-child', 'lock': threading.Lock(),
            'counts': {'chat': 0, 'embedding': 0}, 'limits': {'chat': 1, 'embedding': 0}, 'deadline': time.monotonic()+30, 'events': []}
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            from openai import APIStatusError
            with self.assertRaises(APIStatusError):
                asyncio.run(self.provider_call(f'http://127.0.0.1:{server.server_port}/chat/v1', 'synthetic-child'))
            self.assertEqual(len(forwarded), 1)
            self.assertEqual(server.state['counts']['chat'], 1)
            self.assertEqual(len(server.state['events']), 2)  # Retry asks locally; no second paid call.
            self.assertTrue(all('usage' not in event for event in server.state['events']))
        finally:
            server.shutdown()
            server.server_close()


class CredentialTests(unittest.TestCase):
    def test_cleanup401_refreshes_normal_session_once_and_revokes_exact_id(self):
        class Expired(FakeGateway):
            base = 'http://127.0.0.1:1/api/v1/ai-gateway'
            def call(self, path, payload=None, token=None):
                if path.endswith('/revoke'):
                    self.calls.append((path, payload, token))
                    return 401, {}
                return super().call(path, payload, token)
        expired, fresh, report = Expired(), FakeGateway(), {}
        def refresh(base):
            self.assertEqual(base, expired.base)
            fresh.created, fresh.name = True, expired.name
            return fresh
        with patch('run_gateway.normal_local_gateway', side_effect=refresh) as refreshed:
            with relay_credential(expired, report):
                pass
        self.assertEqual(refreshed.call_count, 1)
        self.assertEqual([path for path, _, _ in expired.calls if path.endswith('/revoke')], ['/personal-access-tokens/owned-test-id/revoke'])
        self.assertEqual([path for path, _, _ in fresh.calls], ['/personal-access-tokens/owned-test-id/revoke', '/personal-access-tokens'])
        self.assertEqual(report['credential']['revoke_http'], 401)
        self.assertEqual(report['credential']['revoke_after_refresh_http'], 200)
        self.assertTrue(report['credential']['revoked_confirmed'])

    def test_unknown_revoke_is_never_replayed_and_403_never_refreshes(self):
        class Failed(FakeGateway):
            base = 'http://127.0.0.1:1/api/v1/ai-gateway'
            def __init__(self, mode, committed):
                super().__init__()
                self.mode, self.committed = mode, committed
            def call(self, path, payload=None, token=None):
                if path.endswith('/revoke'):
                    self.calls.append((path, payload, token))
                    if self.mode == 'unknown':
                        self.revoked = self.committed
                        raise TimeoutError('synthetic lost revoke receipt')
                    return 403, {}
                if path == '/personal-access-tokens' and payload is None:
                    self.calls.append((path, payload, token))
                    return (401 if self.mode == 'unknown' else 403), {}
                return super().call(path, payload, token)
        for mode, committed in (('unknown', True), ('unknown', False), ('forbidden', False)):
            old, fresh, report = Failed(mode, committed), FakeGateway(), {}
            def refresh(base):
                fresh.created, fresh.name, fresh.revoked = True, old.name, committed
                return fresh
            with patch('run_gateway.normal_local_gateway', side_effect=refresh) as refreshed:
                if committed:
                    with relay_credential(old, report):
                        pass
                else:
                    with self.assertRaises(AssertionError):
                        with relay_credential(old, report):
                            pass
            self.assertEqual(len([p for p, _, _ in old.calls if p.endswith('/revoke')]), 1)
            self.assertFalse(any(p.endswith('/revoke') for p, _, _ in fresh.calls))
            self.assertEqual(refreshed.call_count, 0 if mode == 'forbidden' else 1)
            self.assertEqual(report['credential']['cleanup_requires_followup'], not committed)

    def test_body_failure_revoke_then_confirmation(self):
        gateway, report = FakeGateway(), {}
        with self.assertRaises(RuntimeError):
            with relay_credential(gateway, report) as token:
                self.assertEqual(token, 'synthetic-secret')
                raise RuntimeError('synthetic failure')
        self.assertTrue(report['credential']['revoked_confirmed'])
        self.assertFalse(report['credential']['cleanup_requires_followup'])
        cleanup = [path for path, payload, _ in gateway.calls if path.endswith('/revoke') or (path == '/personal-access-tokens' and payload is None)]
        self.assertEqual(cleanup, ['/personal-access-tokens/owned-test-id/revoke', '/personal-access-tokens'])
        self.assertNotIn('synthetic-secret', json.dumps(report))

    def test_runtime_converges_before_credential_revocation(self):
        events = []
        class ObservedGateway(FakeGateway):
            def call(self, path, payload=None, token=None):
                if path.endswith('/revoke'):
                    events.append('revoke')
                return super().call(path, payload, token)
        api = SimpleNamespace(poll=lambda: None, terminate=lambda: events.append('terminate'), wait=lambda timeout: events.append('wait'))
        proxy = SimpleNamespace(shutdown=lambda: events.append('shutdown'), server_close=lambda: events.append('join-proxy'))
        state = {}
        with relay_credential(ObservedGateway(), {}):
            close_runtime(api, proxy, state)
            close_runtime(api, proxy, state)
        self.assertEqual(events, ['terminate', 'wait', 'shutdown', 'join-proxy', 'revoke'])

    def test_lost_create_reconciles_without_recreation(self):
        gateway, report = FakeGateway('lost-create'), {}
        with self.assertRaises(TimeoutError):
            with relay_credential(gateway, report):
                self.fail('lost creation cannot enter model work')
        self.assertTrue(report['credential']['revoked_confirmed'])
        self.assertEqual(sum(path == '/personal-access-tokens' and bool(body) for path, body, _ in gateway.calls), 1)


if __name__ == '__main__':
    unittest.main()
