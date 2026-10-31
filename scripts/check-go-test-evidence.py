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

WORKFLOW_REQUEST_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/workflow",
    "parent": "TestWorkflowRequestReceiptsWithPostgres",
    "children": ["concurrent_original_claim", "changed_input_conflicts", "restart_preserves_unknown_and_original_id",
                 "completion_is_immutable_and_idempotent", "actor_isolation", "missing_run_keeps_tombstone"],
}


DOCKER_CONFIGURATION_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/docker",
    "parent": "TestDockerConditionalConfigurationWithPostgres",
    "children": ["concurrent_cas_one_winner", "restart_replay_and_changed_input", "legacy_writer_and_aba_conflict",
                 "partial_preserves_runtime_precision_and_expiry", "explicit_zero_empty_and_large_object", "host_move_uid_and_scope_fences",
                 "definition_failure_rolls_back_receipt_and_revision", "delete_tombstone_survives_recreation",
                 "children_and_runtime_history_block_deregistration", "registration_is_metadata_only_and_replayable",
                 "key_ceiling_denies_without_mutation", "receipt_failure_rolls_back_deletion", "reviewed_deploy_configuration_fence"],
}


DOCKER_RUNTIME_IDENTITY_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/docker",
    "parent": "TestDockerRuntimeIdentityWithPostgres",
    "children": ["legacy_default_and_explicit_opt_in", "service_opt_in_and_disable_preserves_existing", "version_update_rejected", "legacy_runner_cannot_claim_bound",
                 "capable_claim_preserves_original_binding", "unknown_protocol_is_not_claimed", "callback_fence_required",
                 "atomic_container_start_binds_database_uids", "callback_transaction_rollback_and_recreation_fence",
                 "confirmed_inventory_removal_and_atomic_callback", "queued_project_freezes_registered_container_set"],
}


PROTECTED_STORAGE_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/copilot",
    "parent": "TestPrivateDurableStorageWithPostgres",
    "children": ["workflow_intent_prepared_and_revision_ciphertext", "workflow_revision_cas_retains_sealed_state",
                 "native_checkpoint_and_journal_ciphertext", "native_resume_reseals_without_losing_budget",
                 "native_public_observation_copies_never_store_private_input_or_output",
                 "native_missing_key_and_corruption_rejected", "native_personal_cleanup_lookup_is_read_only_and_task_bound"],
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
    parser.add_argument("--workflow-requests", action="store_true")
    parser.add_argument("--protected-storage", action="store_true")
    parser.add_argument("--docker-configuration-mutations", action="store_true")
    parser.add_argument("--docker-runtime-identity", action="store_true")
    args = parser.parse_args()
    try:
        scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": "TestMainSessionWithPostgres", "children": ["personal_assistant_identity"]} if args.main_chat else {}
        if args.identity:
            scope = IDENTITY_SCOPE
        if args.workflow_requests:
            scope = WORKFLOW_REQUEST_SCOPE
        if args.protected_storage:
            scope = PROTECTED_STORAGE_SCOPE
        if args.docker_configuration_mutations:
            scope = DOCKER_CONFIGURATION_SCOPE
        if args.docker_runtime_identity:
            scope = DOCKER_RUNTIME_IDENTITY_SCOPE
        print(json.dumps(validate(args.log.read_text(), args.exit_code, **scope)))
    except (ValueError, OSError) as exc:
        parser.exit(1, str(exc) + "\n")
