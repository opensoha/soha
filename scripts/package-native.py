#!/usr/bin/env python3
"""Package the same-release server and required public installation files."""

import argparse
import gzip
import hashlib
from pathlib import Path
import tarfile


ROOT = Path(__file__).resolve().parents[1]


def package(binary: Path, output: Path) -> None:
    checksum = output.with_name(output.name + ".sha256")
    if output.exists() or output.is_symlink() or checksum.exists() or checksum.is_symlink():
        raise ValueError("refusing to overwrite an existing release artifact")
    migrations = sorted((ROOT / "migrations/postgres").glob("*.sql"))
    if ROOT / "migrations/postgres/0001_init.sql" not in migrations:
        raise ValueError("missing initial migration")
    files = [(binary, "soha"), (ROOT / "LICENSE", "LICENSE"),
             (ROOT / "configs/config.example.yaml", "configs/config.example.yaml"),
             (ROOT / "deploy/native/README.md", "README.md")]
    files += [(path, path.relative_to(ROOT).as_posix()) for path in migrations]
    for source, _ in files:
        repo_paths = (path for path in (source, *source.parents) if path != ROOT and path.is_relative_to(ROOT))
        if any(path.is_symlink() for path in repo_paths):
            raise ValueError(f"symlink input: {source}")
        if source.is_symlink() or not source.is_file() or source.stat().st_size == 0:
            raise ValueError(f"missing, empty or symlink input: {source}")
    with output.open("xb") as raw:
        receipt_created = False
        try:
            with gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=0) as zipped:
                with tarfile.open(fileobj=zipped, mode="w|") as archive:
                    for source, relative in files:
                        info = tarfile.TarInfo("soha/" + relative)
                        info.size = source.stat().st_size
                        info.mode = 0o755 if relative == "soha" else 0o644
                        with source.open("rb") as content:
                            archive.addfile(info, content)
            raw.flush()
            with output.open("rb") as content:
                digest = hashlib.file_digest(content, "sha256").hexdigest()
            with checksum.open("x") as receipt:
                receipt_created = True
                receipt.write(f"{digest}  {output.name}\n")
        except BaseException:
            output.unlink()
            if receipt_created:
                checksum.unlink()
            raise


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    package(args.binary.absolute(), args.output.absolute())
