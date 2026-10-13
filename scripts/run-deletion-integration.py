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

ROOT = Path(__file__).resolve().parents[1]
IMAGE = "pgvector/pgvector:0.8.5-pg18-trixie"


def command(args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def check_owner(info, container_id, run_id):
    if info.get("Id") != container_id or info.get("Config", {}).get("Labels", {}).get("soha.test.run") != run_id or info.get("Config", {}).get("Image") != IMAGE:
        raise ValueError("Container ownership changed; refusing cleanup")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=ROOT / ".tmp/quality/deletion-test.json")
    parser.add_argument("--main-chat", action="store_true")
    parser.add_argument("--native-harness", action="store_true")
    parser.add_argument("--knowledge", action="store_true")
    parser.add_argument("--knowledge-ann", action="store_true")
    parser.add_argument("--personal-environment", action="store_true")
    parser.add_argument("--identity", action="store_true")
    parser.add_argument("--virtualization-capacity", action="store_true")
    parser.add_argument("--feed-search",action="store_true")
    args = parser.parse_args()
    run_id = str(uuid.uuid4())
    args.output.parent.mkdir(parents=True, exist_ok=True)
    digest = hashlib.sha256(subprocess.check_output(['git', 'diff', 'HEAD', '--binary'], cwd=ROOT))
    for name in subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '-z'], cwd=ROOT).decode().split('\0'):
        if name: digest.update(name.encode()); digest.update((ROOT / name).read_bytes())
    manifest = {'runId': run_id, 'mode': 'postgres-real', 'coreCommit': command(['git', 'rev-parse', 'HEAD'], cwd=ROOT), 'dirtyPatchDigest': digest.hexdigest(), 'result': 'FAIL', 'cleanup': 'NOT_RUN'}
    container = None
    try:
        container = command(["docker", "run", "--detach", "--rm", "--label", "soha.test.run=" + run_id,
                             "--publish", "127.0.0.1::5432", "--env", "POSTGRES_DB=soha",
                             "--env", "POSTGRES_USER=pgsql", "--env", "POSTGRES_PASSWORD=test-only", IMAGE], timeout=90)
        info = json.loads(command(["docker", "inspect", container]))[0]
        check_owner(info, container, run_id)
        manifest['environment'] = {'id': container, 'disposable': True, 'image': IMAGE, 'imageDigest': info['Image']}
        port = info["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostPort"]
        if info["NetworkSettings"]["Ports"]["5432/tcp"][0]["HostIp"] != "127.0.0.1":
            raise ValueError("PG must bind loopback")
        for _ in range(60):
            if subprocess.run(["docker", "exec", container, "pg_isready", "-U", "pgsql", "-d", "soha"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                break
            time.sleep(1)
        else:
            raise ValueError("Disposable PG readiness timed out")
        env = {**os.environ, "GOWORK": "off", "SOHA_APPLICATION_TEST_POSTGRES_PORT": port, "SOHA_CATALOG_TEST_POSTGRES_PORT": port}
        with args.output.open("w") as log, args.output.with_suffix('.stderr.log').open('w') as diagnostics:
            parent = "TestFeedSearchWithPostgres" if args.feed_search else "TestKnowledgeVectorIndexesWithPostgres" if args.knowledge_ann else "TestPersonalEnvironmentWithPostgres" if args.personal_environment else "TestKnowledgeRetrievalWithPostgres" if args.knowledge else "TestNativeRunWithPostgres" if args.native_harness else "TestMainSessionWithPostgres" if args.main_chat else "TestApplicationAndClusterDeletionWithPostgres"
            if args.identity:
                parent = "TestSessionAuthorizationWithPostgres"
            if args.virtualization_capacity:
                parent = "TestCapacityAdmissionWithPostgres"
            package = "knowledge" if args.knowledge or args.knowledge_ann else "copilot" if args.identity or args.feed_search or args.main_chat or args.native_harness or args.personal_environment else "application"
            if args.virtualization_capacity:
                package = "virtualization"
            result = subprocess.run(["go", "test", "-json", "-count=1", "-race", "-timeout=10m",
                                     "-run", "^" + parent + "$",
                                     "./internal/repository/" + package], cwd=ROOT, env=env, stdout=log, stderr=diagnostics)
        spec = importlib.util.spec_from_file_location("evidence", ROOT / "scripts/check-go-test-evidence.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        scope = {"package": "github.com/opensoha/soha/internal/repository/copilot", "parent": parent, "children": ["personal_assistant_identity"]} if args.main_chat else {}
        if args.identity:
            scope = module.IDENTITY_SCOPE
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
        evidence = module.validate(args.output.read_text(), result.returncode, **scope)
        manifest['result'] = evidence
    except Exception as error:
        manifest['failure'] = str(error)
        raise
    finally:
        try:
            inspected = subprocess.run(['docker', 'inspect', container], text=True, capture_output=True) if container else None
            if inspected is None:
                manifest['cleanup'] = 'NOT_CREATED'
            elif inspected.returncode and any(reason in inspected.stderr.lower() for reason in ('no such object:', 'no such container:')):
                manifest['cleanup'] = 'ALREADY_REMOVED'
            else:
                if inspected.returncode: raise ValueError('Cannot verify container cleanup state')
                info = json.loads(inspected.stdout)[0]
                check_owner(info, container, run_id)
                subprocess.run(["docker", "rm", "--force", container], check=True, stdout=subprocess.DEVNULL)
                manifest['cleanup'] = 'PASS'
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
