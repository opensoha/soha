import importlib.util
import io
import json
import pathlib
import sys
import types
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
sys.modules.setdefault("gateway", types.ModuleType("gateway"))
session_context = types.ModuleType("gateway.session_context")
session_context.get_session_env = lambda _: ""
sys.modules["gateway.session_context"] = session_context
spec = importlib.util.spec_from_file_location("soha_tools", pathlib.Path(__file__).with_name("__init__.py"))
plugin = importlib.util.module_from_spec(spec)
spec.loader.exec_module(plugin)


class PluginTest(unittest.TestCase):
    def test_only_trusted_runtime_key_authorizes_and_remote_http_is_rejected(self):
        args = {"toolName": "knowledge.search", "input": {"query": "maintenance"}}
        opener = Mock()
        with patch.object(plugin.urllib.request, "build_opener", return_value=opener):
            self.assertIn("error", json.loads(plugin.soha_query(args, session_id="soha-tools:" + "B" * 26)))
            opener.open.assert_not_called()
            with patch.object(plugin, "get_session_env", return_value="soha-tools:" + "A" * 26):
                with patch.dict(plugin.os.environ, {"SOHA_RUNNER_URL": "http://external.example/api/v1"}):
                    self.assertIn("error", json.loads(plugin.soha_query(args)))
                    opener.open.assert_not_called()
                with patch.dict(plugin.os.environ, {"SOHA_RUNNER_URL": "http://127.0.0.1:18642/api/v1"}):
                    opener.open.return_value = io.BytesIO(b'{"data":{"runId":"a","output":{"ready":true}}}')
                    result = json.loads(plugin.soha_query(args))
                    self.assertTrue(result["output"]["ready"])
                    request = opener.open.call_args.args[0]
                    self.assertEqual(request.get_header("Authorization"), "Bearer soha-tools:" + "A" * 26)
                    self.assertEqual(json.loads(request.data), args)


if __name__ == "__main__":
    unittest.main()
