#!/usr/bin/env python3
"""Plan and run selected Soha tests from the local Git change set."""

import argparse
from concurrent.futures import ThreadPoolExecutor
from itertools import zip_longest
import json
import os
from pathlib import Path
import runpy
import shlex
import signal
import subprocess
import sys
import threading
import time


ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = ROOT / "scripts"
SUPPORTED = ("core", "web")
BASE_REFS = ("refs/remotes/opensoha/main", "refs/remotes/origin/main", "refs/heads/main")
PROCESSES = set()
PROCESS_LOCK = threading.Lock()
CANCELLED = threading.Event()


def git(*arguments, root=ROOT):
    return subprocess.check_output(["git", *arguments], cwd=root, text=False,
                                   stderr=subprocess.PIPE).decode()


def resolve_base(base, root=ROOT):
    if base is None:
        refs = set(git("for-each-ref", "--format=%(refname)", *BASE_REFS, root=root).splitlines())
        upstream = next((ref for ref in BASE_REFS if ref in refs), None)
        if upstream is None:
            raise ValueError("main ref unavailable; specify --base")
        base = git("merge-base", upstream, "HEAD", root=root).strip()
    else:
        base = git("rev-parse", "--verify", "--end-of-options", f"{base}^{{commit}}", root=root).strip()
    git("merge-base", "--is-ancestor", base, "HEAD", root=root)
    return base


def load_diff(base=None, root=ROOT):
    base = resolve_base(base, root)
    data = subprocess.check_output(
        ["git", "--no-optional-locks", "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z",
         "--find-renames", base, "--"], cwd=root, stderr=subprocess.PIPE)
    for path in subprocess.check_output(
            ["git", "ls-files", "--others", "--exclude-standard", "-z"], cwd=root,
            stderr=subprocess.PIPE).split(b"\0"):
        if path:
            data += b"A\0" + path + b"\0"
    selector = runpy.run_path(str(SCRIPTS / "ci-impact.py"), run_name="ci_impact")
    try:
        entries = selector["changed_entries"](data)
    except (UnicodeError, ValueError):
        entries = []
    return base, selector["select"](data), entries


def safe_relative(value):
    parts = Path(value).parts
    return (value and not Path(value).is_absolute() and ".." not in parts
            and not value.startswith("-") and "\0" not in value)


def derive_packages(entries, root=ROOT):
    packages = []
    for status, path in entries:
        parts = Path(path).parts
        if parts and (parts[0] in {"contracts", "configs", "migrations", ".github", "scripts"}
                      or parts[-1] in {"go.mod", "go.sum", "go.work", "go.work.sum"}):
            return ["./..."]
        if not parts or parts[0] not in {"cmd", "internal", "pkg"}:
            continue
        if status[0] in "DRTU" or not path.endswith(".go"):
            return ["./..."]
        directory = Path(path).parent
        if not (root / directory).is_dir():
            return ["./..."]
        package = "." if directory == Path(".") else "./" + directory.as_posix()
        if package not in packages:
            packages.append(package)
    return packages or ["./..."]


def build_jobs(selected, packages, web_test, base):
    jobs = {}
    if selected.get("core"):
        jobs["core"] = [["bash", "scripts/run-pr-go-lint.sh", base],
                        ["go", "test", "-count=1", *packages]]
    if selected.get("web"):
        jobs["web"] = [["npm", "run", "lint", "--workspace", "web"],
                       ["npm", "run", "typecheck", "--workspace", "web"]]
        test = ["npm", "test", "--workspace", "web"]
        if web_test:
            test += ["--", web_test]
        jobs["web"].append(test)
    return jobs


def make_plan(base=None, packages=None, web_test=None, root=ROOT):
    base, selected, entries = load_diff(base, root)
    if packages:
        bad = [value for value in packages if not safe_relative(value)]
        if bad:
            raise ValueError("unsafe Go package path: " + ", ".join(bad))
    else:
        packages = ["./..."] if all(selected.values()) else derive_packages(entries, root)
    if web_test:
        web = (root / "web").resolve()
        target = (web / web_test).resolve()
        if not safe_relative(web_test) or not target.is_relative_to(web) or not target.is_file():
            raise ValueError("web test must be an existing path below web/")
    chosen = [job for job in SUPPORTED if selected.get(job)]
    unsupported = sorted(job for job, required in selected.items()
                         if required and job not in SUPPORTED)
    return {"base": base, "selected": sorted(job for job, required in selected.items() if required),
            "changed": [path for _, path in entries], "unsupported": unsupported,
            "subset": {"go_packages": packages, "web_test": web_test},
            "jobs": build_jobs(selected, packages, web_test, base), "chosen": chosen}


def signal_group(child, signum):
    child.poll()
    try:
        os.killpg(child.pid, signum)
        return True
    except ProcessLookupError:
        return False


def terminate(signum, frame):
    CANCELLED.set()
    with PROCESS_LOCK:
        children = list(PROCESSES)
    for child in children:
        signal_group(child, signum)
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline and any(signal_group(child, 0) for child in children):
        time.sleep(0.02)
    for child in children:
        signal_group(child, signal.SIGKILL)
    raise SystemExit(128 + signum)


def execute(jobs, workers, root=ROOT, json_output=False):
    env = dict(os.environ, GOWORK=str(root / "go.work"))
    results = {name: {"commands": []} for name in jobs}
    # Interleave independent Go and Web commands while using a single worker cap.
    groups = [[(name, argv) for argv in commands] for name, commands in jobs.items()]
    tasks = [task for row in zip_longest(*groups) for task in row if task is not None]

    def command(task):
        name, argv = task
        started = time.monotonic()
        output = ""
        try:
            with PROCESS_LOCK:
                if CANCELLED.is_set():
                    return name, {"argv": argv, "status": "cancelled", "returncode": None, "seconds": 0}
                child = subprocess.Popen(argv, cwd=root, env=env, start_new_session=True,
                                         stdout=subprocess.PIPE if json_output else None,
                                         stderr=subprocess.STDOUT if json_output else None,
                                         text=True, errors="replace")
                PROCESSES.add(child)
            try:
                output, _ = child.communicate()
                code = child.returncode
            finally:
                with PROCESS_LOCK:
                    PROCESSES.discard(child)
        except OSError as error:
            code = 127
            output = str(error)
            if not json_output:
                print(f"{shlex.join(argv)}: {error}", file=sys.stderr)
        return name, {"argv": argv, "status": "success" if code == 0 else "failure",
                      "returncode": code, "seconds": round(time.monotonic() - started, 3),
                      **({"output": (output or "")[-4000:]} if json_output else {})}

    with ThreadPoolExecutor(max_workers=workers) as pool:
        for name, result in pool.map(command, tasks):
            results[name]["commands"].append(result)
    for result in results.values():
        commands = result["commands"]
        result["status"] = "success" if all(c["status"] == "success" for c in commands) else "failure"
        result["seconds"] = round(sum(c["seconds"] for c in commands), 3)
    return results


def ensure_guard(argv):
    if any(os.environ.get(key, "").lower() in ("1", "true", "yes")
           for key in ("CI", "GITHUB_ACTIONS", "OPENSOHA_VERIFY_RELEASE")):
        return
    namespace = runpy.run_path(str(SCRIPTS / "local-verify.py"), run_name="local_verify")
    if namespace["nested_run"]():
        return
    command = [sys.executable, str(SCRIPTS / "local-verify.py"), "run", "--",
               sys.executable, str(Path(__file__).resolve()), *argv]
    os.execv(sys.executable, command)


def print_plan(plan, json_output):
    if json_output:
        print(json.dumps(plan, indent=2, sort_keys=True))
        return
    print(f"base: {plan['base']}")
    print(f"workers: {plan['workers']}")
    print("selected: " + (", ".join(plan["selected"]) or "none"))
    if plan["unsupported"]:
        print("unsupported: " + ", ".join(plan["unsupported"]))
    print(f"go packages: {' '.join(plan['subset']['go_packages'])}")
    print("web test: " + (plan["subset"]["web_test"] or "whole web suite"))
    for name, commands in plan["jobs"].items():
        print(name + ":")
        for argv in commands:
            print("  " + shlex.join(argv))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["plan", "run"])
    parser.add_argument("--base", help="explicit base commit (default: upstream main merge-base)")
    parser.add_argument("--go-package", action="append", dest="packages")
    parser.add_argument("--web-test", help="web-relative test file")
    parser.add_argument("--jobs", type=int, choices=range(1, 5), default=2,
                        help="maximum concurrent commands (default: 2)")
    parser.add_argument("--json", action="store_true")
    options = parser.parse_args()
    if options.mode == "run":
        ensure_guard(sys.argv[1:])
    plan = make_plan(options.base, options.packages, options.web_test)
    plan["workers"] = options.jobs
    if options.mode == "plan":
        print_plan(plan, options.json)
        return 0
    if plan["unsupported"]:
        print("Selected CI jobs not run locally: " + ", ".join(plan["unsupported"]), file=sys.stderr)
    previous = {signum: signal.signal(signum, terminate)
                for signum in (signal.SIGINT, signal.SIGTERM)}
    started = time.monotonic()
    try:
        results = execute(plan["jobs"], options.jobs, json_output=options.json)
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)
    wall_seconds = round(time.monotonic() - started, 3)
    if options.json:
        print(json.dumps({"base": plan["base"], "jobs": results, "workers": options.jobs,
                          "wall_seconds": wall_seconds,
                          "ci_jobs_not_run_locally": plan["unsupported"]}, indent=2, sort_keys=True))
    else:
        print(f"Local checks: {wall_seconds:.3f}s elapsed")
        for name, result in results.items():
            print(f"{name}: {result['status']} ({result['seconds']:.3f}s total command time)")
            for item in result["commands"]:
                print(f"  {item['status']}: {shlex.join(item['argv'])} ({item['seconds']:.3f}s)")
    if not plan["chosen"] and plan["selected"]:
        return 2
    return 1 if any(result["status"] != "success" for result in results.values()) else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"test-runner: {error}", file=sys.stderr)
        sys.exit(1)
