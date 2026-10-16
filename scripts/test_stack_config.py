"""Exercise stack expectations through real Compose resolution and stub Kafka."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SETTINGS = (
    "EVENTS_PARTITIONS",
    "ALERTS_PARTITIONS",
    "EVENTS_RETENTION_MS",
    "ALERTS_RETENTION_MS",
    "DLQ_RETENTION_MS",
)
DEFAULTS = ("6", "3", "86400000", "604800000", "604800000")
DOCKER_STUB = r"""#!/usr/bin/env python3
import json, os, subprocess, sys
args = sys.argv[1:]
with open(os.environ["STUB_LOG"], "a") as log:
    log.write(json.dumps(args) + "\n")
if "config" in args:
    if os.environ.get("STUB_CONFIG_FAIL"):
        sys.exit(23)
    sys.exit(subprocess.run([os.environ["REAL_DOCKER"], *args], check=False).returncode)
if "ps" in args:
    print("healthy")
elif "--describe" in args:
    topic = args[args.index("--topic") + 1]
    prefix = {"security.events.v1": "EVENTS", "security.alerts.v1": "ALERTS",
              "security.events.dlq.v1": "DLQ"}[topic]
    parts = "1" if prefix == "DLQ" else os.environ["STUB_" + prefix + "_PARTITIONS"]
    retention = os.environ['STUB_' + prefix + '_RETENTION_MS']
    print(f"Topic: {topic} PartitionCount: {parts} ReplicationFactor: 1 "
          f"Configs: retention.ms={retention}")
elif "--list" in args:
    # Stop after all topic checks, before host TCP/HTTP checks outside this test.
    print("undeclared.topic.check")
"""


class StackConfigTests(unittest.TestCase):
    def run_checker(self, dotenv=(), shell=(), actual=None, config_fail=False):
        docker = shutil.which("docker")
        self.assertIsNotNone(docker, "Docker Compose CLI is required (no daemon needed)")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "deployments/compose").mkdir(parents=True)
            (root / "bin").mkdir()
            shutil.copyfile(ROOT / "scripts/check-stack.sh", root / "scripts/check-stack.sh")
            shutil.copyfile(
                ROOT / "deployments/compose/docker-compose.yml",
                root / "deployments/compose/docker-compose.yml",
            )
            (root / ".env").write_text(
                "SENTINEL_POSTGRES_PASSWORD=synthetic-secret\nSENTINEL_CLICKHOUSE_PASSWORD=synthetic-secret\n"
                + "".join(f"{k}={v}\n" for k, v in dotenv)
            )
            stub = root / "bin/docker"
            stub.write_text(DOCKER_STUB)
            stub.chmod(0o700)
            env = {
                k: v
                for k, v in os.environ.items()
                if k not in SETTINGS
                and k not in ("SENTINEL_POSTGRES_PASSWORD", "SENTINEL_CLICKHOUSE_PASSWORD")
            }
            env.update(shell)
            expected = dict(zip(SETTINGS, DEFAULTS, strict=True))
            expected.update(dotenv)
            expected.update(shell)
            env.update({"STUB_" + k: v for k, v in (actual or expected).items()})
            env.update(
                PATH=str(root / "bin") + os.pathsep + env["PATH"],
                REAL_DOCKER=docker,
                STUB_LOG=str(root / "calls.jsonl"),
            )
            if config_fail:
                env["STUB_CONFIG_FAIL"] = "1"
            result = subprocess.run(  # noqa: S603 - execute only the copied repository script
                ["/bin/bash", str(root / "scripts/check-stack.sh")],
                env=env,
                capture_output=True,
                text=True,
                timeout=15,
                check=False,
            )
            self.assertNotIn("synthetic-secret", result.stdout + result.stderr)
            return result, (root / "calls.jsonl").read_text()

    def assert_topics_accepted(self, result):
        self.assertEqual(result.returncode, 1)
        self.assertIn("FAIL: undeclared topic was auto-created", result.stderr)
        self.assertEqual(result.stdout.count("PASS: topic "), 3, result.stdout + result.stderr)

    def test_defaults_match_real_compose(self):
        result, _ = self.run_checker()
        self.assert_topics_accepted(result)

    def test_dotenv_retention_and_partition_overrides(self):
        values = tuple(
            zip(SETTINGS, ("8", "4", "172800000", "691200000", "777600000"), strict=True)
        )
        result, _ = self.run_checker(dotenv=values)
        self.assert_topics_accepted(result)

    def test_shell_environment_overrides_dotenv(self):
        values = tuple(
            zip(SETTINGS, ("8", "4", "172800000", "691200000", "777600000"), strict=True)
        )
        result, _ = self.run_checker(
            dotenv=tuple(zip(SETTINGS, DEFAULTS, strict=True)), shell=values
        )
        self.assert_topics_accepted(result)

    def test_reports_actual_retention_mismatch(self):
        actual = dict(zip(SETTINGS, DEFAULTS, strict=True))
        actual["EVENTS_RETENTION_MS"] = "864000000"  # expected value is a prefix
        result, _ = self.run_checker(actual=actual)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("security.events.v1 retention.ms != 86400000", result.stderr)

    def test_compose_failure_preserves_status(self):
        result, calls = self.run_checker(config_fail=True)
        self.assertEqual(result.returncode, 23, result.stdout + result.stderr)
        self.assertIn('"config"', calls)


if __name__ == "__main__":
    unittest.main()
