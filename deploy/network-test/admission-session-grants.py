#!/usr/bin/env python3
"""Correlate trusted Core-authorized RADIUS/NAS events with current hostapd state."""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import signal
import stat
import time

spec = importlib.util.spec_from_file_location("admission_lab", Path(__file__).with_name("admission-linux-lab.py"))
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)
VERSION = "soha-admission-event/v1"
GRANT_VERSION = "soha-admission-grant/v1"
PORTS = ("ap0", "w1", "w2")
BASE = {"schemaVersion", "event", "nasId", "nasPortId", "stationId", "coreSessionId", "observedAt"}
AUTH = {"sessionTimeout", "filterId", "vlanId"}
ACCT = {"accountingSessionId", "sessionTimeSeconds"}


def mac(value):
    if not isinstance(value, str) or not re.fullmatch(r"(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}|(?:[0-9A-Fa-f]{2}-){5}[0-9A-Fa-f]{2}", value):
        raise ValueError("invalid station MAC")
    return value.lower().replace("-", ":")


def number(value):
    return type(value) in (int, float) and math.isfinite(value)


def private_read(path, uids):
    p = Path(path)
    if not p.is_absolute() or any(x.is_symlink() for x in (p, *p.parents)):
        raise ValueError("absolute non-symlink private file required")
    fd = os.open(p, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid not in uids or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError("untrusted file ownership/type/permissions")
        data = b""
        while True:
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            data += chunk
            if len(data) > 16 * 1024 * 1024:
                raise ValueError("lab event/grant file exceeds bounded size")
        after = os.fstat(fd)
        if (after.st_dev, after.st_ino) != (p.stat().st_dev, p.stat().st_ino) or after.st_size < len(data):
            raise ValueError("file replaced/truncated while reading")
        return data, info
    finally:
        os.close(fd)


def atomic(path, value):
    lab.write(Path(path), value)


class Source:
    def __init__(self, c):
        self.c = c
        self.pin = Path(c["state"]) / "event-source.json"
        self.previous = b""
        self.identity = None

    def read(self, persist=True):
        data, info = private_read(self.c["event_file"], {0, self.c["event_uid"]})
        identity = [info.st_dev, info.st_ino]
        if self.pin.exists():
            old = json.loads(private_read(self.pin, {os.geteuid()})[0])
            if (old["identity"] != identity or len(data) < old["length"] or
                    hashlib.sha256(data[:old["length"]]).hexdigest() != old["sha256"]):
                raise ValueError("event source identity/prefix changed; explicit recovery required")
        if self.identity is not None and (identity != self.identity or not data.startswith(self.previous)):
            raise ValueError("event source changed")
        raw = data
        if data and not data.endswith(b"\n"):
            # A short partially appended line is not an event until its newline arrives.
            complete, _, partial = data.rpartition(b"\n")
            if len(partial) > 8192:
                raise ValueError("oversize partial event")
            data = complete + (b"\n" if complete else b"")
        self.identity, self.previous = identity, raw
        if persist:
            atomic(self.pin, {"identity": identity, "length": len(raw), "sha256": hashlib.sha256(raw).hexdigest()})
        records = []
        for line in data.splitlines():
            if not line or len(line) > 8192:
                raise ValueError("invalid event line")
            records.append(json.loads(line))
        return records


class Events:
    def __init__(self, c, now):
        self.c, self.now = c, now
        self.sessions, self.seen, self.accounting_ids = {}, set(), set()

    def accept(self, event):
        if not isinstance(event, dict):
            raise ValueError("event must be object")
        kind = event.get("event")
        expected = BASE | (AUTH if kind == "authorize" else ACCT)
        if kind not in ("authorize", "start", "interim-update", "stop") or set(event) != expected or event["schemaVersion"] != VERSION:
            raise ValueError("unknown event/schema/fields")
        if event["nasId"] != self.c["nas_id"] or event["nasPortId"] not in PORTS:
            raise ValueError("unknown NAS/ingress")
        station = mac(event["stationId"])
        core = event["coreSessionId"]
        if not isinstance(core, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,127}", core):
            raise ValueError("invalid Core session identity")
        observed = event["observedAt"]
        if not number(observed) or observed <= 0 or observed > self.now + 2:
            raise ValueError("invalid event observation time")
        canonical = json.dumps({**event, "stationId": station}, sort_keys=True)
        if canonical in self.seen:
            raise ValueError("replayed event")
        self.seen.add(canonical)
        key = (event["nasId"], event["nasPortId"], station, core)
        if kind == "authorize":
            if type(event["sessionTimeout"]) is not int or not 0 < event["sessionTimeout"] <= 86400:
                raise ValueError("invalid authorization hard timeout")
            if event["filterId"] != "lab-tcp8000-only" or event["vlanId"] is not None:
                raise ValueError("unsupported filter/VLAN; not enforced")
            if key in self.sessions or any(k[3] == core for k in self.sessions):
                raise ValueError("Core session authorization reused")
            self.sessions[key] = {"deadline": observed + event["sessionTimeout"], "observed": observed,
                                  "accounting": None, "elapsed": -1, "stopped": False, "retired": False}
            return
        accounting = event["accountingSessionId"]
        elapsed = event["sessionTimeSeconds"]
        if (not isinstance(accounting, str) or not re.fullmatch(r"[A-Za-z0-9.-]{1,128}", accounting) or
                type(elapsed) is not int or elapsed < 0):
            raise ValueError("invalid accounting session/time")
        state = self.sessions.get(key)
        if state is None or state["stopped"] or observed < state["observed"]:
            raise ValueError("unmatched/terminal/out-of-order accounting")
        if kind == "start":
            if state["accounting"] is not None or elapsed != 0 or accounting in self.accounting_ids or observed >= state["deadline"]:
                raise ValueError("invalid/reused/expired Start")
            # Only one current session may own an ingress; old sessions stay terminal.
            for other_key, other in self.sessions.items():
                if other_key[:3] == key[:3] and other_key != key and other["accounting"] is not None:
                    other["retired"] = True
            state["accounting"], state["started"] = accounting, observed
            self.accounting_ids.add(accounting)
        else:
            if state["accounting"] != accounting or elapsed < state["elapsed"]:
                raise ValueError("cross-session/reversed accounting")
            if kind == "interim-update" and (elapsed <= state["elapsed"] or observed <= state["observed"]):
                raise ValueError("replayed/out-of-order Interim")
            if elapsed > observed - state["started"] + 2:
                raise ValueError("accounting duration exceeds observation")
            if kind == "stop":
                state["stopped"] = True
        state["observed"], state["elapsed"] = observed, elapsed

    def grants(self, stations):
        result = []
        for (nas, port, station, core), state in self.sessions.items():
            if (state["stopped"] or state["retired"] or state["accounting"] is None or state["deadline"] <= self.now or
                    stations.get(port) != (station, state["accounting"])):
                continue
            result.append({"schemaVersion": GRANT_VERSION, "producerOwner": self.c["owner"],
                           "producerProject": self.c["project"], "nasId": nas, "coreSessionId": core,
                           "ingress": port, "mac": station, "session": state["accounting"],
                           "profile": "lab-tcp8000-only", "resource": "10.254.122.2", "protocol": "tcp", "port": 8000,
                           "produced_at": self.now, "hard_expires_at": state["deadline"],
                           "expires_at": min(state["deadline"], self.now + self.c["lease_seconds"])})
        if len({g["ingress"] for g in result}) != len(result):
            raise ValueError("ambiguous current ingress authorization")
        return result


def evaluate(c, source, now, stations):
    events = Events(c, now)
    for event in source.read():
        events.accept(event)
    return events.grants(stations)


def tick(c, source, clock=time.time):
    journal = lab.load(c)
    lab.owned(c, journal)
    stations = {}
    for port in PORTS:
        output = lab.ns(c, "nas", "hostapd_cli", "-p", str(Path(c["state"]) / ("ctrl-" + port)), "-i", port, "all_sta")
        stations[port] = lab.station(output)
    result = evaluate(c, source, clock(), stations)
    atomic(c["session_grants"], result)
    return result


def execute(c):
    source = Source(c)
    previous = {}
    def stop(*_):
        raise KeyboardInterrupt()
    for sig in (signal.SIGINT, signal.SIGTERM):
        previous[sig] = signal.signal(sig, stop)
    try:
        atomic(c["session_grants"], [])
        while True:
            tick(c, source)
            time.sleep(0.25)
    finally:
        atomic(c["session_grants"], [])
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("run", "status"))
    parser.add_argument("--config", type=Path, required=True)
    args = parser.parse_args()
    c = lab.read_config(args.config)
    if type(c.get("event_uid")) is not int or c["event_uid"] < 0 or not c.get("event_file") or not c.get("session_grants"):
        raise ValueError("explicit event_file/event_uid/session_grants required")
    lab.load(c)
    if args.action == "status":
        source = Source(c)
        records = source.read(persist=False)
        events = Events(c, time.time())
        for event in records:
            events.accept(event)
        print(json.dumps({"sourceIdentity": source.identity, "records": len(records),
                          "authorizations": len(events.sessions), "dataPlaneVerified": False, "dynamicVLAN": "unsupported"}))
        return
    path = Path(c["state"]) / "session-grants.lock"
    with os.fdopen(lab.private_open(path, os.O_RDWR | os.O_CREAT), "r+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        execute(c)


if __name__ == "__main__":
    main()
