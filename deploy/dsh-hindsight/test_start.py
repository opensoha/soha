import os
from pathlib import Path
import runpy
import unittest
from unittest.mock import patch


class Launched(Exception):
    pass


class StartupTest(unittest.TestCase):
    def launch(self, **overrides):
        settings = {
            "SOHA_HINDSIGHT_GATEWAY_URL": "https://gateway.invalid/llm/openai/v1",
            "SOHA_HINDSIGHT_LLM_MODEL": "extract",
            "SOHA_HINDSIGHT_EMBEDDING_MODEL": "Qwen/Qwen3-Embedding-0.6B",
            "SOHA_HINDSIGHT_EMBEDDING_DIMENSIONS": "1024",
            "SOHA_HINDSIGHT_DATABASE_URL_FILE": "/private/database-url",
            "SOHA_HINDSIGHT_GATEWAY_PAT_FILE": "/private/gateway-pat",
            "HINDSIGHT_API_LLM_BASE_URL": "https://unapproved.invalid",
            "HINDSIGHT_API_EMBEDDINGS_OPENAI_EXTRA_BODY": '{"encoding_format":"base64"}',
        }
        settings.update(overrides)
        with patch.dict(os.environ, settings, clear=True), patch.object(Path, "read_text", return_value="test-only-secret"), patch("os.chdir"), patch("os.execvpe", side_effect=Launched) as execute:
            with self.assertRaises(Launched):
                runpy.run_path(str(Path(__file__).with_name("start.py")))
            return execute.call_args.args[2]

    def test_independent_embedding_provider_route(self):
        env = self.launch(SOHA_HINDSIGHT_EMBEDDING_GATEWAY_URL="https://gateway.invalid/llm/qwen/v1")
        self.assertEqual(env["HINDSIGHT_API_LLM_BASE_URL"], "https://gateway.invalid/llm/openai/v1")
        self.assertEqual(env["HINDSIGHT_API_EMBEDDINGS_OPENAI_BASE_URL"], "https://gateway.invalid/llm/qwen/v1")
        self.assertEqual(env["HINDSIGHT_API_EMBEDDINGS_OPENAI_EXTRA_BODY"], '{"encoding_format":"float"}')
        self.assertEqual(env["HINDSIGHT_API_EMBEDDINGS_OPENAI_DIMENSIONS"], "1024")
        self.assertEqual(env["HINDSIGHT_API_LLM_MAX_RETRIES"], "1")
        self.assertEqual(env["HINDSIGHT_API_LLM_TIMEOUT"], "95")
        self.assertEqual(env["HINDSIGHT_API_REFLECT_LLM_TIMEOUT"], "95")

    def test_existing_shared_route_remains_supported(self):
        env = self.launch(SOHA_HINDSIGHT_EMBEDDING_GATEWAY_URL="")
        self.assertEqual(env["HINDSIGHT_API_EMBEDDINGS_OPENAI_BASE_URL"], env["HINDSIGHT_API_LLM_BASE_URL"])

    def test_embedding_url_rejects_credentials_and_query(self):
        for url in ("https://secret@gateway.invalid/v1", "https://gateway.invalid/v1?secret=x", "file:///tmp/endpoint"):
            with self.subTest(url=url), self.assertRaises(SystemExit):
                self.launch(SOHA_HINDSIGHT_EMBEDDING_GATEWAY_URL=url)


if __name__ == "__main__":
    unittest.main()
