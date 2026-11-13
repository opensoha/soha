"""Resolve the declared SDK and verify its extracted source before CI consumes it."""

import json
import os
from pathlib import Path
import subprocess
import sys

MODULE = "github.com/opensoha/soha-contracts"


def resolve_contracts(requested=""):
    env = {**os.environ, "GOWORK": "off", "GOFLAGS": "-mod=readonly"}

    def query(*args):
        return json.loads(subprocess.check_output(["go", *args], env=env, text=True))

    declared = query("list", "-m", "-json", MODULE)
    version = declared.get("Version")
    if declared.get("Path") != MODULE or declared.get("Replace") or not version:
        raise ValueError("contracts must have a declared version without replacement")
    if requested and requested != version:
        raise ValueError("contracts input differs from the reviewed go.mod dependency")
    downloaded = query("mod", "download", "-json", MODULE)
    checksum = downloaded.get("Sum")
    if downloaded.get("Error") or downloaded.get("Version") != version or not checksum or (declared.get("Sum") and declared["Sum"] != checksum):
        raise ValueError("contracts download version/checksum mismatch")
    subprocess.run(["go", "mod", "verify"], env=env, check=True, stdout=sys.stderr)
    resolved = query("list", "-m", "-json", MODULE)
    directory = resolved.get("Dir")
    if resolved.get("Replace") or resolved.get("Version") != version or not directory or directory != downloaded.get("Dir") or not Path(directory).is_absolute() or not Path(directory).is_dir():
        raise ValueError("contracts source directory does not match the verified download")
    result = {"ref": version, "dir": directory, "sum": checksum}
    if any("\n" in value or "\r" in value for value in result.values()):
        raise ValueError("contracts identity cannot contain newlines")
    return result


def main():
    result = resolve_contracts(os.environ.get("REQUESTED_CONTRACTS_REF", ""))
    with Path(os.environ["GITHUB_OUTPUT"]).open("a") as output:
        output.write("".join(f"{key}={value}\n" for key, value in result.items()))
    print(json.dumps(result))


if __name__ == "__main__":
    main()
