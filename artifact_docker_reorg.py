#!/usr/bin/env python3
import argparse
import os
import re
import shutil
import sys
import tempfile
from pathlib import Path, PurePath

_ARM_PATTERN = re.compile(r'arm(?P<version>v\d)')


def docker_target_platform(platform: str) -> PurePath:
    """
    Given an artifact platform suffix, e.g. linux-armv6, returns the Docker
    TARGETPLATFORM identifier, e.g. linux/arm/v6.

    :param platform: The platform section of an archive name, e.g. linux-armv6,
                     windows-amd64.
    :return: The a directory hierarchy for the platform compatible with Docker,
             e.g. linux/arm/v6 and windows/amd64 respectively.
    """
    os, rest = platform.split('-', 1)
    pieces = [os]

    match = _ARM_PATTERN.fullmatch(rest)
    if match:
        pieces.extend(['arm', match.group('version')])
    else:
        pieces.append(rest)

    return PurePath(*pieces)


def unpack(archive: Path, base_output: Path) -> None:
    """
    Extracts an archive to the correct subdirectory under an output path.

    :param archive: Path of the archive to extract. Anything supported by
                    shutil.unpack_archive() will work.
    :param base_output: The root parent directory to extract to. Directories
                        will be created within this as necessary, e.g.
                        linux/arm/v6.
    """
    inner_dir = archive.name.rstrip('.tar.gz')
    _, platform = inner_dir.rsplit('.', 1)
    final_dir = base_output / docker_target_platform(platform)
    with tempfile.TemporaryDirectory() as tmp_dir:
        tmp_dir = Path(tmp_dir)
        shutil.unpack_archive(archive, tmp_dir)
        shutil.move(tmp_dir / inner_dir, final_dir)


def _parse_args(argv: [str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description='Untars Linux archives to appropriate directories for Docker buildx.')
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
        unpack(Path(path), output)
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
