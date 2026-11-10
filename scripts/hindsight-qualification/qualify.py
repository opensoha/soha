#!/usr/bin/env python3
"""Destructive only to freshly created banks; use a disposable local service."""
import argparse
import json
import urllib.error
import urllib.parse
import urllib.request
import uuid
import time


def request(base, method, path, payload=None, deadline=None):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(base + path, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    # Qualification never inherits proxy credentials or follows a redirect.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    timeout = 300 if deadline is None else min(300, deadline - time.monotonic())
    if timeout <= 0:
        raise TimeoutError('qualification deadline')
    with opener.open(req, timeout=timeout) as response:
        return json.load(response)


def qualify(base, full=False, report=None, late_control=None, manual_models=False, purge_control=None, storage_counts=None, observation_storage=None):
    parsed = urllib.parse.urlsplit(base)
    if parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or not parsed.port or parsed.path or parsed.query or parsed.fragment or parsed.username:
        raise ValueError("require an explicitly allocated http://127.0.0.1:PORT disposable service")
    prefix = "task123-hq-" + uuid.uuid4().hex
    banks = [prefix + suffix for suffix in ("-user-a-project-1", "-user-a-project-2", "-user-b")]
    owned = []
    manual_release = None
    manual_operation = None
    manual_id = None
    facts = ["HQ_CANARY_A1 prefers cobalt tea.", "HQ_CANARY_A2 prefers amber tea.", "HQ_CANARY_B prefers violet tea."]
    if report is None:
        report = {}
    report.update({"bank_prefix": prefix, "checks": {}, "cleanup": []})
    def path(bank, tail=""):
        return "/v1/default/banks/" + bank + tail
    def recall(bank):
        return request(base, "POST", path(bank, "/memories/recall"), {
            "query": "Which tea is preferred?", "types": ["world", "experience"],
            "include": {"entities": None, "chunks": {}, "source_facts": {}}, "max_tokens": 4096})
    try:
        for bank, fact in zip(banks, facts):
            report["stage"] = "retain:" + bank.rsplit("-", 1)[-1]
            # Random identity + preflight avoid touching an existing bank on collision.
            try:
                request(base, "GET", path(bank, "/config"))
            except urllib.error.HTTPError as error:
                if error.code != 404:
                    raise
            else:
                raise RuntimeError("bank already exists; refusing overwrite")
            owned.append(bank)  # include uncertain create results in cleanup
            request(base, "PUT", path(bank), {"name": bank})
            result = request(base, "POST", path(bank, "/memories"), {"async": False, "items": [{
                "content": fact, "document_id": "source-r1", "metadata": {"source_id": "synthetic-message", "source_revision": "1"},
                "tags": ["scope:isolated"]}]})
            assert result["success"] and not result["async"], result
        for bank, fact in zip(banks, facts):
            report["stage"] = "recall-provenance"
            recalled = recall(bank)
            results = recalled["results"]
            assert results and any(fact.split()[0] in item["text"] for item in results), recalled
            assert all(item.get("document_id") == "source-r1" for item in results), recalled
            assert all((item.get("metadata") or {}).get("source_revision") == "1" for item in results), recalled
            foreign = [other.split()[0] for other in facts if other != fact]
            assert all(marker not in json.dumps(recalled) for marker in foreign), recalled
        report["checks"]["retain_recall_provenance_three_banks"] = "PASS"
        if full:
            report["stage"] = "consolidate-observation"
            operation = request(base, "POST", path(banks[0], "/consolidate"), {})
            report["consolidation_operation"] = operation["operation_id"]
            wait_operation(base, banks[0], operation["operation_id"], report)
            derived = request(base, "POST", path(banks[0], "/memories/recall"), {
                "query": "Which tea is preferred?", "types": ["observation"], "include": {"source_facts": {}}, "max_tokens": 2048})
            observations = derived["results"]
            assert observations, "consolidation produced no observation; derived deletion unproved"
            assert all(item.get("source_fact_ids") for item in observations), "missing observation provenance"
            source_facts = derived.get("source_facts") or {}
            assert not derived.get("source_facts_truncated"), "truncated observation provenance"
            assert all(source_id in source_facts for item in observations for source_id in item["source_fact_ids"])
            assert all(item.get("document_id") == "source-r1" and (item.get("metadata") or {}).get("source_revision") == "1" for item in source_facts.values())
            report["observation_count"] = len(observations)
            report["checks"]["real_observation_provenance"] = "PASS"
            if observation_storage:
                report["observation_storage_before"] = observation_storage(banks[0], "source-r1")
                assert {item["id"] for item in observations} <= set(report["observation_storage_before"]["observation_ids"])

        if manual_models:
            report["stage"] = "manual-mental-model-generation"
            manual_id = "hq-manual-profile"
            created = request(base, "POST", path(banks[0], "/mental-models"), {
                "id": manual_id, "name": "Qualification tea preference", "source_query": "What tea does HQ_CANARY_A1 prefer? Use current memory evidence and answer in one sentence.",
                "tags": ["scope:isolated"], "max_tokens": 256,
                "trigger": {"refresh_after_consolidation": False, "budget": "low", "exclude_mental_models": True}})
            wait_operation(base, banks[0], created["operation_id"], report)
            model = request(base, "GET", path(banks[0], "/mental-models/" + manual_id))
            report["manual_model_before"] = model
            assert model.get("content") and "cobalt" in model["content"].lower(), "mental model has no actual stated preference"
            based_on = (model.get("reflect_response") or {}).get("based_on") or {}
            cited = [entry.get("id") if isinstance(entry, dict) else entry for entries in based_on.values() if isinstance(entries, list) for entry in entries]
            known = {item["id"] for item in observations} | set(source_facts)
            assert cited and set(cited) & known, "mental model lacks current source grounding"
            report["checks"]["manual_mental_model_real_grounding"] = "PASS"
            request(base, "PATCH", path(banks[0], "/mental-models/" + manual_id), {
                "source_query": "What tea does HQ_CANARY_A1 prefer? Use current memory evidence and respond as a Markdown table with Person and Preferred tea columns."})
            refreshed = request(base, "POST", path(banks[0], "/mental-models/" + manual_id + "/refresh"), {"mode": "full"})
            wait_operation(base, banks[0], refreshed["operation_id"], report)
            actual_history = request(base, "GET", path(banks[0], "/mental-models/" + manual_id + "/history"))
            assert isinstance(actual_history, list) and any("cobalt" in (entry.get("previous_content") or "").lower() for entry in actual_history), "no real fact-bearing historical model snapshot"
            report["manual_history_before_delete"] = actual_history
            report["checks"]["manual_history_real_generated_content"] = "PASS"
            assert late_control is not None
            entered, manual_release = late_control()
            try:
                refreshing = request(base, "POST", path(banks[0], "/mental-models/" + manual_id + "/refresh"), {"mode": "full"})
                manual_operation = refreshing["operation_id"]
                assert entered.wait(timeout=45), "manual model refresh did not reach model barrier"
            except Exception:
                manual_release.set()
                raise
        report["stage"] = "delete-source-with-observations"
        deleted = request(base, "DELETE", path(banks[0], "/documents/source-r1"))
        assert deleted["success"] and deleted["document_id"] == "source-r1" and deleted["memory_units_deleted"] >= 1, deleted
        assert not recall(banks[0])["results"], "deleted raw facts still recalled"
        assert recall(banks[1])["results"] and recall(banks[2])["results"], "delete crossed banks"
        report["checks"]["delete_raw_source_and_bank_isolation"] = "PASS"
        if full:
            derived = request(base, "POST", path(banks[0], "/memories/recall"), {"query": "Which tea is preferred?", "types": ["observation"], "max_tokens": 2048})
            assert not derived["results"], "deleted source observations still recalled"
            report["checks"]["derived_observation_delete"] = "PASS"
            if observation_storage:
                before = report["observation_storage_before"]
                after = observation_storage(banks[0], "source-r1", before)
                report["observation_storage_after_source_delete"] = after
                assert after["observation_rows"] == 0 and after["history_rows"] == 0, "observation/history rows survived source deletion"
                report["checks"]["observation_history_delete"] = "PASS" if before["history_rows"] > 0 and before["canary_history_rows"] > 0 else "NOT_PROVEN_HISTORY"

            report["stage"] = "correction-new-revision"
            if manual_models:
                report["stage"] = "manual-model-source-and-model-delete"
                try:
                    after_source = request(base, "GET", path(banks[0], "/mental-models/" + manual_id))
                    history = request(base, "GET", path(banks[0], "/mental-models/" + manual_id + "/history"))
                    report["manual_model_after_source_delete"] = after_source
                    report["manual_history_after_source_delete"] = history
                    residue = "cobalt" in json.dumps(after_source).lower() or "cobalt" in json.dumps(history).lower()
                    report["checks"]["manual_model_source_delete"] = "UNSAFE_CONTENT_OR_HISTORY_REMAINS" if residue else "PASS"
                    if storage_counts:
                        report["manual_storage_after_source_delete"] = storage_counts(banks[0], manual_id)
                        assert report["manual_storage_after_source_delete"]["models"] == 1 and report["manual_storage_after_source_delete"]["history"] > 0
                    report["manual_purge_route"] = "production_guard_document_delete" if purge_control else "native_explicit_model_delete"
                    report["manual_purge_receipt"] = purge_control(banks[0], "source-r1") if purge_control else request(base, "DELETE", path(banks[0], "/mental-models/" + manual_id))
                    if storage_counts:
                        report["manual_storage_after_purge"] = storage_counts(banks[0], manual_id)
                        assert report["manual_storage_after_purge"] == {"models": 0, "history": 0}

                finally:
                    manual_release.set()
                wait_operation(base, banks[0], manual_operation, report, allow_cancelled=True)
                model_status, model_body = optional_get(base, path(banks[0], "/mental-models/" + manual_id))
                history_status, history_body = optional_get(base, path(banks[0], "/mental-models/" + manual_id + "/history"))
                report["manual_model_after_explicit_delete"] = {"http": model_status, "body": model_body}
                report["manual_history_after_explicit_delete"] = {"http": history_status, "body": history_body}
                cleared = model_status == 404 and (history_status == 404 or not history_body)
                report["checks"]["manual_model_history_purge_inflight_refresh"] = "PASS" if cleared else "UNSAFE_REMAINS_OR_REAPPEARED"
                if storage_counts:
                    report["manual_storage_after_late_refresh"] = storage_counts(banks[0], manual_id)
                    assert report["manual_storage_after_late_refresh"] == {"models": 0, "history": 0}

            report["stage"] = "correction-new-revision"
            corrected = request(base, "POST", path(banks[0], "/memories"), {"async": False, "items": [{
                "content": "HQ_CANARY_A1 now prefers jasmine tea.", "document_id": "source-r2", "metadata": {"source_revision": "2"}}]})
            assert corrected["success"] and not corrected["async"]
            corrected_recall = recall(banks[0])["results"]
            assert corrected_recall and any("jasmine" in item["text"].lower() for item in corrected_recall)
            assert all(item.get("document_id") == "source-r2" and (item.get("metadata") or {}).get("source_revision") == "2" for item in corrected_recall)
            report["checks"]["correction_new_revision_only"] = "PASS"
            if purge_control:
                report["protected_200_delete"] = purge_control(banks[0], "source-r2")
                assert report["protected_200_delete"]["http"] == 200
            else:
                request(base, "DELETE", path(banks[0], "/documents/source-r2"))
            assert not recall(banks[0])["results"]
            report["checks"]["corrected_source_delete"] = "PASS"
            report["checks"]["late_retain_after_delete"] = "NOT_RUN"
            if late_control is not None:
                report["stage"] = "late-retain-delete-barrier"
                entered, release = late_control()
                operation_id = str(uuid.uuid4())
                try:
                    receipt = request(base, "POST", path(banks[0], "/memories"), {"async": True, "operation_id": operation_id, "items": [{
                        "content": "HQ_CANARY_A1 prefers emerald tea.", "document_id": "source-late", "metadata": {"source_revision": "3"}}]})
                    assert receipt["operation_id"] == operation_id
                    assert entered.wait(timeout=45), "late retain never reached model barrier"
                    try:
                        request(base, "DELETE", path(banks[0], "/documents/source-late"))
                    except urllib.error.HTTPError as error:
                        assert error.code == 404
                finally:
                    release.set()
                wait_operation(base, banks[0], operation_id, report)
                late = recall(banks[0])["results"]
                report["checks"]["late_retain_after_delete"] = "UNSAFE_REAPPEARED" if late else "PASS"
                report["late_result_count"] = len(late)
                request(base, "DELETE", path(banks[0], "/documents/source-late"))
                assert not recall(banks[0])["results"]
            report["checks"].update({"mental_models_delete": "SEE_MANUAL_MODEL_CHECKS" if manual_models else "NOT_RUN", "authorization": "NOT_RUN", "real_gateway_quality": "PASS; synthetic factual recall only"})
        else:
            report["checks"].update({"derived_memory_delete": "NOT_RUN", "authorization": "NOT_RUN", "real_gateway_quality": "NOT_RUN"})
    finally:
        if manual_release is not None:
            manual_release.set()
        failures = []
        for bank in owned:
            try:
                request(base, "DELETE", path(bank))
                report["cleanup"].append({"bank": bank, "status": "deleted"})
            except Exception as error:
                failures.append({"bank": bank, "error_type": type(error).__name__})
        if failures:
            report["cleanup_failures"] = failures
            raise RuntimeError("cleanup failed: " + json.dumps(failures))
    return report


def final_refresh_proof(terminal):
    assert terminal['status'] in ('completed', 'failed'), 'cooperative cancellation/not-found is not a worker settlement fence'
    return 'PASS' if terminal['status'] == 'completed' else 'NOT_PROVEN_FAILED_REFRESH'


def wait_final_barrier(base, bank, operation_id, entered, report, deadline):
    states = report.setdefault('final_barrier_operation_states', [])
    while time.monotonic() < deadline:
        result = request(base, 'GET', '/v1/default/banks/' + bank + '/operations/' + operation_id, deadline=deadline)
        state = result['status']
        if not states or states[-1] != state:
            states.append(state)
        if entered.is_set():
            assert state == 'processing', 'final response must pause a genuinely processing operation'
            return
        if state not in ('pending', 'processing'):
            report['qualifier_barrier_failure'] = {'status': 'FAIL_QUALIFIER_BARRIER', 'operation_id': operation_id, 'terminal': result}
            raise AssertionError('FAIL_QUALIFIER_BARRIER: operation ended without matching final wire')
        entered.wait(timeout=max(0, min(.5, deadline-time.monotonic())))
    raise TimeoutError('qualification final barrier deadline')


def qualify_remaining(base, report, late_control, purge_control, storage_counts, observation_storage, deadline):
    """A06 only: reuse A05 Core/isolation/MM-history proof without rerunning it."""
    prefix = 'task123-remaining-' + uuid.uuid4().hex
    bank = prefix
    root = '/v1/default/banks/' + bank
    report.update({'bank_prefix': prefix, 'checks': {}, 'cleanup': [], 'reuses': 'H1/A05 Core guard, three-bank isolation, complete provenance, fact-bearing MM history purge'})
    owned = False
    release = None
    documents = ['source-r1']
    def call(method, path, payload=None):
        return request(base, method, path, payload, deadline=deadline)
    def retain(document, revision, text):
        result = call('POST', root + '/memories', {'async': False, 'items': [{
            'content': text, 'document_id': document, 'metadata': {'source_revision': str(revision)}, 'tags': ['scope:isolated']}]})
        assert result['success'] and not result['async']
    def recall():
        return call('POST', root + '/memories/recall', {'query': 'Which tea is preferred?', 'types': ['world', 'experience'], 'max_tokens': 4096})['results']
    def consolidate():
        operation = call('POST', root + '/consolidate', {})
        wait_operation(base, bank, operation['operation_id'], report, deadline=deadline)
    try:
        status, _ = optional_get(base, root + '/config', deadline=deadline)
        assert status == 404, 'refuse existing bank'
        owned = True
        call('PUT', root, {'name': bank})
        report['stage'] = 'observation-history-initial'
        retain('source-r1', 1, 'HQ_CANARY_A1 prefers cobalt tea.')
        consolidate()
        snapshot = observation_storage(bank, documents)
        report['observation_history_initial'] = snapshot
        witness = False
        report['observation_update_rounds'] = []
        details = ['HQ_CANARY_A1 prefers cobalt tea served chilled in a glass cup.',
                   "More detail about HQ_CANARY_A1's cobalt tea preference: it must be unsweetened as well as chilled."]
        for index, text in enumerate(details, 1):
            report['stage'] = 'observation-history-update-' + str(index)
            previous_ids = set(snapshot['observation_ids'])
            document = 'source-detail-' + str(index)
            documents.append(document)
            retain(document, 1, text)
            consolidate()
            snapshot = observation_storage(bank, documents)
            report['observation_update_rounds'].append(snapshot)
            witness = any(row['observation_id'] in previous_ids and 'cobalt' in (row.get('previous_text') or '').lower() for row in snapshot['history_previous_text'])
            if witness:
                break
        report['checks']['observation_history_real_update'] = 'PASS' if witness else 'NOT_PROVEN_HISTORY'
        report['stage'] = 'manual-final-response-create'
        model_id = 'hq-manual-profile'
        created = call('POST', root + '/mental-models', {'id': model_id, 'name': 'Qualification preference',
            'source_query': 'What tea does HQ_CANARY_A1 prefer? Use current stored evidence and answer in one sentence.',
            'tags': ['scope:isolated'], 'max_tokens': 256, 'trigger': {'refresh_after_consolidation': False, 'budget': 'low', 'exclude_mental_models': True}})
        wait_operation(base, bank, created['operation_id'], report, deadline=deadline)
        model = call('GET', root + '/mental-models/' + model_id)
        assert 'cobalt' in (model.get('content') or '').lower()
        based_on = (model.get('reflect_response') or {}).get('based_on') or {}
        assert any(based_on.get(key) for key in ('world', 'observation'))
        report['manual_model_before_final_refresh'] = model
        report['stage'] = 'manual-after-final-response'
        entered, release = late_control(after_final=True, marker='cobalt')
        refreshing = call('POST', root + '/mental-models/' + model_id + '/refresh', {'mode': 'full'})
        wait_final_barrier(base, bank, refreshing['operation_id'], entered, report, deadline)
        report['final_barrier_entered'] = True
        try:
            report['stage'] = 'manual-source-delete-and-purge'
            report['native_source_deletes'] = []
            for document in documents:
                deleted = call('DELETE', root + '/documents/' + document)
                assert deleted['success'] and deleted['document_id'] == document
                report['native_source_deletes'].append(deleted)
            after = observation_storage(bank, documents, snapshot)
            report['observation_storage_after_source_delete'] = after
            assert after['observation_rows'] == 0 and after['history_rows'] == 0
            report['checks']['observation_history_delete'] = 'PASS' if witness else 'NOT_PROVEN_HISTORY'
            report['manual_model_after_source_delete'] = call('GET', root + '/mental-models/' + model_id)
            report['manual_history_after_source_delete'] = call('GET', root + '/mental-models/' + model_id + '/history')
            report['manual_storage_after_source_delete'] = storage_counts(bank, model_id)
            report['checks']['native_manual_model_source_delete'] = 'UNSAFE_CONTENT_REMAINS' if 'cobalt' in json.dumps(report['manual_model_after_source_delete']).lower() else 'PASS'
            report['manual_purge_receipt'] = purge_control(bank, 'source-r1')
            assert report['manual_purge_receipt']['http'] == 404
            report['manual_storage_after_purge'] = storage_counts(bank, model_id)
            assert report['manual_storage_after_purge'] == {'models': 0, 'history': 0}
        finally:
            release.set()
        report['stage'] = 'manual-final-response-settlement'
        terminal = wait_operation(base, bank, refreshing['operation_id'], report, allow_cancelled=True, deadline=deadline)
        refresh_proof = final_refresh_proof(terminal)
        report['manual_refresh_terminal'] = terminal
        report['manual_storage_after_final_response'] = storage_counts(bank, model_id)
        assert report['manual_storage_after_final_response'] == {'models': 0, 'history': 0}
        assert optional_get(base, root + '/mental-models/' + model_id, deadline=deadline)[0] == 404
        assert optional_get(base, root + '/mental-models/' + model_id + '/history', deadline=deadline)[0] == 404
        report['checks']['guard_final_response_purge_no_resurrection'] = refresh_proof
        report['stage'] = 'correction-revision2'
        retain('source-r2', 2, 'HQ_CANARY_A1 now prefers jasmine tea.')
        results = recall()
        assert results and any('jasmine' in item['text'].lower() for item in results)
        assert all(item.get('document_id') == 'source-r2' and (item.get('metadata') or {}).get('source_revision') == '2' for item in results)
        report['checks']['correction_new_revision_only'] = 'PASS'
        report['protected_200_delete'] = purge_control(bank, 'source-r2')
        assert report['protected_200_delete']['http'] == 200 and not recall()
        report['checks']['guard_200_delete'] = 'PASS'
        report['stage'] = 'late-retain-native-counterexample'
        entered, release = late_control()
        operation_id = str(uuid.uuid4())
        try:
            receipt = call('POST', root + '/memories', {'async': True, 'operation_id': operation_id, 'items': [{
                'content': 'HQ_CANARY_A1 prefers emerald tea.', 'document_id': 'source-late', 'metadata': {'source_revision': '3'}}]})
            assert receipt['operation_id'] == operation_id
            assert entered.wait(timeout=max(0, min(45, deadline - time.monotonic())))
            try:
                report['late_native_delete'] = {'http': 200, 'receipt': call('DELETE', root + '/documents/source-late')}
            except urllib.error.HTTPError as error:
                assert error.code == 404
                report['late_native_delete'] = {'http': 404}
        finally:
            release.set()
        outcome = wait_operation(base, bank, operation_id, report, deadline=deadline)
        metadata = outcome.get('result_metadata') or {}
        assert metadata.get('extraction_errors_count') == 0 and metadata.get('unit_ids_count', 0) >= 1
        assert outcome.get('child_operations') and all(item.get('status') == 'completed' for item in outcome['child_operations'])
        report['late_retain_terminal'] = {'status': outcome['status'], 'result_metadata': metadata}
        late = recall()
        report['late_recalled_results'] = late
        report['checks']['native_late_retain_after_delete'] = 'UNSAFE_REAPPEARED' if late else 'PASS'
        report['late_terminal_cleanup'] = purge_control(bank, 'source-late')
        assert report['late_terminal_cleanup']['http'] in (200, 404) and not recall()
        report['checks']['late_terminal_guard_cleanup'] = 'PASS'
    finally:
        if release is not None:
            release.set()
        if owned:
            try:
                request(base, 'DELETE', root)
                report['cleanup'].append({'bank': bank, 'status': 'deleted'})
            except Exception as error:
                report['cleanup_failures'] = [{'bank': bank, 'error_type': type(error).__name__}]
                raise
    return report


def optional_get(base, path, deadline=None):
    try:
        return 200, request(base, "GET", path, deadline=deadline)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return 404, None
        raise


def wait_operation(base, bank, operation_id, report, allow_cancelled=False, deadline=None):
    deadline = time.monotonic() + 120 if deadline is None else deadline
    states = []
    report.setdefault("operations", []).append({"id": operation_id, "states": states})
    while time.monotonic() < deadline:
        result = request(base, "GET", "/v1/default/banks/" + bank + "/operations/" + operation_id, deadline=deadline)
        state = result["status"]
        if not states or states[-1] != state:
            states.append(state)
        if state == "completed" or (allow_cancelled and state in ("failed", "cancelled", "not_found")):
            return result
        assert state in ("pending", "processing"), "background operation did not complete"
        time.sleep(.5)
    raise TimeoutError("background operation deadline")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    print(json.dumps(qualify(parser.parse_args().base_url), indent=2))
