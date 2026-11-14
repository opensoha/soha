#!/usr/bin/env python3
"""Explicit, disposable Linux software NAS. No physical NAS/CoA capability claim."""
import argparse
from contextlib import ExitStack
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import re
import select
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time


ROOT_UID = 0
PRIVATE_LIMIT = 16 * 1024 * 1024
POSITIVE_INTEGER = r"[1-9][0-9]*"
FLUSH = "flush set bridge admission admitted_in\nflush set bridge admission admitted_out\n"


def run(*args, input=None):
    return subprocess.run(args, input=input, text=True, capture_output=True, check=True).stdout


def protected_dirs(path, private=False):
    if not path.is_absolute() or ".." in path.parts or path.anchor != "/":
        raise ValueError("absolute canonical path required")
    for directory in (*reversed(path.parents), path):
        info = directory.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != ROOT_UID or info.st_mode & 0o022:
            raise ValueError("unprotected directory: " + str(directory))
        if private and directory == path and stat.S_IMODE(info.st_mode) != 0o700:
            raise ValueError("state directory must have mode 0700")


def private_open(path, flags):
    protected_dirs(path.parent)
    fd = os.open(path, flags | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != ROOT_UID or
                info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600):
            raise ValueError("untrusted private file: " + str(path))
        return fd
    except BaseException:
        os.close(fd)
        raise


def read_private(path):
    with os.fdopen(private_open(path, os.O_RDONLY), "r") as stream:
        text = stream.read(PRIVATE_LIMIT + 1)
    if len(text) > PRIVATE_LIMIT:
        raise ValueError("private file exceeds size limit")
    return text


def write_private(path, text):
    protected_dirs(path.parent)
    try:
        os.close(private_open(path, os.O_RDONLY))
    except FileNotFoundError:
        pass
    fd, temporary = tempfile.mkstemp(prefix=".admission-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            stream.write(text)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def write(path, value):
    write_private(path, json.dumps(value, indent=2) + "\n")


def read_config(path):
    if os.geteuid() != ROOT_UID:
        raise PermissionError("admission lab requires root")
    return validate(json.loads(read_private(path)))


def validate(c):
    for key in ("owner", "project", "nas_id", "ap_phy", "sta_phy"):
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,62}", c[key]):
            raise ValueError("invalid identifier: " + key)
    if c["ap_phy"] == c["sta_phy"]:
        raise ValueError("distinct preallocated hwsim radios required")
    if type(c["lease_seconds"]) is not int or not 2 <= c["lease_seconds"] <= 30:
        raise ValueError("lease must be 2..30 seconds")
    for key in ("state", "radius_config", "secret_file", "supplicant_config"):
        p = Path(c[key])
        if not p.is_absolute() or any(x.is_symlink() for x in (p, *p.parents)):
            raise ValueError("absolute non-symlink paths required: " + key)
    if Path(c["state"]) == Path("/"):
        raise ValueError("state cannot be root")
    for key in ("ap_phy", "sta_phy"):
        if not re.fullmatch(r"phy[0-9]+", c[key]):
            raise ValueError("explicit hwsim phy index required")
    if any(key in c for key in ("event_file", "event_uid", "session_grants")):
        if type(c.get("event_uid")) is not int or c["event_uid"] < 0 or not c.get("event_file") or not c.get("session_grants"):
            raise ValueError("complete event source identity and grant path required")
        for key in ("event_file", "session_grants"):
            p = Path(c[key])
            if not p.is_absolute() or any(x.is_symlink() for x in (p, *p.parents)):
                raise ValueError("explicit non-symlink event/grant paths required")
        reserved = {str(Path(c["state"]) / name) for name in ("owner.json", "resources.json", "event-source.json", "lock", "session-grants.lock")}
        reserved.update(str(Path(c[key])) for key in ("event_file", "secret_file", "supplicant_config"))
        if str(Path(c["session_grants"])) in reserved:
            raise ValueError("grant output conflicts with an input/state file")
    return c


def names(c):
    prefix = "al" + hashlib.sha256((c["owner"] + ":" + c["project"]).encode()).hexdigest()[:8]
    return {role: prefix + "-" + role for role in ("nas", "aaa", "sta", "w1", "w2", "svc")}


def plan(c):
    return {"owner": c["owner"], "project": c["project"], "namespaces": names(c),
            "radios": {"ap": c["ap_phy"], "sta": c["sta_phy"]},
            "ingress": {"ap0": "sta", "w1": "w1", "w2": "w2"},
            "aaa": "10.254.121.0/30", "business": ["10.254.122.0/24", "fd42:122::/64"],
            "leaseMaximumSeconds": c["lease_seconds"], "managementLink": None,
            "coa": "unsupported", "accounting": "hostapd NAS events; radius-lab radclient is synthetic"}


def prepare(c):
    folder = Path(c["state"])
    protected_dirs(folder.parent)
    if folder.exists():
        load(c)
        return
    folder.mkdir(mode=0o700)
    protected_dirs(folder, private=True)
    write(folder / "owner.json", c)
    write(folder / "resources.json", {"boot": boot_id(), "origin": origin_inode(),
                                     "namespaces": {}, "radios": [], "processes": []})


def load(c):
    folder = Path(c["state"])
    protected_dirs(folder, private=True)
    if json.loads(read_private(folder / "owner.json")) != c:
        raise RuntimeError("owner/config mismatch; refusing reuse")
    journal = json.loads(read_private(folder / "resources.json"))
    validate_journal(c, journal)
    return journal


def save(c, journal):
    write(Path(c["state"]) / "resources.json", journal)


def boot_id():
    return Path("/proc/sys/kernel/random/boot_id").read_text().strip()


def ns(c, role, *args, input=None):
    return run("ip", "netns", "exec", names(c)[role], *args, input=input)


def origin_inode():
    return Path("/proc/self/ns/net").stat().st_ino


def inode(name):
    return Path("/run/netns", name).stat().st_ino


def validate_journal(c, j):
    if (not isinstance(j, dict) or set(j) != {"boot", "origin", "namespaces", "radios", "processes"} or
            j["boot"] != boot_id() or type(j["origin"]) is not int or j["origin"] != origin_inode()):
        raise ValueError("invalid journal or different boot/origin netns")
    netns = j["namespaces"]
    if (not isinstance(netns, dict) or set(netns) - set(names(c).values()) or
            any(type(i) is not int or i <= 0 or i == j["origin"] for i in netns.values()) or
            len(set(netns.values())) != len(netns)):
        raise ValueError("invalid namespace journal")
    if not isinstance(j["radios"], list) or not isinstance(j["processes"], list):
        raise ValueError("invalid radio/process journal")
    validate_radios(c, netns, j["radios"])
    validate_processes(c, netns, j["processes"])


def validate_radios(c, netns, radios):
    expected = {"nas": (c["ap_phy"], "ap0"), "sta": (c["sta_phy"], "sta0")}
    seen = set()
    for r in radios:
        if not isinstance(r, dict) or set(r) != {"phy", "role", "interface", "ifindex"}:
            raise ValueError("invalid radio record")
        role, index = r["role"], r["ifindex"]
        if (not isinstance(role, str) or role in seen or expected.get(role) != (r["phy"], r["interface"]) or
                names(c)[role] not in netns or (index is not None and (type(index) is not int or index <= 0))):
            raise ValueError("invalid radio identity")
        seen.add(role)


def validate_processes(c, netns, processes):
    expected = {names(c)[role]: limit for role, limit in
                (("aaa", 1), ("nas", 3 + bool(c.get("event_file"))), ("sta", 1), ("w1", 1), ("w2", 1))}
    seen = set()
    for p in processes:
        if not isinstance(p, dict) or set(p) != {"pid", "start", "namespace"}:
            raise ValueError("invalid process record")
        pid, start, name = p["pid"], p["start"], p["namespace"]
        if (type(pid) is not int or pid <= 1 or pid in seen or not isinstance(start, str) or
                not re.fullmatch(POSITIVE_INTEGER, start) or not isinstance(name, str) or
                name not in netns or expected.get(name, 0) < 1):
            raise ValueError("invalid process identity")
        expected[name] -= 1
        seen.add(pid)


def owned(c, j):
    for name, ident in j["namespaces"].items():
        if inode(name) != ident:
            raise RuntimeError("namespace replaced: " + name)


def rules():
    # No conntrack established bypass: existing TCP/UDP flows lose access too.
    return """table bridge admission {
 set admitted_in { type ifname . ether_addr; flags timeout; }
 set admitted_out { type ifname . ether_addr; flags timeout; }
 chain input { type filter hook input priority -200; policy drop;
  iifname { "ap0", "w1", "w2" } ether type 0x888e accept
 }
 chain forward { type filter hook forward priority -200; policy drop;
  oifname "resource" iifname . ether saddr @admitted_in ether type arp arp daddr ip 10.254.122.2 accept
  iifname "resource" oifname . ether daddr @admitted_out ether type arp arp saddr ip 10.254.122.2 accept
  oifname "resource" iifname . ether saddr @admitted_in ip daddr 10.254.122.2 tcp dport 8000 accept
  iifname "resource" oifname . ether daddr @admitted_out ip saddr 10.254.122.2 tcp sport 8000 accept
  oifname "resource" iifname . ether saddr @admitted_in ip6 daddr fd42:122::2 tcp dport 8000 accept
  iifname "resource" oifname . ether daddr @admitted_out ip6 saddr fd42:122::2 tcp sport 8000 accept
 }
}
"""


def preflight(c):
    if not hasattr(os, "pidfd_open") or not hasattr(signal, "pidfd_send_signal"):
        raise RuntimeError("Linux Python with pidfd support required for exact process cleanup")
    for tool in ("ip", "bridge", "nft", "iw", "hostapd", "hostapd_cli", "wpa_supplicant", "freeradius"):
        if not shutil.which(tool):
            raise RuntimeError("missing tool: " + tool)
    protected_dirs(Path(c["radius_config"]))
    read_private(Path(c["supplicant_config"]))
    secret = read_private(Path(c["secret_file"])).strip()
    if c.get("session_grants"):
        read_grants(c)
    if not re.fullmatch(r"[A-Za-z0-9._~+/@%=-]{16,256}", secret):
        raise ValueError("invalid disposable RADIUS secret")
    for name in names(c).values():
        if Path("/run/netns", name).exists():
            raise RuntimeError("refusing existing namespace: " + name)
    for phy in (c["ap_phy"], c["sta_phy"]):
        driver = Path("/sys/class/ieee80211", phy, "device/driver").resolve().name
        if driver != "mac80211_hwsim":
            raise RuntimeError("radio is not a preallocated hwsim device: " + phy)
        if run("iw", "phy", phy, "info").find("Wiphy " + phy) < 0:
            raise RuntimeError("radio unavailable")
        # Supplied radios must be empty; never remove a preexisting interface.
        listing = run("iw", "dev")
        index = int(phy.removeprefix("phy"))
        block = re.search(r"phy#" + str(index) + r"\n(.*?)(?=phy#|\Z)", listing, re.S)
        if block and "Interface " in block.group(1):
            raise RuntimeError("preallocated radio must have no interfaces")


def link(c, a, ai, b, bi):
    # Both endpoints are born in owned namespaces, never in host management netns.
    ns(c, a, "ip", "link", "add", ai, "type", "veth", "peer", "name", bi, "netns", names(c)[b])
    for role, iface in ((a, ai), (b, bi)):
        ns(c, role, "ip", "link", "set", iface, "up")


def config_file(c, name, text):
    p = Path(c["state"]) / name
    write_private(p, text)
    return str(p)


def process(c, j, role, argv):
    path = Path(c["state"]) / (role + "-" + str(len(j["processes"])) + ".log")
    with os.fdopen(private_open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND), "ab") as log:
        p = subprocess.Popen(["ip", "netns", "exec", names(c)[role], *argv], stdout=log, stderr=log)
    try:
        name = names(c)[role]
        deadline = time.monotonic() + 2
        while p.poll() is None and time.monotonic() < deadline:
            identity = process_identity(p.pid)
            if identity and identity[1] == j["namespaces"][name]:
                j["processes"].append({"pid": p.pid, "start": identity[0], "namespace": name})
                save(c, j)
                return
            time.sleep(0.01)
        raise RuntimeError("process did not enter the owned namespace")
    except BaseException:
        if p.poll() is None:
            p.kill()
        p.wait()
        raise


def radio_ifindex(c, role, phy, iface):
    info = ns(c, role, "iw", "dev", iface, "info")
    index = re.search(r"(?m)^\s*ifindex (" + POSITIVE_INTEGER + r")$", info)
    if not index or not re.search(r"(?m)^\s*wiphy " + phy.removeprefix("phy") + r"$", info):
        raise RuntimeError("radio/interface identity mismatch")
    return int(index[1])


def apply(c):
    j = load(c)
    if any(j[key] for key in ("namespaces", "radios", "processes")):
        raise RuntimeError("resources already recorded; inspect/cleanup first")
    preflight(c)
    if read_grants(c) is not None:
        write(Path(c["session_grants"]), [])
    for role, name in names(c).items():
        run("ip", "netns", "add", name)
        j["namespaces"][name] = inode(name)
        save(c, j)
        ns(c, role, "ip", "link", "set", "lo", "up")
    # An incomplete move/create stays recorded for explicit operator recovery.
    for key, role, iface, kind in (("ap_phy", "nas", "ap0", "__ap"), ("sta_phy", "sta", "sta0", "managed")):
        phy = c[key]
        radio = {"phy": phy, "role": role, "interface": iface, "ifindex": None}
        j["radios"].append(radio)
        save(c, j)
        run("iw", "phy", phy, "set", "netns", "name", names(c)[role])
        ns(c, role, "iw", "phy", phy, "interface", "add", iface, "type", kind)
        radio["ifindex"] = radio_ifindex(c, role, phy, iface)
        save(c, j)
    ns(c, "nas", "ip", "link", "add", "br-data", "type", "bridge")
    ns(c, "nas", "ip", "link", "set", "br-data", "up")
    ns(c, "nas", "nft", "-f", "-", input=rules())
    link(c, "nas", "aaa0", "aaa", "radius0")
    ns(c, "nas", "ip", "addr", "add", "10.254.121.2/30", "dev", "aaa0")
    ns(c, "aaa", "ip", "addr", "add", "10.254.121.1/30", "dev", "radius0")
    for client, host in (("w1", 11), ("w2", 12), ("svc", 2)):
        ingress = "resource" if client == "svc" else client
        link(c, "nas", ingress, client, "data0")
        ns(c, "nas", "ip", "link", "set", ingress, "master", "br-data")
        ns(c, client, "ip", "addr", "add", "10.254.122." + str(host) + "/24", "dev", "data0")
        ns(c, client, "ip", "-6", "addr", "add", "fd42:122::" + str(host) + "/64", "dev", "data0")
    for ingress in ("w1", "w2"):
        ns(c, "nas", "bridge", "link", "set", "dev", ingress, "learning", "off", "locked", "on")
    # Wi-Fi controlled port is also filtered independently of hostapd.
    ns(c, "nas", "ip", "link", "set", "ap0", "master", "br-data")
    ns(c, "sta", "ip", "addr", "add", "10.254.122.10/24", "dev", "sta0")
    ns(c, "sta", "ip", "-6", "addr", "add", "fd42:122::10/64", "dev", "sta0")
    resource_mac = json.loads(ns(c, "svc", "ip", "-j", "link", "show", "data0"))[0]["address"]
    for role, iface in (("sta", "sta0"), ("w1", "data0"), ("w2", "data0")):
        ns(c, role, "ip", "-6", "neigh", "replace", "fd42:122::2", "lladdr", resource_mac, "nud", "permanent", "dev", iface)
        client_mac = json.loads(ns(c, role, "ip", "-j", "link", "show", iface))[0]["address"]
        host = {"sta": "10", "w1": "11", "w2": "12"}[role]
        ns(c, "svc", "ip", "-6", "neigh", "replace", "fd42:122::" + host, "lladdr", client_mac, "nud", "permanent", "dev", "data0")
    process(c, j, "aaa", ["freeradius", "-f", "-d", c["radius_config"]])
    secret = read_private(Path(c["secret_file"])).strip()
    for ingress in ("ap0", "w1", "w2"):
        ctrl = str(Path(c["state"]) / ("ctrl-" + ingress))
        base = ("interface=" + ingress + "\nctrl_interface=" + ctrl + "\n"
                "ieee8021x=1\neap_server=0\nauth_server_addr=10.254.121.1\nauth_server_port=1812\n"
                "auth_server_shared_secret=" + secret + "\nown_ip_addr=10.254.121.2\nnas_identifier=" + c["nas_id"] + "\n"
                "acct_server_addr=10.254.121.1\nacct_server_port=1813\nacct_server_shared_secret=" + secret + "\n"
                "radius_auth_req_attr=87:s:" + ingress + "\nradius_acct_req_attr=87:s:" + ingress + "\n")
        extra = ("driver=nl80211\nbridge=br-data\nssid=Soha-Isolated-Lab\nhw_mode=g\nchannel=1\nwpa=2\n"
                 "wpa_key_mgmt=WPA-EAP\nrsn_pairwise=CCMP\n") if ingress == "ap0" else "driver=wired\nuse_pae_group_addr=1\n"
        process(c, j, "nas", ["hostapd", config_file(c, ingress + ".conf", base + extra)])
    for role, iface, driver in (("sta", "sta0", "nl80211"), ("w1", "data0", "wired"), ("w2", "data0", "wired")):
        process(c, j, role, ["wpa_supplicant", "-i", iface, "-D", driver, "-c", c["supplicant_config"]])
    # No admission while authenticators initialize; missing control sockets fail closed.
    deadline = time.monotonic() + 10
    for port in ("ap0", "w1", "w2"):
        while not (Path(c["state"]) / ("ctrl-" + port) / port).exists():
            if time.monotonic() >= deadline or not all(alive(p, j) for p in j["processes"]):
                raise RuntimeError("authenticator startup failed; no admission granted")
            time.sleep(0.1)
    if c.get("event_file"):
        process(c, j, "nas", [sys.executable, str(Path(__file__).with_name("admission-session-grants.py").resolve()),
                              "run", "--config", str(Path(c["state"]) / "owner.json")])
    # Foreground supervisor: killing it leaves only bounded nft timeouts.
    supervise(c, j)


class Leases:
    def __init__(self, c, clock=time.monotonic):
        self.c, self.clock, self.active, self.spent = c, clock, {}, set()

    def revoke(self, port):
        if port in self.active:
            mac, session, deadline = self.active.pop(port)
            self.spent.add((port, mac, session))
            self.sync()
            if port != "ap0":
                ns(self.c, "nas", "bridge", "fdb", "del", mac, "dev", port, "master", "static")

    def update(self, port, mac=None, session=None, authorized=False, grant_seconds=0):
        if port not in ("ap0", "w1", "w2"):
            raise ValueError("unknown ingress")
        authorized = authorized and 0 < grant_seconds <= self.c["lease_seconds"]
        now = self.clock()
        current = self.active.get(port)
        if current and (not authorized or (mac, session) != current[:2] or now >= current[2]):
            self.revoke(port)
        if not authorized:
            return
        if not re.fullmatch(r"(?:[0-9a-f]{2}:){5}[0-9a-f]{2}", mac or "") or not re.fullmatch(r"[A-Za-z0-9.-]{1,128}", session or ""):
            self.revoke(port)
            raise ValueError("invalid authenticated station/session")
        if (port, mac, session) in self.spent:
            return
        if port in self.active:
            if now + grant_seconds < self.active[port][2]:
                self.active[port] = (mac, session, now + grant_seconds)
                self.sync()
            return
        ttl = grant_seconds
        if port != "ap0":
            ns(self.c, "nas", "bridge", "fdb", "replace", mac, "dev", port, "master", "static")
        self.active[port] = (mac, session, now + ttl)
        self.sync()

    def sync(self):
        # Atomic rebuild also revokes already-expired elements without ENOENT.
        text = "flush set bridge admission admitted_in\nflush set bridge admission admitted_out\n"
        now = self.clock()
        for port, (mac, session, deadline) in self.active.items():
            milliseconds = int((deadline - now) * 1000)
            if milliseconds <= 0:
                continue
            for name in ("admitted_in", "admitted_out"):
                text += ("add element bridge admission " + name + " { " + port + " . " + mac
                         + " timeout " + str(milliseconds) + "ms }\n")
        ns(self.c, "nas", "nft", "-f", "-", input=text)


def station(output):
    blocks = re.split(r"(?m)^(?=[0-9a-f]{2}:)", output.strip())
    found = []
    for block in blocks:
        lines = block.splitlines()
        if not lines or not re.fullmatch(r"(?:[0-9a-f]{2}:){5}[0-9a-f]{2}", lines[0]):
            continue
        fields = dict(line.split("=", 1) for line in lines[1:] if "=" in line)
        if "[AUTHORIZED]" in fields.get("flags", ""):
            found.append((lines[0], fields.get("acct_session_id")))
    if len(found) > 1:
        raise ValueError("one station per isolated ingress required")
    return found[0] if found else (None, None)


def read_grants(c):
    if not c.get("session_grants"):
        return None
    path = Path(c["session_grants"])
    protected_dirs(path.parent)
    try:
        records = json.loads(read_private(path))
    except FileNotFoundError:
        return None
    if not isinstance(records, list) or len(records) > 3:
        raise ValueError("invalid grant document")
    by_port = {}
    for record in records:
        validate_grant(c, record)
        port = record["ingress"]
        if port in by_port:
            raise ValueError("duplicate grant ingress")
        by_port[port] = record
    return by_port


def validate_grant(c, record):
    expected = {"schemaVersion": "soha-admission-grant/v1", "producerOwner": c["owner"],
                "producerProject": c["project"], "nasId": c["nas_id"], "profile": "lab-tcp8000-only",
                "resource": "10.254.122.2", "protocol": "tcp", "port": 8000}
    patterns = {"mac": r"(?:[0-9a-f]{2}:){5}[0-9a-f]{2}", "session": r"[A-Za-z0-9.-]{1,128}",
                "coreSessionId": r"[A-Za-z0-9][A-Za-z0-9._:-]{0,127}"}
    times = ("produced_at", "expires_at", "hard_expires_at")
    if (not isinstance(record, dict) or set(record) != {*expected, *patterns, *times, "ingress"} or
            record["ingress"] not in ("ap0", "w1", "w2") or
            any(type(record[key]) is not type(value) or record[key] != value for key, value in expected.items()) or
            any(not isinstance(record[key], str) or not re.fullmatch(pattern, record[key]) for key, pattern in patterns.items()) or
            any(type(record[key]) not in (int, float) or not math.isfinite(record[key]) for key in times)):
        raise ValueError("invalid grant record")
    start, end, hard = (record[key] for key in times)
    if not 0 < start < end <= min(hard, start + c["lease_seconds"]):
        raise ValueError("invalid grant bounds")


def grant(c, grants, port, mac, session, clock=time.time):
    g = grants.get(port) if grants else None
    now = clock()
    if (not g or (mac, session) != (g["mac"], g["session"]) or
            not 0 <= now - g["produced_at"] <= c["lease_seconds"]):
        return 0
    seconds = int(min(g["expires_at"], g["hard_expires_at"]) - now)
    return max(0, min(seconds, c["lease_seconds"]))


def process_identity(pid):
    proc = Path("/proc", str(pid))
    try:
        tail = (proc / "stat").read_text().rsplit(") ", 1)[1].split()
        if tail[0] == "Z":
            return None
        namespace = (proc / "ns/net").stat().st_ino
        after = (proc / "stat").read_text().rsplit(") ", 1)[1].split()
        if tail[19] != after[19] or after[0] == "Z":
            return None
        return tail[19], namespace
    except FileNotFoundError:
        return None


def alive(p, j):
    return process_identity(p["pid"]) == (p["start"], j["namespaces"][p["namespace"]])


def supervise(c, j):
    leases = Leases(c)
    grant_seen = False
    def stop(*_):
        raise KeyboardInterrupt()
    previous = {s: signal.signal(s, stop) for s in (signal.SIGTERM, signal.SIGINT)}
    try:
        while True:
            owned(c, j)
            if not all(alive(p, j) for p in j["processes"]):
                raise RuntimeError("lab process died; closing admission")
            grants = read_grants(c)
            if grants is None and grant_seen:
                raise RuntimeError("published session grants disappeared")
            grant_seen = grant_seen or grants is not None
            for port in ("ap0", "w1", "w2"):
                output = ns(c, "nas", "hostapd_cli", "-p", str(Path(c["state"]) / ("ctrl-" + port)), "-i", port, "all_sta")
                mac, session = station(output)
                seconds = grant(c, grants, port, mac, session)
                leases.update(port, mac, session, mac is not None, seconds)
                if mac and (port, mac, session) in leases.spent:
                    ns(c, "nas", "hostapd_cli", "-p", str(Path(c["state"]) / ("ctrl-" + port)), "-i", port, "deauthenticate", mac)
            time.sleep(0.25)
    finally:
        try:
            owned(c, j)
            ns(c, "nas", "nft", "-f", "-", input=FLUSH)
        finally:
            for s, handler in previous.items():
                signal.signal(s, handler)


def ensure_namespace_pids(j, processes):
    for name in j["namespaces"]:
        expected = {p["pid"] for p in processes if p["namespace"] == name}
        actual = {int(pid) for pid in run("ip", "netns", "pids", name).split()}
        if actual - expected:
            raise RuntimeError("unrecorded process remains; retaining namespace")


def cleanup_pids(j, stack):
    result = []
    for p in j["processes"]:
        try:
            fd = os.pidfd_open(p["pid"])
        except ProcessLookupError:
            continue
        stack.callback(os.close, fd)
        identity = process_identity(p["pid"])
        if identity is None:
            continue
        if identity != (p["start"], j["namespaces"][p["namespace"]]):
            raise RuntimeError("process identity changed; retaining resources")
        result.append((p, fd))
    return result


def ensure_radio(c, radio):
    if radio["ifindex"] is None or radio_ifindex(c, radio["role"], radio["phy"], radio["interface"]) != radio["ifindex"]:
        raise RuntimeError("radio/interface identity unconfirmed; retaining resources")


def stop_processes(c, j, processes):
    if names(c)["nas"] in j["namespaces"]:
        tables = ns(c, "nas", "nft", "list", "tables")
        if "table bridge admission" in tables.splitlines():
            ns(c, "nas", "nft", "-f", "-", input=FLUSH)
    exits = select.poll()
    pending = {fd for _, fd in processes}
    for fd in pending:
        exits.register(fd, select.POLLIN)
        try:
            signal.pidfd_send_signal(fd, signal.SIGTERM)
        except ProcessLookupError:
            pass
    deadline = time.monotonic() + 5
    while pending:
        if time.monotonic() > deadline:
            raise RuntimeError("process termination unconfirmed; resources retained")
        for fd, events in exits.poll(50):
            if events & (select.POLLIN | select.POLLHUP):
                exits.unregister(fd)
                pending.remove(fd)
    j["processes"] = []
    save(c, j)


def cleanup(c):
    j = load(c)
    owned(c, j)
    with ExitStack() as stack:
        processes = cleanup_pids(j, stack)
        ensure_namespace_pids(j, [p for p, _ in processes])
        for radio in j["radios"]:
            ensure_radio(c, radio)
        stop_processes(c, j, processes)
    ensure_namespace_pids(j, [])
    for radio in list(reversed(j["radios"])):
        owned(c, j)
        ensure_radio(c, radio)
        role, phy = radio["role"], radio["phy"]
        ns(c, role, "iw", "dev", radio["interface"], "del")
        ns(c, role, "iw", "phy", phy, "set", "netns", str(os.getpid()))
        j["radios"].remove(radio)
        save(c, j)
    for name in list(reversed(j["namespaces"])):
        owned(c, j)
        if run("ip", "netns", "pids", name).strip():
            raise RuntimeError("unrecorded process remains; retaining namespace")
        run("ip", "netns", "del", name)
        del j["namespaces"][name]
        save(c, j)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "prepare", "apply", "status", "cleanup"))
    parser.add_argument("--config", required=True, type=Path, help="explicit JSON target, no defaults")
    args = parser.parse_args()
    c = read_config(args.config)
    if args.action == "plan":
        print(json.dumps(plan(c), indent=2))
        return
    if args.action == "prepare":
        prepare(c)
        return
    load(c)
    with os.fdopen(private_open(Path(c["state"]) / "lock", os.O_RDWR | os.O_CREAT), "r+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.action == "status":
            j = load(c)
            owned(c, j)
            print(json.dumps({"resources": j, "processesAlive": [alive(p, j) for p in j["processes"]],
                              "dataPlaneVerified": False, "coa": "unsupported"}, indent=2))
        elif args.action == "apply":
            apply(c)
        else:
            cleanup(c)


if __name__ == "__main__":
    main()
