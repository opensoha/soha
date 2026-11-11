#!/usr/bin/env python3
"""Route actual Git changes and reject incomplete required-job results."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys

JOBS = ("core", "web", "contracts", "deletion")


def changed_paths(data):
    fields = data.decode("utf-8", errors="strict").split("\0")
    if fields.pop() != "":
        raise ValueError("unterminated Git diff")
    paths = []
    while fields:
        status = fields.pop(0)
        if not re.fullmatch(r"[ACDMRTUXB](?:[0-9]{1,3})?", status):
            raise ValueError("invalid Git diff status")
        count = 2 if status[0] in "RC" else 1
        if len(fields) < count:
            raise ValueError("missing Git diff path")
        for _ in range(count):
            path = fields.pop(0)
            if not path or path.startswith("/") or ".." in path.split("/"):
                raise ValueError("invalid Git diff path")
            paths.append(path)
    return paths


def select(data):
    try:
        paths = changed_paths(data)
    except (UnicodeError, ValueError):
        return dict.fromkeys(JOBS, True)
    result = dict.fromkeys(JOBS, False)
    for path in paths:
        # Shared tooling and public contracts can affect every consumer.
        if path.startswith((".github/", "scripts/", "contracts/", "deploy/", ".agents/", "web/.github/")) or path in {
            "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "package-lock.json", "Makefile", "Dockerfile",
        }:
            return dict.fromkeys(JOBS, True)
        if path.startswith(("docs/", "web/docs/")) or path.endswith(".md") or path in {"LICENSE", "web/LICENSE", "contracts/LICENSE"}:
            continue
        if path.startswith("web/"):
            result.update(core=True, web=True)
        elif path.startswith(("cmd/", "internal/", "pkg/", "migrations/", "configs/")):
            result.update(core=True, deletion=True)
        else:
            return dict.fromkeys(JOBS, True)
    return result


def check_summary(needs):
    route = needs.get("route", {})
    if route.get("result") != "success":
        raise ValueError("impact route did not succeed")
    for job in JOBS:
        required = route.get("outputs", {}).get(job)
        if required not in {"true", "false"}:
            raise ValueError(f"missing/invalid route output: {job}")
        result = needs.get(job, {}).get("result")
        allowed = {"success"} if required == "true" else {"success", "skipped"}
        if result not in allowed:
            raise ValueError(f"{job}: required={required}, result={result!r}")


def git_changes():
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
    if os.environ.get("GITHUB_EVENT_NAME") == "pull_request":
        base = event["pull_request"]["base"]["sha"]
    elif os.environ.get("GITHUB_EVENT_NAME") == "push":
        base = event["before"]
    else:
        return None
    head = os.environ["GITHUB_SHA"]
    if not all(re.fullmatch(r"[0-9a-f]{40}", sha) and set(sha) != {"0"} for sha in (base, head)):
        raise ValueError("missing exact Git comparison")
    subprocess.run(["git", "merge-base", "--is-ancestor", base, head], check=True)
    return subprocess.check_output(["git", "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames", base, head])


def web_baseline(base, root=Path.cwd()):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args], text=True, stderr=subprocess.PIPE).strip()
    if not base:
        base = git("rev-parse", "refs/remotes/origin/main")
    if not re.fullmatch(r"[0-9a-f]{40}", base) or set(base) == {"0"}:
        raise ValueError("Web baseline requires an exact nonzero commit")
    git("merge-base", "--is-ancestor", base, "HEAD")
    present = subprocess.run(["git", "-C", str(root), "cat-file", "-e", f"{base}:web/package.json"], capture_output=True)
    if present.returncode == 0:
        return base
    # The first migration compares against the reviewed imported Web tree, preserving its debt.
    additions = git("log", "--first-parent", "--diff-filter=A", "--format=%H", "HEAD", "--", "web/package.json").splitlines()
    if not additions:
        raise ValueError("reviewed Web import is missing")
    imported = additions[0]
    source = json.loads((root / "contracts/compat/consumer-revisions.json").read_text())["soha-web"]
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("reviewed Web source SHA is missing")
    git("merge-base", "--is-ancestor", base, imported)
    git("merge-base", "--is-ancestor", imported, "HEAD")
    git("merge-base", "--is-ancestor", source, "HEAD")
    if git("rev-parse", f"{imported}:web") != git("rev-parse", f"{source}^{{tree}}"):
        raise ValueError("Web import differs from the reviewed source tree")
    return imported


if __name__ == "__main__":
    if sys.argv[1:] == ["summary"]:
        check_summary(json.loads(os.environ["CI_NEEDS"]))
        print("All required component gates succeeded")
    elif sys.argv[1:] == ["route"]:
        try:
            data = git_changes()
            selected = select(data) if data is not None else dict.fromkeys(JOBS, True)
        except (KeyError, ValueError, OSError, subprocess.CalledProcessError) as error:
            print(f"Full validation: cannot classify diff: {error}", file=sys.stderr)
            selected = dict.fromkeys(JOBS, True)
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            for job, required in selected.items():
                print(f"{job}={str(required).lower()}", file=output)
        print(json.dumps(selected, sort_keys=True))
    elif sys.argv[1:] == ["web-base"]:
        baseline = web_baseline(os.environ.get("SOURCE_BASE_SHA"))
        if os.environ.get("GITHUB_OUTPUT"):
            with open(os.environ["GITHUB_OUTPUT"], "a") as output:
                print(f"sha={baseline}", file=output)
        print(baseline)
    else:
        sys.exit("usage: ci-impact.py route|summary|web-base")
