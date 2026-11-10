#!/usr/bin/env python3
"""Read route inventory; opt in to one bounded real probe with a temporary relay PAT."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import math
from pathlib import Path
import subprocess
import urllib.error
import urllib.parse
import urllib.request
import uuid


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


def safe_code(value):
    return value if isinstance(value, str) and len(value) < 64 and all(c.isalnum() or c in "_.-" for c in value) else None


class Gateway:
    def __init__(self, profile):
        url = urllib.parse.urlsplit(profile["serverUrl"])
        if url.scheme != "http" or url.hostname not in ("localhost", "127.0.0.1") or url.username or url.password or url.query or url.fragment:
            raise ValueError("require the local loopback profile")
        self.base = urllib.parse.urlunsplit((url.scheme, url.netloc, "/api/v1/ai-gateway", "", ""))
        self.jwt = profile["accessToken"]
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def call(self, path, payload=None, token=None):
        data = None if payload is None else json.dumps(payload).encode()
        req = urllib.request.Request(self.base + path, data=data, headers={"Authorization": "Bearer " + (token or self.jwt), "Content-Type": "application/json"})
        try:
            with self.opener.open(req, timeout=90) as response:
                body = response.read(1024 * 1024 + 1)
                if len(body) > 1024 * 1024:
                    raise ValueError("response exceeds qualification bound")
                return response.status, json.loads(body)
        except urllib.error.HTTPError as error:
            try:
                body = json.loads(error.read(1024 * 1024))
            except (ValueError, UnicodeError):
                body = {}
            native = body.get("error", {}) if isinstance(body, dict) else {}
            native = native if isinstance(native, dict) else {}
            return error.code, {"error_type": safe_code(native.get("type")), "error_code": safe_code(native.get("code"))}


def extract(call, model):
    # Mirrors the fixed Hindsight extraction shape, using synthetic Chinese input.
    properties = {key: {"type": "string"} for key in ("what", "when", "where", "who", "why")}
    properties.update({"fact_type": {"type": "string", "enum": ["world", "assistant"]}, "entities": {"type": "array", "items": {"type": "string"}}})
    schema = {"type": "object", "properties": {"facts": {"type": "array", "items": {"type": "object", "properties": properties, "required": list(properties), "additionalProperties": False}}}, "required": ["facts"], "additionalProperties": False}
    status, result = call("/llm/openai/v1/chat/completions", {
        "model": model, "messages": [{"role": "system", "content": "Extract only stated facts into the schema. Use world for preferences, N/A for unstated fields."}, {"role": "user", "content": "资格测试用户喜欢喝茉莉花茶。"}],
        "stream": False, "max_completion_tokens": 160, "reasoning_effort": "low",
        "response_format": {"type": "json_schema", "json_schema": {"name": "hindsight_fact_extraction", "strict": True, "schema": schema}}})
    chat = {"http": status}
    if status == 200:
        chat["usage_present"] = isinstance(result.get("usage"), dict)
        chat["usage"] = {key: value for key, value in (result.get("usage") or {}).items() if key in ("prompt_tokens", "completion_tokens", "total_tokens") and type(value) is int}
        try:
            choice = result["choices"][0]
            chat["finish_reason"] = safe_code(choice.get("finish_reason"))
            structured = json.loads(choice["message"]["content"])
            assert isinstance(structured, dict) and set(structured) == {"facts"}
            facts = structured["facts"]
            assert choice["finish_reason"] == "stop", "incomplete structured response"
            assert isinstance(facts, list) and facts
            for fact in facts:
                assert isinstance(fact, dict) and set(fact) == set(properties)
                assert all(isinstance(fact[key], str) for key in ("what", "when", "where", "who", "why"))
                assert fact["fact_type"] in ("world", "assistant")
                assert isinstance(fact["entities"], list) and all(isinstance(entity, str) for entity in fact["entities"])
            assert any("茉莉" in fact["what"] for fact in facts), "missing stated synthetic fact"
            chat.update({"structured_extraction": "PASS", "fact_count": len(facts)})
        except (AssertionError, ValueError, KeyError, IndexError, TypeError) as error:
            chat.update({"structured_extraction": "FAIL", "validation_error_class": type(error).__name__})
    else:
        chat.update(result)
    return chat


def routes_from(body):
    data = body.get("data", body)
    return data.get("items", data.get("routes", [])) if isinstance(data, dict) else data


def run(gateway, args, report, action_key=None):
    if action_key is not None:
        if not isinstance(action_key, str) or not action_key or len(action_key) > 200 or not all(c.isalnum() or c in "/_.:-" for c in action_key):
            raise ValueError("invalid action key")
        report["action_key"] = action_key
    status, body = gateway.call("/relay/model-routes")
    report["route_inventory_http"] = status
    if status != 200:
        report["status"] = "FAIL"
        return
    routes = [r for r in routes_from(body) if r.get("enabled")]
    report["enabled_routes"] = [{k: r.get(k) for k in ("publicModel", "upstreamModel", "providerKind")} for r in routes]
    compatible_routes = [r for r in routes if r.get("providerKind") in ("openai", "qwen")]
    models = {r.get("publicModel") for r in compatible_routes}
    report["embedding_route"] = "NOT_CONFIGURED_FOR_PROBE" if not args.embedding_model else args.embedding_model
    if getattr(args, "discover_upstreams", False):
        status, body = gateway.call("/relay/upstreams")
        report["upstream_inventory_http"] = status
        report["upstream_discovery"] = []
        if status == 200:
            for upstream in routes_from(body):
                if not (upstream.get("enabled") is True or upstream.get("status") == "active") or upstream.get("providerKind") != "openai":
                    continue
                status, probe = gateway.call("/relay/upstreams/" + urllib.parse.quote(upstream["id"], safe="") + "/test", {})
                data = probe.get("data", probe)
                report["upstream_discovery"].append({"id": upstream["id"], "http": status,
                    **{k: data.get(k) for k in ("status", "httpStatus", "modelCount", "models")}})
    if not args.invoke:
        report["status"] = "INVENTORY_ONLY"
        return
    requested_models = ({args.embedding_model} if getattr(args, "embedding_only", False) else {args.chat_model} | ({args.embedding_model} if args.embedding_model else set()))
    if not requested_models or not requested_models.issubset(models):
        report["status"] = "FAIL"
        report["reason"] = "requested model lacks an enabled OpenAI-compatible route; no credential created"
        return
    intent = "2" if getattr(args, "embedding_only", False) else "1"
    name = "task123-hq-b5-u2-a0" + intent + "-" + uuid.uuid4().hex
    report["action_key"] = action_key or "Task123/gateway-qualification/hindsight-u2/" + intent
    report["token_name"] = name
    token_id = None
    attempted = False
    try:
        attempted = True  # never retry creation, even if the receipt is lost
        report["cleanup_requires_followup"] = True
        expires = (datetime.now(timezone.utc) + timedelta(minutes=15)).isoformat()
        allowed = sorted(requested_models)
        provider_kinds = sorted({r["providerKind"] for r in compatible_routes if r.get("publicModel") in requested_models})
        status, created = gateway.call("/personal-access-tokens", {"name": name, "permissionKeys": ["ai.gateway.relay.invoke"], "scopes": ["llm-relay"], "expiresAt": expires,
            "metadata": {"purpose": "llm-relay", "allowedModels": allowed, "allowedProviderKinds": provider_kinds}})
        report["credential"] = {"create_http": status, "expires_at": expires, "allowed_models": allowed, "type": "PAT", "permission_keys": ["ai.gateway.relay.invoke"]}
        if status not in (200, 201):
            raise RuntimeError("PAT creation rejected")
        token_id = created["data"]["token"]["id"]
        pat = created["data"]["value"]
        report["credential"]["id"] = token_id
        if created["data"]["token"]["permissionKeys"] != ["ai.gateway.relay.invoke"]:
            raise RuntimeError("unexpected PAT permissions")
        def relay(path, payload):
            return gateway.call(path, payload, token=pat)
        if getattr(args, "embedding_only", False):
            report["chat"] = {"structured_extraction": "NOT_RUN", "reason": "reuse intent1; no repeated paid chat"}
            embedding_provider = next(r["providerKind"] for r in compatible_routes if r.get("publicModel") == args.embedding_model)
            status, models = gateway.call(f"/llm/{embedding_provider}/v1/models", token=pat)
            report["relay_models"] = {"http": status, "models": [item.get("id") for item in models.get("data", [])] if status == 200 else []}
        else:
            report["chat"] = extract(relay, args.chat_model)
        if args.embedding_model:
            embedding_provider = next(r["providerKind"] for r in compatible_routes if r.get("publicModel") == args.embedding_model)
            status, result = relay(f"/llm/{embedding_provider}/v1/embeddings", {"model": args.embedding_model, "input": ["资格测试：茉莉花茶"], "encoding_format": "float"})
            report["embeddings"] = {"http": status}
            if status == 200:
                vector = result["data"][0]["embedding"]
                assert vector and all(type(v) in (int, float) and math.isfinite(v) for v in vector)
                norm = math.sqrt(sum(v * v for v in vector))
                assert math.isfinite(norm) and norm > 0, "invalid zero vector"
                report["embeddings"].update({"actual_vector": "PASS", "dimensions": len(vector), "l2_norm": norm, "usage_present": isinstance(result.get("usage"), dict)})
            else:
                report["embeddings"].update(result)
        report["status"] = "PREFLIGHT_PASS" if (getattr(args, "embedding_only", False) or report["chat"].get("structured_extraction") == "PASS") and (not args.embedding_model or report["embeddings"].get("actual_vector") == "PASS") else "FAIL"
    finally:
        # Reconcile this exact unique action, including definite API errors: no replay.
        if attempted:
            if token_id is None:
                status, listed = gateway.call("/personal-access-tokens")
                assert status == 200, "PAT reconciliation failed"
                matches = [item for item in listed["items"] if item.get("name") == name]
                assert len(matches) <= 1, "ambiguous PAT identity"
                if matches:
                    token_id = matches[0]["id"]
            if token_id:
                # A known creation receipt is enough to revoke; listing must not gate it.
                report["revocation"] = {"id": token_id, "confirmed": False}
                try:
                    status, receipt = gateway.call("/personal-access-tokens/" + urllib.parse.quote(token_id, safe="") + "/revoke", {})
                    report["revocation"].update({"http": status, "receipt_ok": status == 200 and receipt.get("status") == "ok"})
                except Exception as error:
                    # An unknown revoke result is reconciled, never replayed.
                    report["revocation"]["error_class"] = type(error).__name__
                status, listed = gateway.call("/personal-access-tokens")
                report["revocation"]["confirmation_http"] = status
                assert status == 200, "PAT revocation confirmation failed"
                item = next((x for x in listed["items"] if x.get("id") == token_id), None)
                report["revocation"]["confirmed"] = bool(item and item.get("revokedAt"))
                assert report["revocation"]["confirmed"], "PAT revocation unconfirmed"
                report["cleanup_requires_followup"] = False
            else:
                create_http = report.get("credential", {}).get("create_http")
                rejected = isinstance(create_http, int) and 400 <= create_http < 500 and create_http != 408
                report["revocation"] = {"confirmed": rejected, "no_token_observed": True}
                report["cleanup_requires_followup"] = not rejected
                assert rejected, "lost creation receipt requires later reconciliation"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--invoke", action="store_true", help="create/revoke a 15-minute relay PAT and make one structured chat request")
    parser.add_argument("--chat-model", default="gpt-6-luna")
    parser.add_argument("--embedding-model", help="explicit configured route only; never guess or add a route")
    parser.add_argument("--embedding-only", action="store_true", help="intent2: models GET and one embedding request, no chat")
    parser.add_argument("--discover-upstreams", action="store_true", help="normal management API model discovery for enabled OpenAI upstreams")
    parser.add_argument("--action-key", help="caller-assigned qualification action identity; does not authorize new actions")
    args = parser.parse_args()
    if args.embedding_only and (not args.invoke or not args.embedding_model):
        parser.error("--embedding-only requires --invoke and --embedding-model")
    report = {"attempt": "Task123/B5/U2/A01", "status": "FAIL", "full_hindsight_real_gateway": "NOT_RUN", "derived_memory_delete": "NOT_RUN",
              "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    try:
        # Supported CLI refresh persists rotated credentials safely; capture all output.
        session = subprocess.run(["soha", "token", "list", "--profile", "local"], capture_output=True, timeout=50)
        report["normal_cli_session_exit"] = session.returncode
        if session.returncode:
            raise RuntimeError("normal CLI login unavailable")
        profile = json.loads((Path.home() / ".soha/config.json").read_text())["profiles"]["local"]
        run(Gateway(profile), args, report, action_key=args.action_key)
    except Exception as error:
        report["status"] = "FAIL"
        report["error_class"] = type(error).__name__  # exception strings may contain credentials
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["status"] in ("INVENTORY_ONLY", "PREFLIGHT_PASS") else 1


if __name__ == "__main__":
    raise SystemExit(main())
