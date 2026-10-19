"""Behavioral guards for the private current-run operator command."""

import unittest
import os
import tempfile
import subprocess
import json
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest import mock
from scripts import seed_scenarios as scenarios


class ScenarioTests(unittest.TestCase):
    def test_late_counter_refuses_restart_reset_and_unexpected_delta(self):
        identity = {"container_id": "stable", "started_at": "same", "pid": 42}
        scenarios.verify_late_observation(10, 10, 0, identity, identity)
        scenarios.verify_late_observation(10, 11, 1, identity, identity)
        for after, other in ((0, identity), (10, dict(identity, pid=43)), (11, identity)):
            with self.subTest(after=after, identity=other), self.assertRaises(scenarios.SeedError):
                scenarios.verify_late_observation(10, after, 0, identity, other)

    def test_actual_orchestration_uses_snapshot_then_capture_then_ack_then_verify(self):
        calls = []
        certify = True
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            keys = root / ".local/keys"
            keys.mkdir(parents=True, mode=0o700)
            (keys / "demo.key").write_text("synthetic-api-key")
            (keys / "demo.key").chmod(0o600)
            settings = {"PGPASSWORD": "pg-secret", "SENTINEL_CLICKHOUSE.PASSWORD": "ch-secret"}

            def external(command, **kwargs):
                calls.append((command, kwargs))
                if "./tools/detection-state" in command:
                    request = kwargs["payload"]
                    return dict(
                        schema_version=1,
                        producers_quiescent=True,
                        snapshot_at=request["started_at"],
                        deadline=request["deadline"],
                        source_partitions={
                            key: i % 6 for i, key in enumerate(request["source_keys"])
                        },
                        source_snapshots=[
                            dict(
                                partition=p,
                                snapshot_end=0,
                                processed_next=0,
                                empty=True,
                                max_event_time=None,
                            )
                            for p in range(6)
                        ],
                    )
                if "capture" in command:
                    result = scenarios.read_private(Path(command[-1]))
                    self.assertEqual(
                        result["run_start"],
                        next(c[1]["payload"]["started_at"] for c in reversed(calls)
                             if "./tools/detection-state" in c[0]),
                    )
                    result["kafka_start"] = {"0": 9, "1": 7, "2": 2}
                    return result
                if "verify" in command:
                    actual = scenarios.read_private(Path(command[-1]))
                    self.assertTrue(actual["event_ids"])
                    self.assertEqual(actual["kafka_start"], {"0": 9, "1": 7, "2": 2})
                    return dict(verified=certify, drained=True, prior_rows_preserved=True)
                return {}

            def sender(_schedule, scenario, key):
                self.assertEqual(key, "synthetic-api-key")
                return list(dict.fromkeys(str(e["event_id"]) for e in scenario.events))

            with (
                mock.patch.multiple(
                    scenarios,
                    ROOT=root,
                    stack_settings=lambda: (settings, 300),
                    run_command=external,
                    late_metric=lambda _deadline: 0,
                    detector_identity=lambda _deadline: {
                        "container_id": "synthetic",
                        "started_at": "same",
                        "pid": 42,
                    },
                    send_scheduled=sender,
                ),
                mock.patch.multiple(
                    scenarios.seed,
                    ROOT=root,
                    KEY_DIRECTORY=keys,
                    _prepare_key_directory=lambda: None,
                ),
            ):
                output = root / "run.json"
                proof = scenarios.execute(output=output)
                successful_calls = calls.copy()
                certify = False
                with self.assertRaises(scenarios.SeedError):
                    scenarios.execute(output=output)
                calls = successful_calls
            self.assertTrue(proof["verified"])
            self.assertEqual(proof["new_late_drops"], 0)
            snapshot_calls = [call for call in calls if "./tools/detection-state" in call[0]]
            self.assertEqual(len(snapshot_calls), 2)
            self.assertEqual(
                snapshot_calls[0][1]["payload"]["deadline"],
                snapshot_calls[1][1]["payload"]["deadline"],
            )
            capture = next(i for i, c in enumerate(calls) if "capture" in c[0])
            verify = next(i for i, c in enumerate(calls) if "verify" in c[0])
            self.assertLess(capture, verify)
            self.assertEqual(
                calls[capture][0][0:5],
                ["go", "run", "./tools/check-alerts", "capture", "--manifest"],
            )
            for command, kwargs in calls:
                self.assertLessEqual(kwargs["timeout"], 65)
                self.assertNotIn("pg-secret", str(command))
                self.assertNotIn("synthetic-api-key", str(command))
                self.assertNotIn("SENTINEL_API_KEY", kwargs["env"])
            self.assertEqual(output.stat().st_mode & 0o077, 0)

    def test_live_mapping_and_budget_preserve_exact_independent_expectations(self):
        from sentinel_generator.scenarios import build_scenario, format_time

        start = datetime.now(UTC)
        names = set()
        for mode in ("all", "lateness", "boundary"):
            fixed = build_scenario(name=mode, seed=42, tenant="demo", timebase=start)
            names.update(str(e["source_ip"]) for e in fixed.events)
        mapping = scenarios.source_mapping(names, 42)
        self.assertTrue(all(ip.startswith("2001:db8:") for ip in mapping.values()))
        self.assertTrue(
            set(mapping.values()).isdisjoint(scenarios.source_mapping(names, 43).values())
        )
        keys = ["demo|" + ip for ip in mapping.values()]
        snapshot = dict(
            schema_version=1,
            producers_quiescent=True,
            snapshot_at=format_time(start),
            deadline=format_time(start + timedelta(minutes=15)),
            source_partitions={key: index % 6 for index, key in enumerate(keys)},
            source_snapshots=[
                dict(partition=p, snapshot_end=0, processed_next=0, empty=True, max_event_time=None)
                for p in range(6)
            ],
        )
        schedule, scenario = scenarios.build_schedule(snapshot, 42, mapping, True, 300)
        self.assertEqual(set(schedule["source_partitions"]), set(keys))
        self.assertEqual(len(scenario.expected_alerts), 10)
        self.assertEqual(len(scenario.dropped_late_ids), 1)
        self.assertEqual(
            datetime.fromisoformat(schedule["deadline"]),
            datetime.fromisoformat(snapshot["deadline"]) - timedelta(seconds=60),
        )
        expected = [json.loads(a) for a in scenario.expected_alerts]
        self.assertEqual(
            {a["rule_id"] for a in expected},
            {"AUTH_BRUTE_FORCE", "PORT_SCAN", "REQUEST_FLOOD", "DENY_SPIKE"},
        )
        markers = set(scenario.marker_ids)
        first_marker = next(i for i, e in enumerate(scenario.events) if e["event_id"] in markers)
        self.assertTrue(all(e["event_id"] in markers for e in scenario.events[first_marker:]))
        snapshot["deadline"] = format_time(start + timedelta(seconds=60))
        with self.assertRaises(RuntimeError):
            scenarios.build_schedule(snapshot, 42, mapping, True, 300)

    def test_children_receive_only_their_credentials(self):
        settings = {
            "PGPASSWORD": "pg-secret",
            "SENTINEL_CLICKHOUSE.PASSWORD": "ch-secret",
            "SENTINEL_API_KEY": "api-secret",
        }
        with mock.patch.dict(os.environ, settings):
            generator = scenarios.child_environment("generator", settings)
            verifier = scenarios.child_environment("verifier", settings)
        self.assertNotIn("PGPASSWORD", generator)
        self.assertNotIn("SENTINEL_CLICKHOUSE.PASSWORD", generator)
        self.assertNotIn("SENTINEL_API_KEY", verifier)

    def test_private_manifest_rejects_symlink_public_and_oversize(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "target"
            target.write_text("{}")
            link = root / "link"
            link.symlink_to(target)
            for path in (link, target):
                with self.subTest(path=path), self.assertRaises(scenarios.SeedError):
                    scenarios.read_private(path)
            target.chmod(0o600)
            target.write_bytes(b" " * (1024 * 1024 + 1))
            with self.assertRaises(scenarios.SeedError):
                scenarios.read_private(target)

    def test_private_manifest_rejects_duplicate_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest"
            path.write_text('{"verified":true,"verified":false}')
            path.chmod(0o600)
            with self.assertRaises(scenarios.SeedError):
                scenarios.read_private(path)

    def test_subprocess_bounded_arguments_and_private_stdin(self):
        command = ["go", "run", "./tools/check-alerts", "--manifest", "/private/run.json"]
        with mock.patch.object(
            subprocess, "run", return_value=subprocess.CompletedProcess(command, 0, "{}", "")
        ) as run:
            scenarios.run_command(
                command,
                cwd=Path("backend"),
                env={"PGPASSWORD": "secret"},
                timeout=12,
                payload={"private": True},
            )
        self.assertTrue(run.called)
        self.assertEqual(run.call_args.args[0], command)
        self.assertEqual(run.call_args.kwargs["timeout"], 12)
        self.assertIn('"private": true', run.call_args.kwargs["input"])
        self.assertNotIn("secret", str(run.call_args.args[0]))

    def test_timeout_and_nonzero_are_sanitized(self):
        for error in (
            subprocess.TimeoutExpired(["compose"], 1, stderr="private-secret"),
            subprocess.CalledProcessError(23, ["compose"], stderr="private-secret"),
        ):
            with (
                self.subTest(error=type(error)),
                mock.patch.object(subprocess, "run", side_effect=error),
            ):
                with self.assertRaises(scenarios.SeedError) as failure:
                    scenarios.run_command(["docker", "compose"], cwd=Path("."), env={}, timeout=1)
                self.assertNotIn("private-secret", str(failure.exception))

    def test_entrypoint_propagates_failure_and_sanitizes_details(self):
        with mock.patch.object(
            scenarios, "execute", side_effect=scenarios.SeedError("safe failure")
        ):
            self.assertEqual(scenarios.main([]), 1)

    def test_entrypoint_reports_verified_success(self):
        with mock.patch.object(scenarios, "execute", return_value={"verified": True}):
            self.assertEqual(scenarios.main([]), 0)


if __name__ == "__main__":
    unittest.main()
