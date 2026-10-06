#!/usr/bin/env python3
"""Own a disposable PG instance and remove only the container created by this run."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
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
        env = {**os.environ, "GOWORK": "off", "SOHA_APPLICATION_TEST_POSTGRES_PORT": port}
        with args.output.open("w") as log, args.output.with_suffix('.stderr.log').open('w') as diagnostics:
            result = subprocess.run(["go", "test", "-json", "-count=1", "-race", "-timeout=10m",
                                     "-run", "^TestApplicationAndClusterDeletionWithPostgres$",
                                     "./internal/repository/application"], cwd=ROOT, env=env, stdout=log, stderr=diagnostics)
        spec = importlib.util.spec_from_file_location("evidence", ROOT / "scripts/check-go-test-evidence.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        evidence = module.validate(args.output.read_text(), result.returncode)
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
    main()
