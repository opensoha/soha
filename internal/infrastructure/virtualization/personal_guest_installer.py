"""Fixed QGA installer. It accepts no commands, provider credentials or user paths."""
import fcntl
import errno
import hashlib
import ipaddress
import json
import os
import pathlib
import pwd
import re
import ssl
import socket
import stat
import struct
import subprocess
import sys
import time
import urllib.request
import uuid


def protected_dir(path, mode=0o700):
    path = pathlib.Path(path)
    if path.parent != path:
        protected_dir(path.parent, 0o755)
    try:
        os.mkdir(path, mode)
    except FileExistsError:
        pass
    st = os.lstat(path)
    if not stat.S_ISDIR(st.st_mode) or st.st_uid != 0 or st.st_mode & 0o022:
        raise RuntimeError("untrusted provisioning directory")


def private_read(path, uid, maximum):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode) or st.st_uid != uid or stat.S_IMODE(st.st_mode) != 0o600 or st.st_size > maximum:
            raise RuntimeError("untrusted private provisioning file")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(maximum + 1)
        if len(raw) != st.st_size:
            raise RuntimeError("private provisioning file changed")
        return raw
    finally:
        os.close(fd)


def atomic_write(path, raw, uid=0, mode=0o600):
    if os.path.lexists(path):
        st = os.lstat(path)
        if not stat.S_ISREG(st.st_mode) or st.st_uid != uid or stat.S_IMODE(st.st_mode) != mode:
            raise RuntimeError("unmanaged provisioning target")
    temp = str(path) + ".next-" + str(uuid.uuid4())
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        os.fchown(stream.fileno(), uid, uid)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, path)
    fd = os.open(str(pathlib.Path(path).parent), os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def run(args, timeout=60):
    return subprocess.run(args, check=True, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                          timeout=timeout, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C", "DEBIAN_FRONTEND": "noninteractive"}).stdout


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise RuntimeError("guest endpoint redirect refused")


def verify_project_namespace(workspace):
    probe = workspace / (".quota-namespace-proof-" + str(uuid.uuid4()))
    os.mkdir(probe, 0o700)
    fd = os.open(probe, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        # Linux fsxattr ABI: 5 uint32 fields and 8 padding bytes; fixed Linux/amd64 guest.
        raw = bytearray(28)
        fcntl.ioctl(fd, 0x801C581F, raw, True)
        original = list(struct.unpack("=5I8s", raw))
        if original[3] != 49 or not original[0] & 0x200:
            raise RuntimeError("project quota inheritance is missing")
        for index, value in ((3, 50), (0, original[0] & ~0x200)):
            changed = original.copy()
            changed[index] = value
            try:
                fcntl.ioctl(fd, 0x401C5820, struct.pack("=5I8s", *changed))
            except OSError as error:
                if error.errno in (errno.EINVAL, errno.EPERM):
                    continue
            raise RuntimeError("guest can alter project quota authority")
        fcntl.ioctl(fd, 0x801C581F, raw, True)
        if list(struct.unpack("=5I8s", raw)) != original:
            raise RuntimeError("project quota identity changed")
    finally:
        os.close(fd)


def network_denied(address, port):
    try:
        with socket.create_connection((address, port), timeout=3):
            pass
    except TimeoutError:
        return True
    except OSError as error:
        return error.errno in (errno.EPERM, errno.EACCES)
    return False


def verify_runtime(unit, bundle, address, port, workspace=None):
    group = run(["/usr/bin/systemctl", "show", "--property=ControlGroup", "--value", unit]).decode().strip()
    if not group.startswith("/system.slice/soha-personal-guest-") or ".." in group:
        raise RuntimeError("unconfirmed guest resource scope")
    scope = pathlib.Path("/sys/fs/cgroup" + group)
    pid = int(run(["/usr/bin/systemctl", "show", "--property=MainPID", "--value", unit]))
    user_namespace = "/proc/" + str(pid) + "/ns/user"
    if pid < 1 or os.readlink(user_namespace) == os.readlink("/proc/self/ns/user"):
        raise RuntimeError("guest requires its own user namespace")
    namespace_fd = os.open(user_namespace, os.O_RDONLY)
    if "NoNewPrivs:\t1" not in pathlib.Path("/proc/" + str(pid) + "/status").read_text():
        raise RuntimeError("guest privilege escalation is not disabled")
    quota, period = map(int, (scope / "cpu.max").read_text().split())
    if quota * 1000 != period * bundle["cpuQuotaMilli"] or int((scope / "memory.max").read_text()) != bundle["memoryBytes"] or int((scope / "pids.max").read_text()) != bundle["pidsLimit"] or int((scope / "memory.swap.max").read_text()) != 0:
        raise RuntimeError("guest process hard quotas differ from policy")
    # systemd filters packets, so a live control target may time out rather than return EPERM.
    with socket.create_connection(("127.0.0.1", 22), timeout=3):
        pass
    child = os.fork()
    if child == 0:
        try:
            (scope / "cgroup.procs").write_text(str(os.getpid()))
            os.setgroups([])
            os.setns(namespace_fd, 0)
            os.setgid(1000)
            os.setuid(1000)
            if workspace is not None:
                verify_project_namespace(workspace)
            with socket.create_connection((address, port), timeout=10):
                pass
            os._exit(0 if network_denied("127.0.0.1", 22) else 1)
        except Exception:
            os._exit(1)
    if os.waitpid(child, 0)[1] != 0:
        raise RuntimeError("guest network hard policy is not enforced")
    os.close(namespace_fd)
    with socket.create_connection(("127.0.0.1", 22), timeout=3):
        pass


STAGE = "validation"


def install(bundle):
    global STAGE
    if os.geteuid() != 0 or pwd.getpwnam("soha").pw_uid != 1000 or pwd.getpwnam("soha").pw_gid != 1000:
        raise RuntimeError("expected dedicated non-root guest account")
    if os.readlink("/proc/self/ns/mnt") != os.readlink("/proc/1/ns/mnt"):
        raise RuntimeError("personal data must mount in the system service namespace")
    plan, binding = bundle["plan"], bundle["plan"]["binding"]
    data_ref, binding_id = binding["dataRef"], binding["id"]
    for value in (data_ref, binding_id, plan["operationId"]):
        if str(uuid.UUID(value)) != value:
            raise RuntimeError("invalid operation identity")
    digest = bundle["artifactSHA256"]
    if re.fullmatch("[a-f0-9]{64}", digest) is None or bundle["network"]:
        raise RuntimeError("invalid artifact or network policy")
    parsed = urllib.parse.urlparse(bundle["endpoint"])
    address = str(ipaddress.ip_address(parsed.hostname))
    if parsed.scheme != "https" or parsed.path or parsed.query or parsed.fragment or parsed.username:
        raise RuntimeError("explicit HTTPS IP endpoint required")
    controllers = pathlib.Path("/sys/fs/cgroup/cgroup.controllers").read_text().split()
    if not {"cpu", "memory", "pids"}.issubset(controllers):
        raise RuntimeError("hard process resource controllers unavailable")
    root = pathlib.Path("/var/lib/soha/personal-guest-data")
    protected_dir(root)
    lock_file = root / (data_ref + ".bootstrap.lock")
    if not lock_file.exists():
        fd = os.open(lock_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        os.close(fd)
    private_read(lock_file, 0, 64)
    lock_fd = os.open(lock_file, os.O_RDONLY | os.O_NOFOLLOW)
    fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    stable = {k: v for k, v in binding.items() if k not in ("id", "version", "issuedAt", "expiresAt")}
    owner_file = root / (data_ref + ".owner.json")
    if owner_file.exists():
        if json.loads(private_read(owner_file, 0, 8192)) != stable:
            raise RuntimeError("managed workspace identity changed")
    else:
        atomic_write(owner_file, json.dumps(stable).encode())
    # Only the new guest's fixed bootstrap key is copied inside its own VM.
    home = pathlib.Path("/home/soha/.soha-personal-guest")
    for parent in (pathlib.Path("/home/soha"), home):
        st = os.lstat(parent)
        if not stat.S_ISDIR(st.st_mode) or st.st_uid != 1000 or st.st_mode & 0o022:
            raise RuntimeError("untrusted guest bootstrap directory")
    key = private_read(home / "bootstrap.key", 1000, 8192)
    root_cert, root_key, root_ca = (root / (data_ref + suffix) for suffix in (".pem", ".key", ".ca.pem"))
    for path, raw in ((root_cert, bundle["certificatePEM"].encode()), (root_key, key), (root_ca, bundle["clientCAPEM"].encode())):
        atomic_write(path, raw)
    context = ssl.create_default_context(cafile=str(root_ca))
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(str(root_cert), str(root_key))
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context), NoRedirect())
    base_url = bundle["endpoint"] + "/api/personal-guest/v1/bindings/" + binding_id
    STAGE = "artifact"
    with opener.open(base_url + "/artifact", timeout=30) as response:
        artifact = response.read((64 << 20) + 1)
        if response.status != 200 or response.headers.get("Cache-Control") != "no-store":
            raise RuntimeError("untrusted artifact response")
    if len(artifact) > 64 << 20 or hashlib.sha256(artifact).hexdigest() != digest:
        raise RuntimeError("guest artifact digest mismatch")
    binary_dir = pathlib.Path("/usr/local/libexec/soha-personal-guest") / digest
    protected_dir(binary_dir, 0o755)
    binary = binary_dir / "personal-guest"
    if binary.exists():
        st = os.lstat(binary)
        if not stat.S_ISREG(st.st_mode) or st.st_uid != 0 or st.st_mode & 0o022 or hashlib.sha256(binary.read_bytes()).hexdigest() != digest:
            raise RuntimeError("unmanaged guest binary")
    else:
        atomic_write(binary, artifact, mode=0o755)
    STAGE = "packages"
    if not pathlib.Path("/usr/sbin/mkfs.xfs").exists() or not pathlib.Path("/usr/sbin/xfs_quota").exists():
        run(["/usr/bin/apt-get", "update"], timeout=180)
        run(["/usr/bin/apt-get", "--no-remove", "--no-install-recommends", "-y", "install", "xfsprogs"], timeout=180)
    STAGE = "service"
    user_base = pathlib.Path("/var/lib/soha/personal-guests")
    protected_dir(user_base, 0o755)
    user_dir = user_base / data_ref
    try:
        os.mkdir(user_dir, 0o700)
        os.chown(user_dir, 1000, 1000)
    except FileExistsError:
        st = os.lstat(user_dir)
        if not stat.S_ISDIR(st.st_mode) or st.st_uid != 1000 or st.st_gid != 1000 or stat.S_IMODE(st.st_mode) != 0o700:
            raise RuntimeError("unmanaged guest state directory")
    def config(directory):
        return {"endpoint": bundle["endpoint"], "bindingId": binding_id, "enrollmentId": bundle["enrollmentId"], "challenge": bundle["challenge"],
                "certificateFile": str(directory / (binding_id + ".pem")), "keyFile": str(directory / (binding_id + ".key")),
                "caFile": str(directory / (binding_id + ".ca.pem")), "stateFile": str(directory / ("state-" + binding_id + ".json"))}
    user_config = config(user_dir)
    for name, raw in (("certificateFile", bundle["certificatePEM"].encode()), ("keyFile", key), ("caFile", bundle["clientCAPEM"].encode())):
        atomic_write(user_config[name], raw, uid=1000)
    user_config_path = user_dir / ("guest-" + binding_id + ".json")
    atomic_write(user_config_path, json.dumps(user_config).encode(), uid=1000)
    root_config = config(root)
    root_config.update(certificateFile=str(root_cert), keyFile=str(root_key), caFile=str(root_ca))
    atomic_write(root / (data_ref + ".guest.json"), json.dumps(root_config).encode())
    plan_path = root / (plan["operationId"] + ".plan.json")
    if plan_path.exists():
        previous = json.loads(private_read(plan_path, 0, 8192))
        prior_identity = {k: v for k, v in previous["binding"].items() if k not in ("id", "version", "issuedAt", "expiresAt")}
        if previous["operationId"] != plan["operationId"] or previous["diskQuotaBytes"] != plan["diskQuotaBytes"] or prior_identity != stable:
            raise RuntimeError("original operation plan changed")
    atomic_write(plan_path, json.dumps(plan).encode())
    guest_name = "soha-personal-guest-" + data_ref + ".service"
    data_name = "soha-personal-data-" + data_ref + ".service"
    unit_path = pathlib.Path("/etc/systemd/system") / guest_name
    if unit_path.exists() and "# Soha data " + data_ref not in unit_path.read_text():
        raise RuntimeError("unmanaged guest unit")
    unit = f"""# Soha data {data_ref}
[Unit]
Description=Soha personal guest
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
User=soha
Group=soha
ExecStart={binary} --config={user_config_path}
Restart=on-failure
RestartSec=10
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateUsers=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths={user_dir}
CPUQuota={bundle['cpuQuotaMilli']/10}%
MemoryMax={bundle['memoryBytes']}
MemorySwapMax=0
TasksMax={bundle['pidsLimit']}
IPAddressDeny=any
IPAddressAllow={address}
[Install]
WantedBy=multi-user.target
"""
    atomic_write(unit_path, unit.encode())
    run(["/usr/bin/systemctl", "daemon-reload"])
    run(["/usr/bin/systemctl", "enable", guest_name])
    run(["/usr/bin/systemctl", "restart", guest_name])
    STAGE = "registration"
    for attempt in range(30):
        try:
            with opener.open(base_url, timeout=12) as response:
                if response.status == 200 and json.load(response) == binding:
                    break
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(2)
    else:
        raise RuntimeError("registered guest authority not confirmed")
    STAGE = "runtime_scope"
    verify_runtime(guest_name, bundle, address, parsed.port or 443)
    STAGE = "data_initialization"
    result = json.loads(run([str(binary), "--initialize-data=" + str(plan_path)], timeout=150))
    STAGE = "project_namespace"
    verify_runtime(guest_name, bundle, address, parsed.port or 443, pathlib.Path("/srv/soha-personal-data") / data_ref / "mount/workspace")
    STAGE = "long_service"
    data_unit_path = pathlib.Path("/etc/systemd/system") / data_name
    if data_unit_path.exists() and "# Soha data " + data_ref not in data_unit_path.read_text():
        raise RuntimeError("unmanaged data unit")
    # Liveness starts first to recover interrupted renewals; it grants no execution readiness.
    data_unit = f"""# Soha data {data_ref}
[Unit]
Description=Soha initialized personal data reconciliation
Requires={guest_name}
After=network-online.target {guest_name}
[Service]
Type=oneshot
ExecStart={binary} --reconcile-data={data_ref}
TimeoutStartSec=180
UMask=0077
NoNewPrivileges=yes
"""
    atomic_write(data_unit_path, data_unit.encode())
    unit = unit.replace("Wants=network-online.target", "Wants=network-online.target " + data_name)
    atomic_write(unit_path, unit.encode())
    run(["/usr/bin/systemctl", "daemon-reload"])
    return result


if __name__ == "__main__":
    try:
        raw = sys.stdin.buffer.read(65537)
        if len(raw) > 65536:
            raise RuntimeError("oversized provisioning input")
        print(json.dumps(install(json.loads(raw))))
    except Exception:
        # Never expose bootstrap input, TLS keys or child-process output through QGA.
        print("managed personal guest provisioning failed stage=" + STAGE + "; retain the original operation", file=sys.stderr)
        sys.exit(1)
