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

IDENTITY_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/copilot",
    "parent": "TestSessionAuthorizationWithPostgres",
    "children": ["current_permission_and_owner", "revoked_before_refresh", "old_version_stays_revoked",
                 "current_and_legacy_deny_resource_reads", "unrelated_user_and_history_preserved",
                 "revoked_session_cannot_refresh"],
}


def validate(raw, exit_code=0, package=PACKAGE, parent=PARENT, children=CHILDREN):
    if exit_code != 0:
        raise ValueError("Go process did not succeed")
    events = [json.loads(line) for line in raw.splitlines() if line.strip()]
    if not events or any(not isinstance(e, dict) or not isinstance(e.get("Action"), str) for e in events):
        raise ValueError("Missing or malformed Go JSON events")
    if any(e.get("Action") == "fail" for e in events):
        raise ValueError("Go package or test failed")
    scoped = [e for e in events if e.get("Package") == package]
    if any("(cached)" in e.get("Output", "") for e in scoped):
        raise ValueError("Cached evidence is not a new integration run")
    required = [parent, *[parent + "/" + child for child in children]]
    for name in required:
        actions = [e["Action"] for e in scoped if e.get("Test") == name]
        if "run" not in actions or not actions or actions[-1] != "pass":
            raise ValueError("Required scenario missing, incomplete or not passed: " + name)
    target = [e for e in scoped if e.get("Test", "") == parent or e.get("Test", "").startswith(parent + "/")]
    if any(e["Action"] in ("skip", "fail") for e in target):
        raise ValueError("Required descendant skipped or failed")
    if not any(e["Action"] == "pass" and "Test" not in e for e in scoped):
        raise ValueError("Package completion missing")
    return {"result": "PASS", "parent": parent, "children": len(children)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("log", type=Path)
    parser.add_argument("--exit-code", type=int, required=True)
    parser.add_argument("--main-chat", action="store_true")
    parser.add_argument("--identity", action="store_true")
    args = parser.parse_args()
    try:
        scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": "TestMainSessionWithPostgres", "children": ["personal_assistant_identity"]} if args.main_chat else {}
        if args.identity:
            scope = IDENTITY_SCOPE
        print(json.dumps(validate(args.log.read_text(), args.exit_code, **scope)))
    except (ValueError, OSError) as exc:
        parser.exit(1, str(exc) + "\n")
