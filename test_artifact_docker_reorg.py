import unittest
from pathlib import PurePath

from artifact_docker_reorg import docker_target_platform


class TestDockerTargetPlatform(unittest.TestCase):
    def test_valid(self):
        tests = {
            'windows-amd64': PurePath('windows/amd64'),
            'darwin-amd64': PurePath('darwin/amd64'),
            'darwin-arm64': PurePath('darwin/arm64'),
            'linux-amd64': PurePath('linux/amd64'),
            'linux-arm64': PurePath('linux/arm64'),
            'linux-armv6': PurePath('linux/arm/v6'),
            'linux-armv7': PurePath('linux/arm/v7'),
        }
        for platform, expected in tests.items():
            with self.subTest(platform=platform):
                self.assertEqual(docker_target_platform(platform), expected)
