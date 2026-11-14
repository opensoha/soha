import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("admission", Path(__file__).with_name("admission-linux-lab.py"))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


class LabTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        lstat = Path.lstat
        def fixture_ancestors(path):
            if path in self.base.parents:
                return SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=os.geteuid())
            return lstat(path)
        for guard in (patch.object(lab, "ROOT_UID", os.geteuid()),
                      patch.object(Path, "lstat", fixture_ancestors),
                      patch.object(lab, "boot_id", return_value="fixture-boot"),
                      patch.object(lab, "origin_inode", return_value=456)):
            guard.start()
            self.addCleanup(guard.stop)
        self.c = dict(owner="task122-b3-l02-a01", project="task122-admission", state=str(self.base / "lab"),
                      nas_id="nas-task122", ap_phy="phy91", sta_phy="phy92", lease_seconds=3,
                      radius_config="/isolated/radius", secret_file="/isolated/secret", supplicant_config="/isolated/wpa.conf")

    def test_explicit_target_and_reuse(self):
        lab.validate(self.c)
        lab.prepare(self.c)
        lab.prepare(self.c)
        with self.assertRaises(RuntimeError):
            lab.prepare({**self.c, "owner": "foreign"})
        self.assertEqual(lab.load(self.c)["namespaces"], {})
        with self.assertRaises(ValueError):
            lab.validate({**self.c, "sta_phy": "phy91"})
        with self.assertRaises(ValueError):
            lab.validate({**self.c, "lease_seconds": 31})

    def test_unknown_or_multiple_stations_never_authorize(self):
        self.assertEqual(lab.station("UNKNOWN COMMAND\n"), (None, None))
        self.assertEqual(lab.station("02:00:00:00:00:01\nflags=[AUTH]\n"), (None, None))
        station = "02:00:00:00:00:01\nflags=[AUTH][AUTHORIZED]\nacct_session_id=123-456\n"
        self.assertEqual(lab.station(station), ("02:00:00:00:00:01", "123-456"))
        with self.assertRaises(ValueError):
            lab.station(station + station.replace(":01", ":02"))

    def test_ingress_mac_session_timeout_and_no_automatic_renewal(self):
        now = [0]
        leases = lab.Leases(self.c, clock=lambda: now[0])
        mac = "02:00:00:00:00:01"
        with patch.object(lab, "ns") as command:
            leases.update("w1", mac, "s1", True, 3)
            nft = [x for x in command.call_args_list if "input" in x.kwargs][0].kwargs["input"]
            self.assertIn("w1 . " + mac + " timeout 3000ms", nft)
            self.assertNotIn("w2", nft)
            count = command.call_count
            now[0] = 2
            leases.update("w1", mac, "s1", True, 3)
            self.assertEqual(command.call_count, count)
            now[0] = 3
            leases.update("w1", mac, "s1", True, 3)
            self.assertNotIn("w1", leases.active)
            leases.update("w1", mac, "s2", True, 3)
            self.assertEqual(leases.active["w1"][1], "s2")
            leases.update("w1", authorized=False)
            self.assertEqual(leases.active, {})
            with self.assertRaises(ValueError):
                leases.update("unknown", mac, "s3", True, 3)
            with self.assertRaises(ValueError):
                leases.update("w2", mac, None, True, 3)
        self.assertEqual(leases.active, {})

    def test_rules_no_conntrack_bypass_and_control_only_before_auth(self):
        rules = lab.rules()
        self.assertEqual(rules.count("policy drop"), 2)
        self.assertIn("ether type 0x888e accept", rules)
        self.assertIn("ip daddr 10.254.122.2 tcp dport 8000", rules)
        self.assertNotIn("ct state", rules)
        self.assertIn("iifname . ether saddr @admitted_in", rules)
        self.assertIn("oifname . ether daddr @admitted_out", rules)
        self.assertNotIn("iifname \"aaa0\"", rules)

    def test_grant_requires_exact_private_session_resource_and_expiry(self):
        self.assertEqual(lab.grant(self.c, lab.read_grants(self.c), "w1", "02:00:00:00:00:01", "s1"), 0)
        path = Path(self.temp.name).resolve() / "grants.json"
        self.c["session_grants"] = str(path)
        record = dict(ingress="w1", mac="02:00:00:00:00:01", session="s1",
                      profile="lab-tcp8000-only", resource="10.254.122.2", protocol="tcp", port=8000, expires_at=102,
                      schemaVersion="soha-admission-grant/v1", producerOwner=self.c["owner"], producerProject=self.c["project"],
                      nasId=self.c["nas_id"], coreSessionId="core1", produced_at=100, hard_expires_at=102)
        lab.write(path, [record])
        grants = lab.read_grants(self.c)
        self.assertEqual(lab.grant(self.c, grants, "w1", record["mac"], "s1", clock=lambda: 100), 2)
        self.assertEqual(lab.grant(self.c, grants, "w2", record["mac"], "s1", clock=lambda: 100), 0)
        self.assertEqual(lab.grant(self.c, grants, "w1", record["mac"], "s1", clock=lambda: 103), 0)
        lab.write(path, [{**record, "producerOwner": "other"}])
        with self.assertRaisesRegex(ValueError, "invalid grant record"):
            lab.read_grants(self.c)
        path.chmod(0o644)
        with self.assertRaises(ValueError):
            lab.read_grants(self.c)
        leases = lab.Leases(self.c)
        with patch.object(lab, "ns") as command:
            leases.update("w1", record["mac"], "s1", True)
        command.assert_not_called()

    def test_cleanup_refuses_namespace_replacement(self):
        lab.prepare(self.c)
        j = lab.load(self.c)
        j["namespaces"][lab.names(self.c)["nas"]] = 100
        lab.save(self.c, j)
        with patch.object(lab, "inode", return_value=101), patch.object(lab, "run") as command:
            with self.assertRaises(RuntimeError):
                lab.cleanup(self.c)
        command.assert_not_called()

    def test_process_failure_flushes_before_exit(self):
        j = {"namespaces": {}, "processes": [{"pid": 123, "start": "x"}], "radios": []}
        with patch.object(lab, "owned"), patch.object(lab, "alive", return_value=False), patch.object(lab, "ns") as command:
            with self.assertRaises(RuntimeError):
                lab.supervise(self.c, j)
        self.assertIn("flush set bridge admission admitted_in", command.call_args.kwargs["input"])

    def test_unsafe_state_ancestors_and_lock_cannot_truncate_sentinel(self):
        parent = self.base / "untrusted"
        parent.mkdir(mode=0o700)
        c = {**self.c, "state": str(parent / "lab")}
        parent.chmod(0o770)
        with self.assertRaisesRegex(ValueError, "unprotected directory"):
            lab.prepare(c)
        parent.chmod(0o700)
        lstat = Path.lstat
        def other_owner(path):
            if path == parent:
                return SimpleNamespace(st_mode=stat.S_IFDIR | 0o700, st_uid=os.geteuid() + 1)
            return lstat(path)
        with patch.object(Path, "lstat", other_owner):
            with self.assertRaisesRegex(ValueError, "unprotected directory"):
                lab.prepare(c)

        lab.prepare(self.c)
        sentinel = self.base / "sentinel"
        sentinel.write_text("unchanged")
        sentinel.chmod(0o600)
        lock = Path(self.c["state"]) / "lock"
        lock.symlink_to(sentinel)
        config = self.base / "lab.json"
        lab.write(config, self.c)
        with patch.object(lab.sys, "argv", ["lab", "cleanup", "--config", str(config)]), patch.object(lab, "cleanup") as cleanup:
            with self.assertRaises(OSError):
                lab.main()
        cleanup.assert_not_called()
        self.assertEqual(sentinel.read_text(), "unchanged")
        lock.unlink()
        os.link(sentinel, lock)
        with self.assertRaisesRegex(ValueError, "untrusted private file"):
            lab.private_open(lock, os.O_RDWR | os.O_CREAT)
        self.assertEqual(sentinel.read_text(), "unchanged")

    def test_invalid_journal_rejected_before_side_effects(self):
        lab.prepare(self.c)
        original = lab.load(self.c)
        nas = lab.names(self.c)["nas"]
        for invalid in (
            {**original, "boot": "other"},
            {**original, "namespaces": {"foreign-nas": 100}},
            {**original, "namespaces": {nas: 100},
             "radios": [{"phy": "phy99", "role": "nas", "interface": "ap0", "ifindex": 3}]},
            {**original, "namespaces": {nas: 100}, "processes": [{"pid": 1, "start": "9", "namespace": nas}]},
        ):
            lab.save(self.c, invalid)
            with patch.object(lab, "run") as command, self.assertRaises(ValueError):
                lab.cleanup(self.c)
            command.assert_not_called()

    def test_cleanup_checks_live_identity_before_flushing(self):
        lab.prepare(self.c)
        j = lab.load(self.c)
        nas = lab.names(self.c)["nas"]
        j["namespaces"][nas] = 100
        j["processes"] = [{"pid": 123, "start": "9", "namespace": nas}]
        lab.save(self.c, j)
        with patch.object(lab, "inode", return_value=100), patch.object(lab, "run") as command, \
                patch.object(lab.os, "pidfd_open", return_value=99, create=True), patch.object(lab.os, "close"), \
                patch.object(lab, "process_identity", return_value=("9", 999)):
            with self.assertRaisesRegex(RuntimeError, "process identity changed"):
                lab.cleanup(self.c)
            command.assert_not_called()

    def test_supervisor_waits_for_first_grants_and_stops_on_disappearance(self):
        path = self.base / "grants.json"
        c = {**self.c, "session_grants": str(path)}
        station = "02:00:00:00:00:01\nflags=[AUTHORIZED]\nacct_session_id=s1\n"
        def after_iteration(_):
            if not path.exists():
                lab.write(path, [])
            else:
                path.unlink()
        with patch.object(lab, "owned"), patch.object(lab, "ns", return_value=station) as command, \
                patch.object(lab.time, "sleep", side_effect=after_iteration):
            with self.assertRaisesRegex(RuntimeError, "grants disappeared"):
                lab.supervise(c, {"processes": []})
        self.assertEqual(sum("all_sta" in call.args for call in command.call_args_list), 6)
        self.assertEqual([call.kwargs["input"] for call in command.call_args_list if "input" in call.kwargs], [lab.FLUSH])

    def test_apply_commands_are_isolated_and_have_distinct_ingress(self):
        secret = Path(self.temp.name).resolve() / "secret"
        secret.write_text("disposable-test-secret")
        secret.chmod(0o600)
        self.c["secret_file"] = str(secret)
        lab.prepare(self.c)
        inodes = {name: 123 + i for i, name in enumerate(lab.names(self.c).values())}
        with patch.object(lab, "preflight"), patch.object(lab, "inode", side_effect=inodes.__getitem__), \
                patch.object(lab, "radio_ifindex", return_value=3), \
                patch.object(lab, "run", return_value='[ {"address": "02:00:00:00:00:01"} ]') as command, patch.object(lab, "process"), patch.object(lab, "supervise"), patch.object(Path, "exists", return_value=True):
            lab.apply(self.c)
        calls = [x.args for x in command.call_args_list]
        self.assertFalse(any("modprobe" in x for x in calls))
        for port in ("w1", "w2"):
            self.assertTrue(any("locked" in x and port in x for x in calls))
        veth = [x for x in calls if "veth" in x]
        self.assertEqual(len(veth), 4)
        self.assertTrue(all(x[:3] == ("ip", "netns", "exec") for x in veth))
        self.assertEqual(set(lab.load(self.c)["namespaces"]), set(lab.names(self.c).values()))


if __name__ == "__main__":
    unittest.main()
