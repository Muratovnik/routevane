#!/usr/bin/env python3
"""Package a release with explicit portable Unix file modes."""

from __future__ import annotations

import argparse
import shutil
import stat
import zipfile
from datetime import datetime
from pathlib import Path


def pack(stage: Path, output: Path, platform: str, created: str) -> None:
    stage = stage.resolve(strict=True)
    stamp = datetime.fromisoformat(created.replace("Z", "+00:00"))
    date = (max(1980, stamp.year), stamp.month, stamp.day, stamp.hour, stamp.minute, stamp.second)
    with zipfile.ZipFile(output, "x", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(stage.rglob("*")):
            if path.is_symlink():
                raise ValueError(f"release staging contains a symlink: {path.name}")
            if path.is_dir():
                continue
            if not path.is_file() or not path.resolve().is_relative_to(stage):
                raise ValueError(f"release staging contains a non-regular file: {path.name}")
            relative = path.relative_to(stage).as_posix()
            executable = not platform.startswith("windows-") and relative in {
                "routevane", "start-routevane.sh"
            }
            info = zipfile.ZipInfo(f"{stage.name}/{relative}", date_time=date)
            info.create_system = 3
            info.external_attr = (stat.S_IFREG | (0o755 if executable else 0o644)) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            with path.open("rb") as source, archive.open(info, "w") as target:
                shutil.copyfileobj(source, target)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stage", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--platform", required=True)
    parser.add_argument("--created", required=True)
    args = parser.parse_args()
    pack(args.stage, args.output, args.platform, args.created)


if __name__ == "__main__":
    main()
