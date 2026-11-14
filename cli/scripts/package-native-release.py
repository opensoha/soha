#!/usr/bin/env python3
"""Package one native binary with deterministic release archive metadata."""
import argparse
from datetime import datetime, timezone
import gzip
from pathlib import Path
import stat
import tarfile
import zipfile


def package(binary, archive, epoch):
    binary, archive = Path(binary), Path(archive)
    if binary.is_symlink() or not binary.is_file() or epoch < 0 or archive.is_symlink() or binary.resolve() == archive.resolve():
        raise ValueError("release input must be a regular binary and nonnegative commit epoch")
    if archive.name.endswith(".tar.gz"):
        with archive.open("wb") as output, gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as bundle:
                info = tarfile.TarInfo(binary.name)
                info.size = binary.stat().st_size
                info.mode = 0o755
                info.mtime = epoch
                with binary.open("rb") as source:
                    bundle.addfile(info, source)
    elif archive.suffix == ".zip":
        # ZIP timestamps begin in 1980 and have two-second resolution.
        date = datetime.fromtimestamp(max(epoch, 315532800), timezone.utc)
        info = zipfile.ZipInfo(binary.name, date.timetuple()[:6])
        info.create_system = 3
        info.external_attr = (stat.S_IFREG | 0o755) << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        with zipfile.ZipFile(archive, "w") as bundle:
            bundle.writestr(info, binary.read_bytes())
    else:
        raise ValueError("release archive must end in .tar.gz or .zip")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("archive", type=Path)
    parser.add_argument("epoch", type=int)
    args = parser.parse_args()
    package(args.binary, args.archive, args.epoch)


if __name__ == "__main__":
    main()
