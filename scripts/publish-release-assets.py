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


def release_metadata(repo, tag, release_id=None):
    # The by-tag REST endpoint does not expose drafts; discover them with authenticated pagination.
    response = subprocess.run(["gh", "api", "--method", "GET", "--paginate", "--slurp", f"repos/{repo}/releases?per_page=100"], capture_output=True, text=True, timeout=60)
    if response.returncode:
        raise ValueError("Cannot establish release state")
    pages = json.loads(response.stdout)
    if not isinstance(pages, list) or any(not isinstance(page, list) for page in pages):
        raise ValueError("Unexpected release listing")
    rows = [row for page in pages for row in page]
    if any(not isinstance(row, dict) or not isinstance(row.get("tag_name"), str) for row in rows):
        raise ValueError("Unexpected release identity in listing")
    matches = [row for row in rows if row["tag_name"] == tag]
    if len(matches) > 1:
        raise ValueError("Ambiguous releases for the exact tag")
    if not matches:
        if release_id is not None:
            raise ValueError("Selected release disappeared or changed tag")
        return None
    if release_id is None:
        release_id = matches[0].get("id")
    elif matches[0].get("id") != release_id:
        raise ValueError("Selected release was replaced")
    if type(release_id) is not int or release_id <= 0:
        raise ValueError("Invalid release ID")
    response = subprocess.run(["gh", "api", "--method", "GET", f"repos/{repo}/releases/{release_id}"], capture_output=True, text=True, timeout=60)
    if response.returncode:
        raise ValueError("Cannot establish selected release state")
    return validate_release(json.loads(response.stdout), tag, release_id)


def validate_release(release, tag, release_id):
    if (type(release_id) is not int or release_id <= 0
            or not isinstance(release, dict) or release.get("id") != release_id
            or release.get("tag_name") != tag or release.get("prerelease") is not False
            or type(release.get("draft")) is not bool or not isinstance(release.get("assets"), list)):
        raise ValueError("Selected release identity differs")
    if any(not isinstance(asset, dict) or not isinstance(asset.get("name"), str)
           or type(asset.get("id")) is not int or asset["id"] <= 0 for asset in release["assets"]):
        raise ValueError("Unexpected release asset metadata")
    if len({asset["id"] for asset in release["assets"]}) != len(release["assets"]):
        raise ValueError("Ambiguous release asset IDs")
    return release


def download_asset(repo, asset, output):
    with output.open("wb") as stream:
        subprocess.run(["gh", "api", "--method", "GET", f"repos/{repo}/releases/assets/{asset['id']}",
                        "--header", "Accept:application/octet-stream"], stdout=stream, check=True, timeout=300)


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
            download(next(asset for asset in assets if asset["name"] == source.name), received)
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
        lambda asset, output: download_asset(repo, asset, output))
    control.verify(release)
    if existing is None:
        created = json.loads(gh("api", "--method", "POST", f"repos/{repo}/releases",
                                "--raw-field", f"tag_name={tag}", "--raw-field", f"target_commitish={release['sha']}",
                                "--raw-field", f"name={title}", "--field", "draft=true",
                                "--field", "prerelease=false", "--raw-field", "make_latest=false"))
        existing = validate_release(created, tag, created.get("id") if isinstance(created, dict) else None)
        if existing["draft"] is not True:
            raise ValueError("Created release is not a draft")
    release_id = existing["id"]
    for file in missing:
        state = release_metadata(repo, tag, release_id)
        if (not {asset["name"] for asset in state["assets"]} <= {source.name for source in files}
                or any(asset["name"] == file.name for asset in state["assets"])):
            raise ValueError("Release assets changed before upload")
        gh("api", "--method", "POST", f"https://uploads.github.com/repos/{repo}/releases/{release_id}/assets?name={quote(file.name, safe='')}",
           "--header", "Content-Type:application/octet-stream", "--header", f"Content-Length:{file.stat().st_size}", "--input", str(file))
    current = release_metadata(repo, tag, release_id)
    if not current or {file.name for file in files} != {item["name"] for item in current.get("assets", [])}:
        raise ValueError("Release asset upload is incomplete")
    compare_existing(files, current["assets"],
        lambda asset, output: download_asset(repo, asset, output))
    control.verify(release)
    final = release_metadata(repo, tag, release_id)
    if {(asset["id"], asset["name"]) for asset in final["assets"]} != {(asset["id"], asset["name"]) for asset in current["assets"]}:
        raise ValueError("Verified release assets changed before publication")
    published = json.loads(gh("api", "--method", "PATCH", f"repos/{repo}/releases/{release_id}",
                              "--field", "draft=false", "--field", "prerelease=false", "--raw-field", "make_latest=" + str(latest).lower()))
    validate_release(published, tag, release_id)
    if published["draft"] is not False:
        raise ValueError("Release is still a draft")
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
