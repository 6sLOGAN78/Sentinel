"""Compose-backed chaos families must never target a protected compose project."""

from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/check-chaos.sh"
PLENTY_DF = str(50 * 1024**3)
PLENTY_MEM_KB = str(8 * 1024 * 1024)
GUARD_EXIT = 6
SEAM_EXIT = 5
ENV_BODY = """SENTINEL_DATABASE.HOST=127.0.0.1
SENTINEL_DATABASE.PORT=15432
SENTINEL_REDIS.ADDRESS=127.0.0.1:16379
SENTINEL_KAFKA.BROKERS=127.0.0.1:29092
SENTINEL_CLICKHOUSE.NATIVE_ADDRESS=127.0.0.1:19000
"""


class CheckChaosProjectGuardTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        tmp = Path(self.tmp.name)
        self.bin = tmp / "bin"
        self.bin.mkdir()
        self.calls = tmp / "docker-calls"
        self.env_file = tmp / "dotenv"
        self.env_file.write_text(ENV_BODY)
        docker_root = tmp / "docker-root"
        docker_root.mkdir()
        # Stub docker: `info` answers the resource gate, `ps` reports the compose
        # project that publishes the requested port (STUB_OWNER, may be empty).
        stub = self.bin / "docker"
        stub.write_text(
            "#!/bin/sh\n"
            f'echo "$*" >> "{self.calls}"\n'
            'case "$1" in\n'
            f"  info) echo {docker_root} ;;\n"
            '  ps) [ -n "$STUB_OWNER" ] && echo "$STUB_OWNER" ;;\n'
            "esac\n"
            "exit 0\n"
        )
        stub.chmod(0o755)

    def run_chaos(self, families: str, **overrides: str) -> subprocess.CompletedProcess[str]:
        env = {
            "PATH": f"{self.bin}{os.pathsep}{os.environ['PATH']}",
            "HOME": os.environ.get("HOME", "/"),
            "CHECK_RESOURCES_TEST": "1",
            "CHECK_RESOURCES_FAKE_DF_BYTES": PLENTY_DF,
            "CHECK_RESOURCES_FAKE_MEM_KB": PLENTY_MEM_KB,
            "CHECK_CHAOS_ENV_FILE": str(self.env_file),
            "CHAOS_FAMILIES": families,
        }
        for name in ("GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS", "GOTOOLCHAIN"):
            if name in os.environ:
                env[name] = os.environ[name]
        env.update(overrides)
        return subprocess.run(
            ["bash", str(SCRIPT)], env=env, capture_output=True, text=True, check=False
        )

    def docker_calls(self) -> str:
        return self.calls.read_text() if self.calls.exists() else ""

    def test_compose_family_requires_explicit_project(self):
        result = self.run_chaos("sse-reconnect", STUB_OWNER="sentinel")
        self.assertEqual(result.returncode, GUARD_EXIT, result.stderr)
        self.assertIn("COMPOSE_PROJECT_NAME", result.stderr)

    def test_protected_projects_are_refused(self):
        for project in ("sentinel", "sentinel-p4-demo-20261024", "sentinel-p4-demo-other"):
            with self.subTest(project=project):
                result = self.run_chaos(
                    "sse-reconnect", COMPOSE_PROJECT_NAME=project, STUB_OWNER=project
                )
                self.assertEqual(result.returncode, GUARD_EXIT, result.stderr)
                self.assertIn("protected", result.stderr)
                self.assertIn(project, result.stderr)

    def test_ports_owned_by_another_project_are_refused(self):
        result = self.run_chaos(
            "sse-reconnect", COMPOSE_PROJECT_NAME="sentinel-chaoscheck", STUB_OWNER="sentinel"
        )
        self.assertEqual(result.returncode, GUARD_EXIT, result.stderr)
        self.assertIn("15432", result.stderr)
        self.assertIn("'sentinel'", result.stderr)
        self.assertIn("publish=15432", self.docker_calls())

    def test_unpublished_ports_are_refused(self):
        result = self.run_chaos(
            "sse-reconnect", COMPOSE_PROJECT_NAME="sentinel-chaoscheck", STUB_OWNER=""
        )
        self.assertEqual(result.returncode, GUARD_EXIT, result.stderr)
        self.assertIn("make dev-up", result.stderr)

    def test_every_dependency_port_is_checked(self):
        result = self.run_chaos(
            "sse-reconnect",
            COMPOSE_PROJECT_NAME="sentinel-chaoscheck",
            STUB_OWNER="sentinel-chaoscheck",
        )
        self.assertEqual(result.returncode, SEAM_EXIT, result.stderr)
        calls = self.docker_calls()
        for port in ("15432", "16379", "29092", "19000"):
            self.assertIn(f"publish={port}", calls)
        self.assertNotIn("compose", calls)

    def test_testcontainer_families_need_no_project(self):
        result = self.run_chaos("dlq")
        self.assertEqual(result.returncode, SEAM_EXIT, result.stderr)
        self.assertNotIn("COMPOSE_PROJECT_NAME", result.stderr)
        self.assertNotIn("publish=", self.docker_calls())


if __name__ == "__main__":
    unittest.main()
