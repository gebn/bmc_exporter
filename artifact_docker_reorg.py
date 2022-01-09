#!/usr/bin/env python3
import argparse
import re
import tempfile
import sys
import shutil
from pathlib import Path, PurePath
import os


ARM_PATTERN = re.compile(r'arm(?P<version>v\d)')


def docker_target_platform(platform: str) -> PurePath:
    """
    Given an artifact platform suffix, e.g. linux-armv6, returns the Docker
    TARGETPLATFORM identifier, e.g. linux/arm/v6.
    """
    os, rest = platform.split('-', 1)
    pieces = [os]
    
    match = ARM_PATTERN.fullmatch(rest)
    if match:
        pieces.extend(['arm', match.group('version')])
    else:
        pieces.append(rest)
    
    return PurePath(*pieces)


def process(archive: Path, base_output: Path):
    inner_dir = archive.name.rstrip('.tar.gz')
    _, platform = inner_dir.rsplit('.', 1)
    final_dir = base_output / docker_target_platform(platform)
    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_dir = Path(tmp_dir)
        shutil.unpack_archive(archive, tmp_dir)
        shutil.move(tmp_dir / inner_dir , final_dir)


def _parse_args(argv: [str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description='Untars Linux archives to appropriate directories for Docker buildx.')
    parser.add_argument('output', help='Root path to move binaries to')
    parser.add_argument('input', help='Root path to read archives from')
    return parser.parse_args(argv[1:])


def main(argv: [str]) -> int:
    ns = _parse_args(argv)
    output = Path(ns.output)
    for entry in os.listdir(ns.input):
        path = os.path.join(ns.input, entry)
        if not os.path.isfile(path):
            continue
        process(Path(path), output)
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
