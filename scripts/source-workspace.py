#!/usr/bin/env python3
"""Check same-checkout SDK inputs and stage the built Web artifact."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SDK = ROOT / "contracts"
SDK_MODULE = "github.com/opensoha/soha-contracts"


def run(args, cwd=ROOT, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def check_go():
    work = ROOT / "go.work"
    if not work.is_file() or work.is_symlink() or not (SDK / "go.mod").is_file():
        raise ValueError("same-checkout go.work and contracts/go.mod are required")
    active = run(["go", "env", "GOWORK"])
    if not active or active == "off" or Path(active).resolve() != work:
        raise ValueError("internal Go checks require this checkout's go.work")
    config = json.loads(run(["go", "work", "edit", "-json"]))
    uses = {(ROOT / entry["DiskPath"]).resolve() for entry in config["Use"]}
    if uses != {ROOT, SDK} or config.get("Replace"):
        raise ValueError("go.work must use only Core and contracts without replacements")
    for name, directory in [("github.com/opensoha/soha", ROOT), (SDK_MODULE, SDK)]:
        module = json.loads(run(["go", "list", "-m", "-json", name]))
        if not module.get("Main") or module.get("Replace") or Path(module["Dir"]).resolve() != directory:
            raise ValueError(f"{name} must resolve to same-checkout source")


def check_npm():
    if SDK.resolve() != SDK or (ROOT / "web").resolve() != ROOT / "web":
        raise ValueError("workspace source directories must not redirect outside this checkout")
    package = json.loads((ROOT / "package.json").read_text())
    lock = json.loads((ROOT / "package-lock.json").read_text())
    sdk = json.loads((SDK / "package.json").read_text())
    web = json.loads((ROOT / "web/package.json").read_text())
    if not package.get("private") or set(package.get("workspaces", [])) != {"contracts", "web"}:
        raise ValueError("the private root workspace must contain contracts and web")
    if any((ROOT / part / "package-lock.json").exists() for part in ["contracts", "web"]):
        raise ValueError("only the root npm lockfile is an installation input")
    if sdk["name"] != "@opensoha/contracts" or web["dependencies"].get(sdk["name"]) != "file:../contracts":
        raise ValueError("Web must declare the same-checkout SDK")
    installed = ROOT / "node_modules/@opensoha/contracts"
    link = lock.get("packages", {}).get("node_modules/@opensoha/contracts", {})
    if installed.resolve() != SDK or not link.get("link") or link.get("resolved") != "contracts":
        raise ValueError("the root SDK installation must link to contracts/")
    locked_sdk = lock["packages"].get("contracts", {})
    if locked_sdk.get("version") != sdk["version"]:
        raise ValueError("root lockfile SDK identity differs from source")
    entry = run(["node", "-e", "console.log(require.resolve('@opensoha/contracts/gen/ts/sohaapi', {paths: [process.argv[1]]}))", str(ROOT / "web")])
    if not Path(entry).resolve().is_relative_to(SDK):
        raise ValueError("Web resolves an SDK outside same-checkout contracts/")


def tidy():
    check_go()
    manifests = [ROOT / name for name in ["go.mod", "go.sum", "go.work"]]
    manifests += [SDK / name for name in ["go.mod", "go.sum"]]
    before = {p: p.read_bytes() for p in manifests}
    env = {**os.environ, "GOWORK": "off"}
    try:
        # tidy is module-scoped; a temporary replacement prevents a release prerequisite.
        with tempfile.TemporaryDirectory(prefix="soha-source-tidy-", dir=os.environ.get("OPENSOHA_VERIFY_DIR")) as directory:
            module = Path(directory) / "go.mod"
            module.write_bytes(before[ROOT / "go.mod"])
            module.with_suffix(".sum").write_bytes(before[ROOT / "go.sum"])
            run(["go", "mod", "edit", f"-modfile={module}", f"-replace={SDK_MODULE}={SDK}"], env=env)
            run(["go", "mod", "tidy", f"-modfile={module}"], env=env)
            run(["go", "mod", "edit", f"-modfile={module}", f"-dropreplace={SDK_MODULE}"], env=env)
            if module.read_bytes() != before[ROOT / "go.mod"]:
                raise ValueError("Core go.mod is not tidy against the source SDK")
            def external_sums(data):
                return [line for line in data.splitlines() if not line.startswith(SDK_MODULE.encode() + b" ")]
            if external_sums(module.with_suffix(".sum").read_bytes()) != external_sums(before[ROOT / "go.sum"]):
                raise ValueError("Core go.sum is not tidy against the source SDK")
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
