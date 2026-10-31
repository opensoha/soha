import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('postgres_fixture', Path(__file__).with_name('run-deletion-integration.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


class DirectPostgresEndpointTests(unittest.TestCase):
    def test_remote_control_with_direct_private_pg(self):
        info = '[{"Endpoints":{"docker":{"Host":"ssh://database"}}}]'
        with patch.object(fixture.subprocess, 'check_output', return_value=info):
            self.assertEqual(fixture.docker_test_endpoint({}, '10.0.0.18', True), ('ssh://database', '10.0.0.18'))

    def test_remote_requires_opt_in_and_private_host(self):
        info = '[{"Endpoints":{"docker":{"Host":"ssh://database"}}}]'
        for host, allowed in [('10.0.0.18', False), (None, True), ('127.0.0.1', True), ('0.0.0.0', True), ('8.8.8.8', True), ('169.254.169.254', True)]:
            with self.subTest(host=host, allowed=allowed), patch.object(fixture.subprocess, 'check_output', return_value=info), self.assertRaises(ValueError):
                fixture.docker_test_endpoint({}, host, allowed)

    def test_context_and_explicit_environment_precedence(self):
        info = '[{"Endpoints":{"docker":{"Host":"ssh://database"}}}]'
        with patch.object(fixture.subprocess, 'check_output', return_value=info), patch.object(fixture.Path, 'is_socket', return_value=True):
            self.assertEqual(fixture.docker_test_endpoint({'DOCKER_HOST': 'unix:///fixture'}), ('unix:///fixture', '127.0.0.1'))
            env = {'DOCKER_HOST': 'unix:///fixture', 'DOCKER_CONTEXT': 'remote', 'SOHA_TEST_POSTGRES_HOST': '10.0.0.18'}
            self.assertEqual(fixture.docker_test_endpoint(env, allow_remote=True), ('ssh://database', '10.0.0.18'))

    def test_missing_local_socket_fails(self):
        info = '[{"Endpoints":{"docker":{"Host":"unix:///nonexistent/task115.sock"}}}]'
        with patch.object(fixture.subprocess, 'check_output', return_value=info), self.assertRaises(ValueError):
            fixture.docker_test_endpoint({})

    def test_docker_uri_must_not_embed_credentials(self):
        info = '[{"Endpoints":{"docker":{"Host":"ssh://user:secret@fixture"}}}]'
        with patch.object(fixture.subprocess, 'check_output', return_value=info), self.assertRaises(ValueError):
            fixture.docker_test_endpoint({}, '10.0.0.18', True)


if __name__ == '__main__':
    unittest.main()
