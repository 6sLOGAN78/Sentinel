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
GROUPS = {"core": EXPECTED[:-2], "scenarios": EXPECTED[-2:]}


class Phase3GuardTests(unittest.TestCase):
    def run_guard(self, *, empty: str = "", failed: str = "", package: str = "", direct: bool = False,
                  arguments: tuple[str, ...] = ()):
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
            command = ["bash", str(root / "scripts" / "check-phase3-tests.sh"), *arguments]
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
        self.assert_exact_calls(calls, EXPECTED)

    def assert_exact_calls(self, calls, expected):
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
            if package in ("./internal/alertwriter/...", "./internal/config/..."):
                self.assertIn("-race", args)
            actual.append((package, selector[1:-1], args[args.index("-timeout") + 1], integration))
        self.assertCountEqual(actual, expected)

    def test_groups_are_disjoint_and_cover_the_unchanged_default(self):
        default, default_calls = self.run_guard()
        explicit, explicit_calls = self.run_guard(arguments=("all",))
        self.assertEqual(default.returncode, 0, default.stderr)
        self.assertEqual(explicit.returncode, 0, explicit.stderr)
        self.assertEqual(explicit_calls, default_calls)
        grouped = []
        for group, expected in GROUPS.items():
            with self.subTest(group=group):
                result, calls = self.run_guard(arguments=(group,))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assert_exact_calls(calls, expected)
                grouped.extend(calls)
        self.assertEqual(grouped, default_calls)
        self.assertTrue(set(GROUPS["core"]).isdisjoint(GROUPS["scenarios"]))
        self.assertEqual(len(GROUPS["core"]), 44)
        self.assertEqual(len(GROUPS["scenarios"]), 2)

    def test_unknown_or_extra_group_arguments_are_rejected_before_go(self):
        for arguments in (("unknown",), ("",), ("--group",), ("core", "scenarios"), ("all", "extra")):
            with self.subTest(arguments=arguments):
                result, calls = self.run_guard(arguments=arguments)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn("usage:", result.stderr)
                self.assertEqual(calls, [], "invalid group must not run any Go command")

    def test_each_group_rejects_empty_and_failed_families_despite_prior_passes(self):
        for group, expected in GROUPS.items():
            for package, name, _, _ in expected:
                for condition in ("empty", "failed"):
                    with self.subTest(group=group, package=package, family=name, condition=condition):
                        result, calls = self.run_guard(arguments=(group,), package=package,
                                                       **{condition: "^" + name + "$"})
                        self.assertNotEqual(result.returncode, 0, "family was accepted: " + name)
                        diagnostic = "no passing tests matched" if condition == "empty" else "go test failed"
                        self.assertIn(diagnostic, result.stderr)
                        self.assertTrue(any(package in args and "^" + name + "$" in args for args in calls))

    def test_ci_runs_exactly_both_groups_without_relaxing_budgets_or_failures(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        self.assert_phase3_workflow(workflow)

    def assert_phase3_workflow(self, workflow):
        self.assertEqual(workflow.count("\n  phase3:\n"), 1)
        self.assertEqual(workflow.count("\n  compose-smoke:\n"), 1)
        # Comments do not end YAML mappings; retain everything until the next
        # actual job key, including fields following the separator comment.
        phase3 = workflow.split("\n  phase3:\n", 1)[1].split("\n  compose-smoke:\n", 1)[0]
        # This small job is a closed policy: compare complete fields/commands,
        # not substrings that accept conditional skips or `|| true`. Deliberate
        # job changes must update this approval and its negative fixtures.
        self.assertEqual(phase3, """    runs-on: ubuntu-24.04
    timeout-minutes: 45
    needs: [go]
    strategy:
      fail-fast: false
      matrix:
        group: [core, scenarios]
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
      - uses: astral-sh/setup-uv@1c37ad07a6a961277cf70c0d37d6f313000f5884 # v10.3.0
      - run: make dev-up
      - run: scripts/check-stack.sh
      - name: independently nonempty Phase 3 families
        run: bash scripts/check-phase3-tests.sh '${{ matrix.group }}'
      - name: tear down
        if: always()
        run: make dev-down

  # Preserved-volume legacy seed followed by two exact scheduled scenario runs.""")

    def test_ci_guard_rejects_skips_masks_and_incomplete_matrices(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        prefix, rest = workflow.split("\n  phase3:\n", 1)
        phase3, suffix = rest.split("\n  #", 1)
        selector = "        run: bash scripts/check-phase3-tests.sh '${{ matrix.group }}'"
        mutations = {
            "job skipped": ("    strategy:\n", "    if: false\n    strategy:\n"),
            "selector skipped": (selector, "        if: false\n" + selector),
            "selector failure masked": (selector, selector + " || true"),
            "startup skipped": ("      - run: make dev-up", "      - if: false\n        run: make dev-up"),
            "startup failure masked": ("run: make dev-up", "run: make dev-up || true"),
            "stack check skipped": ("      - run: scripts/check-stack.sh", "      - if: false\n        run: scripts/check-stack.sh"),
            "stack check failure masked": ("run: scripts/check-stack.sh", "run: scripts/check-stack.sh || true"),
            "job continues after error": ("    strategy:\n", "    continue-on-error: true\n    strategy:\n"),
            "selector continues after error": (selector, "        continue-on-error: true\n" + selector),
            "wrong group": ("group: [core, scenarios]", "group: [core, unknown]"),
            "excluded group": ("group: [core, scenarios]\n", "group: [core, scenarios]\n        exclude:\n          - group: scenarios\n"),
            "fail fast": ("fail-fast: false", "fail-fast: true"),
            "larger budget": ("timeout-minutes: 45", "timeout-minutes: 60"),
        }
        for name, (before, after) in mutations.items():
            with self.subTest(mutation=name):
                self.assertEqual(phase3.count(before), 1, "mutation must target one actual field")
                mutant = prefix + "\n  phase3:\n" + phase3.replace(before, after, 1) + "\n  #" + suffix
                with self.assertRaises(AssertionError, msg="CI bypass accepted: " + name):
                    self.assert_phase3_workflow(mutant)

    def test_ci_guard_checks_job_fields_after_separator_comments(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        separator = "  # Preserved-volume legacy seed followed by two exact scheduled scenario runs.\n"
        self.assertEqual(workflow.count(separator), 1)
        for field in ("    if: false\n", "    continue-on-error: true\n"):
            with self.subTest(field=field):
                # YAML comments do not end mappings: these fields still belong
                # to phase3, until the actual next job key at indentation two.
                mutant = workflow.replace(separator, separator + field, 1)
                with self.assertRaises(AssertionError, msg="post-comment job field accepted: " + field):
                    self.assert_phase3_workflow(mutant)

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
