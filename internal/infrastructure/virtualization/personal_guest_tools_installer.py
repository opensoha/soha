"""Runs with only the existing fixed installer's protected-file helpers."""
STAGE = "validation"


def install_tools(plan):
    global STAGE
    binding = plan["binding"]
    data_ref = binding["dataRef"]
    if os.geteuid() != 0 or plan["schemaVersion"] != "opensoha.dev/personal-guest-tools/v1" or pwd.getpwnam("soha").pw_uid != 1000:
        raise RuntimeError("invalid tools installer identity")
    for value in (data_ref, binding["id"], plan["operationId"]):
        if str(uuid.UUID(value)) != value:
            raise RuntimeError("invalid operation identity")
    digest = plan["artifactSHA256"]
    if not re.fullmatch("[a-f0-9]{64}", digest) or not re.fullmatch(r"docker\.io/library/debian@sha256:[a-f0-9]{64}", plan["dockerImage"]):
        raise RuntimeError("invalid pinned tools artifact")
    root = pathlib.Path("/var/lib/soha/personal-guest-data")
    protected_dir(root)
    receipt = json.loads(private_read(root / (data_ref + ".json"), 0, 8192))
    stable = {k: v for k, v in binding.items() if k not in ("id", "version", "issuedAt", "expiresAt")}
    if receipt["stage"] != "complete" or receipt["filesystemId"] != data_ref or receipt["plan"]["diskQuotaBytes"] != plan["diskQuotaBytes"] or json.loads(private_read(root / (data_ref + ".owner.json"), 0, 8192)) != stable:
        raise RuntimeError("tools require completed owned data")
    cfg = json.loads(private_read(root / (data_ref + ".guest.json"), 0, 16384))
    user_dir = pathlib.Path("/var/lib/soha/personal-guests") / data_ref
    current = json.loads(private_read(user_dir / ("state-" + cfg["bindingId"] + ".json"), 1000, 16384))
    certificate = current.get("activeCertificatePEM")
    if current.get("renewalResult") or not certificate or cfg["bindingId"] != binding["id"]:
        raise RuntimeError("tools require a committed current guest pin")
    cert_path = root / (plan["operationId"] + ".tools.pem")
    atomic_write(cert_path, certificate.encode())
    context = ssl.create_default_context(cafile=cfg["caFile"])
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(str(cert_path), cfg["keyFile"])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context), NoRedirect())
    base_url = cfg["endpoint"] + "/api/personal-guest/v1/bindings/" + cfg["bindingId"]
    def authorize():
        with opener.open(base_url + "/tools-operations/" + plan["operationId"], timeout=15) as response:
            if response.status != 200 or response.headers.get("Cache-Control") != "no-store" or json.loads(response.read(8193)) != plan:
                raise RuntimeError("current fixed tools authority denied")
    authorize()
    lock_file = root / (data_ref + ".bootstrap.lock")
    private_read(lock_file, 0, 64)
    lock_fd = os.open(lock_file, os.O_RDONLY | os.O_NOFOLLOW)
    fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    mount = pathlib.Path("/srv/soha-personal-data") / data_ref / "mount"
    mounted = json.loads(run(["/usr/bin/findmnt", "--json", "--output", "UUID,FSTYPE,OPTIONS", "--target", str(mount)]))["filesystems"]
    if len(mounted) != 1 or mounted[0]["uuid"] != data_ref or mounted[0]["fstype"] != "xfs" or not {"usrquota", "prjquota", "nodev", "nosuid", "noexec"}.issubset(mounted[0]["options"].split(",")):
        raise RuntimeError("owned data hard policy changed")
    STAGE = "artifact"
    with opener.open(base_url + "/artifact", timeout=30) as response:
        artifact = response.read((64 << 20) + 1)
        if response.status != 200 or response.headers.get("Cache-Control") != "no-store" or len(artifact) > 64 << 20 or hashlib.sha256(artifact).hexdigest() != digest:
            raise RuntimeError("guest artifact mismatch")
    binary_dir = pathlib.Path("/usr/local/libexec/soha-personal-guest") / digest
    protected_dir(binary_dir, 0o755)
    binary = binary_dir / "personal-guest"
    if binary.exists():
        if hashlib.sha256(binary.read_bytes()).hexdigest() != digest:
            raise RuntimeError("existing tools artifact mismatch")
    else:
        atomic_write(binary, artifact, mode=0o755)
    STAGE = "packages"
    packages = ["bubblewrap=" + plan["bubblewrapVersion"], "docker.io=" + plan["dockerVersion"], "chromium=" + plan["chromiumVersion"]]
    if any(re.fullmatch(r"[a-z.]+=[0-9][a-zA-Z0-9.+:~_-]{0,127}", p) is None for p in packages):
        raise RuntimeError("invalid frozen package versions")
    authorize()
    run(["/usr/bin/apt-get", "update"], timeout=180)
    run(["/usr/bin/apt-get", "--no-remove", "--no-install-recommends", "-y", "install"] + packages, timeout=420)
    authorize()
    STAGE = "docker"
    run(["/usr/bin/systemctl", "enable", "--now", "docker.service"], timeout=60)
    # Infrastructure image cache stays on the system disk; personal writes stay on XFS.
    run(["/usr/bin/docker", "pull", plan["dockerImage"]], timeout=180)
    authorize()
    STAGE = "service"
    tools_dir = pathlib.Path("/etc/soha/personal-guest-tools")
    protected_dir(tools_dir, 0o755)
    tools_path = tools_dir / (data_ref + ".json")
    atomic_write(tools_path, json.dumps(plan).encode(), mode=0o644)
    for name in ("browser",):
        path = mount / name
        if not path.exists():
            os.mkdir(path, 0o700)
            os.chown(path, 1000, 1000)
        st = os.lstat(path)
        if not stat.S_ISDIR(st.st_mode) or st.st_uid != 1000 or stat.S_IMODE(st.st_mode) != 0o700:
            raise RuntimeError("unmanaged browser data path")
        # Browser profile shares the same UID hard quota; commands cannot mount it.
    guest_name = "soha-personal-guest-" + data_ref + ".service"
    unit_path = pathlib.Path("/etc/systemd/system") / guest_name
    unit = unit_path.read_text()
    if "# Soha data " + data_ref not in unit or "PrivateUsers=yes" not in unit or "NoNewPrivileges=yes" not in unit:
        raise RuntimeError("unmanaged guest unit")
    slice_name = "soha-personal-" + data_ref.replace("-", "") + ".slice"
    slice_path = pathlib.Path("/etc/systemd/system") / slice_name
    if slice_path.exists() and "# Soha data " + data_ref not in slice_path.read_text():
        raise RuntimeError("unmanaged personal slice")
    slice_unit = f"""# Soha data {data_ref}
[Unit]
Description=Soha personal VM aggregate execution budget
[Slice]
CPUQuota={plan['cpuQuotaMilli']/10}%
MemoryMax={plan['memoryBytes']}
MemorySwapMax=0
TasksMax={plan['pidsLimit']}
"""
    atomic_write(slice_path, slice_unit.encode())
    unit = re.sub(r"^ExecStart=.*$", "ExecStart=" + str(binary) + " --config=" + str(user_dir / ("guest-" + binding["id"] + ".json")) + " --tools=" + str(tools_path), unit, flags=re.M)
    # Only this managed guest unit changes; user tasks never receive Docker's socket.
    unit = re.sub(r"^(Slice|SupplementaryGroups)=.*\n", "", unit, flags=re.M)
    unit = unit.replace("[Service]\n", "[Service]\nSlice=" + slice_name + "\nSupplementaryGroups=docker\n", 1)
    unit = re.sub(r"^ReadWritePaths=.*$", "ReadWritePaths=" + str(user_dir) + " " + str(mount / "workspace") + " " + str(mount / "browser"), unit, flags=re.M)
    atomic_write(unit_path, unit.encode())
    run(["/usr/bin/systemctl", "daemon-reload"])
    run(["/usr/bin/systemctl", "restart", guest_name])
    if run(["/usr/bin/systemctl", "is-active", guest_name]).decode().strip() != "active":
        raise RuntimeError("owned guest tools service is not active")
    return {"operationId": plan["operationId"], "dataRef": data_ref, "artifactSHA256": digest}


if __name__ == "__main__":
    try:
        raw = sys.stdin.buffer.read(8193)
        if len(raw) > 8192:
            raise RuntimeError("oversized tools plan")
        print(json.dumps(install_tools(json.loads(raw))))
    except Exception:
        print("managed personal guest tools failed stage=" + STAGE + "; retain the original operation", file=sys.stderr)
        sys.exit(1)
