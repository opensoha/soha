"""Deployment-only Hindsight capability gateway; never logs credentials or bodies."""
import hmac
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import socket
import threading
import time

LIMIT = 1024 * 1024
SLOTS = threading.BoundedSemaphore(8)
DELETE_SLOT = threading.Lock()
DELETE_TIMEOUT = 20
PURGE_LIMIT = 1000
PAGE_SIZE = 100
BANK = r"/v1/default/banks/([A-Za-z0-9_-]{1,128})"
OPERATION_ID = r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}"
DOCUMENT = BANK + r"/documents/([A-Za-z0-9_-]{1,256})"
RETAIN_OPERATION_HEADER = "X-Soha-Retain-Operation-ID"
PURGED_OPERATION_HEADER = "X-Soha-Retain-Operation-Purged"

def upstream(server, method, path, body=b"", deadline=None):
    """Fixed loopback origin, bounded bytes and a wall deadline, including slow reads."""
    deadline = deadline if deadline is not None else time.monotonic() + 25
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError()
    conn = http.client.HTTPConnection("127.0.0.1", server.upstream_port, timeout=remaining)
    response = timer = None
    try:
        conn.connect()
        sock = conn.sock
        def interrupt():
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError()
        timer = threading.Timer(remaining, interrupt)
        timer.daemon = True
        timer.start()
        conn.request(method, path, body=body, headers={"Content-Type": "application/json"})
        response = conn.getresponse()
        data = response.read(2 * LIMIT + 1)
        if len(data) > 2 * LIMIT or time.monotonic() >= deadline:
            raise ValueError("upstream limit")
        return response.status, data
    finally:
        if timer:
            timer.cancel()
        if response:
            response.close()
        conn.close()

def purge_derived(server, bank, deadline):
    """Soha-only banks have no supported hand-authored MM/knowledge-page data.

    Delete MM roots; the pinned schema cascades their history. Re-read page zero
    after each batch: incrementing offsets while deleting would skip survivors.
    """
    base = "/v1/default/banks/" + bank + "/mental-models"
    deleted = 0
    while True:
        status, data = upstream(server, "GET", base + f"?detail=metadata&limit={PAGE_SIZE}&offset=0", deadline=deadline)
        if status == 404:  # The bank itself is absent; its MM roots cascade too.
            return
        if status != 200:
            raise ValueError("model listing failed")
        page = json.loads(data, object_pairs_hook=object_pairs)
        if (not isinstance(page, dict) or not isinstance(page.get("items"), list)
            or type(page.get("total")) is not int or page["total"] < len(page["items"])
            or page.get("limit") != PAGE_SIZE or page.get("offset") != 0
            or len(page["items"]) > PAGE_SIZE):
            raise ValueError("invalid model page")
        if not page["items"]:
            if page["total"] != 0:
                raise ValueError("incomplete model page")
            return
        seen = set()
        for item in page["items"]:
            if (not isinstance(item, dict) or item.get("bank_id") != bank
                or not isinstance(item.get("id"), str)
                or not re.fullmatch(r"[A-Za-z0-9_-]{1,256}", item["id"])
                or item["id"] in seen or deleted >= PURGE_LIMIT):
                raise ValueError("invalid model identity or purge limit")
            seen.add(item["id"])
            status, _ = upstream(server, "DELETE", base + "/" + item["id"], deadline=deadline)
            if status not in (200, 404):
                raise ValueError("model deletion failed")
            deleted += 1

def read_operation(server, path, operation, bank, deadline):
    status, data = upstream(server, "GET", path, deadline=deadline)
    if status != 200:
        raise ValueError("operation lookup failed")
    result = json.loads(data, object_pairs_hook=object_pairs)
    if (not isinstance(result, dict) or result.get("operation_id") != operation
            or result.get("bank_id") not in (None, bank)):
        raise ValueError("invalid operation identity")
    return result

def retain_operations(server, bank, document, operation, deadline):
    """Resolve only the settled retain for this exact document and its child.

    Core supplies the durable outbox dependency after confirming settlement.
    The parent remains queryable until child cleanup completes, including retries.
    """
    base = "/v1/default/banks/" + bank + "/operations/"
    result = read_operation(server, base + operation, operation, bank, deadline)
    state = result.get("status")
    if state == "not_found":
        return []
    metadata = result.get("result_metadata")
    if (state not in ("completed", "failed", "cancelled")
            or not isinstance(metadata, dict) or metadata.get("document_id") != document):
        raise ValueError("operation not settled for document")
    kind = result.get("operation_type")
    if kind == "retain" and state == "completed" and not metadata.get("parent_operation_id"):
        return [operation]
    children = result.get("child_operations")
    if (kind != "batch_retain" or metadata.get("is_parent") is not True
            or type(metadata.get("items_count")) is not int or metadata["items_count"] != 1
            or not isinstance(children, list) or len(children) > 1):
        raise ValueError("invalid retain operation group")
    operations = []
    for child in children:
        if not isinstance(child, dict):
            raise ValueError("invalid retain child")
        child_id = child.get("operation_id")
        if (not isinstance(child_id, str) or not re.fullmatch(OPERATION_ID, child_id)
                or child_id.lower() == operation.lower() or child.get("status") != "completed"):
            raise ValueError("retain child not settled")
        operations.append(child_id)
    return operations + [operation]

def purge_operations(server, bank, operations, deadline):
    for operation in operations:
        path = "/v1/default/banks/" + bank + "/operations/" + operation
        status, data = upstream(server, "DELETE", path + "/delete", deadline=deadline)
        if status == 404 and read_operation(server, path, operation, bank, deadline).get("status") == "not_found":
            continue
        if status != 200:
            raise ValueError("operation deletion failed")
        result = json.loads(data, object_pairs_hook=object_pairs)
        if not isinstance(result, dict) or result.get("operation_id") != operation or result.get("success") is not True:
            raise ValueError("invalid operation deletion receipt")

def object_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate key")
        result[key] = value
    return result

def payload_allowed(method, path, body):
    data = json.loads(body, object_pairs_hook=object_pairs)
    if not isinstance(data, dict):
        return False
    match = re.fullmatch(BANK, path)
    if method == "PUT" and match:
        return data == {"name": match[1]}
    if method != "POST":
        return False
    if re.fullmatch(BANK + r"/memories/recall", path):
        return (set(data) == {"query", "types", "max_tokens"}
                and isinstance(data["query"], str)
                and isinstance(data["types"], list) and bool(data["types"])
                and all(t in ("world", "experience") for t in data["types"])
                and type(data["max_tokens"]) is int and 1 <= data["max_tokens"] <= 4096)
    if re.fullmatch(BANK + r"/memories", path):
        if (set(data) != {"async", "operation_id", "items"} or data["async"] is not True
            or not isinstance(data["operation_id"], str)
            or not re.fullmatch(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}", data["operation_id"])
            or not isinstance(data["items"], list) or len(data["items"]) != 1):
            return False
        item = data["items"][0]
        return (isinstance(item, dict) and set(item) == {"content", "document_id", "metadata"}
                and isinstance(item["content"], str) and bool(item["content"])
                and isinstance(item["document_id"], str)
                and bool(re.fullmatch(r"[A-Za-z0-9_-]{1,256}", item["document_id"]))
                and isinstance(item["metadata"], dict) and set(item["metadata"]) == {"source_revision"}
                and isinstance(item["metadata"]["source_revision"], str)
                and bool(re.fullmatch(r"[1-9][0-9]*", item["metadata"]["source_revision"])))
    return False

def allowed(method, path, body):
    # Canonical ASCII paths only: reject %, query, dot segments and alternate encodings.
    if not re.fullmatch(r"/[A-Za-z0-9_/-]+", path):
        return False
    if method == "GET" and path == "/health":
        return not body
    if method in ("PUT", "POST"):
        try:
            return payload_allowed(method, path, body)
        except (ValueError, UnicodeError):
            return False
    if method == "GET" and re.fullmatch(BANK + r"/operations/" + OPERATION_ID, path):
        return not body
    return method == "DELETE" and not body and bool(re.fullmatch(DOCUMENT, path))

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def handle_one_request(self):
        self.connection.settimeout(5)
        super().handle_one_request()

    def reply(self, status, data=b'{"error":"hindsight_gateway_rejected"}', purged=None):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Connection", "close")
        if purged:
            self.send_header(PURGED_OPERATION_HEADER, purged)
        self.end_headers()
        self.wfile.write(data)
        self.close_connection = True

    def dispatch(self):
        acquired = False
        deleting = False
        try:
            auth = self.headers.get_all("Authorization", [])
            if len(auth) != 1 or not auth[0].isascii() or not hmac.compare_digest(auth[0], "Bearer " + self.server.token):
                return self.reply(401)
            lengths = self.headers.get_all("Content-Length", [])
            if self.headers.get("Transfer-Encoding") or len(lengths) > 1:
                return self.reply(400)
            size = int(lengths[0]) if lengths else 0
            if size < 0 or size > LIMIT:
                return self.reply(413)
            body = self.rfile.read(size)
            if len(body) != size or not allowed(self.command, self.path, body):
                return self.reply(403)
            acquired = SLOTS.acquire(blocking=False)
            if not acquired:
                return self.reply(503)
            match = re.fullmatch(DOCUMENT, self.path)
            operations = self.headers.get_all(RETAIN_OPERATION_HEADER, [])
            if operations and (self.command != "DELETE" or not match
                               or len(operations) != 1 or not re.fullmatch(OPERATION_ID, operations[0])):
                return self.reply(400)
            operation = operations[0] if operations else None
            purged = None
            if self.command == "DELETE" and match:
                deleting = DELETE_SLOT.acquire(blocking=False)
                if not deleting:
                    return self.reply(503)
                deadline = time.monotonic() + DELETE_TIMEOUT
                retain_ids = retain_operations(self.server, match[1], match[2], operation, deadline) if operation else []
                status, data = upstream(self.server, self.command, self.path, deadline=deadline)
                if status == 404 or 200 <= status < 300:
                    purge_derived(self.server, match[1], deadline)
                    purge_operations(self.server, match[1], retain_ids, deadline)
                    purged = operation
            else:
                status, data = upstream(self.server, self.command, self.path, body)
            # Do not relay redirects, upstream errors, diagnostics or headers.
            if status == 404:
                return self.reply(404, b'{"error":"not_found"}', purged)
            if not 200 <= status < 300:
                return self.reply(502)
            return self.reply(status, data, purged)
        except Exception:
            self.reply(502)
        finally:
            if deleting:
                DELETE_SLOT.release()
            if acquired:
                SLOTS.release()

    do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_OPTIONS = dispatch

class Server(ThreadingHTTPServer):
    daemon_threads = True
    upstream_port = 8888  # Tests may bind another loopback port; production has no override.
    connections = threading.BoundedSemaphore(32)

    def process_request(self, request, client_address):
        if not self.connections.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.connections.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.connections.release()
    def handle_error(self, *_):
        pass

if __name__ == "__main__":
    token = Path(os.environ["SOHA_HINDSIGHT_TOKEN_FILE"]).read_text().strip()
    if len(token) < 32 or not token.isascii() or any(c.isspace() for c in token):
        raise SystemExit("invalid service token file")
    server = Server(("0.0.0.0", 8889), Handler)
    server.token = token
    server.serve_forever()
