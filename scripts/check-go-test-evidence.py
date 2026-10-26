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

KUBERNETES_IMPORT_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/delivery",
    "parent": "TestKubernetesImportIdentityWithPostgres",
    "children": ["create_and_idempotent", "uid_replacement_rollback", "ownership_mode_no_upgrade",
                 "preserves_service_metadata", "destination_race_rollback", "cross_namespace_identity",
                 "native_ownership_conflict", "crd_identity_and_shared_reference", "legacy_missing_uid_conflict",
                 "explicit_service_across_environments", "explicit_service_invalid_rollback", "import_fences_default_edit_cas", "helm_explicit_service_mapping", "service_creation_identity_matches_persistence"],
}


RELATION_RULES_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/catalog",
    "parent": "TestTemplateVersionsWithPostgres",
    "children": ["relation-rules-persistence-and-cas"],
}


MANIFEST_ROOT_SCOPE = {"package":"github.com/opensoha/soha/internal/repository/manifest","parent":"TestDeliverySnapshotWithPostgres","children":["root-cr-publication-control", *["root-cr-publication-control/"+name for name in ["valid","withdrawn","uid-replaced","baseline-changed","binding-changed"]]]}

TEMPLATE_ADOPTION_SCOPE = {"package":"github.com/opensoha/soha/internal/repository/templateupdate","parent":"TestTemplateAdoptionWithPostgres","children":["atomic_receipt_and_environment_isolation","stale_and_canceled_worker","policy_cas_and_private_identity"]}

INITIALIZATION_SCOPE = {"package":"github.com/opensoha/soha/internal/repository/delivery","parent":"TestInitializationHistoryWithPostgres","children":["absent","preflight-only","same-plan","same-plan-failed","same-plan-replayed","failed-write","canceled-zero-attempt","cross-owner-version","helm-release-history","admission-rechecks-failed-physical-target","legacy-direct-workers-cannot-claim-initialization"]}
TEMPLATE_MIGRATION_SCOPE = {"package":"github.com/opensoha/soha/internal/repository/catalog","parent":"TestTemplateVersionsWithPostgres","children":["migration-declarations-preserve-legacy-writers"]}
MANIFEST_MIGRATION_SCOPE = {"package":"github.com/opensoha/soha/internal/repository/manifest","parent":"TestDeliverySnapshotWithPostgres","children":["migration-baseline-is-success-only",*["migration-baseline-is-success-only/"+name for name in ["verified","desired-only","stale-generation","failed","digest-mismatch","malformed-identity"]]]}

SERVICE_REFERENCE_CANDIDATES_SCOPE = {
    "package": "github.com/opensoha/soha/internal/repository/delivery",
    "parent": "TestServiceReferenceCandidatesWithPostgres",
    "children": ["save_restore_no_runtime_changes", "update_and_stale_cas", "delete_recreate_aba",
                 "context_drift", *["context_drift/" + name for name in
                    ("consumer_binding", "source_binding", "service_version_without_timestamp",
                     "source_target_without_timestamp", "environment_classification")],
                 "reauthorization_rollback", "concurrent_create", "concurrent_update",
                 "source_deleted_candidate_cleanup", "binding_lock_serializes_source_mutation",
                 "consumer_binding_cascade", "application_cascade_unrelated_preserved",
                 "cancellation_no_partial_write", "metadata_pagination_without_source"],
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
    parser.add_argument("--kubernetes-import", action="store_true")
    parser.add_argument("--service-reference-candidates", action="store_true")
    parser.add_argument("--relation-rules", action="store_true")
    parser.add_argument("--template-adoption", action="store_true")
    parser.add_argument("--template-migration", action="store_true")
    parser.add_argument("--manifest-migration", action="store_true")
    args = parser.parse_args()
    try:
        scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": "TestMainSessionWithPostgres", "children": ["personal_assistant_identity"]} if args.main_chat else {}
        if args.identity:
            scope = IDENTITY_SCOPE
        if args.kubernetes_import:
            scope = KUBERNETES_IMPORT_SCOPE
        if args.service_reference_candidates:
            scope = SERVICE_REFERENCE_CANDIDATES_SCOPE
        if args.relation_rules:
            scope = RELATION_RULES_SCOPE
        if args.template_adoption:
            scope = TEMPLATE_ADOPTION_SCOPE
        if args.template_migration:
            scope = TEMPLATE_MIGRATION_SCOPE
        if args.manifest_migration:
            scope = MANIFEST_MIGRATION_SCOPE
        print(json.dumps(validate(args.log.read_text(), args.exit_code, **scope)))
    except (ValueError, OSError) as exc:
        parser.exit(1, str(exc) + "\n")
