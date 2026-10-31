#!/usr/bin/env python3
"""Own a disposable PG instance and remove only the container created by this run."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import time
import uuid
import hashlib
import socket
import ipaddress
import secrets
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
IMAGE = "pgvector/pgvector:0.8.5-pg18-trixie"


def command(args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def check_owner(info, container_id, run_id):
    if info.get("Id") != container_id or info.get("Config", {}).get("Labels", {}).get("soha.test.run") != run_id or info.get("Config", {}).get("Image") != IMAGE:
        raise ValueError("Container ownership changed; refusing cleanup")


def cleanup_postgres(docker, identifier, name, run_id, attempted):
    if not attempted:
        return 'NOT_CREATED'
    inspected = subprocess.run([*docker, 'inspect', identifier or name], text=True, capture_output=True, timeout=20)
    missing = ('no such object:', 'no such container:')
    if inspected.returncode and any(reason in inspected.stderr.lower() for reason in missing):
        return 'ALREADY_REMOVED' if identifier else 'ABSENT_AFTER_UNKNOWN_CREATE'
    if inspected.returncode:
        raise ValueError('Cannot verify PostgreSQL cleanup state')
    value = json.loads(inspected.stdout)[0]
    if value['Name'] != '/' + name:
        raise ValueError('PostgreSQL container name changed; cleanup refused')
    check_owner(value, identifier or value['Id'], run_id)
    subprocess.run([*docker, 'rm', '--force', value['Id']], check=True, stdout=subprocess.DEVNULL, timeout=30)
    inspected = subprocess.run([*docker, 'inspect', name], text=True, capture_output=True, timeout=20)
    if inspected.returncode == 0 or not any(reason in inspected.stderr.lower() for reason in missing):
        raise ValueError('PostgreSQL removal could not be verified')
    return 'PASS'


def docker_test_endpoint(environment, postgres_host=None, allow_remote=False):
    info = json.loads(subprocess.check_output(['docker', 'context', 'inspect'], env=environment, text=True, timeout=15))[0]
    endpoint = info['Endpoints']['docker']['Host']
    if environment.get('DOCKER_HOST') and not environment.get('DOCKER_CONTEXT'):
        endpoint = environment['DOCKER_HOST']
    parsed = urlsplit(endpoint)
    if parsed.password is not None:
        raise ValueError('Docker endpoint must not contain a password')
    if parsed.scheme == 'unix':
        if not Path(endpoint.removeprefix('unix://')).is_socket():
            raise ValueError('Local Docker socket is unavailable')
    elif parsed.scheme != 'ssh' or not allow_remote:
        raise ValueError('Docker test requires a local socket or explicitly allowed SSH control endpoint')
    host = postgres_host or environment.get('SOHA_TEST_POSTGRES_HOST') or ('127.0.0.1' if parsed.scheme == 'unix' else '')
    try:
        address = ipaddress.IPv4Address(host)
    except ipaddress.AddressValueError as error:
        raise ValueError('Provide --postgres-host with the disposable PG private IPv4 address') from error
    networks = ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8')
    if not any(address in ipaddress.IPv4Network(network) for network in networks):
        raise ValueError('Disposable PG must bind a private or loopback IPv4 address')
    if parsed.scheme == 'ssh' and address.is_loopback:
        raise ValueError('Remote disposable PG requires its directly reachable private address')
    return endpoint, str(address)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / ".tmp/quality/deletion-test.json")
    parser.add_argument("--main-chat", action="store_true")
    parser.add_argument("--native-harness", action="store_true")
    parser.add_argument("--knowledge", action="store_true")
    parser.add_argument("--knowledge-ann", action="store_true")
    parser.add_argument("--personal-environment", action="store_true")
    parser.add_argument("--identity", action="store_true")
    parser.add_argument("--identity-restart", action="store_true")
    parser.add_argument("--cli-auth", action="store_true")
    parser.add_argument("--native-docker-go", action="store_true", help="Run unchanged CLI-auth race tests in an owned Go container near the direct PG")
    parser.add_argument("--contracts-dir", type=Path, help="Explicit candidate SDK source for native Docker testing only")
    parser.add_argument("--docker-identity", action="store_true")
    parser.add_argument("--docker-project-configuration", action="store_true")
    parser.add_argument("--workflow-requests", action="store_true")
    parser.add_argument("--docker-configuration-mutations", action="store_true")
    parser.add_argument("--protected-storage", action="store_true", help="Strict encrypted capability/Native persistence and restart scope")
    parser.add_argument("--allow-remote-docker", action="store_true", help="Explicitly allow the selected SSH Docker control endpoint")
    parser.add_argument("--postgres-host", help="Private IPv4 for this run's disposable PG; also accepts SOHA_TEST_POSTGRES_HOST")
    parser.add_argument("--virtualization-capacity", action="store_true")
    parser.add_argument("--feed-search",action="store_true")
    parser.add_argument("--network-navigation", action="store_true")
    parser.add_argument("--docker-runtime-identity", action="store_true")
    parser.add_argument("--conditional-mutations", action="store_true", help="Strict original Gateway/Native mutation receipt claims and tombstones")
    parser.add_argument("--directory-original-requests", action="store_true", help="Strict directory original claims, cancellation fencing, credentials and stale retention")
    parser.add_argument("--network-command-runtime", action="store_true", help="Strict original NAS command reads, real mTLS callback and durable journal replay without a real NAS")
    parser.add_argument("--mfa-session", action="store_true", help="Strict original MFA transaction/session binding on disposable PostgreSQL")
    parser.add_argument("--healing-requests", action="store_true", help="Strict original healing request transaction and dispatch CAS")
    parser.add_argument("--docker-native-terminals", action="store_true", help="Strict Docker native terminal one-use leases and encrypted recovery")
    args = parser.parse_args()
    if args.docker_native_terminals and any(value is True for name,value in vars(args).items() if name not in ("docker_native_terminals","allow_remote_docker")):
        parser.error("--docker-native-terminals is a separate persistence scope")
    if args.healing_requests and any(value is True for name,value in vars(args).items() if name not in ('healing_requests','allow_remote_docker')):
        parser.error('--healing-requests is a separate owned monitoring scope')
    if args.mfa_session and any(value is True for name,value in vars(args).items() if name not in ('mfa_session','allow_remote_docker')):
        parser.error('--mfa-session is a separate owned authentication scope')
    if args.network_command_runtime and any(value is True for name,value in vars(args).items() if name not in ('network_command_runtime','allow_remote_docker')):
        parser.error('--network-command-runtime is a separate owned runtime scope')
    if args.directory_original_requests and any(value is True for name,value in vars(args).items() if name not in ('directory_original_requests','allow_remote_docker')):
        parser.error('--directory-original-requests is a separate persistence scope')
    if args.conditional_mutations and any(value is True for name,value in vars(args).items() if name not in ('conditional_mutations','allow_remote_docker')):
        parser.error('--conditional-mutations is a separate persistence scope')
    if args.docker_runtime_identity and any(value is True for name,value in vars(args).items() if name not in ('docker_runtime_identity','allow_remote_docker')):
        parser.error('--docker-runtime-identity is a separate persistence scope')
    if args.docker_configuration_mutations and any(value is True for name,value in vars(args).items() if name not in ('docker_configuration_mutations','allow_remote_docker')):
        parser.error('--docker-configuration-mutations is a separate mutation scope')
    if args.protected_storage and any(value is True for name,value in vars(args).items() if name not in ('protected_storage','allow_remote_docker')):
        parser.error('--protected-storage is a separate persistence scope')
    if args.native_docker_go and (not args.cli_auth or not args.allow_remote_docker):
        parser.error('--native-docker-go requires --cli-auth and --allow-remote-docker')
    if args.native_docker_go and any(value is True for name, value in vars(args).items() if name not in ('cli_auth', 'allow_remote_docker', 'native_docker_go')):
        parser.error('--native-docker-go only runs the CLI-auth integration scope')
    if args.contracts_dir and not args.native_docker_go:
        parser.error('--contracts-dir requires --native-docker-go')
    if (args.docker_native_terminals or args.healing_requests or args.native_docker_go or args.docker_identity or args.docker_project_configuration or args.workflow_requests or args.protected_storage or args.docker_configuration_mutations or args.docker_runtime_identity or args.conditional_mutations or args.directory_original_requests or args.network_command_runtime or args.mfa_session) and (args.output.exists() or args.output.with_suffix(".manifest.json").exists()):
        raise ValueError("Refusing to replace prior Docker evidence")
    run_id = str(uuid.uuid4())
    args.output.parent.mkdir(parents=True, exist_ok=True)
    digest = hashlib.sha256(subprocess.check_output(['git', 'diff', 'HEAD', '--binary'], cwd=ROOT))
    for name in subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0'):
        if name: digest.update(name.encode()); digest.update((ROOT / name).read_bytes())
    manifest = {'runId': run_id, 'mode': 'postgres-real', 'coreCommit': command(['git', 'rev-parse', 'HEAD'], cwd=ROOT), 'dirtyPatchDigest': digest.hexdigest(), 'result': 'FAIL', 'cleanup': 'NOT_RUN'}
    container = None
    postgres_name = 'soha-test-pg-' + run_id
    postgres_attempted = False
    docker_command = ['docker']
    try:
        endpoint, host = docker_test_endpoint(os.environ, args.postgres_host, args.allow_remote_docker)
        docker_command = ['docker', '--host', endpoint]
        password = secrets.token_urlsafe(32)
        docker_env = dict(os.environ, POSTGRES_PASSWORD=password)
        manifest['postgresConnection'] = {'mode': 'direct', 'host': host, 'sshTunnel': False}
        postgres_attempted = True
        container = command([*docker_command, "run", "--detach", "--rm", '--name', postgres_name, "--label", "soha.test.run=" + run_id,
                             "--publish", host + "::5432", "--env", "POSTGRES_DB=soha",
                             "--env", "POSTGRES_USER=pgsql", "--env", "POSTGRES_PASSWORD", IMAGE], env=docker_env, timeout=90)
        info = json.loads(command([*docker_command, "inspect", container]))[0]
        check_owner(info, container, run_id)
        manifest['environment'] = {'id': container, 'disposable': True, 'image': IMAGE, 'imageDigest': info['Image']}
        port = info["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
        if info["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostIp"] != host:
            raise ValueError("PG binding differs from the selected direct address")
        manifest['postgresConnection']['port'] = int(port)
        for _ in range(60):
            if subprocess.run([*docker_command, "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "pgsql", "-d", "soha"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                try:
                    with socket.create_connection((host, int(port)), timeout=1):
                        break
                except OSError:
                    pass
            time.sleep(1)
        else:
            raise ValueError("Disposable PG readiness timed out")
        env = {**os.environ, "GOWORK": "off", "SOHA_APPLICATION_TEST_POSTGRES_PORT": port, "SOHA_CATALOG_TEST_POSTGRES_PORT": port,
               "SOHA_TEST_POSTGRES_HOST": host, "SOHA_TEST_POSTGRES_PASSWORD": password}
        with args.output.open("w") as log, args.output.with_suffix('.stderr.log').open('w') as diagnostics:
            parent = "TestFeedSearchWithPostgres" if args.feed_search else "TestKnowledgeVectorIndexesWithPostgres" if args.knowledge_ann else "TestPersonalEnvironmentWithPostgres" if args.personal_environment else "TestKnowledgeRetrievalWithPostgres" if args.knowledge else "TestNativeRunWithPostgres" if args.native_harness else "TestMainSessionWithPostgres" if args.main_chat else "TestApplicationAndClusterDeletionWithPostgres"
            if args.identity:
                parent = "TestSessionAuthorizationWithPostgres"
            if args.cli_auth:
                parent = "TestCLIAuthenticationWithPostgres"
            if args.identity_restart:
                parent = "TestPersistentIdentitySurvivesServiceRestartWithPostgres"
            if args.virtualization_capacity:
                parent = "TestCapacityAdmissionWithPostgres"
            if args.network_navigation:
                parent = "TestNetworkNavigationUpgradeWithPostgres"
            if args.docker_identity:
                parent = "TestDockerResourceIdentityWithPostgres"
            if args.docker_project_configuration:
                parent = "TestDockerProjectConfigurationWithPostgres"
            if args.workflow_requests:
                parent = "TestWorkflowRequestReceiptsWithPostgres"
            if args.protected_storage:
                parent = "TestPrivateDurableStorageWithPostgres"
            if args.docker_configuration_mutations:
                parent = "TestDockerConditionalConfigurationWithPostgres"
            if args.docker_runtime_identity:
                parent = "TestDockerRuntimeIdentityWithPostgres"
            if args.conditional_mutations:
                parent = "TestConditionalMutationReceiptsWithPostgres"
            if args.directory_original_requests:
                parent = "TestOriginalDirectorySyncWithPostgres"
            if args.network_command_runtime:
                parent = "TestNetworkCommandRuntimeWithPostgres"
            if args.docker_native_terminals:
                parent = "TestDockerNativeTerminalLeaseWithPostgres"
            if args.healing_requests:
                parent = "TestHealingOriginalRequestWithPostgres"
            if args.mfa_session:
                parent = "TestMFASessionWithPostgres"
            package = "knowledge" if args.knowledge or args.knowledge_ann else "copilot" if args.identity or args.identity_restart or args.cli_auth or args.feed_search or args.main_chat or args.native_harness or args.personal_environment else "application"
            if args.virtualization_capacity:
                package = "virtualization"
            if args.docker_identity or args.docker_project_configuration or args.docker_configuration_mutations or args.docker_runtime_identity:
                package = "docker"
            if args.workflow_requests:
                package = "workflow"
            if args.protected_storage:
                package = "copilot"
            if args.conditional_mutations:
                package = "conditionalmutation"
            if args.directory_original_requests:
                package = "directorysync"
            if args.docker_native_terminals:
                package = "dockerterminal"
            if args.healing_requests:
                package = "alert"
            if args.mfa_session:
                package = "identitymfa"
            test_command = ["go", "test", "-json", "-count=1", "-race", "-timeout=10m", "-run", "^" + parent + "$",
                            "./internal/radiusadapter" if args.network_command_runtime else "./internal/bootstrap" if args.network_navigation else "./internal/repository/" + package]
            if args.native_docker_go:
                spec = importlib.util.spec_from_file_location('native_pg_race', ROOT / 'scripts/docker-native-go-test.py')
                native = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(native)
                manifest['nativeGo'] = {'harnessSHA256': hashlib.sha256(Path(native.__file__).read_bytes()).hexdigest()}
                code = native.run(docker_command, run_id, ROOT, args.contracts_dir, test_command, env, log, diagnostics, manifest['nativeGo'])
            else:
                code = subprocess.run(test_command, cwd=ROOT, env=env, stdout=log, stderr=diagnostics).returncode
        spec = importlib.util.spec_from_file_location("evidence", ROOT / "scripts/check-go-test-evidence.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": ["personal_assistant_identity"]} if args.main_chat else {}
        if args.identity:
            scope = module.IDENTITY_SCOPE
        if args.cli_auth:
            scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": ["device_claim_refresh_replay", "request_bound_code_and_attempts", "browser_subject_origin_and_confirmation", "claim_pkce_and_encrypted_receipt", "cancel_claim_race", "shared_poll_and_admission", "password_account_admission", "active_capacity_hot_requests", "issuance_atomic_rollback", "issued_grant_admission", "atomic_key_rotation", "key_rotation_audit_failure_recovery", "device_decision_recovery", "audit_events_redacted_and_best_effort", "session_policy_and_authority", "login_disabled_existing_sessions", "v1_compatibility", "governed_approval_persistence"]}
        if args.identity_restart:
            scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": []}
        if args.native_harness:
            scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": ["idempotent_request", "owner_isolation", "request_recovery", "lease_fencing", "durable_events", "parent_child_registration", "cancel_fence", "failure_stop_fence", "unknown_outcome_no_replay", "input_resume_gate", "atomic_result", "shared_tool_budget"]}
        if args.knowledge:
            scope = {"package": "github.com/opensoha/soha/internal/repository/knowledge", "parent": parent, "children": ["candidate_window", "acl_revoke_delete", "external_import_scope", "external_import_scope/default-owner", "external_import_scope/explicit-reader", "external_import_scope/explicit-deny", "explicit_source_tombstone", "immutable_current_version", "model_dimension_isolation", "failed_publication_atomic", "legacy_writer_compatibility", "ingestion_staging_takeover"]}
        if args.knowledge_ann:
            scope = {"package": "github.com/opensoha/soha/internal/repository/knowledge", "parent": parent, "children": ["1536", "1536/partial_ann_fallback", "3072", "3072/partial_ann_fallback"]}
        if args.personal_environment:
            scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": ["owner_isolation", "quota_preflight_before_data", "not_dispatched_restore", "file_read_owner_fence", "frozen_binding_sleep_wake", "binding_identity_fence", "unsupported_backend", "legacy_binding_migration", "legacy_writer_compatibility", "unknown_no_replay", "missing_data_not_recreated", "dispatch_cas", "late_receipt_fence", "quota_scope_ceiling", "canceled_receipt_persisted", "current_permission_denied"]}
        if args.feed_search:
            scope = {"package":"github.com/opensoha/soha/internal/repository/copilot","parent":parent,"children":["message_window_owner_and_long_history","feed_read_and_feedback","refresh_claim_and_settings_fence","expired_lease_no_replay"]}
        if args.virtualization_capacity:
            scope = {"package": "github.com/opensoha/soha/internal/repository/virtualization", "parent": parent, "children": ["provider_identity_claim", "worker_pool_admission", "capacity_policy", "ssh_secret_storage", "managed_cloud_image_sync"]}
        if args.network_navigation:
            scope = {"package": "github.com/opensoha/soha/internal/bootstrap", "parent": parent, "children": ["disabled_upgrade_stays_pending", "failure_rolls_back", "upgrade_preserves_custom_data_and_bindings", "previous_task52_marker_recovers_without_reseed", "second_start_is_idempotent"]}
        if args.docker_identity:
            scope = {"package": "github.com/opensoha/soha/internal/repository/docker", "parent": parent, "children": ["legacy_backfill", "insert_ignores_caller_identity", "configuration_fields_rotate", "runtime_updates_stable", "identity_updates_rejected", "repository_return_matches_storage", "delete_recreate_aba", "concurrent_conditional_update", "transaction_rollback", "legacy_json_stays_private", "migration_sql_replay_stable", "concurrent_migration_replay", "migration_rerun_stable"]}
        if args.docker_project_configuration:
            scope = {"package": "github.com/opensoha/soha/internal/repository/docker", "parent": parent, "children": ["create_definition_is_not_observation", "configuration_preserves_runtime", "explicit_legacy_state_retained", "create_rolls_back_definitions", "update_rolls_back_config_revision_and_inventory", "idempotent_create_rolls_back_receipt", "shared_transaction_rollback", "runtime_update_and_host_fence", "runtime_readback_failure_rolls_back", "cold_host_move_and_observed_move_fence", "legacy_definition_evidence_retained", "bound_ports_and_operations_block_move", "terminal_history_requires_relocation", "stale_reference_writes_rejected", "writers_wait_for_project_lock", "removed_names_do_not_delete_inventory"]}
        if args.workflow_requests:
            scope = module.WORKFLOW_REQUEST_SCOPE
        if args.protected_storage:
            scope = module.PROTECTED_STORAGE_SCOPE
        if args.docker_configuration_mutations:
            scope = module.DOCKER_CONFIGURATION_SCOPE
        if args.docker_runtime_identity:
            scope = module.DOCKER_RUNTIME_IDENTITY_SCOPE
        if args.conditional_mutations:
            scope = {"package": "github.com/opensoha/soha/internal/repository/conditionalmutation", "parent": parent,
                     "children": ["concurrent_original_claim", "changed_input_and_credential", "actor_isolation", "encrypted_reopen", "terminal_compare_and_set", "crashed_unknown_no_replay", "expired_payload_tombstone", "bounded_admission"]}
        if args.directory_original_requests:
            scope = {"package": "github.com/opensoha/soha/internal/repository/directorysync", "parent": parent,
                     "children": ["concurrent_original_claim", "different_active_intent_conflicts", "actor_and_credential_binding", "terminal_reopen_retains_original", "running_cancel_keeps_active_slot", "legacy_cancel_writer_cannot_release_original_slot", "queued_cancel_wins_dispatch_cas", "heartbeat_and_stale_key_retention", "migration_and_legacy_runs_compatible"]}
        if args.network_command_runtime:
            scope = {"package": "github.com/opensoha/soha/internal/radiusadapter", "parent": parent,
                     "children": ["mtls_enrollment_and_runtime_identity", "original_lookup_parent_and_plan_binding", "real_callback_commit_then_ack_loss", "reopen_journal_replays_result_without_execution", "revoked_runtime_cannot_poll_or_callback"]}
        if args.mfa_session:
            scope = {"package": "github.com/opensoha/soha/internal/repository/identitymfa", "parent": parent,
                     "children": ["complete_totp_preserves_cli_metadata", "recent_mfa_is_session_bound", "revoked_session_rolls_back_challenge_consumption"]}
        if args.healing_requests:
            scope = {"package": "github.com/opensoha/soha/internal/repository/alert", "parent": parent,
                     "children": ["atomic_request_and_duplicate", "different_input_or_credential_denied", "concurrent_decision_single_winner", "stale_generation_cannot_overwrite", "receipt_tombstone_no_recreate", "failed_creation_rolls_back_claim"]}
        if args.docker_native_terminals:
            scope = {"package":"github.com/opensoha/soha/internal/repository/dockerterminal", "parent":parent,
                     "children":["actor_and_credential_binding","concurrent_ticket_once","reopened_encrypted_authorization","close_active_then_terminal_cas","close_pending_consumes_ticket","expired_pending_never_consumed","bounded_admission","crashed_active_unknown_and_retention","global_admission"]}
        evidence = module.validate(args.output.read_text(), code, **scope)
        manifest['result'] = evidence
    except Exception as error:
        manifest['failure'] = str(error)
        raise
    finally:
        try:
            manifest['cleanup'] = 'FAIL'
            manifest['cleanup'] = cleanup_postgres(docker_command, container, postgres_name, run_id, postgres_attempted)
        except Exception:
            manifest['cleanup'] = 'FAIL'
            raise
        finally:
            args.output.with_suffix('.manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest))


if __name__ == "__main__":
    guard = shutil.which('opensoha-verify')
    if (os.environ.get('GITHUB_ACTIONS') != 'true' and not os.environ.get('OPENSOHA_VERIFY_ACTIVE')
            and (guard or os.environ.get('OPENSOHA_LOCAL_VERIFY_REQUIRED'))):
        os.execvp(guard or 'opensoha-verify', [guard or 'opensoha-verify', 'run', '--', sys.executable, __file__, *sys.argv[1:]])
    main()
