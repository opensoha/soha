#!/usr/bin/env python3
"""Bounded real Gateway qualification, with disposable local Hindsight and PG."""
import argparse
import base64
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import hashlib
import http.server
import importlib.metadata
import importlib.util
import json
import math
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from gateway_preflight import Gateway, NoRedirect
from qualify import qualify, request
from run_local import free_port

CHAT_MODEL = 'gpt-6-luna'
EMBED_MODEL = 'Qwen/Qwen3-Embedding-0.6B'
QWEN_CHAT_MODEL = 'Qwen/Qwen3-30B-A3B-Instruct-2507'
QUALIFICATION_PROFILES = {
    'gpt': {'model': CHAT_MODEL, 'provider': 'openai', 'stream': True},
    'qwen': {'model': QWEN_CHAT_MODEL, 'provider': 'qwen', 'stream': False},
}
TOKENIZER_ENCODING = 'o200k_base'
MANUAL_VISIBLE_TOKENS = 256


def aggregate_sse(response, deadline, max_bytes=4 * 1024 * 1024):
    """Preserve one OpenAI stream; reject truncated/error/nonterminal streams."""
    result = {"object": "chat.completion"}
    choices = {}
    data = []
    received = 0
    while True:
        assert time.monotonic() <= deadline, "Gateway stream deadline"
        line = response.readline(65537)
        received += len(line)
        assert len(line) <= 65536 and received <= max_bytes, "Gateway stream size bound"
        if not line:
            raise EOFError("Gateway stream ended before DONE")
        line = line.decode("utf-8").rstrip("\r\n")
        if line.startswith("data:"):
            data.append(line[5:].lstrip())
            continue
        if line or not data:
            continue
        frame = "\n".join(data)
        data.clear()
        if frame == "[DONE]":
            assert choices and all(choice.get("finish_reason") for choice in choices.values()), "missing terminal choice"
            for choice in choices.values():
                message = choice["message"]
                assert message.get("role") == "assistant"
                if "tool_calls" in message:
                    calls = [message["tool_calls"][index] for index in sorted(message["tool_calls"])]
                    for call in calls:
                        assert call.get("id") and call.get("type") == "function" and call["function"].get("name")
                        assert isinstance(json.loads(call["function"]["arguments"]), dict), "incomplete tool arguments"
                    message["tool_calls"] = calls
            usage = result.get("usage")
            assert isinstance(usage, dict) and all(type(usage.get(key)) is int and usage[key] >= 0 for key in ("prompt_tokens", "completion_tokens", "total_tokens")), "missing real stream usage"
            assert usage["prompt_tokens"] + usage["completion_tokens"] == usage["total_tokens"], "inconsistent stream usage"
            assert isinstance(result.get("id"), str) and result["id"] and isinstance(result.get("model"), str)
            result["choices"] = [choices[index] for index in sorted(choices)]
            return result
        event = json.loads(frame)
        assert isinstance(event, dict) and "error" not in event, "Gateway stream error"
        for key in ("id", "model", "created"):
            if key in event:
                assert key not in result or result[key] == event[key], "stream identity changed"
                result[key] = event[key]
        if event.get("usage") is not None:
            assert isinstance(event["usage"], dict)
            result["usage"] = event["usage"]
        assert isinstance(event.get("choices"), list)
        for item in event["choices"]:
            index = item["index"]
            assert type(index) is int and 0 <= index < 8
            choice = choices.setdefault(index, {"index": index, "message": {}, "finish_reason": None})
            assert not choice["finish_reason"], "data after terminal choice"
            delta = item.get("delta") or {}
            message = choice["message"]
            if delta.get("role") is not None:
                assert delta["role"] == "assistant"
                message["role"] = delta["role"]
            for key in ("content", "refusal", "reasoning_content"):
                if delta.get(key) is not None:
                    assert isinstance(delta[key], str)
                    message[key] = message.get(key, "") + delta[key]
            for call in delta.get("tool_calls") or []:
                tool_index = call["index"]
                assert type(tool_index) is int and 0 <= tool_index < 32
                tool = message.setdefault("tool_calls", {}).setdefault(tool_index, {"function": {"name": "", "arguments": ""}})
                for key in ("id", "type"):
                    if call.get(key) is not None:
                        assert key not in tool or tool[key] == call[key]
                        tool[key] = call[key]
                for key in ("name", "arguments"):
                    value = (call.get("function") or {}).get(key)
                    if value is not None:
                        assert isinstance(value, str)
                        tool["function"][key] += value
            if item.get("finish_reason") is not None:
                assert item["finish_reason"] in ("stop", "tool_calls", "length", "content_filter", "function_call")
                choice["finish_reason"] = item["finish_reason"]


def validate_nonstream_completion(result):
    assert isinstance(result, dict) and 'error' not in result
    assert isinstance(result.get('id'), str) and result['id'] and isinstance(result.get('model'), str)
    choices = result.get('choices')
    assert isinstance(choices, list) and len(choices) == 1
    choice = choices[0]
    assert choice.get('finish_reason') in ('stop', 'tool_calls', 'length', 'content_filter', 'function_call'), 'missing native terminal choice'
    message = choice.get('message') or {}
    assert message.get('role') == 'assistant'
    assert isinstance(message.get('content'), str) or message.get('refusal') or message.get('tool_calls'), 'missing native assistant output'
    usage = result.get('usage')
    assert isinstance(usage, dict) and all(type(usage.get(key)) is int and usage[key] >= 0 for key in ('prompt_tokens', 'completion_tokens', 'total_tokens')), 'missing real native usage'
    assert usage['prompt_tokens'] + usage['completion_tokens'] == usage['total_tokens'], 'inconsistent native usage'


def done_visible_tokens(args):
    from hindsight_api.engine.reflect.structured_doc import split_markdown, render_document
    from hindsight_api.engine.token_encoding import count_tokens
    markdown = args.get('markdown')
    answer = render_document(split_markdown(markdown)).strip() if isinstance(markdown, str) else args.get('answer', '').strip()
    # reflect.tokenization.count_prompt_tokens delegates to this exact helper.
    # Explicit encoding matches the isolated child without parent config-cache drift.
    return count_tokens(answer, encoding=TOKENIZER_ENCODING)


def named_done_schema(request_body):
    from hindsight_api.engine.llm_interface import LLMToolChoice
    from hindsight_api.engine.providers.tools_via_schema import turn_request
    from hindsight_api.engine.providers.openai_compatible_llm import _ensure_json_word_in_user_message
    try:
        messages = request_body['messages']
        system = messages[0]['content']
        assert messages[0]['role'] == 'system' and messages[-1]['role'] == 'user'
        declarations, _ = json.JSONDecoder().raw_decode(system.rsplit('## Functions\n\nYou act by calling these functions:\n\n', 1)[1])
        functions = [item for item in declarations if item.get('name') == 'done']
        assert len(functions) == 1
        tool = {'type': 'function', 'function': functions[0]}
        response_format = request_body['response_format']
        expected_turn = turn_request([tool], LLMToolChoice.named('done'))
        if response_format['type'] == 'json_object':
            predicted = [dict(message) for message in messages]
            predicted[-1]['content'] = expected_turn
            expected_turn = _ensure_json_word_in_user_message(predicted)[-1]['content']
        assert messages[-1]['content'] == expected_turn
        if response_format['type'] == 'json_schema':
            schema = response_format['json_schema']['schema']
        else:
            assert response_format['type'] == 'json_object'
            schema = json.loads(system.rsplit('\n\nYou must respond with valid JSON matching this schema:\n', 1)[1])
        properties = schema['properties']
        assert schema['type'] == 'object' and {'answer_found', 'markdown', 'memory_ids', 'observation_ids'} <= set(properties)
        assert set(properties) == set(functions[0]['parameters']['properties']) and 'call' not in properties
        return schema
    except (AssertionError, KeyError, IndexError, TypeError, ValueError, AttributeError):
        return None


def final_done_payload(request_body, completion, marker):
    schema = named_done_schema(request_body)
    if schema is None:
        return None
    choices = completion.get('choices') or []
    if len(choices) != 1 or choices[0].get('finish_reason') != 'stop':
        return None
    message = choices[0].get('message') or {}
    if message.get('tool_calls') or message.get('refusal') or not isinstance(message.get('content'), str):
        return None
    try:
        args = json.loads(message['content'])
    except ValueError:
        return None
    # Fixed tools_via_schema._unwrapped accepts these two named-done wrappers.
    if isinstance(args, dict) and len(args) == 1:
        if isinstance(args.get('done'), dict):
            args = args['done']
        elif isinstance(args.get('call'), dict) and args['call'].get('name') == 'done':
            args = args['call'].get('arguments')
    if not isinstance(args, dict) or args.get('answer_found') is not True:
        return None
    from jsonschema import Draft202012Validator
    if not Draft202012Validator(schema).is_valid(args):
        return None
    text = args.get('markdown', args.get('answer'))
    cited = any(isinstance(args.get(key), list) and args[key] and all(isinstance(value, str) and value for value in args[key]) for key in ('memory_ids', 'observation_ids'))
    if not (isinstance(text, str) and text.strip() and marker.lower() in text.lower() and cited):
        return None
    # Fixed agent._process_done_tool renders then may call the model again to
    # rewrite overlong answers. Only the official early-return path is final.
    return args if done_visible_tokens(args) <= MANUAL_VISIBLE_TOKENS else None


class BudgetProxy(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, status, body):
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_CONNECT(self):
        self.reply(403, b'{"error":"unapproved_egress"}')

    def do_GET(self):
        self.reply(403, b'{"error":"unapproved_egress"}')

    def do_POST(self):
        state = self.server.state
        profile = QUALIFICATION_PROFILES[state.get('qualification_profile', 'gpt')]
        routes = {'/chat/v1/chat/completions': ('chat', profile['model'], '/llm/' + profile['provider'] + '/v1/chat/completions'),
                  '/embed/v1/embeddings': ('embedding', EMBED_MODEL, '/llm/qwen/v1/embeddings')}
        event = {'path': self.path, 'started_utc': datetime.now(timezone.utc).isoformat()}
        try:
            assert self.headers.get('Authorization') == 'Bearer ' + state['local_key']
            assert self.path in routes
            size = int(self.headers.get('Content-Length', '0'))
            assert 0 < size <= 1024 * 1024
            payload = json.loads(self.rfile.read(size))
            kind, model, endpoint = routes[self.path]
            assert payload.get('model') == model and not payload.get('stream')
            assert state['pat'], 'credential not ready'
            if kind == 'embedding':
                inputs = payload.get('input')
                assert isinstance(inputs, str) or (isinstance(inputs, list) and inputs and all(isinstance(text, str) for text in inputs))
                event['input_type'] = 'text'
                event['input_count'] = 1 if isinstance(inputs, str) else len(inputs)
            with state['lock']:
                assert time.monotonic() <= state['deadline']
                assert state['counts'][kind] < state['limits'][kind]
                phase = state.get('phase_report', {}).get('stage', '')
                manual = phase.startswith('manual-')
                observation_extra = state.get('remaining_only', False) and phase.startswith('observation-history-update')
                event['purpose'] = 'manual_model' if manual else 'observation_history' if observation_extra else 'lifecycle_core'
                base_counts = state.setdefault('base_counts', {'chat': 0, 'embedding': 0})
                if not manual and not observation_extra:
                    assert base_counts[kind] < {'chat': 12, 'embedding': 30}[kind]
                    base_counts[kind] += 1
                state['counts'][kind] += 1
                state['events'].append(event)
            if kind == 'chat':
                with state['lock']:
                    barrier = state.pop('pause_next_chat', None)
                if barrier:
                    entered, release = barrier
                    entered.set()
                    event['barrier_released'] = release.wait(timeout=max(0, min(45, state['deadline'] - time.monotonic())))
                    assert event['barrier_released'], 'late job barrier timeout'
                # Bound generation cost without changing the extraction schema or facts.
                requested = payload.pop('max_tokens', payload.get('max_completion_tokens', 2048))
                cap = min(requested, 2048) if type(requested) is int and requested > 0 else 2048
                if profile['stream']:
                    payload['max_completion_tokens'] = cap
                    payload['reasoning_effort'] = 'low'
                    payload['stream'] = True
                    payload['stream_options'] = {'include_usage': True}
                    event['transport'] = 'gateway_sse_aggregated'
                else:
                    payload.pop('max_completion_tokens', None)
                    payload.pop('reasoning_effort', None)
                    payload.pop('stream_options', None)
                    payload.update(max_tokens=cap, stream=False)
                    event['transport'] = 'gateway_json'
            if kind == 'embedding':
                assert payload.get('encoding_format') == 'float', 'set official embedding extra_body; proxy does not rewrite input or format'
            req = urllib.request.Request(state['gateway'].base + endpoint, data=json.dumps(payload).encode(),
                headers={'Authorization': 'Bearer ' + state['pat'], 'Content-Type': 'application/json'})
            try:
                remaining = state['deadline'] - time.monotonic()
                assert remaining > 0, 'qualification deadline before forwarding'
                with state['gateway'].opener.open(req, timeout=min(90, remaining)) as response:
                    status = response.status
                    body = json.dumps(aggregate_sse(response, min(state['deadline'], time.monotonic() + 90))).encode() if kind == 'chat' and status == 200 and profile['stream'] else response.read(4 * 1024 * 1024 + 1)
            except urllib.error.HTTPError as error:
                status = error.code
                # Do not relay upstream diagnostic bodies into vendor logs.
                body = b'{"error":{"message":"Gateway rejected qualification request"}}'
            assert len(body) <= 4 * 1024 * 1024
            event['http'] = status
            if status == 200:
                result = json.loads(body)
                if kind == 'chat' and not profile['stream']:
                    validate_nonstream_completion(result)
                usage = result.get('usage') or {}
                event['usage'] = {k: v for k, v in usage.items() if k in ('prompt_tokens', 'completion_tokens', 'total_tokens') and type(v) is int}
                if kind == 'embedding':
                    vectors = result['data']
                    assert vectors and all(len(v['embedding']) == 1024 and all(type(x) in (int, float) and math.isfinite(x) for x in v['embedding']) and any(x != 0 for x in v['embedding']) for v in vectors)
                    event['vectors'] = len(vectors)
                else:
                    event['finish'] = [v.get('finish_reason') for v in result.get('choices', [])]
                    with state['lock']:
                        after_final = state.get('pause_after_final_chat')
                    final_args = final_done_payload(payload, result, after_final[2]) if after_final else None
                    if final_args is not None:
                        with state['lock']:
                            assert state.pop('pause_after_final_chat') is after_final
                        entered, release, _ = after_final
                        event['barrier_kind'] = 'after_final_chat'
                        event['final_done_payload'] = final_args
                        event['final_wire_format'] = payload['response_format']['type']
                        event['named_done_schema_sha256'] = hashlib.sha256(json.dumps(named_done_schema(payload), sort_keys=True).encode()).hexdigest()
                        event['rendered_visible_tokens'] = done_visible_tokens(final_args)
                        event['manual_visible_token_limit'] = MANUAL_VISIBLE_TOKENS
                        event['tokenizer_encoding'] = TOKENIZER_ENCODING
                        event['complete_gateway_response_sha256'] = hashlib.sha256(body).hexdigest()
                        entered.set()
                        event['barrier_released'] = release.wait(timeout=max(0, min(60, state['deadline'] - time.monotonic())))
                        assert event['barrier_released'], 'final model result barrier timeout'
                        assert time.monotonic() < state['deadline'], 'qualification deadline before final delivery'

            self.reply(status, body)
        except Exception as error:
            event['error_class'] = type(error).__name__
            with state['lock']:
                if event not in state['events']:
                    state['events'].append(event)
            self.reply(502, b'{"error":{"message":"bounded qualification proxy rejected request"}}')
        finally:
            event['finished_utc'] = datetime.now(timezone.utc).isoformat()


def normal_local_gateway(expected_base=None):
    session = subprocess.run(['soha', 'token', 'list', '--profile', 'local'], capture_output=True, timeout=50)
    assert session.returncode == 0, 'normal local management session refresh failed'
    gateway = Gateway(json.loads((Path.home() / '.soha/config.json').read_text())['profiles']['local'])
    assert expected_base is None or gateway.base == expected_base, 'local profile destination changed'
    return gateway


def management_seconds_remaining(gateway):
    try:
        segment = gateway.jwt.split('.')[1]
        expiry = json.loads(base64.urlsafe_b64decode(segment + '=' * (-len(segment) % 4)))['exp']
        return max(0, int(expiry - time.time()))
    except (ValueError, KeyError, IndexError, TypeError):
        return None


@contextmanager
def relay_credential(gateway, report):
    profile = QUALIFICATION_PROFILES[report.get('qualification_profile', 'gpt')]
    name = 'task123-hindsight-h1-' + report.get('attempt', 'A01').lower() + '-' + uuid.uuid4().hex
    report['credential'] = {'name': name, 'action_key': report.get('action_key', 'Task123/hindsight-real/H1/A01'), 'cleanup_requires_followup': True}
    receipt = report['credential']
    token_id = None
    try:
        status, body = gateway.call('/personal-access-tokens', {'name': name, 'permissionKeys': ['ai.gateway.relay.invoke'],
            'scopes': ['llm-relay'], 'expiresAt': (datetime.now(timezone.utc) + timedelta(minutes=30)).isoformat(),
            'metadata': {'purpose': 'llm-relay', 'allowedModels': [profile['model'], EMBED_MODEL], 'allowedProviderKinds': sorted({profile['provider'], 'qwen'})}})
        receipt['create_http'] = status
        assert status in (200, 201)
        token_id = body['data']['token']['id']
        receipt['id'] = token_id
        assert body['data']['token']['permissionKeys'] == ['ai.gateway.relay.invoke']
        yield body['data']['value']
    finally:
        if token_id is None:
            status, listed = gateway.call('/personal-access-tokens')
            assert status == 200
            matches = [item for item in listed['items'] if item.get('name') == name]
            assert len(matches) <= 1
            if matches:
                token_id = matches[0]['id']
        if token_id:
            receipt['id'] = token_id
            manager = gateway
            refreshed = False
            revoke_status = None
            try:
                status, _ = gateway.call('/personal-access-tokens/' + token_id + '/revoke', {})
                receipt['revoke_http'] = status
                revoke_status = status
                if status == 401:  # Definite auth rejection, not an ambiguous executed revoke.
                    refreshed = True
                    manager = normal_local_gateway(gateway.base)
                    receipt['cleanup_management_refresh'] = 'normal_local_cli_after_401'
                    status, _ = manager.call('/personal-access-tokens/' + token_id + '/revoke', {})
                    receipt['revoke_after_refresh_http'] = status
            except Exception as error:
                receipt['revoke_error_class'] = type(error).__name__
            status, listed = manager.call('/personal-access-tokens')
            if status == 401 and not refreshed and revoke_status != 403:
                manager = normal_local_gateway(gateway.base)
                receipt['cleanup_management_refresh'] = 'normal_local_cli_for_readonly_confirmation'
                status, listed = manager.call('/personal-access-tokens')  # Never replay an unknown revoke.
            receipt['confirmation_http'] = status
            assert status == 200
            item = next((i for i in listed['items'] if i.get('id') == token_id), None)
            receipt['revoked_confirmed'] = bool(item and item.get('revokedAt'))
            assert receipt['revoked_confirmed']
            receipt['cleanup_requires_followup'] = False
        else:
            raise RuntimeError('credential creation result unresolved; reconcile exact unique name')


def observation_snapshot(cursor, bank, document, previous=None):
    if previous is None:
        documents = [document] if isinstance(document, str) else document
        cursor.execute("SELECT id::text FROM memory_units WHERE bank_id=%s AND document_id=ANY(%s) AND fact_type IN ('world','experience')", (bank, documents))
        sources = [row[0] for row in cursor.fetchall()]
        assert sources, 'no stored source units for observation history proof'
        cursor.execute("SELECT id::text FROM memory_units WHERE bank_id=%s AND fact_type='observation' AND source_memory_ids && %s::uuid[]", (bank, sources))
        observations = [row[0] for row in cursor.fetchall()]
    else:
        sources, observations = previous['source_unit_ids'], previous['observation_ids']
    cursor.execute("SELECT (SELECT count(*) FROM memory_units WHERE bank_id=%s AND id=ANY(%s::uuid[]) AND fact_type='observation'), (SELECT count(*) FROM observation_history WHERE bank_id=%s AND observation_id=ANY(%s::uuid[])), (SELECT count(*) FROM observation_history WHERE bank_id=%s AND observation_id=ANY(%s::uuid[]) AND content::text ILIKE ANY(%s))",
        (bank, observations, bank, observations, bank, observations, ['%HQ_CANARY_A1%', '%cobalt%']))
    live, history, canary = cursor.fetchone()
    cursor.execute("SELECT observation_id::text, content->>'previous_text' FROM observation_history WHERE bank_id=%s AND observation_id=ANY(%s::uuid[]) ORDER BY id DESC LIMIT 20", (bank, observations))
    samples = [{'observation_id': row[0], 'previous_text': row[1]} for row in cursor.fetchall()]
    return {'source_unit_ids': sources, 'observation_ids': observations, 'observation_rows': live, 'history_rows': history, 'canary_history_rows': canary, 'history_previous_text': samples}


def purge_guarded_document(gateway, base, cap, bank, document, deadline):
    def call(method, suffix):
        req = urllib.request.Request(base + '/v1/default/banks/' + urllib.parse.quote(bank, safe='') + suffix,
            method=method, headers={'Authorization': 'Bearer ' + cap})
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError('qualification deadline before guarded purge')
        try:
            with gateway.opener.open(req, timeout=min(30, remaining)) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, None
    forbidden, _ = call('GET', '/mental-models')
    assert forbidden == 403, 'production guard exposed derived model API'
    status, body = call('DELETE', '/documents/' + urllib.parse.quote(document, safe=''))
    assert status in (200, 404), 'production derived purge did not acknowledge'
    return {'http': status, 'receipt': body, 'external_mental_models_http': forbidden}


def close_runtime(api, proxy, state):
    if state.get('runtime_closed'):
        return
    guard = state.get('guard')
    if guard is not None:
        guard.shutdown()
        guard.server_close()
    if api is not None and api.poll() is None:
        api.terminate()
        try:
            api.wait(timeout=15)
        except subprocess.TimeoutExpired:
            api.kill()
            api.wait(timeout=10)
    proxy.shutdown()
    # ThreadingHTTPServer joins its in-flight bounded forwarding threads here.
    proxy.server_close()
    state['runtime_closed'] = True


def run(report, core_check=False, prepare_only=False, manual_models=False, chat_limit=12, embedding_limit=30, production_guard=False, remaining_only=False):
    profile_name = report.get('qualification_profile', 'gpt')
    profile = QUALIFICATION_PROFILES[profile_name]
    assert importlib.metadata.version('hindsight-api-slim') == '1.2.0'
    pg_bin = Path(os.environ.get('HQ_PG_BIN', '/opt/homebrew/opt/postgresql@18/bin'))
    assert shutil.disk_usage(tempfile.gettempdir()).free >= 5 * 1024**3
    gateway = normal_local_gateway()
    report['management_session'] = {'seconds_remaining_at_readiness': management_seconds_remaining(gateway), 'cleanup_refresh': 'one normal local refresh on definite401; unknown revoke never replayed'}
    assert report['management_session']['seconds_remaining_at_readiness'] != 0, 'management session expired at readiness'
    status, routes = gateway.call('/relay/model-routes')
    assert status == 200
    items = routes.get('items', routes.get('data', {}).get('items', []))
    assert {(r['publicModel'], r['providerKind']) for r in items if r.get('enabled')} >= {(profile['model'], profile['provider']), (EMBED_MODEL, 'qwen')}
    with tempfile.TemporaryDirectory(prefix='task123-hindsight-live-', dir=os.environ.get('OPENSOHA_VERIFY_DIR')) as root_name:
        root = Path(root_name)
        report['temporary_root'] = str(root)
        pgdata = root / 'pgdata'
        pg_port, api_port = free_port(), free_port()
        report['ports'] = {'pg': pg_port, 'api': api_port}
        api = None
        watchdog = None
        pg_started = False
        proxy = http.server.ThreadingHTTPServer(('127.0.0.1', 0), BudgetProxy)
        state = {'gateway': gateway, 'pat': None, 'local_key': secrets.token_urlsafe(32), 'lock': threading.Lock(),
                 'limits': {'chat': chat_limit, 'embedding': embedding_limit}, 'counts': {'chat': 0, 'embedding': 0}, 'events': [], 'deadline': 0}
        state['qualification_profile'] = profile_name
        state['phase_report'] = report
        state['remaining_only'] = remaining_only
        proxy.state = state
        report['ports']['proxy'] = proxy.server_port
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
        try:
            with (root / 'initdb.log').open('w') as log:
                subprocess.run([str(pg_bin / 'initdb'), '-D', str(pgdata), '-U', 'hq', '-A', 'trust', '--no-locale', '--encoding=UTF8'], stdout=log, stderr=log, check=True)
            with (pgdata / 'postgresql.conf').open('a') as conf:
                conf.write("\nunix_socket_directories = ''\n")
            subprocess.run([str(pg_bin / 'pg_ctl'), '-D', str(pgdata), '-l', str(root / 'pg.log'), '-o', f'-h 127.0.0.1 -p {pg_port}', '-w', 'start'], check=True, stdout=subprocess.DEVNULL)
            pg_started = True
            core_command = None
            if core_check:
                modfile = root / 'core.mod'
                shutil.copyfile('go.mod', modfile)
                shutil.copyfile('go.sum', root / 'core.sum')
                go_env = dict(os.environ, GOWORK='off')
                subprocess.run(['go', 'mod', 'edit', '-modfile=' + str(modfile), '-replace=github.com/opensoha/soha/contracts=' + str(Path('contracts').resolve(strict=True))], env=go_env, check=True, capture_output=True)
                compiled = subprocess.run(['go', 'test', '-c', '-modfile=' + str(modfile), '-o', str(root / 'core-hindsight.test'), './internal/infrastructure/hindsight'], env=go_env, capture_output=True, timeout=120)
                report['core_sources_before'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in [*Path('internal/infrastructure/hindsight').glob('*.go'), *Path('internal/application/memory').glob('*.go')] if not p.name.endswith('_test.go') or p.name == 'client_real_integration_test.go'}
                report['core_test_sha256'] = hashlib.sha256(Path('internal/infrastructure/hindsight/client_real_integration_test.go').read_bytes()).hexdigest()
                report['core_compile_exit'] = compiled.returncode
                assert compiled.returncode == 0, 'Core integration test compile failed'
                core_command = ['go', 'tool', 'test2json', '-t', '-p', 'github.com/opensoha/soha/internal/infrastructure/hindsight', str(root / 'core-hindsight.test'), '-test.run=^TestClientRealHindsightLifecycle$', '-test.v', '-test.count=1']
            state['deadline'] = time.monotonic() + 600
            proxy_url = f'http://127.0.0.1:{proxy.server_port}'
            env = {k: os.environ[k] for k in ('PATH', 'LANG') if k in os.environ}
            env.update({'HOME': str(root), 'TMPDIR': str(root), 'XDG_CACHE_HOME': str(root / 'cache'), 'HF_HUB_OFFLINE': '1',
                'HINDSIGHT_API_DATABASE_URL': f'postgresql://hq@127.0.0.1:{pg_port}/postgres',
                'HINDSIGHT_API_LLM_PROVIDER': 'openai', 'HINDSIGHT_API_LLM_API_KEY': state['local_key'], 'HINDSIGHT_API_LLM_MODEL': profile['model'],
                'HINDSIGHT_API_LLM_BASE_URL': proxy_url + '/chat/v1', 'HINDSIGHT_API_EMBEDDINGS_PROVIDER': 'openai',
                'HINDSIGHT_API_EMBEDDINGS_OPENAI_BASE_URL': proxy_url + '/embed/v1', 'HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY': state['local_key'],
                'HINDSIGHT_API_EMBEDDINGS_OPENAI_EXTRA_BODY': json.dumps({'encoding_format': 'float'}),
                    'HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL': EMBED_MODEL, 'HINDSIGHT_API_EMBEDDINGS_OPENAI_DIMENSIONS': '1024',
                'HINDSIGHT_API_RERANKER_PROVIDER': 'rrf', 'HINDSIGHT_API_TOKENIZER_ENCODING': TOKENIZER_ENCODING, 'HINDSIGHT_API_ENABLE_AUTO_CONSOLIDATION': 'false',
                'HINDSIGHT_API_ENABLE_OBSERVATION_HISTORY': 'true', 'HINDSIGHT_API_SKIP_LLM_VERIFICATION': 'true', 'HINDSIGHT_API_LLM_MAX_RETRIES': '1', 'HINDSIGHT_API_RETAIN_LLM_MAX_RETRIES': '1',
                'HINDSIGHT_API_LLM_TIMEOUT': '95', 'HINDSIGHT_API_REFLECT_LLM_TIMEOUT': '95',
                'HINDSIGHT_API_CONSOLIDATION_LLM_MAX_RETRIES': '1', 'HINDSIGHT_API_CONSOLIDATION_MAX_ATTEMPTS': '1',
                'HINDSIGHT_API_EMBEDDINGS_MAX_RETRIES': '0', 'HINDSIGHT_API_WORKER_MAX_RETRIES': '0',
                'HINDSIGHT_API_RETAIN_MAX_COMPLETION_TOKENS': '2048', 'HINDSIGHT_API_RETAIN_CHUNK_SIZE': '1024', 'HINDSIGHT_API_LLM_TRACE_ENABLED': 'false',
                'HINDSIGHT_API_OTEL_TRACES_ENABLED': 'false', 'HINDSIGHT_API_WORKER_HTTP_PORT': str(free_port()),
                'HTTP_PROXY': proxy_url, 'HTTPS_PROXY': proxy_url, 'ALL_PROXY': proxy_url, 'NO_PROXY': '127.0.0.1,localhost'})
            with (root / 'api.log').open('w') as log:
                api = subprocess.Popen([sys.executable, '-m', 'uvicorn', 'hindsight_api.server:app', '--host', '127.0.0.1', '--port', str(api_port)], env=env, cwd=root, stdout=log, stderr=log)
            watchdog = threading.Timer(max(1, state['deadline'] - time.monotonic()), api.terminate)
            watchdog.daemon = True
            watchdog.start()
            base = f'http://127.0.0.1:{api_port}'
            deadline = time.monotonic() + 90
            while time.monotonic() < deadline:
                assert api.poll() is None, 'Hindsight exited during startup'
                try:
                    request(base, 'GET', '/health')
                    break
                except Exception:
                    time.sleep(.2)
            else:
                raise RuntimeError('Hindsight startup timeout')
            report['postgres_actual'] = subprocess.check_output([str(pg_bin / 'postgres'), '--version'], text=True).strip()
            report['extensions_actual'] = subprocess.check_output([str(pg_bin / 'psql'), '-h', '127.0.0.1', '-p', str(pg_port), '-U', 'hq', '-d', 'postgres', '-At', '-c', "SELECT extname || '=' || extversion FROM pg_extension WHERE extname IN ('vector','pg_trgm') ORDER BY extname"], text=True).splitlines()
            assert report['extensions_actual'] == ['pg_trgm=1.6', 'vector=0.8.7']
            if prepare_only:
                report['status'] = 'PREPARED'
                return
            guarded_base = base
            guard_cap = 'task123-h1-loopback-synthetic-cap'
            if production_guard:
                guard_source = Path('deploy/dsh-hindsight/gateway.py').resolve()
                spec = importlib.util.spec_from_file_location('h1_production_guard', guard_source)
                guard_module = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(guard_module)
                guard = guard_module.Server(('127.0.0.1', 0), guard_module.Handler)
                guard.daemon_threads = False  # Own/join all bounded test requests during cleanup.
                guard.upstream_port = api_port
                guard_cap = secrets.token_urlsafe(32)
                guard.token = guard_cap
                state['guard'] = guard
                threading.Thread(target=guard.serve_forever, daemon=True).start()
                guarded_base = f'http://127.0.0.1:{guard.server_port}'
                report['ports']['guard'] = guard.server_port
                report['production_guard'] = {'source_sha256': hashlib.sha256(guard_source.read_bytes()).hexdigest(), 'native_url': base, 'guard_url': guarded_base,
                    'core_business_route': 'guard', 'core_bank_cleanup_route': 'native-owner-only', 'vendor_research_route': 'native'}
            def purge_control(bank, document):
                return purge_guarded_document(gateway, guarded_base, guard_cap, bank, document, state['deadline'])
            def storage_counts(bank, model):
                import psycopg2
                with psycopg2.connect(host='127.0.0.1', port=pg_port, user='hq', dbname='postgres', connect_timeout=5) as conn:
                    with conn.cursor() as cursor:
                        cursor.execute('SELECT (SELECT count(*) FROM mental_models WHERE bank_id=%s AND id=%s), (SELECT count(*) FROM mental_model_history WHERE bank_id=%s AND mental_model_id=%s)', (bank, model, bank, model))
                        models, history = cursor.fetchone()
                        return {'models': models, 'history': history}
            def observation_storage(bank, document, previous=None):
                import psycopg2
                with psycopg2.connect(host='127.0.0.1', port=pg_port, user='hq', dbname='postgres', connect_timeout=5) as conn:
                    with conn.cursor() as cursor:
                        return observation_snapshot(cursor, bank, document, previous)
            with relay_credential(gateway, report) as pat:
                try:
                    state['pat'] = pat
                    def late_control(after_final=False, marker='cobalt'):
                        barrier = (threading.Event(), threading.Event())
                        with state['lock']:
                            state['pause_after_final_chat' if after_final else 'pause_next_chat'] = (*barrier, marker) if after_final else barrier
                        return barrier
                    if core_command:
                        core_env = dict(os.environ, GOWORK='off', SOHA_TEST_HINDSIGHT_REAL='1', SOHA_TEST_HINDSIGHT_URL=guarded_base, SOHA_TEST_HINDSIGHT_TOKEN=guard_cap, SOHA_TEST_HINDSIGHT_CLEANUP_URL=base)
                        remaining = state['deadline'] - time.monotonic()
                        assert remaining >= 150, 'no Core integration time budget remaining'
                        core = subprocess.run(core_command, env=core_env, cwd=Path('internal/infrastructure/hindsight').resolve(), capture_output=True, text=True, timeout=150)
                        report['core_sources_after'] = {name: hashlib.sha256(Path(name).read_bytes()).hexdigest() for name in report['core_sources_before']}
                        assert report['core_sources_before'] == report['core_sources_after'], 'Core input changed during real acceptance'
                        report['core_exit'] = core.returncode
                        report['core_events'] = [json.loads(line) for line in core.stdout.splitlines() if line.startswith('{')]
                        passed = core.returncode == 0 and any(event.get('Action') == 'pass' and event.get('Test') == 'TestClientRealHindsightLifecycle' for event in report['core_events']) and not any(event.get('Action') == 'skip' for event in report['core_events'])
                        report['core_phase'] = 'PASS' if passed else 'FAIL'
                        if not passed:
                            outputs = ''.join(event.get('Output', '') for event in report['core_events'])
                            assert 'H2 identities:' not in outputs or ('H2 cleanup:' in outputs and 'deleted=true http=200' in outputs), 'Core bank cleanup unconfirmed; stop vendor phase'
                    if remaining_only:
                        from qualify import qualify_remaining
                        qualify_remaining(base, report, late_control, purge_control, storage_counts, observation_storage, state['deadline'])
                    else:
                        qualify(base, full=True, report=report, late_control=late_control, manual_models=manual_models, purge_control=purge_control if production_guard else None, storage_counts=storage_counts if manual_models else None, observation_storage=observation_storage)
                    report['status'] = 'FAIL' if report.get('core_phase') == 'FAIL' else 'PARTIAL' if any(value.startswith(('UNSAFE', 'NOT_RUN', 'NOT_PROVEN')) for value in report['checks'].values()) else 'PASS'
                finally:
                    close_runtime(api, proxy, state)
        finally:
            if watchdog is not None:
                watchdog.cancel()
            close_runtime(api, proxy, state)
            state['pat'] = None
            if pg_started:
                subprocess.run([str(pg_bin / 'pg_ctl'), '-D', str(pgdata), '-m', 'immediate', '-w', 'stop'], check=True, stdout=subprocess.DEVNULL)
            if (root / 'api.log').exists():
                log = (root / 'api.log').read_text(errors='replace')
                report['vendor_log_tail'] = log[-6000:].replace(state['local_key'], '[LOCAL_CAP]').replace(guard_cap if 'guard_cap' in locals() else state['local_key'], '[GUARD_CAP]')
                report['vendor_log_error_categories'] = {word: log.count(word) for word in ('ERROR', 'ValidationError', 'AuthenticationError', 'RateLimitError', 'TimeoutError', 'retry', 'schema', 'context_length_exceeded')}
            if production_guard and 'production_guard' in report:
                report['production_guard']['source_unchanged'] = report['production_guard']['source_sha256'] == hashlib.sha256(Path('deploy/dsh-hindsight/gateway.py').read_bytes()).hexdigest()
                if not report['production_guard']['source_unchanged']:
                    report['status'] = 'FAIL'
            report['model_limits'] = dict(state['limits'], wall_seconds=600, max_completion_tokens=2048)
            report['model_counts_base'] = state.get('base_counts', {'chat': 0, 'embedding': 0})
            report['model_counts'] = state['counts']
            report['model_events'] = state['events']
            if any(event.get('barrier_released') is False for event in state['events']):
                report['status'] = 'FAIL'
                report['barrier_timeout'] = True
            report['process_cleanup'] = {'api_stopped': api is None or api.poll() is not None, 'proxy_closed': True, 'guard_closed': state.get('guard') is None or state['guard'].fileno() == -1, 'pg_stopped': True}
    report['temporary_root_removed'] = not Path(root_name).exists()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence', required=True)
    parser.add_argument('--attempt', default='A01', help='caller-registered H1 attempt identity')
    parser.add_argument('--qualification-profile', choices=tuple(QUALIFICATION_PROFILES), default='gpt', help='fixed approved model/provider/transport combination')
    parser.add_argument('--remaining-only', action='store_true', help='A06: only unproved correction/history/final-response/late-retain scenarios; reuse A05 proof')
    parser.add_argument('--production-guard', action='store_true', help='run Core and derived purge through the actual production capability gateway')
    parser.add_argument('--manual-models', action='store_true', help='include real manual mental model/history and in-flight refresh deletion')
    parser.add_argument('--chat-limit', type=int, default=12)
    parser.add_argument('--embedding-limit', type=int, default=30)
    parser.add_argument('--prepare-only', action='store_true', help='validate private PG/API startup without a Gateway credential or model calls')
    parser.add_argument('--core-check', action='store_true', help='compile and execute the isolated Core client lifecycle acceptance')
    args = parser.parse_args()
    if args.core_check and not args.production_guard:
        parser.error('--core-check requires --production-guard for operation purge receipts')
    if not (1 <= args.chat_limit <= 20 and 1 <= args.embedding_limit <= 48):
        parser.error('limits exceed registered maximum 20 chat / 48 embedding')
    if args.remaining_only and ((args.core_check and args.qualification_profile != 'qwen') or not args.production_guard or args.chat_limit > 20 or args.embedding_limit > 40):
        parser.error('remaining-only requires guard, Core repeat only for qwen compatibility, and maximum 20 chat / 40 embedding')
    if len(args.attempt) != 3 or not args.attempt.startswith('A') or not args.attempt[1:].isdigit():
        parser.error('attempt must be A followed by two digits')
    path = Path(args.evidence)
    history = json.loads(path.read_text()) if path.exists() else {'attempts': []}
    report = {'identity': 'Task123/Hindsight/H1/' + args.attempt, 'attempt': args.attempt, 'qualification_profile': args.qualification_profile, 'mode': 'prepare_only' if args.prepare_only else 'real', 'action_key': 'Task123/hindsight-real/H1/' + args.attempt, 'status': 'FAIL', 'started_utc': datetime.now(timezone.utc).isoformat(),
              'versions': json.loads(Path(__file__).with_name('versions.json').read_text()),
              'source_hashes': {name: hashlib.sha256(Path(__file__).with_name(name).read_bytes()).hexdigest() for name in ('run_gateway.py', 'qualify.py', 'gateway_preflight.py', 'requirements.lock')}}
    try:
        run(report, core_check=args.core_check, prepare_only=args.prepare_only, manual_models=args.manual_models, chat_limit=args.chat_limit, embedding_limit=args.embedding_limit, production_guard=args.production_guard, remaining_only=args.remaining_only)
    except Exception as error:
        report['status'] = 'FAIL'
        report['error_class'] = type(error).__name__
        if isinstance(error, urllib.error.HTTPError):
            report['hindsight_http'] = error.code
    if report.get('temporary_root'):
        report['temporary_root_removed'] = not Path(report['temporary_root']).exists()
    report['finished_utc'] = datetime.now(timezone.utc).isoformat()
    history['attempts'].append(report)
    path.write_text(json.dumps(history, ensure_ascii=False, indent=2))
    print(json.dumps({k: report.get(k) for k in ('identity', 'status', 'error_class', 'hindsight_http', 'model_counts', 'credential', 'process_cleanup')}, indent=2))
    return 0 if report['status'] in ('PASS', 'PARTIAL', 'PREPARED') else 1


if __name__ == '__main__':
    raise SystemExit(main())
