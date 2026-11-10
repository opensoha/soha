"""Launch the pinned API with only explicit Gateway providers and independent DB."""
import os
from pathlib import Path

# Do not inherit provider-specific, per-operation, bank-routing or dotenv overrides.
env = {k: v for k, v in os.environ.items() if not k.startswith(("HINDSIGHT_", "OPENAI_", "ANTHROPIC_"))}
def required(name):
    value = os.environ[name].strip()
    if not value:
        raise SystemExit("missing deployment setting")
    return value

def secret(name):
    value = Path(required(name)).read_text().strip()
    if not value or "\n" in value or "\r" in value:
        raise SystemExit("invalid secret file")
    return value

from urllib.parse import urlsplit
def gateway_url(value):
    u = urlsplit(value)
    if u.scheme not in ("http", "https") or not u.hostname or u.username or u.password or u.query or u.fragment:
        raise SystemExit("invalid Gateway base URL")
    return value

base = gateway_url(required("SOHA_HINDSIGHT_GATEWAY_URL"))
embedding_base = gateway_url(os.environ.get("SOHA_HINDSIGHT_EMBEDDING_GATEWAY_URL", "").strip() or base)
env.update({
    "HINDSIGHT_API_DATABASE_URL": secret("SOHA_HINDSIGHT_DATABASE_URL_FILE"),
    "HINDSIGHT_API_LLM_PROVIDER": "openai",
    "HINDSIGHT_API_LLM_MODEL": required("SOHA_HINDSIGHT_LLM_MODEL"),
    "HINDSIGHT_API_LLM_BASE_URL": base,
    "HINDSIGHT_API_LLM_API_KEY": secret("SOHA_HINDSIGHT_GATEWAY_PAT_FILE"),
    "HINDSIGHT_API_LLM_MAX_RETRIES": "1",
    "HINDSIGHT_API_LLM_TIMEOUT": "95",
    "HINDSIGHT_API_REFLECT_LLM_TIMEOUT": "95",
    "HINDSIGHT_API_EMBEDDINGS_PROVIDER": "openai",
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_BASE_URL": embedding_base,
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY": secret("SOHA_HINDSIGHT_GATEWAY_PAT_FILE"),
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL": required("SOHA_HINDSIGHT_EMBEDDING_MODEL"),
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_DIMENSIONS": required("SOHA_HINDSIGHT_EMBEDDING_DIMENSIONS"),
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_EXTRA_BODY": '{"encoding_format":"float"}',
    "HINDSIGHT_API_RERANKER_PROVIDER": "rrf",
    "HINDSIGHT_API_ENABLE_AUTO_CONSOLIDATION": "false",
    "HINDSIGHT_API_ENABLE_BANK_CONFIG_API": "false",
    "HINDSIGHT_API_ENABLE_BANK_LLM_HEALTH": "false",
    "HINDSIGHT_API_LLM_TRACE_ENABLED": "false",
    "HF_HUB_OFFLINE": "1", "TRANSFORMERS_OFFLINE": "1",
    "XDG_CACHE_HOME": "/tmp/cache", "HOME": "/tmp",
})
# Empty working directory prevents upstream .env discovery changing this configuration.
os.chdir("/tmp")
os.execvpe("/app/api/.venv/bin/python", ["python", "-m", "uvicorn", "hindsight_api.server:app", "--host", "127.0.0.1", "--port", "8888", "--no-access-log"], env)
