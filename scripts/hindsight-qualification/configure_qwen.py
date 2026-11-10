#!/usr/bin/env python3
"""Task123 A08: one owned Qwen route and bounded normal-Gateway capability probe."""
import argparse
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import hashlib
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from gateway_preflight import Gateway, safe_code

ACTION = 'Task123/hindsight-qwen-llm/A08'
UPSTREAM = '79da6578-0bb1-4bc4-abca-8b94a6cc9400'
MODEL = 'Qwen/Qwen3-30B-A3B-Instruct-2507'
EMBED = 'Qwen/Qwen3-Embedding-0.6B'
FIELDS = ('name', 'providerKind', 'baseUrl', 'status', 'priority', 'weight', 'timeoutSeconds',
          'streamTimeoutSeconds', 'maxConcurrency', 'supportedModels', 'defaultHeaders', 'proxyUrl', 'health', 'metadata')
SCHEMA = {'type': 'object', 'properties': {'facts': {'type': 'array', 'minItems': 1, 'maxItems': 1,
    'items': {'type': 'object', 'properties': {'subject': {'type': 'string'}, 'preference': {'type': 'string'}},
              'required': ['subject', 'preference'], 'additionalProperties': False}}},
    'required': ['facts'], 'additionalProperties': False}


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def route_fingerprint(value):
    canonical = deepcopy(value)
    # pgx v5.11.0 timestamptz.Encode divides Nanosecond()/1000 (truncation).
    # POST returns the in-memory nanoseconds; GET returns the stored microseconds.
    for key in ('createdAt', 'updatedAt'):
        if key in canonical:
            parsed = datetime.fromisoformat(canonical[key].replace('Z', '+00:00'))
            assert parsed.tzinfo is not None
            canonical[key] = parsed.astimezone(timezone.utc).isoformat(timespec='microseconds')
    return fingerprint(canonical)


def upstream_input(row):
    # Full PUT preserves every existing mutable field; API key is deliberately omitted.
    return {key: deepcopy(row[key]) for key in FIELDS if key in row}


def api(gateway, method, path, payload=None, timeout=90):
    req = urllib.request.Request(gateway.base + path, method=method,
        data=None if payload is None else json.dumps(payload).encode(),
        headers={'Authorization': 'Bearer ' + gateway.jwt, 'Content-Type': 'application/json'})
    try:
        with gateway.opener.open(req, timeout=timeout) as response:
            return response.status, json.loads(response.read(1024 * 1024 + 1))
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read(1024 * 1024 + 1))


def inventory(gateway):
    s, b = api(gateway, 'GET', '/relay/upstreams')
    assert s == 200
    upstream = next(row for row in b['items'] if row['id'] == UPSTREAM)
    s, b = api(gateway, 'GET', '/relay/model-routes?includeDisabled=true')
    assert s == 200
    return upstream, b['items']


def same_config(left, right):
    return {k: v for k, v in upstream_input(left).items() if k != 'health'} == {k: v for k, v in upstream_input(right).items() if k != 'health'} and left.get('apiKeyPrefix') == right.get('apiKeyPrefix')


def format_unsupported(status, body):
    text = json.dumps(body).lower()
    return status == 400 and any(k in text for k in ('json_schema', 'response_format')) and any(k in text for k in ('unsupported', 'not supported', 'does not support', '不支持'))


def validate_completion(body):
    from jsonschema import Draft202012Validator
    choices = body['choices']
    assert len(choices) == 1 and choices[0]['finish_reason'] == 'stop'
    message = choices[0]['message']
    assert not message.get('refusal') and not message.get('tool_calls')
    content = json.loads(message['content'])
    Draft202012Validator(SCHEMA).validate(content)
    assert content == {'facts': [{'subject': 'HQ_CANARY_A08', 'preference': 'cobalt tea'}]}
    return content


@contextmanager
def credential(gateway, report):
    name = 'task123-hindsight-qwen-a08-' + uuid.uuid4().hex
    receipt = report['credential'] = {'name': name, 'action_key': ACTION, 'cleanup_requires_followup': True}
    token_id = None
    try:
        s, b = api(gateway, 'POST', '/personal-access-tokens', {'name': name, 'permissionKeys': ['ai.gateway.relay.invoke'],
            'scopes': ['llm-relay'], 'expiresAt': (datetime.now(timezone.utc) + timedelta(minutes=15)).isoformat(),
            'metadata': {'purpose': 'llm-relay', 'allowedModels': [MODEL, EMBED], 'allowedProviderKinds': ['qwen']}})
        receipt['create_http'] = s
        assert s in (200, 201)
        token_id = b['data']['token']['id']
        receipt['id'] = token_id
        assert b['data']['token']['permissionKeys'] == ['ai.gateway.relay.invoke']
        yield b['data']['value']
    finally:
        if token_id is None:
            s, b = api(gateway, 'GET', '/personal-access-tokens')
            assert s == 200
            matches = [r for r in b['items'] if r.get('name') == name]
            assert len(matches) <= 1
            if matches:
                token_id = matches[0]['id']
        if token_id:
            try:
                s, _ = api(gateway, 'POST', '/personal-access-tokens/' + token_id + '/revoke', {})
                receipt['revoke_http'] = s
            except Exception as error:
                receipt['revoke_error_class'] = type(error).__name__
            s, b = api(gateway, 'GET', '/personal-access-tokens')
            receipt['confirmation_http'] = s
            receipt['revoked_confirmed'] = s == 200 and any(r['id'] == token_id and r.get('revokedAt') for r in b['items'])
            assert receipt['revoked_confirmed'], 'PAT cleanup needs followup; no revoke replay'
            receipt['cleanup_requires_followup'] = False
        else:
            receipt['cleanup_requires_followup'] = not (400 <= receipt.get('create_http', 0) < 500 and receipt.get('create_http') != 408)


def probe(gateway, report):
    report['probes'] = []
    deadline = time.monotonic() + 180
    with credential(gateway, report) as token:
        relay = Gateway({'serverUrl': gateway.base.split('/api/v1/')[0], 'accessToken': token})
        for mode in ('json_schema', 'json_object'):
            remaining = deadline - time.monotonic()
            assert remaining > 0 and len(report['probes']) < 2
            response_format = {'type': mode}
            if mode == 'json_schema':
                response_format['json_schema'] = {'name': 'qualification_fact', 'strict': True, 'schema': SCHEMA}
            payload = {'model': MODEL, 'messages': [{'role': 'system', 'content': 'Extract only the stated fact as JSON. Return facts with subject and preference; no other fields.'},
                {'role': 'user', 'content': 'HQ_CANARY_A08 prefers cobalt tea.'}], 'response_format': response_format,
                'max_tokens': 256, 'stream': False}
            started = time.monotonic()
            event = {'mode': mode, 'output_limit': 256}
            report['probes'].append(event)
            s, b = api(relay, 'POST', '/llm/qwen/v1/chat/completions', payload, timeout=min(90, remaining))
            event.update({'http': s, 'duration_ms': round((time.monotonic()-started)*1000), 'usage': b.get('usage', 'UNKNOWN')})
            if s == 200:
                event['validated_fact'] = validate_completion(b)
                report['capability'] = 'PASS'
                return
            event['format_unsupported'] = format_unsupported(s, b)
            native = b.get('error') or {}
            event['error_code'] = safe_code(native.get('code')) if isinstance(native, dict) else None
            assert mode == 'json_schema' and event['format_unsupported'], 'probe rejected; no blind retry'
    raise AssertionError('no valid structured completion')


def cleanup_configuration(gateway, report, original, wanted, route, route_attempted, route_known, route_receipt):
    cleanup = report['configuration_cleanup'] = {'requires_followup': True}
    current, current_routes = inventory(gateway)
    matches = [r for r in current_routes if r['id'] == route['id']]
    if matches:
        candidate = matches[0]
        owned = all(candidate.get(k) == v for k, v in route.items())
        unchanged = route_receipt is not None and route_fingerprint(candidate) == route_fingerprint(route_receipt)
        if not (owned and unchanged):
            cleanup['unverified_or_changed_route'] = True
            return
        try:
            s, _ = api(gateway, 'DELETE', '/relay/model-routes/' + route['id'])
            cleanup['route_delete_http'] = s
        except Exception as error:
            cleanup['route_delete_error_class'] = type(error).__name__
        current, current_routes = inventory(gateway)
        if any(r['id'] == route['id'] for r in current_routes):
            return
    elif route_attempted and not route_known:
        cleanup['unknown_route_create'] = True
        return
    if same_config(current, original):
        status = report['upstream'].get('update_http')
        cleanup['requires_followup'] = status is None or status >= 500 or status == 408
        return
    if not same_config(current, wanted) or any(r.get('upstreamModel') == MODEL or r.get('publicModel') == MODEL for r in current_routes):
        return
    # Preserve current runtime health; detect concurrent writes before full PUT.
    again, again_routes = inventory(gateway)
    if fingerprint(again) != fingerprint(current) or any(r.get('upstreamModel') == MODEL or r.get('publicModel') == MODEL for r in again_routes):
        return
    restore = upstream_input(current)
    restore['supportedModels'] = original['supportedModels']
    try:
        s, _ = api(gateway, 'PUT', '/relay/upstreams/' + UPSTREAM, restore)
        cleanup['models_restore_http'] = s
    except Exception as error:
        cleanup['models_restore_error_class'] = type(error).__name__
    final, final_routes = inventory(gateway)
    cleanup['requires_followup'] = final['supportedModels'] != original['supportedModels'] or not same_config(final, original) or any(r['id'] == route['id'] for r in final_routes)


def run(gateway, report, previous=None):
    observed, routes = inventory(gateway)
    original = deepcopy(observed)
    if previous is not None:
        report['configuration_cleanup'] = {'requires_followup': True, 'resume_preconditions_pending': True}
        assert previous['action_key'] == ACTION and previous['status'] == 'FAIL'
        assert previous['upstream']['update_http'] == 200 and 'route_create_http' not in previous and 'credential' not in previous
        old = previous['upstream']['original_models']
        expected = sorted(old + [MODEL])
        assert MODEL not in old and sorted(previous['upstream']['new_models']) == expected
        assert observed['supportedModels'] == expected
        fields = upstream_input(observed)
        hashes = previous['upstream']['original_field_hashes']
        assert set(fields) == set(hashes) and all(fingerprint(value) == hashes[key] for key, value in fields.items() if key != 'supportedModels'), 'resume field drift'
        # An unrelated later key/config write would also move updatedAt beyond this known PUT window.
        updated = datetime.fromisoformat(observed['updatedAt'].replace('Z', '+00:00'))
        assert datetime.fromisoformat(previous['started_utc']) <= updated <= datetime.fromisoformat(previous['finished_utc']), 'resume update receipt window drift'
        original['supportedModels'] = deepcopy(old)
        report['resume_from'] = {'artifact_sha256': fingerprint(previous), 'route_id': previous['route']['id'], 'skip_upstream_put': True}
    assert not any(r['publicModel'] == MODEL for r in routes), 'existing route is not owned'
    assert original['providerKind'] == 'qwen' and original['status'] == 'active'
    assert not original.get('defaultHeaders'), 'nonempty/masked headers need a separate lossless update decision'
    assert not original.get('metadata'), 'nonempty/redacted metadata needs a separate lossless update decision'
    assert not original.get('proxyUrl'), 'nonempty proxy settings need a separate lossless update decision'
    assert set(original.get('health') or {}) <= {'consecutiveFailures', 'lastSuccessAt', 'lastFailureAt', 'lastErrorCode',
        'circuitState', 'circuitOpenUntil', 'retryAfter', 'updatedBy'}, 'unknown/redacted health fields cannot be round-tripped'
    assert not any(marker in json.dumps(upstream_input(original)).lower() for marker in ('***', '[redacted]', '[masked]')), 'masked input cannot be round-tripped'
    assert MODEL not in original['supportedModels'], 'existing model entry is not owned'
    wanted = deepcopy(original)
    wanted['supportedModels'] = sorted(original['supportedModels'] + [MODEL])
    report['upstream'] = deepcopy(previous['upstream']) if previous is not None else {'id': UPSTREAM, 'original_models': original['supportedModels'], 'new_models': wanted['supportedModels'],
        'original_fields_sha256': fingerprint(original), 'updated_input_sha256': fingerprint(upstream_input(wanted)), 'cas': 'unavailable; immediate snapshot reread'}
    if previous is None:
        report['upstream']['original_field_hashes'] = {key: fingerprint(value) for key, value in upstream_input(original).items()}
    route = deepcopy(previous['route']) if previous is not None else {'id': str(uuid.uuid4()), 'publicModel': MODEL, 'providerKind': 'qwen', 'upstreamId': UPSTREAM, 'upstreamModel': MODEL,
        'enabled': True, 'priority': 0, 'weight': 1, 'metadata': {'task': 'Task123', 'action_key': ACTION, 'owner_nonce': uuid.uuid4().hex, 'nonDefault': True}}
    assert route['publicModel'] == route['upstreamModel'] == MODEL and route['upstreamId'] == UPSTREAM and route['providerKind'] == 'qwen'
    assert route['enabled'] is True
    assert route['metadata']['action_key'] == ACTION and route['metadata']['owner_nonce'] and route['metadata']['nonDefault'] is True
    if previous is not None:
        again, routes = inventory(gateway)
        assert fingerprint(again) == fingerprint(observed) and not any(r['publicModel'] == MODEL for r in routes), 'resume concurrent change'
    report['route'] = deepcopy(route)
    upstream_attempted, route_attempted = previous is not None, False
    route_known = False
    route_receipt = None
    try:
        if previous is None:
            current, current_routes = inventory(gateway)
            assert fingerprint(current) == fingerprint(original) and not any(r['publicModel'] == MODEL for r in current_routes), 'concurrent configuration change'
            upstream_attempted = True
            s, _ = api(gateway, 'PUT', '/relay/upstreams/' + UPSTREAM, upstream_input(wanted))
            report['upstream']['update_http'] = s
            assert s == 200
            current, current_routes = inventory(gateway)
            assert same_config(current, wanted) and current.get('health') == wanted.get('health')
            assert not any(r['publicModel'] == MODEL for r in current_routes), 'concurrent route appeared'
            report['upstream']['updated_fields_sha256'] = fingerprint(current)
        route_attempted = True
        s, b = api(gateway, 'POST', '/relay/model-routes', route)
        report['route_create_http'] = s
        route_known = 400 <= s < 500 and s != 408
        assert s in (200, 201) and b['data']['id'] == route['id']
        route_known = True
        route_receipt = deepcopy(b['data'])  # Exact POST receipt precedes any concurrent GET change.
        _, current_routes = inventory(gateway)
        candidate = next(r for r in current_routes if r['id'] == route['id'])
        assert route_fingerprint(candidate) == route_fingerprint(route_receipt), 'route changed since creation receipt'
        assert all(route_receipt.get(k) == v for k, v in route.items())
        report['route_fields_sha256'] = fingerprint(route_receipt)
        report['route_storage_fields_sha256'] = route_fingerprint(route_receipt)
        probe(gateway, report)
        report['status'] = 'PASS'
        report['live_configuration_retained'] = True
        report.pop('configuration_cleanup', None)
    finally:
        if report.get('status') != 'PASS' and upstream_attempted:
            cleanup_configuration(gateway, report, original, wanted, route, route_attempted, route_known, route_receipt)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute', action='store_true')
    parser.add_argument('--resume-known-append', action='store_true', help='A08 only: verify the recorded successful PUT and continue without repeating it')
    parser.add_argument('--evidence', required=True)
    args = parser.parse_args()
    if not args.execute:
        parser.error('explicit --execute is required for the registered A08 configuration action')
    report = {'identity': 'Task123/Hindsight/H1-model/A08', 'action_key': ACTION, 'status': 'FAIL',
        'started_utc': datetime.now(timezone.utc).isoformat(), 'source_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    path = Path(args.evidence)
    history = json.loads(path.read_text())
    try:
        previous = None
        if args.resume_known_append:
            assert not any(r.get('action_key') == ACTION and 'resume_from' in r for r in history.get('configuration_probes', [])), 'A08 resume already recorded; owner reconciliation required, no replay'
            matches = [r for r in history.get('configuration_probes', []) if r.get('action_key') == ACTION and r.get('upstream', {}).get('update_http') == 200 and 'resume_from' not in r]
            assert len(matches) == 1, 'ambiguous original A08 artifact'
            previous = matches[0]
        refreshed = subprocess.run(['soha', 'token', 'list', '--profile', 'local'], capture_output=True, timeout=50)
        assert refreshed.returncode == 0
        run(Gateway(json.loads((Path.home()/'.soha/config.json').read_text())['profiles']['local']), report, previous=previous)
    except Exception as error:
        report['error_class'] = type(error).__name__
    report['finished_utc'] = datetime.now(timezone.utc).isoformat()
    history.setdefault('configuration_probes', []).append(report)
    path.write_text(json.dumps(history, ensure_ascii=False, indent=2)+'\n')
    print(json.dumps(report, ensure_ascii=False, indent=2))
    raise SystemExit(0 if report['status'] == 'PASS' else 1)


if __name__ == '__main__':
    main()
