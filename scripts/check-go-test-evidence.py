#!/usr/bin/env python3
"""Validate required uncached PostgreSQL test evidence, not merely package success."""
import argparse
import json
from pathlib import Path

PARENT = "TestApplicationAndClusterDeletionWithPostgres"
CHILDREN = [
    "application_history_archived=false", "application_history_archived=true",
    "completed_shared_batch_is_retained",
    *["application_blocked_by_" + state for state in (
        "queued", "dispatching", "running", "canceling", "workflow", "batch",
        "external_reference", "cross_application_binding")],
    *["cluster_blocked_by_" + ref for ref in (
        "legacy_environment", "release_target", "manifest_binding", "worker_pool")],
    "cluster_FK_fallback_rolls_back_credentials",
]
PACKAGE = "github.com/opensoha/soha/internal/repository/application"


def validate(raw, exit_code=0):
    if exit_code != 0:
        raise ValueError("Go process did not succeed")
    events = [json.loads(line) for line in raw.splitlines() if line.strip()]
    if not events or any(not isinstance(e, dict) or not isinstance(e.get("Action"), str) for e in events):
        raise ValueError("Missing or malformed Go JSON events")
    if any(e.get("Action") == "fail" for e in events):
        raise ValueError("Go package or test failed")
    scoped = [e for e in events if e.get("Package") == PACKAGE]
    if any("(cached)" in e.get("Output", "") for e in scoped):
        raise ValueError("Cached evidence is not a new integration run")
    required = [PARENT, *[PARENT + "/" + child for child in CHILDREN]]
    for name in required:
        actions = [e["Action"] for e in scoped if e.get("Test") == name]
        if "run" not in actions or not actions or actions[-1] != "pass":
            raise ValueError("Required scenario missing, incomplete or not passed: " + name)
    target = [e for e in scoped if e.get("Test", "") == PARENT or e.get("Test", "").startswith(PARENT + "/")]
    if any(e["Action"] in ("skip", "fail") for e in target):
        raise ValueError("Required descendant skipped or failed")
    if not any(e["Action"] == "pass" and "Test" not in e for e in scoped):
        raise ValueError("Package completion missing")
    return {"result": "PASS", "parent": PARENT, "children": len(CHILDREN)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("log", type=Path)
    parser.add_argument("--exit-code", type=int, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(validate(args.log.read_text(), args.exit_code)))
    except (ValueError, OSError) as exc:
        parser.exit(1, str(exc) + "\n")
