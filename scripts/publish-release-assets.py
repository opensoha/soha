#!/usr/bin/env python3
"""Publish missing assets without replacing immutable release bytes."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
from urllib.parse import quote

SPEC = importlib.util.spec_from_file_location("release_control", Path(__file__).with_name("release-control.py"))
control = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(control)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def gh(*args):
    return subprocess.check_output(["gh", *args], text=True, timeout=300)


def release_metadata(repo, tag):
    response = subprocess.run(["gh", "api", "--method", "GET", f"repos/{repo}/releases/tags/{quote(tag, safe='')}"], capture_output=True, text=True, timeout=60)
    if response.returncode:
        if "(HTTP 404)" in response.stderr:
            return None
        raise ValueError("Cannot establish release state; no writes attempted")
    return json.loads(response.stdout)


def compare_existing(files, assets, download):
    names = [item["name"] for item in assets]
    if len(names) != len(set(names)):
        raise ValueError("Ambiguous existing release asset names")
    missing = []
    with tempfile.TemporaryDirectory(prefix="release-assets-") as temporary:
        for source in files:
            if source.name not in names:
                missing.append(source)
                continue
            received = Path(temporary) / source.name
            download(source.name, received)
            if digest(source) != digest(received):
                raise ValueError(f"Existing immutable asset differs: {source.name}")
    return missing


def publish(release, files, title, latest=False):
    if not files or any(not file.is_file() or file.is_symlink() or not re.fullmatch(r"[A-Za-z0-9_.+-]+", file.name) for file in files):
        raise ValueError("Only explicitly selected regular release assets are allowed")
    if len({file.name for file in files}) != len(files):
        raise ValueError("Duplicate local release asset names")
    if latest and release["component"] != "core":
        raise ValueError("Only Core owns the Root latest release")
    control.verify(release)
    repo, tag = release["repository"], release["tag"]
    existing = release_metadata(repo, tag)
    if existing and (existing.get("tag_name") != tag or existing.get("prerelease") is not False):
        raise ValueError("Existing release identity differs")
    if existing and not {item["name"] for item in existing.get("assets", [])} <= {file.name for file in files}:
        raise ValueError("Existing release has assets outside the approved set")
    missing = compare_existing(files, existing.get("assets", []) if existing else [],
        lambda name, output: gh("release", "download", tag, "--repo", repo, "--pattern", name, "--output", str(output)))
    control.verify(release)
    if existing is None:
        gh("release", "create", tag, "--repo", repo, "--target", release["sha"], "--draft", "--latest=false", "--title", title)
    for file in missing:
        gh("release", "upload", tag, str(file), "--repo", repo)
    current = release_metadata(repo, tag)
    if not current or not {file.name for file in files} <= {item["name"] for item in current.get("assets", [])}:
        raise ValueError("Release asset upload is incomplete")
    compare_existing(files, current["assets"],
        lambda name, output: gh("release", "download", tag, "--repo", repo, "--pattern", name, "--output", str(output)))
    control.verify(release)
    gh("release", "edit", tag, "--repo", repo, "--draft=false", "--prerelease=false", "--latest=" + str(latest).lower())
    print(json.dumps({"tag": tag, "sha": release["sha"], "assets": [file.name for file in files], "result": "PASS"}))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--component", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--title")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--directory", type=Path)
    group.add_argument("--files", nargs="+", type=Path)
    parser.add_argument("--latest", action="store_true")
    args = parser.parse_args()
    release = control.identity(args.component, args.tag, args.sha)
    files = sorted(args.directory.iterdir()) if args.directory else args.files
    publish(release, files, args.title or args.tag, args.latest)


if __name__ == "__main__":
    main()
