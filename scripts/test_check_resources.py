"""Exercise the read-only resource gate through its test seams and a stub docker."""

from __future__ import annotations

import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/check-resources.sh"
GIB = 1024**3
PLENTY_DF = str(50 * GIB)
PLENTY_MEM_KB = str(8 * 1024 * 1024)
REFUSAL = "Sentinel will not prune; free space yourself"


class CheckResourcesTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.bin = Path(self.tmp.name) / "bin"
        self.bin.mkdir()
        self.docker_root = Path(self.tmp.name) / "docker-root"
        self.docker_root.mkdir()
        self.write_docker(f"#!/bin/sh\necho {self.docker_root}\n")

    def write_docker(self, body: str) -> None:
        stub = self.bin / "docker"
        stub.write_text(body)
        stub.chmod(0o755)

    def run_gate(self, **overrides: str) -> subprocess.CompletedProcess[str]:
        env = {
            "PATH": f"{self.bin}{os.pathsep}{os.environ['PATH']}",
            "HOME": os.environ.get("HOME", "/"),
            "CHECK_RESOURCES_TEST": "1",
            "CHECK_RESOURCES_FAKE_DF_BYTES": PLENTY_DF,
            "CHECK_RESOURCES_FAKE_MEM_KB": PLENTY_MEM_KB,
        }
        env.update(overrides)
        return subprocess.run(
            ["bash", str(SCRIPT)], env=env, capture_output=True, text=True, check=False
        )

    def test_passes_at_or_above_floor(self):
        result = self.run_gate(
            CHECK_RESOURCES_FAKE_DF_BYTES=str(10 * GIB),
            CHECK_RESOURCES_FAKE_MEM_KB=str(3 * 1024 * 1024),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("PASS", result.stdout)
        self.assertIn(str(ROOT), result.stdout)
        self.assertIn(str(self.docker_root), result.stdout)

    def test_disk_below_floor_refuses(self):
        result = self.run_gate(CHECK_RESOURCES_FAKE_DF_BYTES=str(GIB))
        self.assertEqual(result.returncode, 3)
        self.assertIn("1.00 GiB", result.stderr)
        self.assertIn("floor is 10 GiB", result.stderr)
        self.assertIn(REFUSAL, result.stderr)
        self.assertIn(str(self.docker_root), result.stderr)

    def test_memory_below_floor_refuses(self):
        result = self.run_gate(CHECK_RESOURCES_FAKE_MEM_KB=str(2 * 1024 * 1024))
        self.assertEqual(result.returncode, 3)
        self.assertIn("MemAvailable is 2.00 GiB", result.stderr)
        self.assertIn("floor is 3 GiB", result.stderr)
        self.assertIn(REFUSAL, result.stderr)

    def test_lowered_floor_is_refused(self):
        for name, value in (("MIN_FREE_GIB", "5"), ("MIN_MEM_GIB", "1"), ("MIN_FREE_GIB", "x")):
            with self.subTest(name=name, value=value):
                result = self.run_gate(
                    **{name: value, "CHECK_RESOURCES_FAKE_DF_BYTES": "1"}
                )
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn(name, result.stderr)

    def test_raised_floor_is_honored(self):
        result = self.run_gate(MIN_FREE_GIB="100")
        self.assertEqual(result.returncode, 3)
        self.assertIn("floor is 100 GiB", result.stderr)

    def test_seams_ignored_outside_test_mode(self):
        # Without CHECK_RESOURCES_TEST=1 an impossible fake value must not be used.
        result = self.run_gate(
            CHECK_RESOURCES_TEST="0", CHECK_RESOURCES_FAKE_DF_BYTES="0", MIN_FREE_GIB="10"
        )
        self.assertNotIn("has 0.00 GiB free", result.stderr)

    def test_docker_unavailable_fails(self):
        self.write_docker("#!/bin/sh\nexit 1\n")
        result = self.run_gate()
        self.assertEqual(result.returncode, 4)
        self.assertIn("docker unavailable", result.stderr)

    def test_script_has_no_destructive_tokens(self):
        code = [
            line
            for line in SCRIPT.read_text().splitlines()
            if not re.match(r"^\s*#", line)
        ]
        hits = [line for line in code if re.search(r"\b(prune|rm|stop)\b", line)]
        self.assertEqual(hits, [])
        self.assertTrue(os.access(SCRIPT, os.X_OK), "gate must be executable")


if __name__ == "__main__":
    unittest.main()
