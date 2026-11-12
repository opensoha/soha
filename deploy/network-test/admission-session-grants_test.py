import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("grants", Path(__file__).with_name("admission-session-grants.py"))
g = importlib.util.module_from_spec(spec)
spec.loader.exec_module(g)


class GrantTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        lstat = Path.lstat
        def fixture_ancestors(path):
            if path in self.base.parents:
                return SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=os.geteuid())
            return lstat(path)
        for guard in (patch.object(g.lab, "ROOT_UID", os.geteuid()),
                      patch.object(Path, "lstat", fixture_ancestors),
                      patch.object(g.lab, "boot_id", return_value="fixture-boot"),
                      patch.object(g.lab, "origin_inode", return_value=456)):
            guard.start()
            self.addCleanup(guard.stop)
        self.c = dict(owner="task122-b3-l02-a02", project="task122-admission", state=str(self.base / "state"),
                      nas_id="nas-task122", ap_phy="phy91", sta_phy="phy92", lease_seconds=10,
                      radius_config="/isolated/radius", secret_file="/isolated/secret", supplicant_config="/isolated/wpa.conf",
                      event_file=str(self.base / "events.jsonl"), event_uid=os.geteuid(), session_grants=str(self.base / "grants.json"))
        g.lab.prepare(self.c)
        self.event_file = Path(self.c["event_file"])
        self.event_file.write_text("")
        self.event_file.chmod(0o600)
        self.mac = "02:00:00:00:00:01"
        self.auth = dict(schemaVersion=g.VERSION, event="authorize", nasId=self.c["nas_id"], nasPortId="w1",
                         stationId="02-00-00-00-00-01", coreSessionId="core-1", observedAt=100,
                         sessionTimeout=30, filterId="lab-tcp8000-only", vlanId=None)
        self.start = {k: v for k, v in self.auth.items() if k not in g.AUTH}
        self.start.update(event="start", accountingSessionId="acct-1", sessionTimeSeconds=0, observedAt=101)
        self.stations = {"w1": (self.mac, "acct-1")}

    def model(self, *events, now=105):
        model = g.Events(self.c, now)
        for event in events:
            model.accept(event)
        return model

    def append(self, *events):
        with self.event_file.open("a") as stream:
            for event in events:
                stream.write(json.dumps(event) + "\n")

    def test_success_requires_authorization_start_and_exact_current_hostapd(self):
        self.assertEqual(self.model(self.auth).grants(self.stations), [])
        model = self.model(self.auth, self.start)
        grants = model.grants(self.stations)
        self.assertEqual(len(grants), 1)
        self.assertEqual(grants[0]["coreSessionId"], "core-1")
        self.assertEqual(grants[0]["hard_expires_at"], 130)
        self.assertEqual(grants[0]["expires_at"], 115)
        for stations in ({}, {"w2": (self.mac, "acct-1")}, {"w1": (self.mac, "old-acct")}, {"w1": ("02:00:00:00:00:02", "acct-1")}):
            with self.subTest(stations=stations):
                self.assertEqual(model.grants(stations), [])

    def test_cross_session_nas_ingress_station_and_unknown_schema_fail_closed(self):
        cases = [dict(coreSessionId="other"), dict(nasId="other"), dict(nasPortId="w2"),
                 dict(stationId="02:00:00:00:00:02"), dict(stationId="endpoint1"), dict(schemaVersion="unknown"),
                 dict(event="synthetic"), dict(extra="password-must-not-exist"), dict(accountingSessionId="bad_id")]
        for change in cases:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.model(self.auth, {**self.start, **change})

    def test_replay_out_of_order_and_accounting_duration(self):
        interim = {**self.start, "event": "interim-update", "observedAt": 104, "sessionTimeSeconds": 3}
        cases = [(self.auth, self.auth), (self.auth, self.start, self.start), (self.start, self.auth),
                 (self.auth, self.start, {**interim, "observedAt": 100}),
                 (self.auth, self.start, interim, interim),
                 (self.auth, self.start, {**interim, "sessionTimeSeconds": 20}),
                 (self.auth, self.start, interim, {**interim, "observedAt": 105, "sessionTimeSeconds": 2})]
        for events in cases:
            with self.subTest(events=events), self.assertRaises(ValueError):
                self.model(*events)

    def test_stop_terminal_and_old_stop_cannot_grant_or_end_new_session(self):
        stop = {**self.start, "event": "stop", "observedAt": 104, "sessionTimeSeconds": 3}
        model = self.model(self.auth, self.start, stop)
        self.assertEqual(model.grants(self.stations), [])
        with self.assertRaises(ValueError):
            model.accept({**stop, "observedAt": 105})
        auth2 = {**self.auth, "coreSessionId": "core-2", "observedAt": 104}
        start2 = {**self.start, "coreSessionId": "core-2", "accountingSessionId": "acct-2", "observedAt": 104}
        model = self.model(self.auth, self.start, auth2, start2)
        model.accept(stop)
        self.assertEqual(model.grants({"w1": (self.mac, "acct-2")})[0]["coreSessionId"], "core-2")

    def test_interim_never_extends_original_hard_deadline(self):
        interim = {**self.start, "event": "interim-update", "observedAt": 129, "sessionTimeSeconds": 28}
        grant = self.model(self.auth, self.start, interim, now=129).grants(self.stations)[0]
        self.assertEqual(grant["expires_at"], 130)
        self.assertEqual(self.model(self.auth, self.start, interim, now=130).grants(self.stations), [])
        with self.assertRaises(ValueError):
            self.model(self.auth, {**self.start, "observedAt": 131}, now=131)

    def test_unsupported_attributes_and_invalid_times(self):
        for change in (dict(filterId="full"), dict(vlanId=20), dict(vlanId=0), dict(sessionTimeout=0),
                       dict(sessionTimeout=True), dict(sessionTimeout=86401), dict(observedAt=float("nan")),
                       dict(observedAt=108), dict(accountingSessionId="forbidden")):
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.model({**self.auth, **change})
        self.assertEqual(g.mac("AA-BB-CC-DD-EE-FF"), "aa:bb:cc:dd:ee:ff")
        with self.assertRaises(ValueError):
            g.mac("aa-bb:cc-dd-ee-ff")

    def test_source_pin_rejects_replace_truncate_prefix_and_untrusted_permissions(self):
        self.append(self.auth, self.start)
        source = g.Source(self.c)
        source.read()
        # Restart must bind the same inode and historical prefix.
        self.assertEqual(len(g.Source(self.c).read()), 2)
        self.event_file.chmod(0o644)
        with self.assertRaises(ValueError):
            source.read()
        self.event_file.chmod(0o600)
        contents = self.event_file.read_text()
        self.event_file.write_text(contents.replace("core-1", "core-9"))
        with self.assertRaises(ValueError):
            g.Source(self.c).read()
        self.event_file.write_text(contents)
        self.event_file.write_text("")
        with self.assertRaises(ValueError):
            source.read()
        self.event_file.write_text(contents)
        replacement = self.base / "replacement"
        replacement.write_text(contents)
        replacement.chmod(0o600)
        replacement.replace(self.event_file)
        with self.assertRaises(ValueError):
            g.Source(self.c).read()

    def test_partial_line_waits_but_cannot_be_rewritten_or_truncated(self):
        partial = json.dumps(self.auth)
        self.event_file.write_text(partial)
        source = g.Source(self.c)
        self.assertEqual(source.read(), [])
        with self.event_file.open("a") as stream:
            stream.write("\n")
        self.assertEqual(source.read(), [self.auth])
        self.event_file.write_text(partial)
        with self.assertRaises(ValueError):
            source.read()

    def test_tick_writes_exact_grant_consumer_rejects_wrong_owner_or_extended_deadline(self):
        self.append(self.auth, self.start)
        output = self.mac + "\nflags=[AUTH][AUTHORIZED]\nacct_session_id=acct-1\n"
        with patch.object(g.lab, "owned"), patch.object(g.lab, "ns", side_effect=lambda c, role, *a: output if "w1" in a else ""):
            grants = g.tick(self.c, g.Source(self.c), clock=lambda: 105)
        self.assertEqual(len(grants), 1)
        self.assertEqual(g.lab.grant(self.c, g.lab.read_grants(self.c), "w1", self.mac, "acct-1", clock=lambda: 105), 10)
        for change in (dict(producerOwner="other"), dict(coreSessionId="bad id"), dict(hard_expires_at=106)):
            with self.subTest(change=change):
                g.atomic(self.c["session_grants"], [{**grants[0], **change}])
                with self.assertRaises(ValueError):
                    g.lab.read_grants(self.c)
        g.atomic(self.c["session_grants"], [{**grants[0], "produced_at": 80, "expires_at": 90}])
        self.assertEqual(g.lab.grant(self.c, g.lab.read_grants(self.c), "w1", self.mac, "acct-1", clock=lambda: 105), 0)

    def test_producer_failure_clears_existing_output(self):
        g.atomic(self.c["session_grants"], [{"old": "grant"}])
        with patch.object(g, "tick", side_effect=ValueError("untrusted source")):
            with self.assertRaises(ValueError):
                g.execute(self.c)
        self.assertEqual(json.loads(Path(self.c["session_grants"]).read_text()), [])

    def test_untrusted_writer_symlink_and_hardlink_rejected(self):
        self.append(self.auth)
        with self.assertRaises(ValueError):
            g.private_read(self.event_file, {os.geteuid() + 9999})
        alias = self.base / "alias"
        alias.symlink_to(self.event_file)
        with self.assertRaises(ValueError):
            g.private_read(alias, {os.geteuid()})
        alias.unlink()
        os.link(self.event_file, alias)
        with self.assertRaises(ValueError):
            g.private_read(self.event_file, {os.geteuid()})


if __name__ == "__main__":
    unittest.main()
