"""Fetch and validate a canonical Core source archive inside the build init container."""

import json
from pathlib import Path
import re
import shutil
import sys
import tarfile
import tempfile
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, build_opener

CHUNK = 64 * 1024
MAX_COMPRESSED = 30 * 1024 * 1024
MAX_UNCOMPRESSED = 150 * 1024 * 1024
MAX_ENTRIES = 2000
MAX_MANIFEST = 8 * 1024 * 1024

IGNORED_FILES = {"Dockerfile", "Dockerfile.dockerignore", ".dockerignore", "soha-host.json"}
IGNORED_COMPONENTS = {".git", ".npmrc", ".venv", ".deps", "node_modules"}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def download(url, archive):
    opener = build_opener(ProxyHandler({}), NoRedirect())
    with opener.open(url, timeout=20) as response:
        total = 0
        while chunk := response.read(CHUNK):
            total += len(chunk)
            if total > MAX_COMPRESSED:
                raise ValueError()
            archive.write(chunk)
    archive.seek(0)


def unpack(archive, root):
    total = 0
    seen = set()
    with tarfile.open(fileobj=archive, mode="r|gz") as bundle:
        for entry in bundle:
            parts = entry.name.split("/")
            if (
                len(seen) >= MAX_ENTRIES
                or entry.name in seen
                or len(entry.name.encode("utf-8")) > 1024
                or "\\" in entry.name
                or any(part in {"", ".", ".."} for part in parts)
                or entry.type not in {tarfile.REGTYPE, tarfile.AREGTYPE}
                or entry.sparse is not None
                or entry.size < 0
            ):
                raise ValueError()
            seen.add(entry.name)
            total += entry.size
            if total > MAX_UNCOMPRESSED:
                raise ValueError()
            if entry.name in IGNORED_FILES or IGNORED_COMPONENTS.intersection(parts):
                continue
            target = root.joinpath(*parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            with bundle.extractfile(entry) as source, target.open("xb") as output:
                shutil.copyfileobj(source, output, CHUNK)


def read_manifest(root, name):
    with (root / name).open("rb") as manifest:
        content = manifest.read(MAX_MANIFEST + 1)
    if len(content) > MAX_MANIFEST:
        raise ValueError()
    return content.decode("utf-8")


def validate_node(root):
    if not (root / "server.js").is_file():
        raise ValueError()
    package = json.loads(read_manifest(root, "package.json"))
    lock = json.loads(read_manifest(root, "package-lock.json"))
    if any(field in package for field in ("workspaces", "overrides")):
        raise ValueError()
    if lock.get("lockfileVersion") not in {2, 3} or "" not in lock["packages"]:
        raise ValueError()
    for name, entry in lock["packages"].items():
        if name:
            validate_node_lock_entry(name, entry)
        validate_node_requirements(entry)
    validate_node_requirements(package)


def validate_node_lock_entry(name, entry):
    if not name.startswith("node_modules/") or any(part in {"", ".", ".."} for part in name.split("/")):
        raise ValueError()
    origin = urlsplit(entry.get("resolved", ""))
    if (
        entry.get("link")
        or origin.scheme != "https"
        or origin.netloc != "registry.npmjs.org"
        or not origin.path.endswith(".tgz")
        or origin.query
        or origin.fragment
        or not re.fullmatch(r"sha512-[A-Za-z0-9+/]{86}==", entry.get("integrity", ""))
    ):
        raise ValueError()


def validate_node_requirements(entry):
    for field in ("dependencies", "devDependencies", "optionalDependencies", "peerDependencies"):
        for version in entry.get(field, {}).values():
            if not re.fullmatch(r"[0-9vVxX*~^=<>| ][A-Za-z0-9*^~<>=+|. -]*", version):
                raise ValueError()


def validate_python(root):
    if not (root / "app.py").is_file():
        raise ValueError()
    # pip interprets requirements files as options as well as dependency declarations.
    requirements = []
    for line in read_manifest(root, "requirements.txt").splitlines():
        requirement = line.partition("#")[0].strip()
        if not requirement:
            continue
        if not re.fullmatch(
            r"[A-Za-z0-9][A-Za-z0-9_.-]*==[A-Za-z0-9][A-Za-z0-9_.+!-]*",
            requirement,
        ):
            raise ValueError()
        requirements.append(requirement)
    (root / "requirements.txt").write_text("\n".join(requirements) + "\n", encoding="utf-8")


def main():
    with tempfile.TemporaryFile(dir="/scratch") as archive:
        try:
            url = Path("/source-credential/source-url").read_text(encoding="utf-8")
            download(url, archive)
        except Exception:
            raise SystemExit("Hosted source download failed") from None
        try:
            root = Path("/source")
            unpack(archive, root)
            {"node": validate_node, "python": validate_python}[sys.argv[1]](root)
        except Exception:
            raise SystemExit("Hosted source failed archive or template validation") from None


if __name__ == "__main__":
    main()
