from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ORDINARY = (
    "TestDetectionPipelineAuth", "TestDetectionPipelineScan", "TestDetectionPipelineVolume",
    "TestDetectorCheckpoint", "TestDetectorRetention", "TestDetectorConfigRestart",
    "TestDetectorCapacity", "TestDetectorMidWindowRecovery", "TestDetectorThirdReplicaRebalance",
    "TestDetectorCrashBoundaries", "TestAlertWriterPipeline", "TestAlertWriterConflict",
    "TestAlertWriterConcurrentReplay", "TestAlertWriterPartialStorage", "TestAlertWriterPoison",
    "TestAlertWorkflowReplay", "TestDetectorPoison", "TestConfigLedgerHandover",
    "TestDetectorPGDependency", "TestAlertsSchema", "TestDetectionDecisionsSchema",
    "TestDetectorCooldownFixtureIsolation",
    "TestDetectorCooperativeAcquisition",
    "TestAlertWriterLockLifetime", "TestAlertWriterDifferingPayloadLockLifetime",
    "TestAlertWriterCancellationLockLifetime",
    "TestAlertReservationsSchema", "TestAlertWriterReservation",
    "TestAlertWriterReservationAmbiguity", "TestAlertWriterReservationRestart",
    "TestAlertWriterReservationNativeAckLoss",
)
COOLDOWN = tuple("TestDetectorCooldown" + suffix for suffix in (
    "Visible", "Expiry", "Replay", "Crash", "Outage", "Owners", "RestoreBehindCheckpoint",
))
UNIT = ("TestConfigWindowEnlargement", "TestConfigLedgerHandover")
RESERVATION_UNIT = ("TestAlertWriterReservationProtocol", "TestAlertWriterFailpointBuildBoundary")
EXPECTED = (
    [("./test/...", name, "6m", True) for name in ORDINARY]
    + [("./test/...", name, "8m", True) for name in COOLDOWN]
    + [("./internal/detection/...", name, "60s", False) for name in UNIT]
    + [("./internal/kafkax/...", "TestDetectorCooperativeOffsetFetch", "60s", False)]
    + [("./internal/config/...", "TestLoadAlertWriterResourceAdmission", "60s", False)]
    + [("./internal/alertwriter/...", name, "60s", False) for name in RESERVATION_UNIT]
    + [("./test/...", "TestDetectionScenarios", "16m", True),
       ("./test/...", "TestDetectionScenariosPreservedRepeat", "32m", True)]
)


class Phase3GuardTests(unittest.TestCase):
    def run_guard(self, *, empty: str = "", failed: str = "", package: str = "", direct: bool = False):
        """Run the real shell entry point/nonempty runner against a fake Go process."""
        with tempfile.TemporaryDirectory(prefix="sentinel-phase3-guard-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "backend").mkdir()
            for name in ("check-phase3-tests.sh", "go-test-nonempty.sh"):
                shutil.copy2(ROOT / "scripts" / name, root / "scripts" / name)
            fake = root / "go"
            fake.write_text(
                f"#!{sys.executable}\n"
                "import json, os, sys\n"
                "args = sys.argv[1:]\n"
                "with open(os.environ['PHASE3_CALLS'], 'a') as log:\n"
                "    log.write(json.dumps({'args': args, 'cwd': os.getcwd()}) + '\\n')\n"
                "selector = args[args.index('-run') + 1]\n"
                "package = next(arg for arg in args if arg.startswith('./'))\n"
                "selected_package = os.environ['PHASE3_PACKAGE'] in ('', package)\n"
                "if selected_package and selector == os.environ['PHASE3_FAILED']:\n"
                "    print('--- PASS: misleading_prior_success (0.00s)')\n"
                "    sys.exit(7)\n"
                "if not selected_package or selector != os.environ['PHASE3_EMPTY']:\n"
                "    print('--- PASS: ' + selector.strip('^$') + ' (0.00s)')\n"
                "print('ok synthetic')\n",
                encoding="utf-8",
            )
            fake.chmod(0o700)
            log = root / "calls.jsonl"
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ.get("PATH", ""),
                       PHASE3_CALLS=str(log), PHASE3_EMPTY=empty, PHASE3_FAILED=failed,
                       PHASE3_PACKAGE=package)
            command = ["bash", str(root / "scripts" / "check-phase3-tests.sh")]
            if direct:
                command = ["bash", str(root / "scripts" / "go-test-nonempty.sh"),
                           "./test/...", "^TestExample$", "-tags=integration", "-timeout", "6m", "-p", "1"]
            result = subprocess.run(command, cwd="/tmp", env=env, text=True,
                                    capture_output=True, timeout=10)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            for call in calls:
                self.assertEqual(call["cwd"], str(root / "backend"))
            return result, [call["args"] for call in calls]

    def test_every_exact_family_runs_independently_with_its_budget_and_build_tag(self):
        result, calls = self.run_guard()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        actual = []
        for args in calls:
            self.assertEqual(args[0], "test")
            self.assertIn("-count=1", args)
            self.assertIn("-v", args)
            self.assertIn("-timeout", args, "every family needs a finite Go timeout")
            self.assertIn("-run", args)
            selector = args[args.index("-run") + 1]
            self.assertRegex(selector, r"^\^Test[A-Za-z0-9]+\$$", "broad regex cannot prove each family")
            package = next(arg for arg in args if arg.startswith("./"))
            integration = package == "./test/..."
            self.assertEqual("-tags=integration" in args, integration)
            if integration:
                self.assertEqual(args[args.index("-p") + 1], "1")
            if package == "./internal/alertwriter/...":
                self.assertIn("-race", args)
            actual.append((package, selector[1:-1], args[args.index("-timeout") + 1], integration))
        self.assertCountEqual(actual, EXPECTED)

    def test_each_empty_family_fails_even_if_other_families_pass(self):
        for package, name, _, _ in EXPECTED:
            with self.subTest(package=package, family=name):
                result, calls = self.run_guard(empty="^" + name + "$", package=package)
                self.assertNotEqual(result.returncode, 0, "empty family was accepted: " + name)
                self.assertIn("no passing tests matched", result.stderr)
                self.assertTrue(any(package in args and "^" + name + "$" in args for args in calls))

    def test_each_go_failure_is_propagated_despite_a_pass_line(self):
        for package, name, _, _ in EXPECTED:
            with self.subTest(package=package, family=name):
                result, _ = self.run_guard(failed="^" + name + "$", package=package)
                self.assertNotEqual(result.returncode, 0, "failed family was accepted: " + name)
                self.assertIn("go test failed", result.stderr)

    def test_existing_nonempty_runner_rejects_empty_and_failed_go(self):
        # These guards already work; record characterization, not invented RED.
        for option in ("empty", "failed"):
            with self.subTest(option=option):
                result, _ = self.run_guard(direct=True, **{option: "^TestExample$"})
                self.assertNotEqual(result.returncode, 0)

    def test_ci_requires_phase3_and_retains_phase2_with_live_prerequisites(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        self.assertIn("./internal/detector ./internal/eventwriter ./internal/alertwriter", workflow)
        integration = workflow.split("\n  integration:\n", 1)[1].split("\n  #", 1)[0]
        self.assertIn("make test-integ", integration)
        for group in ("API key", "ingest", "dedupe schema and TTL", "writer and DLQ",
                      "recovery", "rate limit", "readiness"):
            self.assertIn(group + " integration group is nonempty", integration)
        self.assertIn("\n  phase3:\n", workflow, "Phase 3 behavior must be a required CI job")
        phase3 = workflow.split("\n  phase3:\n", 1)[1].split("\n  #", 1)[0]
        self.assertIn("bash scripts/check-phase3-tests.sh", phase3)
        for job in (integration, phase3, workflow.split("\n  compose-smoke:\n", 1)[1]):
            self.assertIn("timeout-minutes: 45", job)
            self.assertIn("astral-sh/setup-uv@1c37ad07a6a961277cf70c0d37d6f313000f5884", job)
            self.assertIn("make dev-up", job)
            self.assertIn("scripts/check-stack.sh", job)
        self.assertIn("test_phase3_guards.py", workflow)
        self.assertIn("test_seed_scenarios.py", workflow)
        smoke = workflow.split("\n  compose-smoke:\n", 1)[1]
        self.assertIn("timeout 32m", smoke)
        self.assertIn("^TestDetectionScenariosPreservedRepeat$", smoke)

    def test_ci_scenario_tests_use_the_synced_generator_environment(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        python = workflow.split("\n  python:\n", 1)[1].split("\n  schema:\n", 1)[0]
        scenario = python.split("      - name: scenario orchestration tests\n", 1)[1].split(
            "      - name:", 1
        )[0]
        self.assertIn("working-directory: .", scenario)
        self.assertIn(
            "run: generator/.venv/bin/python -B -m unittest discover -s scripts "
            "-p test_seed_scenarios.py",
            scenario,
            "scenario orchestration imports httpx from the locked generator environment",
        )
        self.assertLess(python.index("uv sync --locked"), python.index(scenario))


if __name__ == "__main__":
    unittest.main()
