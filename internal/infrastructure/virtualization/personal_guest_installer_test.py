import errno
import importlib.util
import pathlib
import struct
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("installer", pathlib.Path(__file__).with_name("personal_guest_installer.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class ProjectNamespaceTest(unittest.TestCase):
    def check_guard(self, deny):
        def ioctl(fd, command, buffer, mutate=False):
            if command == 0x801C581F:
                buffer[:] = struct.pack("=5I8s", 0x200, 0, 0, 49, 0, b"\0" * 8)
            elif command == 0x401C5820 and deny:
                raise OSError(errno.EINVAL, "namespace rejects project state changes")

        with patch.object(installer.os, "mkdir"), patch.object(installer.os, "open", return_value=7), patch.object(installer.os, "close"), patch.object(installer.fcntl, "ioctl", side_effect=ioctl):
            installer.verify_project_namespace(pathlib.Path("/srv/fixed-test-workspace"))

    def test_kernel_rejects_both_project_and_inheritance_changes(self):
        self.check_guard(True)

    def test_successful_project_mutation_cannot_pass(self):
        with self.assertRaises(RuntimeError):
            self.check_guard(False)


class NetworkPolicyTest(unittest.TestCase):
    def test_packet_drop_on_live_control_target_counts_as_denial(self):
        with patch.object(installer.socket, "create_connection", side_effect=TimeoutError()):
            self.assertTrue(installer.network_denied("127.0.0.1", 22))

    def test_closed_control_port_does_not_count_as_denial(self):
        with patch.object(installer.socket, "create_connection", side_effect=ConnectionRefusedError(errno.ECONNREFUSED, "closed")):
            self.assertFalse(installer.network_denied("127.0.0.1", 22))

    def test_reachable_control_target_rejects_policy(self):
        with patch.object(installer.socket, "create_connection"):
            self.assertFalse(installer.network_denied("127.0.0.1", 22))


if __name__ == "__main__":
    unittest.main()
