"""Resolve same-Root SDK source, or explicitly verify a declared external SDK."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import runpy
import stat
import subprocess
import sys
import tempfile

MODULE = "github.com/opensoha/soha/contracts"


def resolve_source_contracts(root=None):
    root = Path(root or Path(__file__).resolve().parents[2]).resolve(strict=True)
    expected = {".": "github.com/opensoha/soha", "contracts": MODULE,
                "cli": "github.com/opensoha/soha/cli", "agent": "github.com/opensoha/soha/agent"}
    workspace = root / "go.work"
    workspace_sum = root / "go.work.sum"
    if (workspace.is_symlink() or not workspace.is_file()
            or workspace_sum.is_symlink() or not workspace_sum.is_file()):
        raise ValueError("same-Root source mode requires the committed four-module go.work")
    workspace_bytes = workspace.read_bytes()
    edit_env = {**os.environ, "GOWORK": "off", "GOTOOLCHAIN": "local",
                "GOPROXY": "off", "GOSUMDB": "off", "GOFLAGS": ""}

    def edit(kind, path, contents):
        try:
            # Parse the same bytes used for literal checks and the workspace digest.
            with tempfile.TemporaryDirectory(prefix="soha-manifest-") as temporary:
                snapshot = Path(temporary) / path.name
                snapshot.write_bytes(contents)
                return json.loads(subprocess.check_output(
                    ["go", kind, "edit", "-json", str(snapshot)], cwd=path.parent, env=edit_env,
                ))
        except subprocess.CalledProcessError as error:
            raise ValueError(f"cannot parse source manifest: {path.relative_to(root)}") from error

    literal_rows = runpy.run_path(str(Path(__file__).resolve().parents[2] / "scripts/source-workspace.py"))[
        "literal_directive_rows"
    ]
    config = edit("work", workspace, workspace_bytes)
    uses = []
    for entry in config.get("Use") or []:
        path = entry["DiskPath"]
        if path not in {".", "./contracts", "./cli", "./agent"}:
            raise ValueError("source workspace use must name an exact Root module directory")
        directory = (root / path).resolve(strict=True)
        uses.append(str(directory.relative_to(root)) if directory != root else ".")
    if len(uses) != 4 or set(uses) != set(expected):
        raise ValueError("source mode must use exactly Root, contracts, cli and agent")
    if list(literal_rows(workspace_bytes.decode(), "use")) != [[entry["DiskPath"]] for entry in config["Use"]]:
        raise ValueError("source workspace use must name an exact Root module directory")
    versions = set()
    literal_versions = set()
    for directory, module in expected.items():
        module_directory = root / directory
        manifest = root / directory / "go.mod"
        sums = manifest.with_name("go.sum")
        if (module_directory.is_symlink() or not module_directory.is_dir() or module_directory.resolve(strict=True) != module_directory
                or manifest.is_symlink() or not manifest.is_file() or sums.is_symlink() or not sums.is_file()):
            raise ValueError("source module identity is missing or changed: " + directory)
        contents = manifest.read_bytes()
        config_mod = edit("mod", manifest, contents)
        spelling = contents.decode()
        if ((config_mod.get("Module") or {}).get("Path") != module or config_mod.get("Replace")
                or re.search(r"(?m)^\s*replace\b", spelling)):
            raise ValueError("source modules must keep their identities without go.mod replacements")
        if directory != "contracts":
            requirements = [entry["Version"] for entry in config_mod.get("Require") or [] if entry["Path"] == MODULE]
            if len(requirements) != 1:
                raise ValueError("each source consumer must declare exactly one SDK version")
            literal = [row[1] for row in literal_rows(spelling, "require") if len(row) == 2 and row[0] == MODULE]
            if len(literal) != 1 or not re.fullmatch(
                    r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", literal[0]):
                raise ValueError("each source consumer must declare exactly one SDK version")
            versions.add(requirements[0])
            literal_versions.add(literal[0])
    aliases = config.get("Replace") or []
    expected_aliases = {(MODULE, version, "./contracts", "") for version in versions}
    actual_aliases = {(entry["Old"]["Path"], entry["Old"].get("Version", ""),
                       entry["New"]["Path"], entry["New"].get("Version", "")) for entry in aliases}
    literal_aliases = list(literal_rows(workspace_bytes.decode(), "replace"))
    expected_literals = {(MODULE, version, "=>", "./contracts") for version in literal_versions}
    if (actual_aliases != expected_aliases or len(literal_aliases) != len(expected_literals)
            or {tuple(row) for row in literal_aliases} != expected_literals):
        raise ValueError("workspace aliases must match only the declared SDK versions to ./contracts")
    schema = root / "contracts/agent/dsh-runtime.schema.json"
    if schema.parent.is_symlink() or schema.is_symlink() or not schema.is_file() or not isinstance(json.loads(schema.read_text()), dict):
        raise ValueError("same-Root contracts is missing the dsh runtime schema")

    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args])

    if Path(git("rev-parse", "--show-toplevel").decode().strip()).resolve() != root:
        raise ValueError("source provenance must identify the actual Root repository")
    revision = git("rev-parse", "HEAD").decode().strip()
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("source provenance requires a full Root commit")
    names = git("ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "contracts").decode().split("\0")
    files = []
    for name in sorted(set(names) - {""}):
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("SDK source inputs must be regular files")
        path.resolve(strict=True).relative_to(root / "contracts")
        files.append({"path": name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                      "mode": "100755" if path.stat().st_mode & stat.S_IXUSR else "100644"})
    if "contracts/go.mod" not in {item["path"] for item in files}:
        raise ValueError("SDK source manifest is incomplete")
    return {"mode": "source", "ref": revision, "dir": str(root / "contracts"), "repository": str(root),
            "goWorkSHA256": hashlib.sha256(workspace_bytes).hexdigest(),
            "sourceSHA256": hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()}


def resolve_contracts(requested="", module_directory=None):
    env = {**os.environ, "GOWORK": "off", "GOFLAGS": "-mod=readonly"}
    module_directory = Path(module_directory or Path(__file__).resolve().parents[1])

    def query(*args):
        return json.loads(subprocess.check_output(["go", *args], cwd=module_directory, env=env, text=True))

    declared = query("list", "-m", "-json", MODULE)
    version = declared.get("Version")
    if declared.get("Path") != MODULE or declared.get("Replace") or not version or not declared.get("Sum"):
        raise ValueError("contracts must have a declared checksummed version without replacement")
    if requested and requested != version:
        raise ValueError("contracts input differs from the reviewed go.mod dependency")
    downloaded = query("mod", "download", "-json", MODULE)
    checksum = downloaded.get("Sum")
    if downloaded.get("Error") or downloaded.get("Version") != version or not checksum or not downloaded.get("GoModSum") or declared["Sum"] != checksum:
        raise ValueError("contracts download version/checksum mismatch")
    subprocess.run(["go", "mod", "verify"], cwd=module_directory, env=env, check=True, stdout=sys.stderr)
    resolved = query("list", "-m", "-json", MODULE)
    directory = resolved.get("Dir")
    if resolved.get("Replace") or resolved.get("Version") != version or not directory or directory != downloaded.get("Dir") or not Path(directory).is_absolute() or not Path(directory).is_dir():
        raise ValueError("contracts source directory does not match the verified download")
    result = {"ref": version, "dir": directory, "sum": checksum}
    if any("\n" in value or "\r" in value for value in result.values()):
        raise ValueError("contracts identity cannot contain newlines")
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("source", "declared"), default="source")
    parser.add_argument("--consumer", choices=("cli", "agent"), default="agent")
    args = parser.parse_args(argv)
    if args.mode == "source" and args.consumer != "agent":
        parser.error("--consumer is only supported for declared SDK verification")
    requested = os.environ.get("REQUESTED_CONTRACTS_REF", "")
    if args.mode == "source" and requested:
        raise ValueError("a requested external SDK ref requires --mode declared")
    result = (resolve_source_contracts() if args.mode == "source" else
              {"mode": "declared", **resolve_contracts(requested, Path(__file__).resolve().parents[2] / args.consumer)})
    if any("\n" in value or "\r" in value for value in result.values()):
        raise ValueError("contracts identity cannot contain newlines")
    if os.environ.get("GITHUB_OUTPUT"):
        with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
            output.write("".join(f"{key}={value}\n" for key, value in result.items()))
    print(json.dumps(result))


if __name__ == "__main__":
    main()
