#!/usr/bin/env python3
"""Check exact release identity and exclusive publisher ownership before writes."""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
from urllib.parse import quote
from urllib.error import HTTPError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
ACTIVE = ("queued", "in_progress", "waiting", "pending", "requested")


def git(*args):
    return subprocess.check_output(["git", "-C", str(ROOT), *args], text=True).strip()


def github(endpoint, paginated=False):
    args = ["gh", "api", "--method", "GET"]
    if paginated:
        args += ["--paginate", "--slurp"]
    response = subprocess.run([*args, endpoint], capture_output=True, text=True, timeout=60)
    if response.returncode:
        raise ValueError(f"Cannot verify GitHub state: {endpoint}")
    return json.loads(response.stdout)


def identity(component, tag, sha, root=ROOT):
    config = json.loads((root / ".github/release-components.json").read_text())
    if config.get("schemaVersion") != 1 or config.get("repository") != "opensoha/soha":
        raise ValueError("Unexpected release owner configuration")
    owner = config["components"].get(component)
    if not owner or not re.fullmatch(r"[a-f0-9]{40}", sha):
        raise ValueError("Known component and full immutable source SHA required")
    match = re.fullmatch(re.escape(owner["tagPrefix"]) + r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", tag)
    if not match:
        raise ValueError("Release tag must match the component's stable version namespace")
    version = ".".join(match.groups())
    if owner.get("versionFile"):
        manifest = root / owner["versionFile"]
        if manifest.is_symlink() or json.loads(manifest.read_text())["version"] != version:
            raise ValueError("Release version differs from its source package")
    return {"component": component, "repository": config["repository"], "directory": owner["directory"],
            "tag": tag, "version": version, "sha": sha, "workflow": owner["workflow"],
            "environment": owner["environment"]}


def check_remote(release, config, api=github):
    owner = config["components"][release["component"]]
    repo = release["repository"]
    if os.environ.get(owner["enableVariable"]) != "true":
        raise ValueError("Publisher handoff has not been explicitly enabled")
    target = api(f"repos/{repo}/git/ref/tags/{quote(release['tag'], safe='')}")["object"]
    for _ in range(4):
        if target.get("type") == "commit":
            break
        if target.get("type") != "tag":
            raise ValueError("Release tag does not point to a commit")
        target = api(f"repos/{repo}/git/tags/{target['sha']}")["object"]
    if target.get("type") != "commit" or target.get("sha") != release["sha"]:
        raise ValueError("Remote immutable tag differs from the approved source")
    environment = api(f"repos/{repo}/environments/{quote(owner['environment'], safe='')}")
    if not environment.get("deployment_branch_policy", {}).get("custom_branch_policies"):
        raise ValueError("Release environment must restrict deployment refs")
    policies = api(f"repos/{repo}/environments/{quote(owner['environment'], safe='')}/deployment-branch-policies")
    rows = policies.get("branch_policies", [])
    allowed = {(row.get("name"), row.get("type")) for row in rows}
    if (policies.get("total_count") != len(rows) or not allowed
            or not allowed <= {("main", "branch"), (owner["tagPrefix"] + "*", "tag")}
            or (owner["tagPrefix"] + "*", "tag") not in allowed):
        raise ValueError("Release environment allows refs outside main and the component tag namespace")
    workflow = api(f"repos/{repo}/actions/workflows/{quote(config['requiredWorkflow'], safe='')}")
    pages = api(f"repos/{repo}/actions/workflows/{workflow['id']}/runs?head_sha={release['sha']}&per_page=100", True)
    runs = [run for page in pages for run in page.get("workflow_runs", [])]
    matching = [run for run in runs if run.get("head_sha") == release["sha"]
                and run.get("head_branch") == "main" and run.get("event") == "push"]
    latest = max(matching, key=lambda run: (run["id"], run.get("run_attempt", 1)), default={})
    if latest.get("status") != "completed" or latest.get("conclusion") != "success":
        raise ValueError("Exact source SHA has no successful main source CI run")
    for tag in owner.get("requiredReleaseTags", []):
        dependency = api(f"repos/{repo}/releases/tags/{quote(tag, safe='')}")
        if dependency.get("draft") is not False or dependency.get("prerelease") is not False or dependency.get("tag_name") != tag:
            raise ValueError(f"Required component release is not public/stable: {tag}")
    for legacy in owner["legacy"]:
        endpoint = f"repos/{legacy['repository']}/actions/workflows/{quote(legacy['workflow'], safe='')}"
        previous = api(endpoint)
        if previous.get("state") != "disabled_manually":
            raise ValueError(f"Legacy publisher is not disabled: {legacy['repository']}")
        for status in ACTIVE:
            pages = api(f"repos/{legacy['repository']}/actions/workflows/{previous['id']}/runs?status={status}&per_page=100", True)
            if any(page.get("workflow_runs") for page in pages):
                raise ValueError(f"Legacy publisher still has {status} runs: {legacy['repository']}")


def verify(release, root=ROOT):
    if os.environ.get("GITHUB_REPOSITORY") != release["repository"]:
        raise ValueError("Publisher must run in the owning Root GitHub repository")
    if git("rev-parse", "HEAD") != release["sha"]:
        raise ValueError("Checkout differs from the approved source SHA")
    if git("rev-parse", f"refs/tags/{release['tag']}^{{commit}}") != release["sha"]:
        raise ValueError("Immutable release tag differs from the source SHA")
    subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", release["sha"], "origin/main"], check=True)
    if git("diff", "--name-only") or git("diff", "--cached", "--name-only"):
        raise ValueError("Tracked source changed after checkout")
    config = json.loads((root / ".github/release-components.json").read_text())
    check_remote(release, config)


def registry_json(url, authorization):
    request = Request(url, headers={"Authorization": authorization,
        "Accept": "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"})
    try:
        with urlopen(request, timeout=30) as response:
            return response.status, json.load(response)
    except HTTPError as error:
        if error.code != 404:
            raise ValueError("Cannot establish authenticated GHCR manifest state") from None
        return error.code, json.load(error)


def check_images_unused(release, config, fetch=registry_json):
    actor, token = os.environ.get("GITHUB_ACTOR"), os.environ.get("GH_TOKEN")
    if not actor or not token:
        raise ValueError("Authenticated GHCR identity required")
    basic = "Basic " + base64.b64encode(f"{actor}:{token}".encode()).decode()
    for image in config["components"][release["component"]]["images"]:
        repository, tag = image["repository"], image["tagPrefix"] + release["version"]
        status, credential = fetch(f"https://ghcr.io/token?service=ghcr.io&scope=repository:{repository}:pull", basic)
        if status != 200 or not credential.get("token"):
            raise ValueError("Cannot establish authenticated GHCR read capability")
        status, manifest = fetch(f"https://ghcr.io/v2/{repository}/manifests/{tag}", "Bearer " + credential["token"])
        codes = [item.get("code") for item in manifest.get("errors", [])]
        first_package = codes == ["NAME_UNKNOWN"] and image.get("firstRelease") == release["version"]
        if status != 404 or not (codes == ["MANIFEST_UNKNOWN"] or first_package):
            raise ValueError(f"Immutable image version occupied or unverified: ghcr.io/{repository}:{tag}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["identity", "verify"])
    parser.add_argument("--component", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--github-output", action="store_true")
    parser.add_argument("--images-unused", action="store_true")
    args = parser.parse_args()
    release = identity(args.component, args.tag, args.sha)
    if args.mode == "verify":
        verify(release)
        if args.images_unused:
            check_images_unused(release, json.loads((ROOT / ".github/release-components.json").read_text()))
    elif args.images_unused:
        parser.error("--images-unused requires verify mode")
    if args.github_output:
        output = Path(os.environ["GITHUB_OUTPUT"])
        with output.open("a") as stream:
            for name, value in release.items():
                stream.write(f"{name}={value}\n")
    print(json.dumps({**release, "mode": args.mode, "verified": args.mode == "verify"}))


if __name__ == "__main__":
    main()
