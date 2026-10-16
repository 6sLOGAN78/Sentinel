from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("docker_context", ROOT / "scripts/check-docker-context.py")
assert SPEC is not None and SPEC.loader is not None
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class DockerContextTests(unittest.TestCase):
    def test_current_policy_excludes_private_material_in_real_export(self) -> None:
        CHECKER.check_context(ROOT / ".dockerignore")

    def test_removing_pem_exclusion_leaks_in_real_export(self) -> None:
        original = (ROOT / ".dockerignore").read_text(encoding="utf-8")
        self.assertIn("**/*.pem\n", original)
        with tempfile.TemporaryDirectory(prefix="sentinel-policy-mutation-") as directory:
            policy = Path(directory) / ".dockerignore"
            policy.write_text(original.replace("**/*.pem\n", ""), encoding="utf-8")
            with self.assertRaisesRegex(RuntimeError, "private fixture leaked.*backend/fixture.pem"):
                CHECKER.check_context(policy)

    def test_docker_unavailable_and_failure_are_not_skips(self) -> None:
        for error in (FileNotFoundError(), subprocess.CalledProcessError(1, "docker")):
            with self.subTest(error=type(error).__name__):
                with mock.patch.object(CHECKER.subprocess, "run", side_effect=error):
                    with self.assertRaisesRegex(RuntimeError, "Docker fixture export failed"):
                        CHECKER.check_context(ROOT / ".dockerignore")

    def test_export_timeout_is_bounded_and_fails(self) -> None:
        with mock.patch.object(
            CHECKER.subprocess, "run", side_effect=subprocess.TimeoutExpired("docker", 60)
        ) as run:
            with self.assertRaisesRegex(RuntimeError, "exceeded 60 seconds"):
                CHECKER.check_context(ROOT / ".dockerignore")
            self.assertEqual(run.call_args.kwargs["timeout"], 60)


if __name__ == "__main__":
    unittest.main()
