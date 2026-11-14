"""Resolve same-Root SDK source, or explicitly verify a declared external SDK."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys

MODULE = "github.com/opensoha/soha/contracts"


def directive_rows(text, directive):
    rows = []
    block = False
    for raw in text.splitlines():
        line = raw.split("//", 1)[0].strip()
        if block:
            if line == ")":
                block = False
            elif line:
                rows.append(shlex.split(line))
        elif re.match(r"^" + directive + r"\b", line):
            value = line[len(directive):].strip()
            if value == "(":
                block = True
            else:
                rows.append(shlex.split(value))
    if block:
        raise ValueError("unterminated " + directive + " directive")
    return rows


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
    workspace_text = workspace_bytes.decode()
    uses = []
    for row in directive_rows(workspace_text, "use"):
        if len(row) != 1 or row[0] not in {".", "./contracts", "./cli", "./agent"}:
            raise ValueError("source workspace use must name an exact Root module directory")
        directory = (root / row[0]).resolve(strict=True)
        uses.append(str(directory.relative_to(root)) if directory != root else ".")
    if len(uses) != 4 or set(uses) != set(expected):
        raise ValueError("source mode must use exactly Root, contracts, cli and agent")
    versions = set()
    for directory, module in expected.items():
        module_directory = root / directory
        manifest = root / directory / "go.mod"
        sums = manifest.with_name("go.sum")
        if (module_directory.is_symlink() or not module_directory.is_dir() or module_directory.resolve(strict=True) != module_directory
                or manifest.is_symlink() or not manifest.is_file() or sums.is_symlink() or not sums.is_file()):
            raise ValueError("source module identity is missing or changed: " + directory)
        manifest_text = manifest.read_text()
        if directive_rows(manifest_text, "module") != [[module]] or re.search(r"(?m)^\s*replace\b", manifest_text):
            raise ValueError("source modules must keep their identities without go.mod replacements")
        if directory != "contracts":
            requirements = [row for row in directive_rows(manifest_text, "require") if row and row[0] == MODULE]
            if (len(requirements) != 1 or len(requirements[0]) != 2
                    or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", requirements[0][1])):
                raise ValueError("each source consumer must declare exactly one SDK version")
            versions.add(requirements[0][1])
    aliases = directive_rows(workspace_text, "replace")
    required_aliases = {(MODULE, version, "=>", "./contracts") for version in versions}
    if len(aliases) != len(required_aliases) or {tuple(row) for row in aliases} != required_aliases:
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
