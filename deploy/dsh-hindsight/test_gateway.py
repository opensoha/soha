import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading
import time
import unittest
from unittest.mock import patch
from urllib.parse import urlsplit
import gateway

class GatewayTest(unittest.TestCase):
    def test_paths(self):
        bank = "/v1/default/banks/soha-abc"
        for method, path, body in [("PUT", bank, b'{"name":"soha-abc"}'), ("POST", bank+"/memories", b'{"async":true,"operation_id":"12345678-1234-1234-1234-123456789abc","items":[{"content":"fact","document_id":"doc","metadata":{"source_revision":"1"}}]}'), ("POST", bank+"/memories/recall", b'{"query":"fact","types":["world","experience"],"max_tokens":4096}'), ("GET", bank+"/operations/12345678-1234-1234-1234-123456789abc", b''), ("DELETE", bank+"/documents/doc-123", b'')]:
            self.assertTrue(gateway.allowed(method, path, body), path)
        for method, path, body in [("POST", bank+"/memories/recall", b'{"query":"fact","types":["observation"],"max_tokens":4096}'), ("POST", bank+"/memories", b'{"config":{}}'), ("POST", bank+"/reflect", b'{}'), ("PUT", bank, b'{"name":"soha-abc","config":{}}'), ("PUT", bank, b'{"name":"other"}'), ("POST", bank+"/memories%2frecall", b'{}'), ("POST", bank+"/memories?x=y", b'{}'), ("POST", bank+"/../reflect", b'{}')]:
            self.assertFalse(gateway.allowed(method, path, body), path)

    def test_http_boundary(self):
        server = gateway.Server(("127.0.0.1", 0), gateway.Handler)
        server.token = "x" * 32
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        connection = http.client.HTTPConnection
        try:
            def request(headers=None):
                conn = connection(*server.server_address, timeout=3)
                conn.request("GET", "/health", headers=headers or {})
                response = conn.getresponse()
                result = response.status, response.read()
                conn.close()
                return result
            auth = {"Authorization": "Bearer " + server.token}
            self.assertEqual(request()[0], 401)
            self.assertEqual(request({"Authorization": "Bearer wrong"})[0], 401)
            with patch.object(gateway.http.client, "HTTPConnection") as upstream:
                response = upstream.return_value.getresponse.return_value
                response.status, response.read.return_value = 200, b'{"status":"ok"}'
                self.assertEqual(request(auth), (200, b'{"status":"ok"}'))
                response.status = 302
                self.assertEqual(request(auth)[0], 502)
                response.status, response.read.return_value = 500, b'secret-debug'
                self.assertNotIn(b'secret-debug', request(auth)[1])
            self.assertEqual(request({**auth, "Content-Length": str(gateway.LIMIT+1)})[0], 413)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

class DerivedGatewayTest(unittest.TestCase):
    bank = "soha-" + "a" * 64
    operation = "11111111-1111-1111-1111-111111111111"
    child = "22222222-2222-2222-2222-222222222222"

    def setUp(self):
        state = self
        self.models = ["mm-0"]
        self.raw_exists = True
        self.other_raw_exists = True
        self.fail_model_once = False
        self.invalid = None
        self.delay = 0
        self.list_gate = None
        self.list_entered = threading.Event()
        self.calls = []
        self.operations = {}
        self.fail_operation_once = None
        self.state_lock = threading.Lock()

        class Upstream(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_GET(self):
                if "/operations/" in self.path:
                    with state.state_lock:
                        state.calls.append(("GET", self.path))
                        operation = self.path.rsplit("/", 1)[1]
                        data = state.operations.get(operation, {"status": "not_found", "operation_id": operation})
                        if operation == state.operation and "child_operations" in data:
                            data = {**data, "child_operations": [c for c in data["child_operations"]
                                                                if c["operation_id"] in state.operations]}
                    return self.send(200, json.dumps(data).encode())
                state.list_entered.set()
                if state.list_gate is not None:
                    state.list_gate.wait(2)
                time.sleep(state.delay)
                with state.state_lock:
                    state.calls.append(("GET", self.path))
                    items = [{"id": i, "bank_id": state.bank} for i in state.models[:100]]
                    if state.invalid == "id":
                        items = [{"id": "../escape", "bank_id": state.bank}]
                    if state.invalid == "bank":
                        items = [{"id": "mm-0", "bank_id": "other-bank"}]
                    if state.invalid == "oversized":
                        items = [{"id": str(i), "bank_id": state.bank} for i in range(101)]
                    data = {"items": items, "total": max(len(items), len(state.models)), "limit": 100, "offset": 0}
                self.send(200, b"invalid-json" if state.invalid == "json" else json.dumps(data).encode())

            def do_DELETE(self):
                with state.state_lock:
                    state.calls.append(("DELETE", self.path))
                    if "/documents/" in self.path:
                        status = 200 if state.raw_exists else 404
                        state.raw_exists = False
                        data = b'{"success":true,"document_id":"doc"}'
                    elif "/operations/" in self.path:
                        operation = self.path.split("/")[-2]
                        if state.fail_operation_once == operation:
                            state.fail_operation_once = None
                            status, data = 503, b"PRIVATE_OPERATION_DIAGNOSTIC"
                        else:
                            status = 200 if state.operations.pop(operation, None) is not None else 404
                            data = json.dumps({"operation_id": operation, "success": status == 200}).encode()
                    elif state.fail_model_once:
                        state.fail_model_once = False
                        status, data = 500, b"PRIVATE_MODEL_DIAGNOSTIC"
                    else:
                        model = self.path.rsplit("/", 1)[1]
                        status = 200 if model in state.models else 404
                        if model in state.models:
                            state.models.remove(model)
                        data = b'{"status":"deleted"}'
                self.send(status, data)

            def send(self, status, data):
                try:
                    self.send_response(status)
                    self.send_header("Content-Length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                except (BrokenPipeError, ConnectionResetError):
                    pass

        self.upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        self.upstream.daemon_threads = True
        self.server = gateway.Server(("127.0.0.1", 0), gateway.Handler)
        self.server.token = "x" * 32
        self.server.upstream_port = self.upstream.server_port
        self.threads = []
        for server in (self.upstream, self.server):
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            self.threads.append(thread)

    def tearDown(self):
        if self.list_gate:
            self.list_gate.set()
        for server in (self.server, self.upstream):
            server.shutdown()
            server.server_close()
        for thread in self.threads:
            thread.join(2)
            self.assertFalse(thread.is_alive())

    def request(self, method="DELETE", suffix="/documents/doc", operation=None):
        conn = http.client.HTTPConnection(*self.server.server_address, timeout=3)
        try:
            headers = {"Authorization": "Bearer " + self.server.token}
            if operation is not None:
                headers[gateway.RETAIN_OPERATION_HEADER] = operation
            conn.request(method, "/v1/default/banks/" + self.bank + suffix, headers=headers)
            res = conn.getresponse()
            if operation is not None:
                return res.status, res.read(), res.getheader(gateway.PURGED_OPERATION_HEADER)
            return res.status, res.read()
        finally:
            conn.close()

    def test_success_and_raw404_purge_multiple_first_pages(self):
        for raw_exists in (True, False):
            with self.subTest(raw_exists=raw_exists):
                self.raw_exists = raw_exists
                self.models = ["mm-" + str(i) for i in range(203)]
                self.calls = []
                self.assertEqual(self.request()[0], 200 if raw_exists else 404)
                self.assertEqual(self.models, [])
                self.assertTrue(self.other_raw_exists)
                gets = [p for method, p in self.calls if method == "GET"]
                self.assertEqual(len(gets), 4)
                self.assertTrue(all(urlsplit(p).query == "detail=metadata&limit=100&offset=0" for p in gets))
                self.assertEqual(len([p for method, p in self.calls if method == "DELETE" and "/documents/" in p]), 1)

    def test_model_failure_is_redacted_and_raw404_retry_continues(self):
        self.fail_model_once = True
        status, body = self.request()
        self.assertEqual(status, 502)
        self.assertNotIn(b"PRIVATE_MODEL", body)
        self.assertEqual(self.models, ["mm-0"])
        self.assertEqual(self.request()[0], 404)
        self.assertEqual(self.models, [])

    def test_invalid_pages_and_model_id_fail_closed(self):
        for invalid in ("id", "bank", "oversized", "json"):
            with self.subTest(invalid=invalid):
                self.invalid = invalid
                self.calls = []
                self.assertEqual(self.request()[0], 502)
                self.assertFalse(any("/mental-models/" in p for method, p in self.calls if method == "DELETE"))

    def test_limit_failure_retries_without_skipping_remaining_models(self):
        self.models = ["mm-0", "mm-1", "mm-2"]
        with patch.object(gateway, "PURGE_LIMIT", 2):
            self.assertEqual(self.request()[0], 502)
            self.assertEqual(self.models, ["mm-2"])
            self.assertEqual(self.request()[0], 404)
            self.assertEqual(self.models, [])

    def test_total_deadline_interrupts_slow_upstream(self):
        self.delay = 0.3
        with patch.object(gateway, "DELETE_TIMEOUT", 0.05):
            start = time.monotonic()
            self.assertEqual(self.request()[0], 502)
            self.assertLess(time.monotonic() - start, 0.5)
        self.assertEqual(self.models, ["mm-0"])

    def test_no_early_ack_and_concurrent_delete_rejected(self):
        self.list_gate = threading.Event()
        result = []
        thread = threading.Thread(target=lambda: result.append(self.request()))
        thread.start()
        try:
            self.assertTrue(self.list_entered.wait(1))
            self.assertEqual(result, [])
            self.assertEqual(self.request()[0], 503)
            self.assertEqual(result, [])
        finally:
            self.list_gate.set()
            thread.join(2)
        self.assertFalse(thread.is_alive())
        self.assertEqual(result[0][0], 200)
        self.assertEqual(self.models, [])

    def test_external_mental_model_management_still_forbidden(self):
        for method, suffix in (("GET", "/mental-models"), ("DELETE", "/mental-models/mm-0"),
                               ("GET", "/mental-models/mm-0/history"), ("POST", "/mental-models/mm-0/refresh")):
            self.assertEqual(self.request(method, suffix)[0], 403)
        self.assertEqual(self.calls, [])

    def seed_retain(self, document="doc", child_status="completed"):
        self.operations = {
            self.operation: {
                "operation_id": self.operation,
                "operation_type": "batch_retain",
                "status": "completed",
                "result_metadata": {"is_parent": True, "items_count": 1, "document_id": document},
                "child_operations": [{"operation_id": self.child, "status": child_status}],
            },
            self.child: {"task_payload": {"contents": [{"content": "private fact", "document_id": document}]}},
        }

    def test_purge_retained_payload_before_ack_and_retry_parent(self):
        self.seed_retain()
        self.fail_operation_once = self.operation
        status, body, purged = self.request(operation=self.operation)
        self.assertEqual(status, 502)
        self.assertIsNone(purged)
        self.assertNotIn(b"PRIVATE_OPERATION", body)
        self.assertEqual(list(self.operations), [self.operation])
        self.assertEqual(self.request(operation=self.operation)[::2], (404, self.operation))
        self.assertEqual(self.operations, {})
        self.assertEqual(self.request(operation=self.operation)[::2], (404, self.operation))

    def test_retain_purge_requires_exact_settled_document(self):
        for document, state in (("another-doc", "completed"), ("doc", "processing")):
            with self.subTest(document=document, state=state):
                self.seed_retain(document, state)
                self.calls = []
                self.assertEqual(self.request(operation=self.operation)[::2], (502, None))
                self.assertTrue(self.raw_exists)
                self.assertEqual([method for method, _ in self.calls], ["GET"])
        self.assertEqual(self.request("DELETE", "/operations/" + self.child + "/delete")[0], 403)
        self.assertEqual(self.request(operation="../invalid")[::2], (400, None))


if __name__ == "__main__":
    unittest.main()
