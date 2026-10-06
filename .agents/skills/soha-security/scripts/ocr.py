#!/usr/bin/env python3
"""Thin adapter for OCR 1.12.11 native preview/run-manifest JSON. No model calls by default."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tempfile
import os
import shutil
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse
from urllib.request import Request, build_opener, HTTPRedirectHandler
from uuid import uuid4

VERSION = "1.12.11"


def safe_path(path):
    if not isinstance(path, str) or not path or path == '.' or str(PurePosixPath(path)) != path or PurePosixPath(path).is_absolute() or ".." in PurePosixPath(path).parts or "\\" in path:
        raise ValueError("Invalid report path")
    return path


def normalize(preview, result, exit_code=0, line_counts=None, expected_range=None):
    if not isinstance(preview, dict) or not isinstance(preview.get("files"), list):
        raise ValueError("Invalid native preview")
    paths = []
    for item in preview['files']:
        if not isinstance(item, dict) or not isinstance(item.get('will_review'), bool):
            raise ValueError('Invalid native preview file fields')
        paths.append(safe_path(item.get('path')))
    if len(set(paths)) != len(paths): raise ValueError('Duplicate preview file')
    selected = {safe_path(f["path"]) for f in preview["files"] if f.get("will_review") is True}
    excluded = [{"path": safe_path(f["path"]), "reason": f.get("exclude_reason", "not selected")} for f in preview["files"] if f.get("will_review") is not True]
    if exit_code:
        return {"coverageStatus": "ERROR", "findingStatus": "UNKNOWN", "reason": "process failure", "excluded": excluded}
    if not isinstance(result, dict) or not isinstance(result.get("comments"), list) or not isinstance(result.get("status"), str):
        raise ValueError("Missing native review fields")
    manifest = result.get("manifest")
    if not isinstance(manifest, dict) or manifest.get("schema_version") != "ocr.run-manifest/v1":
        raise ValueError("Required native manifest schema missing/mismatched")
    if result['status'] not in ('complete', 'partial', 'failed', 'skipped') or manifest.get('terminal_state') != result['status']:
        raise ValueError('Unknown or inconsistent native terminal status')
    if expected_range and (manifest.get('input', {}).get('resolved_base'), manifest.get('input', {}).get('resolved_head')) != expected_range:
        raise ValueError('Native result does not match exact requested range')
    if not isinstance(result.get('tool_calls'), int) or isinstance(result['tool_calls'], bool) or result['tool_calls'] < 0:
        raise ValueError('Missing native tool_calls')
    coverage = manifest.get("coverage")
    if not isinstance(coverage, dict):
        raise ValueError("Missing native coverage")
    sets = {}
    for kind in ("selected", "completed", "reused", "failed", "waived"):
        items = coverage.get(kind)
        if not isinstance(items, list):
            raise ValueError("Invalid native coverage array: " + kind)
        sets[kind] = {(item["item_id"], safe_path(item["path"])) for item in items if isinstance(item, dict) and isinstance(item.get("item_id"), str) and item['item_id']}
        if len(sets[kind]) != len(items) or len({identifier for identifier, _ in sets[kind]}) != len(items):
            raise ValueError("Missing or duplicate native coverage item")
    if {p for _, p in sets["selected"]} != selected:
        raise ValueError("Preview and review scope differ")
    completed = sets["completed"] | sets["reused"]
    accounted = completed | sets["failed"] | sets["waived"]
    if accounted - sets["selected"]:
        raise ValueError("Out-of-scope coverage")
    terminals = [sets[kind] for kind in ('completed', 'reused', 'failed', 'waived')]
    if sum(len(group) for group in terminals) != len(accounted):
        raise ValueError('Overlapping coverage dispositions')
    if manifest.get("execution", {}).get("ocr_version") not in (VERSION, "v" + VERSION):
        raise ValueError("Native OCR version mismatch")
    findings = []
    for comment in result["comments"]:
        path = safe_path(comment.get("path"))
        start, end = comment.get("start_line"), comment.get("end_line")
        if path not in selected or not isinstance(start, int) or isinstance(start, bool) or not isinstance(end, int) or isinstance(end, bool) or start < 0 or end < start or (start == 0) != (end == 0) or not isinstance(comment.get("content"), str) or not comment['content'].strip():
            raise ValueError("Invalid or out-of-scope finding")
        if end and (line_counts is None or path not in line_counts or end > line_counts[path]):
            raise ValueError('Finding line bounds require verified source line counts')
        key = hashlib.sha256(f'{path}:{start}:{end}:{comment["content"]}'.encode()).hexdigest()
        if not any(item["id"] == key for item in findings):
            findings.append({"id": key, "path": path, "start": start, "end": end, "content": comment["content"], "triage": "PENDING"})
    if not selected:
        if result['status'] != 'skipped' or accounted or findings:
            raise ValueError('Zero selected files require a genuine skipped run')
        return {"coverageStatus": "NOT_APPLICABLE", "findingStatus": "UNKNOWN", "excluded": excluded}
    if result['status'] in ('failed', 'skipped'):
        return {"coverageStatus": "ERROR", "findingStatus": "UNKNOWN", "reason": 'native ' + result['status'], "excluded": excluded}
    partial = (completed != sets["selected"] or bool(sets["failed"] or sets["waived"]) or bool(result.get("warnings")) or result.get("summary", {}).get("budget_exceeded") or result["status"] == "partial")
    return {"schema": "soha.ocr-summary/v1", "coverageStatus": "PARTIAL" if partial else "COMPLETE", "findingStatus": "NEEDS_TRIAGE" if findings else ("UNKNOWN" if partial else "NO_FINDINGS"), "selected": sorted(selected), "excluded": excluded, "findings": findings}


def git(repo, *args):
    return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()


def resolve_range(repo, base, head):
    repo = Path(repo).resolve()
    if Path(git(repo, "rev-parse", "--show-toplevel")).resolve() != repo:
        raise ValueError("Explicit Git root required")
    for ref in (base, head):
        if not re.fullmatch(r"[a-f0-9]{40}", ref):
            raise ValueError("Exact SHA required; ref strings are not executed")
        git(repo, "cat-file", "-e", ref + "^{commit}")
    if git(repo, "merge-base", base, head) != base:
        raise ValueError("Base must be the exact ancestor, no hidden merge-base range")
    return repo


def container(image, repo=None, rule=None, output=None, network='none'):
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
        raise ValueError('BLOCKED: exact locally built OCR image ID required')
    run_id = uuid4().hex
    command = ['docker', 'run', '--rm', '--name=soha-ocr-' + run_id, '--label=soha.test.run=' + run_id, '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--pids-limit=128', '--memory=1g', '--network=' + network, '--tmpfs=/tmp:rw,size=128m', '--tmpfs=/home/node:rw,size=64m,uid=1000,gid=1000', '-e', 'GIT_CONFIG_COUNT=1', '-e', 'GIT_CONFIG_KEY_0=safe.directory', '-e', 'GIT_CONFIG_VALUE_0=/repo']
    if repo: command += ['--mount', f'type=bind,src={repo},dst=/repo,readonly', '--workdir=/repo']
    if rule: command += ['--mount', f'type=bind,src={rule},dst=/rule.json,readonly']
    if output: command += ['--mount', f'type=bind,src={output},dst=/out']
    return command


def run_container(command, **options):
    name = next(item.split('=', 1)[1] for item in command if item.startswith('--name='))
    run_id = next(item.split('=', 2)[2] for item in command if item.startswith('--label=soha.test.run='))
    try: return subprocess.run(command, **options)
    finally:
        inspected = subprocess.run(['docker', 'inspect', name], capture_output=True, text=True)
        if inspected.returncode and not any(reason in inspected.stderr.lower() for reason in ('no such object:', 'no such container:')):
            raise ValueError('Cannot verify OCR container cleanup state')
        if inspected.returncode == 0:
            instance = json.loads(inspected.stdout)[0]
            if instance['Config']['Labels'].get('soha.test.run') != run_id: raise ValueError('Cleanup ownership mismatch')
            subprocess.run(['docker', 'rm', '-f', instance['Id']], check=True, stdout=subprocess.DEVNULL)


def approved_model(env):
    if env.get('SOHA_OCR_ENABLED') != '1':
        raise ValueError('NOT_RUN: SOHA_OCR_ENABLED=1 not explicitly requested')
    for name in ('SOHA_OCR_APPROVAL_ID', 'SOHA_OCR_ALLOWED_HOST', 'OCR_LLM_URL', 'OCR_LLM_TOKEN', 'OCR_LLM_MODEL'):
        if not env.get(name): raise ValueError('BLOCKED: missing ' + name)
    endpoint = urlparse(env['OCR_LLM_URL'])
    if endpoint.scheme != 'https' or endpoint.hostname != env['SOHA_OCR_ALLOWED_HOST'] or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment:
        raise ValueError('BLOCKED: unapproved model endpoint')
    return endpoint


def trusted_dispatch(env, base, head):
    if env.get('GITHUB_REF') != 'refs/heads/main' or not re.fullmatch(r'[a-f0-9]{40}', env.get('GITHUB_SHA', '')) or env.get('GITHUB_SHA') != env.get('SOHA_OCR_APPROVED_CODE_SHA'):
        raise ValueError('BLOCKED: approved main test code SHA required')
    for name, value in (('BASE', base), ('HEAD', head)):
        if not re.fullmatch(r'[a-f0-9]{40}', value) or value != env.get('SOHA_OCR_APPROVED_' + name): raise ValueError('BLOCKED: approved exact ' + name + ' required')


def broker(env):
    endpoint = approved_model(env)
    capability = uuid4().hex
    state = {'calls': 0, 'rejected': 0, 'cost': 'UNKNOWN', 'host': endpoint.hostname}
    lock = threading.Lock()

    class NoRedirect(HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs): raise ValueError('Model redirect rejected')

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args): pass
        def do_POST(self):
            with lock:
                if self.path != '/v1/chat/completions' or self.headers.get('Authorization') != 'Bearer ' + capability or state['calls'] >= 12:
                    state['rejected'] += 1; self.send_error(403); return
                state['calls'] += 1
            try:
                size = int(self.headers.get('Content-Length', '0'))
                if not 0 < size <= 512 * 1024: raise ValueError('Model request size rejected')
                data = self.rfile.read(size)
                request = Request(env['OCR_LLM_URL'].rstrip('/') + '/chat/completions', data=data, headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + env['OCR_LLM_TOKEN']})
                with build_opener(NoRedirect).open(request, timeout=60) as response:
                    body = response.read(10 * 1024 * 1024 + 1)
                    if len(body) > 10 * 1024 * 1024: raise ValueError('Model response size rejected')
                    self.send_response(response.status); self.send_header('Content-Type', response.headers.get('Content-Type', 'application/json')); self.end_headers(); self.wfile.write(body)
            except Exception:
                state['rejected'] += 1; self.send_error(502, 'Model unavailable/rejected')

    server = ThreadingHTTPServer(('0.0.0.0', 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
    return server, capability, state


def exported_source(repo, head, destination):
    # No untracked files, user config, hooks or credentials are mounted in the tool container.
    env = {**os.environ, 'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null'}
    def command(*args): subprocess.run(['git', '-c', 'core.hooksPath=/dev/null', *args], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    command('init', '-q', str(destination))
    command('-C', str(destination), 'fetch', '--no-tags', str(repo), head)
    command('-C', str(destination), 'checkout', '--detach', head)
    for path in destination.rglob('*'):
        if path.is_symlink(): raise ValueError('BLOCKED: source symlinks require an explicit safe export')
    return destination


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    parse = sub.add_parser("normalize")
    parse.add_argument("preview", type=Path)
    parse.add_argument("result", type=Path)
    parse.add_argument("--exit-code", type=int, required=True)
    parse.add_argument('--line-counts', type=Path, required=True)
    parse.add_argument('--base', required=True)
    parse.add_argument('--head', required=True)
    preview = sub.add_parser("preview")
    preview.add_argument("--repo", required=True)
    preview.add_argument("--base", required=True)
    preview.add_argument("--head", required=True)
    preview.add_argument('--image', required=True)
    preview.add_argument("--output", type=Path, required=True)
    managed = sub.add_parser('review')
    for name in ('repo', 'base', 'head', 'image'): managed.add_argument('--' + name, required=True)
    managed.add_argument('--output', type=Path, required=True)
    guard = sub.add_parser('ci-guard')
    guard.add_argument('--base', required=True)
    guard.add_argument('--head', required=True)
    args = parser.parse_args()
    if args.command == 'ci-guard':
        trusted_dispatch(os.environ, args.base, args.head)
        print('Approved trusted test code and exact review range')
        return 0
    if args.command == "normalize":
        for ref in (args.base, args.head):
            if not re.fullmatch(r'[a-f0-9]{40}', ref): raise ValueError('Exact source SHA required')
        output = normalize(json.loads(args.preview.read_text()), json.loads(args.result.read_text()), args.exit_code, json.loads(args.line_counts.read_text()), (args.base, args.head))
        print(json.dumps(output, ensure_ascii=False))
        return 0 if output["coverageStatus"] in ("COMPLETE", "NOT_APPLICABLE") else 1
    if args.command == 'review': approved_model(os.environ)
    repo = resolve_range(args.repo, args.base, args.head)
    version = run_container(container(args.image) + [args.image, '--version'], check=True, capture_output=True, text=True, timeout=30).stdout
    if not re.search(r"\bv?1\.12\.11\b", version):
        raise ValueError("BLOCKED: pinned OCR 1.12.11 required")
    trusted_rule = git(repo, "show", args.base + ":.opencodereview/rule.json")
    json.loads(trusted_rule)
    with tempfile.TemporaryDirectory(prefix="soha-ocr-rule-") as directory:
        rule = Path(directory) / "rule.json"
        rule.write_text(trusted_rule)
        output_dir = Path(directory) / 'out'; output_dir.mkdir(mode=0o777); output_dir.chmod(0o777)
        source = exported_source(repo, args.head, Path(directory) / 'source')
        # OCR also loads project fallback rules: pin that copy to the approved base.
        project_rule = source / '.opencodereview/rule.json'
        project_rule.parent.mkdir(exist_ok=True)
        project_rule.write_text(trusted_rule)
        # Fetching the head retains its ancestor base; the adapter already checked exact ancestry.
        common = ['review', '--repo', '/repo', '--from', args.base, '--to', args.head, '--rule', '/rule.json', '--audience', 'agent', '--format', 'json']
        run_container(container(args.image, source, rule, output_dir) + [args.image, *common, '--preview', '--output', '/out/preview.json'], check=True, timeout=120, stdout=subprocess.DEVNULL)
        preview_data = json.loads((output_dir / 'preview.json').read_text())
        for item in preview_data['files']: safe_path(item['path'])
        args.output.parent.mkdir(parents=True, exist_ok=True)
        if args.command == 'preview':
            shutil.copyfile(output_dir / 'preview.json', args.output)
            print(json.dumps({'mode': 'preview-only', 'base': args.base, 'head': args.head, 'ruleDigest': hashlib.sha256(trusted_rule.encode()).hexdigest(), 'result': 'NOT_RUN', 'reason': 'No model review requested', 'selected': sum(f['will_review'] for f in preview_data['files'])}))
            return 0
        approved_model(os.environ)
        server, capability, state = broker(os.environ)
        try:
            command = container(args.image, source, rule, output_dir, 'bridge') + ['--add-host=host.docker.internal:host-gateway', '-e', f'OCR_LLM_URL=http://host.docker.internal:{server.server_port}/v1', '-e', 'OCR_LLM_TOKEN=' + capability, '-e', 'OCR_LLM_MODEL=' + os.environ['OCR_LLM_MODEL'], args.image, *common, '--tools', '[]', '--concurrency', '1', '--timeout', '600', '--max-tokens', '2048', '--max-tokens-budget', '20000', '--output', '/out/review.json']
            process = run_container(command, timeout=720, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            native = json.loads((output_dir / 'review.json').read_text()) if (output_dir / 'review.json').exists() else {}
            counts = {item['path']: len(git(source, 'show', args.head + ':' + item['path']).splitlines()) for item in preview_data['files'] if item['will_review'] and (source / item['path']).is_file()}
            result = normalize(preview_data, native, process.returncode, counts, (args.base, args.head))
            if state['rejected']: result['coverageStatus'] = 'ERROR'; result['findingStatus'] = 'UNKNOWN'
            # Reports remain local; public CI artifacts contain only coverage and fingerprints.
            result.update(mode='OCR-managed', base=args.base, head=args.head, model=os.environ['OCR_LLM_MODEL'], approvalId=os.environ['SOHA_OCR_APPROVAL_ID'], budget=state, image=args.image)
            args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2))
            print(json.dumps({key: value for key, value in result.items() if key != 'findings'}))
            return 0 if result['coverageStatus'] in ('COMPLETE', 'NOT_APPLICABLE') else 1
        except Exception as error:
            args.output.write_text(json.dumps({'mode': 'OCR-managed', 'base': args.base, 'head': args.head, 'coverageStatus': 'ERROR', 'findingStatus': 'UNKNOWN', 'reason': type(error).__name__, 'budget': state}, indent=2))
            raise
        finally: server.shutdown(); server.server_close()
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as exc:
        raise SystemExit("OCR adapter ERROR/BLOCKED: " + str(exc))
