#!/usr/bin/env python3
"""Check same-checkout SDK inputs and stage the built Web artifact."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SDK = ROOT / "contracts"
SDK_MODULE = "github.com/opensoha/soha-contracts"


def run(args, cwd=ROOT, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def go_modules():
    return [
        ("github.com/opensoha/soha", ROOT),
        (SDK_MODULE, SDK),
        ("github.com/opensoha/soha-cli", ROOT / "cli"),
        ("github.com/opensoha/soha-agent", ROOT / "agent"),
    ]


def validate_sdk_aliases(config: dict, root: Path) -> set[str]:
    """Bind historical SDK graph metadata to this checkout, without changing release pins."""
    sdk = root / "contracts"
    if sdk.resolve() != sdk or not sdk.is_dir():
        raise ValueError("SDK aliases require confined same-checkout contracts")
    versions = set()
    for part in (".", "cli", "agent"):
        directory = root / part
        manifest = directory / "go.mod"
        if directory.resolve() != directory or manifest.is_symlink() or not manifest.is_file():
            raise ValueError("SDK aliases require confined same-checkout manifests")
        required = []
        in_require = False
        for original in manifest.read_text().splitlines():
            line = original.split("//", 1)[0].strip()
            if line == "require (":
                in_require = True
                continue
            if line == ")":
                in_require = False
                continue
            fields = line.split()
            if fields[:1] == ["require"]:
                fields = fields[1:]
            elif not in_require:
                continue
            if fields[:1] == [SDK_MODULE] and len(fields) == 2:
                required.append(fields[1])
        if len(required) != 1:
            raise ValueError("each consumer must require exactly one SDK version")
        versions.add(required[0])
    found = []
    for entry in config.get("Replace") or []:
        if not isinstance(entry, dict):
            raise ValueError("go.work must contain exactly the version-scoped SDK aliases to ./contracts")
        old, new = entry.get("Old", {}), entry.get("New", {})
        if (not isinstance(old, dict) or not isinstance(new, dict)
                or old.get("Path") != SDK_MODULE or old.get("Version") not in versions
                or new.get("Path") != "./contracts" or new.get("Version")):
            raise ValueError("go.work must contain exactly the version-scoped SDK aliases to ./contracts")
        found.append(old["Version"])
    if len(found) != len(versions) or set(found) != versions:
        raise ValueError("go.work must contain exactly the version-scoped SDK aliases to ./contracts")
    return versions


def check_go():
    work = ROOT / "go.work"
    modules = go_modules()
    if not work.is_file() or work.is_symlink():
        raise ValueError("same-checkout go.work is required")
    for name, directory in modules:
        manifest = directory / "go.mod"
        if (directory.resolve() != directory or not manifest.is_file()
                or manifest.is_symlink()):
            raise ValueError(f"{name} requires a confined same-checkout go.mod")
        if re.search(r"(?m)^\s*replace\b", manifest.read_text()):
            raise ValueError("source go.mod files must not contain replacements")
    active = run(["go", "env", "GOWORK"])
    if not active or active == "off" or Path(active).resolve() != work:
        raise ValueError("internal Go checks require this checkout's go.work")
    config = json.loads(run(["go", "work", "edit", "-json"]))
    uses = {(ROOT / entry["DiskPath"]).resolve() for entry in config["Use"]}
    if len(config["Use"]) != 4 or uses != {directory for _, directory in modules}:
        raise ValueError("go.work must use exactly Core, contracts, CLI and Agent")
    validate_sdk_aliases(config, ROOT)
    for name, directory in modules:
        module = json.loads(run(["go", "list", "-m", "-json", name]))
        if (module.get("Path") != name or not module.get("Main") or module.get("Replace")
                or Path(module["Dir"]).resolve() != directory):
            raise ValueError(f"{name} must resolve to same-checkout source")


def check_npm():
    workspaces = {
        "contracts": "@opensoha/contracts",
        "web": "soha-web",
        "connectors": "@opensoha/connectors",
        "cli/npm/cli": "@opensoha/cli",
    }
    package = json.loads((ROOT / "package.json").read_text())
    lock = json.loads((ROOT / "package-lock.json").read_text())
    if not package.get("private") or set(package.get("workspaces", [])) != set(workspaces):
        raise ValueError("the private root npm workspace must contain contracts, web, connectors and the CLI launcher")
    if set(lock.get("packages", {}).get("", {}).get("workspaces", [])) != set(workspaces):
        raise ValueError("the root npm lockfile workspace set differs from source")
    for part, name in workspaces.items():
        directory = ROOT / part
        manifest = directory / "package.json"
        if directory.resolve() != directory or manifest.is_symlink():
            raise ValueError("workspace source directories must not redirect outside this checkout")
        if (directory / "package-lock.json").exists() or (directory / "package-lock.json").is_symlink():
            raise ValueError("only the root npm lockfile is an installation input for root workspaces")
        owned = json.loads(manifest.read_text())
        installed = ROOT / "node_modules" / name
        linked = lock.get("packages", {}).get("node_modules/" + name, {})
        if owned.get("name") != name or installed.resolve() != directory or not linked.get("link") or linked.get("resolved") != part:
            raise ValueError(f"{name} must install from same-checkout {part}/")
        if lock.get("packages", {}).get(part, {}).get("version") != owned.get("version"):
            raise ValueError(f"root lockfile {name} identity differs from source")
    if lock.get("packages", {}).get("agent/runtimes/dsh"):
        raise ValueError("DSH is a separate Node24 runtime, not a root npm workspace")
    sdk = json.loads((SDK / "package.json").read_text())
    web = json.loads((ROOT / "web/package.json").read_text())
    if web["dependencies"].get(sdk["name"]) != "file:../contracts":
        raise ValueError("Web must declare the same-checkout SDK")
    entry = run(["node", "-e", "console.log(require.resolve('@opensoha/contracts/gen/ts/sohaapi', {paths: [process.argv[1]]}))", str(ROOT / "web")])
    if not Path(entry).resolve().is_relative_to(SDK):
        raise ValueError("Web resolves an SDK outside same-checkout contracts/")


def tidy():
    check_go()
    modules = go_modules()
    manifests = [ROOT / name for name in ["go.work", "go.work.sum"] if (ROOT / name).exists()]
    manifests += [directory / name for _, directory in modules for name in ["go.mod", "go.sum"]]
    if any(path.is_symlink() for path in manifests):
        raise ValueError("module manifests must not be symlinks")
    before = {p: p.read_bytes() for p in manifests}
    env = {**os.environ, "GOWORK": "off"}
    def external_sums(data):
        return [line for line in data.splitlines() if not line.startswith(SDK_MODULE.encode() + b" ")]
    try:
        for module_name, directory in modules:
            if directory == SDK:
                continue
            # tidy is module-scoped; a temporary replacement prevents a release prerequisite.
            with tempfile.TemporaryDirectory(prefix="soha-source-tidy-", dir=os.environ.get("OPENSOHA_VERIFY_DIR")) as temporary:
                module = Path(temporary) / "go.mod"
                module.write_bytes(before[directory / "go.mod"])
                module.with_suffix(".sum").write_bytes(before[directory / "go.sum"])
                run(["go", "mod", "edit", f"-modfile={module}", f"-replace={SDK_MODULE}={SDK}"], cwd=directory, env=env)
                run(["go", "mod", "tidy", f"-modfile={module}"], cwd=directory, env=env)
                run(["go", "mod", "edit", f"-modfile={module}", f"-dropreplace={SDK_MODULE}"], cwd=directory, env=env)
                if module.read_bytes() != before[directory / "go.mod"]:
                    raise ValueError(f"{module_name} go.mod is not tidy against the source SDK")
                if external_sums(module.with_suffix(".sum").read_bytes()) != external_sums(before[directory / "go.sum"]):
                    raise ValueError(f"{module_name} go.sum is not tidy against the source SDK")
        run(["go", "mod", "tidy", "-diff"], cwd=SDK, env=env)
    finally:
        if any(p.read_bytes() != original for p, original in before.items()):
            raise ValueError("tidy unexpectedly changed a checked-in manifest")


def stage_web():
    check_npm()
    source = ROOT / "web/dist"
    destination = ROOT / "internal/staticassets/web/dist"
    output = ROOT / ".tmp/embedded-web.json"
    def validate_receipt():
        if (output.parent.is_symlink() or output.is_symlink()
                or not output.parent.resolve().is_relative_to(ROOT)
                or (output.parent.exists() and not output.parent.is_dir())
                or (output.exists() and not output.is_file())):
            raise ValueError("embedded receipt must remain inside this checkout without symlinks")
    validate_receipt()
    if not (source / "index.html").is_file() or source.is_symlink():
        raise ValueError("build web/dist/index.html before staging")
    if destination.is_symlink() or not destination.parent.resolve().is_relative_to(ROOT):
        raise ValueError("embedded output must remain inside this checkout")
    files = []
    for path in sorted(source.rglob("*")):
        if path.is_symlink():
            raise ValueError("Web artifacts must not contain symlinks")
        if path.is_file():
            files.append({"path": path.relative_to(source).as_posix(), "mode": oct(path.stat().st_mode & 0o777), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    if destination.exists():
        shutil.rmtree(destination)
    shutil.copytree(source, destination)
    for item in files:
        path = destination / item["path"]
        if hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"] or oct(path.stat().st_mode & 0o777) != item["mode"]:
            raise ValueError("embedded artifact bytes or modes differ from Web build")
    receipt = {"head": run(["git", "rev-parse", "HEAD"]), "sdkSource": "contracts/", "webSource": "web/", "files": files,
               "manifestSHA256": hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()}
    validate_receipt()
    output.parent.mkdir(exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile("w", prefix=".embedded-web-", dir=output.parent, delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(receipt, stream, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, output)
    finally:
        if temporary and temporary.exists():
            temporary.unlink()
    print(json.dumps({"embeddedFiles": len(files), "manifestSHA256": receipt["manifestSHA256"]}))


if __name__ == "__main__":
    commands = {"check-go": check_go, "check-npm": check_npm, "tidy": tidy, "stage-web": stage_web}
    try:
        if len(sys.argv) != 2 or sys.argv[1] not in commands:
            raise ValueError("usage: source-workspace.py {check-go|check-npm|tidy|stage-web}")
        commands[sys.argv[1]]()
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        print(f"source-workspace: {error}", file=sys.stderr)
        sys.exit(1)
