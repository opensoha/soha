#!/usr/bin/env python3
"""Route actual Git changes and reject incomplete required-job results."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys

JOBS = ("core", "web", "contracts", "cli", "agent", "connectors", "skills", "deletion")


def changed_entries(data):
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
            paths.append((status, path))
    return paths


def changed_paths(data):
    return [path for _, path in changed_entries(data)]


def select(data):
    try:
        entries = changed_entries(data)
    except (UnicodeError, ValueError):
        return dict.fromkeys(JOBS, True)
    result = dict.fromkeys(JOBS, False)
    for status, path in entries:
        parts = path.split("/")
        # DSH is an independent Node24 runtime, outside the Root npm workspace.
        if path in {"agent/runtimes/dsh/package.json", "agent/runtimes/dsh/package-lock.json"}:
            result["agent"] = True
            continue
        # Shared tooling and public contracts can affect every consumer.
        if path.startswith((".github/", "scripts/", "contracts/", "deploy/", ".agents/")) or ".github" in parts or parts[-1] in {
            "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "package-lock.json", "Makefile", "Dockerfile",
            "npm-shrinkwrap.json",
        } or path in {
            ".prettierrc", ".prettierrc.json", ".prettierrc.yml", ".prettierrc.yaml", ".prettierrc.js",
            ".prettierrc.cjs", ".prettierignore", "prettier.config.js", "prettier.config.mjs", ".editorconfig",
        }:
            return dict.fromkeys(JOBS, True)
        # Only explicit documentation directories/root documents bypass component
        # routing. Runtime SKILL.md, fixtures and generated Markdown remain inputs.
        if path.endswith(".md") and (parts[0] == "docs" or (len(parts) > 2 and parts[0] in {"web", "cli", "agent", "connectors", "skills"} and parts[1] == "docs")):
            continue
        if status[0] in "AM" and (len(parts) == 1 or (len(parts) == 2 and parts[0] in JOBS)) and parts[-1] in {
            "README.md", "CHANGELOG.md", "LICENSE", "LICENSE.md",
        }:
            continue
        if path.startswith("web/"):
            result.update(core=True, web=True)
        elif path.startswith("cli/"):
            result["cli"] = True
        elif path.startswith("agent/"):
            result["agent"] = True
        elif path.startswith("connectors/"):
            result["connectors"] = True
        elif path.startswith("skills/"):
            result.update(skills=True, cli=True, agent=True)
        elif path.startswith(("cmd/", "internal/", "pkg/", "migrations/", "configs/")):
            result.update(core=True, deletion=True)
        else:
            return dict.fromkeys(JOBS, True)
    return result


def check_summary(needs):
    if not isinstance(needs, dict):
        raise ValueError("job results must be an object")
    route = needs.get("route", {})
    if not isinstance(route, dict) or route.get("result") != "success":
        raise ValueError("impact route did not succeed")
    outputs = route.get("outputs")
    if not isinstance(outputs, dict):
        raise ValueError("impact route outputs are missing or invalid")
    for job in JOBS:
        required = outputs.get(job)
        if not isinstance(required, str) or required not in {"true", "false"}:
            raise ValueError(f"missing/invalid route output: {job}")
        entry = needs.get(job)
        result = entry.get("result") if isinstance(entry, dict) else None
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
    additions = git("log", "-m", "--full-history", "--ancestry-path", "--diff-filter=A", "--format=%H", f"{base}..HEAD", "--", "web/package.json").splitlines()
    if not additions:
        raise ValueError("reviewed Web import is missing")
    source = json.loads((root / "contracts/compat/consumer-revisions.json").read_text())["soha-web"]
    if not re.fullmatch(r"[0-9a-f]{40}", source):
        raise ValueError("reviewed Web source SHA is missing")
    git("merge-base", "--is-ancestor", source, "HEAD")
    source_tree = git("rev-parse", f"{source}^{{tree}}")
    for imported in additions:
        if git("rev-parse", f"{imported}:web") == source_tree and source in git(
            "rev-list", "--parents", "-n", "1", imported
        ).split()[1:]:
            return imported
    raise ValueError("Web import differs from the reviewed source tree")


if __name__ == "__main__":
    if sys.argv[1:] == ["summary"]:
        check_summary(json.loads(os.environ["CI_NEEDS"]))
        print("All required component gates succeeded")
    elif sys.argv[1:] == ["route"]:
        try:
            data = git_changes()
            selected = select(data) if data is not None else dict.fromkeys(JOBS, True)
        except (KeyError, TypeError, ValueError, OSError, subprocess.CalledProcessError) as error:
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
