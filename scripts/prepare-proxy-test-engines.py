#!/usr/bin/env python3
"""Prepare pinned native binaries for the real proxy integration tests."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import stat
import tarfile
import tempfile
import urllib.request
import zipfile

LIMIT = 100 * 1024 * 1024
METADATA = Path(__file__).with_name('proxy-test-engines.json')


def copy_bounded(src, dst):
    size = 0
    while chunk := src.read(1024 * 1024):
        size += len(chunk)
        if size > LIMIT:
            raise ValueError('size limit exceeded')
        dst.write(chunk)


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def safe_name(name):
    p = PurePosixPath(name)
    if p.is_absolute() or '..' in p.parts or '\\' in name:
        raise ValueError('unsafe archive path')


def extract(archive, target, member):
    if archive.name.endswith('.tar.gz'):
        with tarfile.open(archive, 'r:gz') as t:
            entries = t.getmembers()
            for e in entries:
                safe_name(e.name)
                if e.issym() or e.islnk():
                    raise ValueError('archive link')
            matches = [e for e in entries if e.name == member and e.isfile()]
            if len(matches) != 1:
                raise ValueError('missing or duplicate binary')
            with t.extractfile(matches[0]) as src, target.open('xb') as dst:
                copy_bounded(src, dst)
    elif archive.name.endswith('.zip'):
        with zipfile.ZipFile(archive) as z:
            entries = z.infolist()
            for e in entries:
                safe_name(e.filename)
                if stat.S_ISLNK(e.external_attr >> 16):
                    raise ValueError('archive link')
            matches = [e for e in entries if e.filename == member and not e.is_dir()]
            if len(matches) != 1:
                raise ValueError('missing or duplicate binary')
            with z.open(matches[0]) as src, target.open('xb') as dst:
                copy_bounded(src, dst)
    else:
        with gzip.open(archive, 'rb') as src, target.open('xb') as dst:
            copy_bounded(src, dst)
    target.chmod(0o700)


def prepare(output, platform_name, metadata):
    if platform_name not in ('linux-amd64', 'darwin-arm64'):
        raise ValueError('unsupported platform')
    if output.is_symlink():
        raise ValueError('output is a symlink')
    if output.exists():
        manifest_path = output / 'manifest.json'
        if not output.is_dir() or manifest_path.is_symlink() or not manifest_path.is_file():
            raise ValueError('unknown existing output')
        manifest = json.loads(manifest_path.read_text())
        if manifest.get('platform') != platform_name or set(p.name for p in output.iterdir()) != set(metadata) | {'manifest.json'}:
            raise ValueError('unknown existing output')
        for name, info in metadata.items():
            f = output / name
            entry = manifest.get('engines', {}).get(name, {})
            if f.is_symlink() or not f.is_file() or entry.get('version') != info['version'] or entry.get('archive_sha256') != info['platforms'][platform_name]['sha256'] or entry.get('binary_sha256') != info['platforms'][platform_name]['binary_sha256'] or digest(f) != info['platforms'][platform_name]['binary_sha256'] or not os.access(f, os.X_OK):
                raise ValueError('existing output failed verification')
        return manifest
    if not output.parent.is_dir():
        raise ValueError('output parent must exist')
    with tempfile.TemporaryDirectory(prefix='.proxy-engines-', dir=output.parent) as tmp:
        stage = Path(tmp) / 'engines'
        stage.mkdir(mode=0o700)
        manifest = {'platform': platform_name, 'engines': {}}
        for name, info in metadata.items():
            asset = info['platforms'][platform_name]
            archive = Path(tmp) / asset['url'].rsplit('/', 1)[-1]
            request = urllib.request.Request(asset['url'], headers={'User-Agent': 'opensoha-proxy-test-fixtures'})
            with urllib.request.urlopen(request, timeout=60) as src, archive.open('xb') as dst:
                if not src.geturl().startswith('https://'):
                    raise ValueError('HTTPS required')
                copy_bounded(src, dst)
            if digest(archive) != asset['sha256']:
                raise ValueError('archive digest mismatch: ' + name)
            extract(archive, stage / name, asset['member'])
            if digest(stage / name) != asset['binary_sha256']:
                raise ValueError('binary digest mismatch: ' + name)
            manifest['engines'][name] = {'version': info['version'], 'archive': asset['url'].rsplit('/', 1)[-1], 'archive_sha256': asset['sha256'], 'binary_sha256': digest(stage / name)}
        (stage / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        # Rename only after exclusive creation reserves the requested name.
        output.mkdir(mode=0o700)
        try:
            for p in stage.iterdir():
                p.rename(output / p.name)
        except Exception:
            shutil.rmtree(output)
            raise
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--platform', choices=('linux-amd64', 'darwin-arm64'))
    args = parser.parse_args()
    system = {'Linux': 'linux', 'Darwin': 'darwin'}.get(platform.system(), '')
    machine = {'x86_64': 'amd64', 'amd64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}.get(platform.machine().lower(), '')
    selected = args.platform or f'{system}-{machine}'
    print(json.dumps(prepare(args.output, selected, json.loads(METADATA.read_text())), indent=2))


if __name__ == '__main__':
    main()
