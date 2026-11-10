#!/usr/bin/env python3
"""Run real Hindsight + private PG with a deliberately synthetic model endpoint."""
import hashlib
import http.server
import importlib.metadata
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time

from qualify import qualify, request


class ModelStub(http.server.BaseHTTPRequestHandler):
    calls = {}

    def log_message(self, *args):
        pass

    def do_POST(self):
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.calls[self.path] = self.calls.get(self.path, 0) + 1
        if self.path == "/v1/embeddings":
            inputs = payload["input"]
            if isinstance(inputs, str):
                inputs = [inputs]
            vectors = []
            for i, value in enumerate(inputs):
                digest = hashlib.sha256(str(value).encode()).digest()
                vectors.append({"object": "embedding", "index": i, "embedding": [(x + 1) / 256 for x in digest]})
            result = {"object": "list", "data": vectors, "model": "hq-stub", "usage": {"prompt_tokens": 10, "total_tokens": 10}}
        elif self.path == "/v1/chat/completions":
            text = json.dumps(payload.get("messages", []))
            markers = re.findall(r"HQ_CANARY_(?:A1|A2|B)", text)
            if not markers:
                self.send_error(400, "stub accepts only qualification extraction")
                return
            marker = markers[-1]
            tea = {"HQ_CANARY_A1": "cobalt", "HQ_CANARY_A2": "amber", "HQ_CANARY_B": "violet"}[marker]
            fact = {"what": f"{marker} prefers {tea} tea.", "when": "N/A", "where": "N/A", "who": "N/A", "why": "N/A", "fact_type": "world", "entities": []}
            result = {"id": "hq", "object": "chat.completion", "created": 0, "model": "hq-stub", "choices": [{"index": 0, "message": {"role": "assistant", "content": json.dumps({"facts": [fact]})}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}}
        else:
            self.send_error(400, "unapproved model endpoint")
            return
        body = json.dumps(result).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    assert importlib.metadata.version("hindsight-api-slim") == "1.2.0"
    pg_bin = Path(os.environ.get("HQ_PG_BIN", "/opt/homebrew/opt/postgresql@18/bin"))
    assert (pg_bin / "initdb").is_file(), "set HQ_PG_BIN to installed PG binaries"
    assert shutil.disk_usage(tempfile.gettempdir()).free >= 5 * 1024**3
    with tempfile.TemporaryDirectory(prefix="task123-hq-run-", dir=os.environ.get("OPENSOHA_VERIFY_DIR")) as root:
        root = Path(root)
        pgdata = root / "pgdata"
        pg_port, api_port = free_port(), free_port()
        model = http.server.ThreadingHTTPServer(("127.0.0.1", 0), ModelStub)
        threading.Thread(target=model.serve_forever, daemon=True).start()
        api = None
        pg_started = False
        try:
            with (root / "initdb.log").open("w") as output:
                subprocess.run([str(pg_bin / "initdb"), "-D", str(pgdata), "-U", "hq", "-A", "trust", "--no-locale", "--encoding=UTF8"], stdout=output, stderr=output, check=True)
            # macOS verifier paths exceed PostgreSQL's Unix socket path limit.
            with (pgdata / "postgresql.conf").open("a") as config:
                config.write("\nunix_socket_directories = ''\n")
            subprocess.run([str(pg_bin / "pg_ctl"), "-D", str(pgdata), "-l", str(root / "pg.log"), "-o", f"-h 127.0.0.1 -p {pg_port}", "-w", "start"], check=True, stdout=subprocess.DEVNULL)
            pg_started = True
            env = {key: value for key, value in os.environ.items() if not key.startswith(("HINDSIGHT_", "OPENAI_", "ANTHROPIC_"))}
            model_url = f"http://127.0.0.1:{model.server_port}/v1"
            env.update({"XDG_CACHE_HOME": str(root / "cache"), "HF_HUB_OFFLINE": "1", "HINDSIGHT_API_DATABASE_URL": f"postgresql://hq@127.0.0.1:{pg_port}/postgres", "HINDSIGHT_API_LLM_PROVIDER": "openai", "HINDSIGHT_API_LLM_API_KEY": "synthetic-not-a-secret", "HINDSIGHT_API_LLM_MODEL": "hq-stub", "HINDSIGHT_API_LLM_BASE_URL": model_url, "HINDSIGHT_API_EMBEDDINGS_PROVIDER": "openai", "HINDSIGHT_API_EMBEDDINGS_OPENAI_BASE_URL": model_url, "HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY": "synthetic-not-a-secret", "HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL": "hq-stub", "HINDSIGHT_API_EMBEDDINGS_OPENAI_DIMENSIONS": "32", "HINDSIGHT_API_RERANKER_PROVIDER": "rrf", "HINDSIGHT_API_ENABLE_AUTO_CONSOLIDATION": "false"})
            with (root / "api.log").open("w") as output:
                api = subprocess.Popen([sys.executable, "-m", "uvicorn", "hindsight_api.server:app", "--host", "127.0.0.1", "--port", str(api_port)], env=env, stdout=output, stderr=output)
            base = f"http://127.0.0.1:{api_port}"
            deadline = time.monotonic() + 90
            while time.monotonic() < deadline:
                if api.poll() is not None:
                    raise RuntimeError("Hindsight exited during startup")
                try:
                    request(base, "GET", "/health")
                    break
                except Exception:
                    time.sleep(0.2)
            else:
                raise RuntimeError("Hindsight startup timeout")
            report = qualify(base)
            assert ModelStub.calls.get("/v1/chat/completions", 0) >= 3
            assert ModelStub.calls.get("/v1/embeddings", 0) >= 3
            extensions = subprocess.check_output([str(pg_bin / "psql"), "-h", "127.0.0.1", "-p", str(pg_port), "-U", "hq", "-d", "postgres", "-At", "-c", "SELECT extname || '=' || extversion FROM pg_extension WHERE extname IN ('vector','pg_trgm') ORDER BY extname"], text=True).splitlines()
            assert extensions == ["pg_trgm=1.6", "vector=0.8.7"], extensions
            report.update({"service": "hindsight-api-slim==1.2.0", "model": "synthetic stub; NOT real Gateway/quality acceptance", "model_calls": ModelStub.calls, "extensions": extensions, "postgres": subprocess.check_output([str(pg_bin / "postgres"), "--version"], text=True).strip()})
            print(json.dumps(report, indent=2))
        except Exception:
            # Synthetic local logs only; retain no real credentials or user data.
            for filename in ("api.log", "pg.log", "initdb.log"):
                path = root / filename
                if path.exists():
                    print(filename + ":\n" + path.read_text()[-6000:], file=sys.stderr)
            raise
        finally:
            if api is not None and api.poll() is None:
                api.terminate()
                try:
                    api.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    api.kill()
                    api.wait()
            model.shutdown()
            model.server_close()
            if pg_started:
                subprocess.run([str(pg_bin / "pg_ctl"), "-D", str(pgdata), "-m", "immediate", "-w", "stop"], check=True, stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
