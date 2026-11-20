"""Phase 5 CI job, its single moved-test list and the aggregate `-skip` stay in lockstep.

scripts/check-phase5-tests.sh is the single list. The Makefile derives the
aggregate skip from it, CI runs each of its groups, and these guards prove
that every moved test runs in exactly one group and is skipped by the
aggregate, that the regex skips nothing else, and that the job cannot be
skipped, masked or given a larger budget without updating this approval.
"""

from __future__ import annotations

import collections
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "scripts" / "check-phase5-tests.sh"
GROUPS = ("chaos", "scale")
# group -> exact moved tests (deliberate approval; order matches the runner).
NAMES = {
    "chaos": (
        "TestChaosKafkaBrokerRestart", "TestChaosClickHouseShortOutageAbsorbed",
        "TestChaosClickHouseLongOutageRestart", "TestChaosGracefulShutdownEventWriter",
        "TestChaosGracefulShutdownDetector", "TestChaosSlowConsumerLag",
        "TestChaosLagAfterRebalance", "TestChaosOverloadBounded",
    ),
    "scale": (
        "TestListEventsScale", "TestListEventsOracleEquivalence", "TestListEventsTenantIsolation",
        "TestSummarizeEventsScale", "TestSummarizeEventsOracleEquivalence",
        "TestSummarizeEventsNoConflictsSkipsCorrection", "TestSummarizeEventsConflictCap",
        "TestDetectorConcurrentStartup", "TestDetectorRestoreRetryWithoutAckedCheckpoint",
        "TestDetectorManifestUpgradePath",
    ),
}
CHAOS_FAMILIES = ("chaos-broker", "chaos-clickhouse", "chaos-shutdown", "slow-consumer", "overload")
# Job budgets in minutes; they may shrink but never grow past the existing 45.
BUDGETS = {"chaos": 30, "scale": 30}
AGGREGATE = "\tcd backend && go test -tags=integration -timeout 32m -p 1 -skip '$(PHASE5_SKIP)' ./test/...\n"
SKIP_VARIABLE = (
    "PHASE5_SKIP = $(or $(shell bash '$(CURDIR)/scripts/check-phase5-tests.sh' --skip-regex),"
    "$(error scripts/check-phase5-tests.sh --skip-regex printed nothing))\n"
)

PHASE5_JOB = """  phase5:
    runs-on: ubuntu-24.04
    timeout-minutes: ${{ matrix.minutes }}
    needs: [go]
    strategy:
      fail-fast: false
      matrix:
        include:
          - group: chaos
            minutes: 30
          - group: scale
            minutes: 30
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
      - name: independently nonempty Phase 5 group
        run: bash scripts/check-phase5-tests.sh '${{ matrix.group }}'"""
GUARD_STEP = """      - name: phase 5 job, aggregate skip and CI guards
        working-directory: .
        run: python3 -B -m unittest discover -s scripts -p test_phase5_guards.py"""


def job_block(workflow: str, name: str) -> str:
    """A job runs until the next real job key; comments never end a mapping."""
    lines = workflow.split("\n")
    start = lines.index(f"  {name}:")
    end = len(lines)
    for index in range(start + 1, len(lines)):
        if re.fullmatch(r"  [A-Za-z0-9_-]+:\s*", lines[index]):
            end = index
            break
    block = lines[start:end]
    while block and (not block[-1].strip() or block[-1].lstrip().startswith("#")):
        block.pop()
    return "\n".join(block)


def step_block(job: str, name: str) -> str:
    head = f"      - name: {name}\n"
    if head not in job:
        raise AssertionError("required step is missing: " + name)
    body = job.split(head, 1)[1]
    return head + body.split("\n      - ", 1)[0].rstrip("\n")


def matrix_groups(job: str) -> list[tuple[str, int]]:
    return [(group, int(minutes)) for group, minutes in
            re.findall(r"\n          - group: (\S+)\n            minutes: (\d+)", job)]


def assert_phase5_workflow(case: unittest.TestCase, workflow: str) -> None:
    case.assertEqual(workflow.count("\n  phase5:\n"), 1)
    job = job_block(workflow, "phase5")
    # Closed policy: the complete job text, so skips, masks, extra or excluded
    # groups, fail-fast and budget changes all need a deliberate approval here.
    case.assertEqual(job, PHASE5_JOB)
    for token in ("if:", "continue-on-error", "|| true", "exclude", "fail-fast: true", "make dev-up"):
        case.assertNotIn(token, job)
    groups = matrix_groups(job)
    case.assertEqual(tuple(group for group, _ in groups), GROUPS)
    for group, minutes in groups:
        case.assertLessEqual(minutes, BUDGETS[group])
        case.assertLessEqual(minutes, 45)
    python = job_block(workflow, "python")
    case.assertEqual(python.count("-p test_phase5_guards.py"), 1)
    case.assertEqual(step_block(python, "phase 5 job, aggregate skip and CI guards"), GUARD_STEP)


def runner(*arguments: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(["bash", str(RUNNER), *arguments], text=True, capture_output=True,
                          timeout=10, check=False)


def skip_regex() -> str:
    result = runner("--skip-regex")
    if result.returncode != 0:
        raise AssertionError(result.stderr)
    return result.stdout.strip()


def integration_tests() -> collections.Counter[str]:
    found: collections.Counter[str] = collections.Counter()
    for path in sorted((ROOT / "backend" / "test").glob("*_test.go")):
        source = path.read_text(encoding="utf-8")
        if not source.startswith("//go:build integration\n"):
            continue
        found.update(re.findall(r"^func (Test[A-Za-z0-9_]+)\(t \*testing\.T\)", source, re.M))
    return found


def chaos_script_families() -> dict[str, tuple[str, list[str], str]]:
    source = (ROOT / "scripts" / "check-chaos.sh").read_text(encoding="utf-8")
    families = {}
    for name, package, names, tag, _ in re.findall(
            r'^  "([a-z-]+)\|([^|]+)\|([^|]+)\|([a-z0-9]+)\|([0-9]+m)"$', source, re.M):
        families[name] = (package, names.split(), tag)
    compose = re.search(r"^compose_families=\(([^)]*)\)$", source, re.M)
    if compose is None:
        raise AssertionError("check-chaos.sh compose_families is missing")
    for name in compose.group(1).split():
        families[name] = ("compose", [], "")
    return families


class Phase5GuardTests(unittest.TestCase):
    # --- the single list -------------------------------------------------

    def test_runner_groups_and_names_are_exactly_the_approved_list(self):
        self.assertEqual(tuple(runner("--groups").stdout.split()), GROUPS)
        for group in GROUPS:
            with self.subTest(group=group):
                self.assertEqual(tuple(runner("--names", group).stdout.split()), NAMES[group])

    def test_every_moved_test_is_in_exactly_one_group_and_in_the_skip(self):
        counts = collections.Counter(name for group in GROUPS for name in NAMES[group])
        self.assertTrue(counts, "the moved list must be nonempty")
        for name, count in counts.items():
            self.assertEqual(count, 1, f"{name} is selected by {count} groups")
        ordered = [name for group in GROUPS for name in NAMES[group]]
        self.assertEqual(skip_regex(), "^(" + "|".join(ordered) + ")$")

    def test_every_skipped_name_exists_once_as_an_integration_test(self):
        found = integration_tests()
        names = skip_regex()[2:-2].split("|")
        self.assertTrue(names)
        for name in names:
            with self.subTest(test=name):
                self.assertRegex(name, r"^Test[A-Za-z0-9_]+$")
                self.assertEqual(found[name], 1, f"func {name}( must exist exactly once in backend/test")

    def test_skip_regex_matches_only_the_moved_tests(self):
        pattern = re.compile(skip_regex())
        matched = {name for name in integration_tests() if pattern.search(name)}
        self.assertEqual(matched, {name for group in GROUPS for name in NAMES[group]})
        # Subtest and prefix lookalikes are not swallowed by the anchors.
        for lookalike in ("TestChaosKafkaBrokerRestartX", "XTestListEventsScale", "TestDetectorCapacity"):
            self.assertIsNone(pattern.search(lookalike))

    def test_chaos_group_is_exactly_the_testcontainers_check_chaos_families(self):
        source = RUNNER.read_text(encoding="utf-8")
        self.assertIn(f'readonly chaos_families="{",".join(CHAOS_FAMILIES)}"\n', source)
        self.assertIn('CHAOS_FAMILIES="$chaos_families" bash scripts/check-chaos.sh', source)
        families = chaos_script_families()
        names = []
        for family in CHAOS_FAMILIES:
            with self.subTest(family=family):
                package, tests, tag = families[family]
                self.assertEqual((package, tag), ("./test/", "integration"), "no compose-backed family")
                names.extend(tests)
        self.assertEqual(names, list(NAMES["chaos"]))

    # --- Makefile aggregate ----------------------------------------------

    def test_aggregate_skips_exactly_the_list_without_a_larger_budget(self):
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        target = makefile.split("\ntest-integ:", 1)[1].split("\n\n", 1)[0]
        self.assertEqual(target.count("\n\t"), 1, "test-integ stays a single recipe line")
        self.assertIn(AGGREGATE, makefile)
        self.assertEqual(makefile.count(SKIP_VARIABLE), 1)
        rendered = subprocess.run(["make", "-s", "-n", "test-integ"], cwd=ROOT, text=True,
                                  capture_output=True, timeout=30, check=True).stdout
        self.assertEqual(
            rendered,
            "cd backend && go test -tags=integration -timeout 32m -p 1 -skip '" + skip_regex() + "' ./test/...\n")

    def test_aggregate_fails_closed_when_the_list_is_unavailable(self):
        with tempfile.TemporaryDirectory(prefix="sentinel-phase5-make-") as directory:
            shutil.copy2(ROOT / "Makefile", Path(directory) / "Makefile")
            result = subprocess.run(["make", "-n", "test-integ"], cwd=directory, text=True,
                                    capture_output=True, timeout=30, check=False)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("--skip-regex printed nothing", result.stderr)
            self.assertNotIn("go test", result.stdout)

    # --- CI ----------------------------------------------------------------

    def test_ci_runs_the_phase5_job_and_its_guard_exactly(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        assert_phase5_workflow(self, workflow)

    def test_ci_guard_rejects_skips_masks_budgets_and_incomplete_matrices(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        selector = "        run: bash scripts/check-phase5-tests.sh '${{ matrix.group }}'"
        guard = "        run: python3 -B -m unittest discover -s scripts -p test_phase5_guards.py"
        chaos = "          - group: chaos\n            minutes: 30\n"
        scale = "          - group: scale\n            minutes: 30\n"
        mutations = {
            "job skipped": ("    needs: [go]\n    strategy:\n      fail-fast: false\n      matrix:\n        include:",
                            "    needs: [go]\n    if: false\n    strategy:\n      fail-fast: false\n"
                            "      matrix:\n        include:"),
            "job continues": ("    needs: [go]\n    strategy:\n      fail-fast: false\n      matrix:\n        include:",
                              "    needs: [go]\n    continue-on-error: true\n    strategy:\n      fail-fast: false\n"
                              "      matrix:\n        include:"),
            "selector skipped": (selector, "        if: false\n" + selector),
            "selector masked": (selector, selector + " || true"),
            "selector continues": (selector, "        continue-on-error: true\n" + selector),
            "fail fast": ("      fail-fast: false\n      matrix:\n        include:",
                          "      fail-fast: true\n      matrix:\n        include:"),
            "group missing": (chaos, ""),
            "unknown group": (scale, scale + "          - group: tracing\n            minutes: 30\n"),
            "group excluded": (scale, scale + "        exclude:\n          - group: scale\n"),
            "larger chaos budget": (chaos, chaos.replace("30", "60")),
            "larger scale budget": (scale, scale.replace("30", "60")),
            "fixed larger budget": ("    timeout-minutes: ${{ matrix.minutes }}\n    needs: [go]\n    strategy:\n"
                                    "      fail-fast: false\n      matrix:\n        include:",
                                    "    timeout-minutes: 360\n    needs: [go]\n    strategy:\n"
                                    "      fail-fast: false\n      matrix:\n        include:"),
            "guard skipped": (guard, "        if: false\n" + guard),
            "guard masked": (guard, guard + " || true"),
            "guard continues": (guard, "        continue-on-error: true\n" + guard),
            "guard removed": (GUARD_STEP + "\n", ""),
        }
        for label, (before, after) in mutations.items():
            with self.subTest(mutation=label):
                self.assertEqual(workflow.count(before), 1, "mutation must target one actual field")
                with self.assertRaises(AssertionError, msg="CI bypass accepted: " + label):
                    assert_phase5_workflow(self, workflow.replace(before, after, 1))

    def test_ci_guard_checks_job_fields_after_separator_comments(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        lines = workflow.split("\n")
        start = lines.index("  phase5:")
        end = next(i for i in range(start + 1, len(lines)) if re.fullmatch(r"  [A-Za-z0-9_-]+:\s*", lines[i]))
        self.assertTrue(lines[end - 1].startswith("  #"), "phase5 must be followed by a separator comment")
        for field in ("    if: false", "    continue-on-error: true"):
            with self.subTest(field=field):
                mutant = lines[:end] + [field] + lines[end:]
                with self.assertRaises(AssertionError, msg="post-comment field accepted"):
                    assert_phase5_workflow(self, "\n".join(mutant))

    def test_existing_aggregate_budgets_are_unchanged(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        integration = job_block(workflow, "integration")
        self.assertIn("    timeout-minutes: 45\n", integration)
        self.assertIn("      - name: integration tests\n        run: make test-integ\n", integration)
        self.assertNotIn("phase5", integration)

    # --- runner orchestration model ----------------------------------------

    def run_model(self, group: str, *, missing_list: str = "", no_pass: str = "", fail: bool = False):
        """Run the real runner against a fake Go and a fake check-chaos.sh.

        This is an orchestration MODEL: it proves selection, flags and exit
        propagation only; it is never evidence that a family passes.
        """
        with tempfile.TemporaryDirectory(prefix="sentinel-phase5-guard-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "backend").mkdir()
            for name in ("check-phase5-tests.sh", "go-test-nonempty.sh"):
                shutil.copy2(ROOT / "scripts" / name, root / "scripts" / name)
            log = root / "calls.jsonl"
            body = (
                "import json, os, sys\n"
                "args = sys.argv[1:]\n"
                "with open(os.environ['PHASE5_CALLS'], 'a') as out:\n"
                "    out.write(json.dumps({'args': args, 'chaos': os.environ.get('CHAOS_FAMILIES')}) + '\\n')\n"
                "if '-list' in args:\n"
                "    name = args[args.index('-list') + 1].strip('^$')\n"
                "    if name != os.environ['PHASE5_MISSING']:\n"
                "        print(name)\n"
                "    print('ok synthetic')\n"
                "    sys.exit(0)\n"
                "names = os.environ['PHASE5_NAMES'].split()\n"
                "for name in names:\n"
                "    if name != os.environ['PHASE5_NO_PASS']:\n"
                "        print('--- PASS: ' + name + ' (0.00s)')\n"
                "sys.exit(7 if os.environ['PHASE5_FAIL'] else 0)\n"
            )
            fake = root / "go"
            fake.write_text(f"#!{sys.executable}\n" + body, encoding="utf-8")
            fake.chmod(0o700)
            # The runner calls `bash scripts/check-chaos.sh`; the stub forwards
            # to the fake so the delegated families are recorded.
            (root / "scripts" / "check-chaos.sh").write_text(
                f'#!/usr/bin/env bash\nexec "{root / "go"}" chaos "$@"\n', encoding="utf-8")
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ.get("PATH", ""),
                       PHASE5_CALLS=str(log), PHASE5_MISSING=missing_list, PHASE5_NO_PASS=no_pass,
                       PHASE5_FAIL="1" if fail else "", PHASE5_NAMES=" ".join(NAMES[group]))
            env.pop("CHAOS_FAMILIES", None)
            result = subprocess.run(["bash", str(root / "scripts" / "check-phase5-tests.sh"), group],
                                    cwd="/tmp", env=env, text=True, capture_output=True, timeout=20,
                                    check=False)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            return result, calls

    def test_runner_rejects_unknown_empty_or_extra_arguments(self):
        for arguments in ((), ("tracing",), ("chaos", "scale"), ("--names",), ("--names", "bogus"),
                          ("--skip-regex", "extra")):
            with self.subTest(arguments=arguments):
                result = runner(*arguments)
                self.assertEqual(result.returncode, 2)
                self.assertIn("usage:", result.stderr)

    def test_scale_runs_one_anchored_nonempty_selector_with_its_budget(self):
        result, calls = self.run_model("scale")
        self.assertEqual(result.returncode, 0, result.stderr)
        listed = [call["args"][call["args"].index("-list") + 1] for call in calls if "-list" in call["args"]]
        self.assertEqual(listed, ["^" + name + "$" for name in NAMES["scale"]])
        runs = [call["args"] for call in calls if "-run" in call["args"]]
        self.assertEqual(len(runs), 1)
        self.assertEqual(runs[0][runs[0].index("-run") + 1], "^(" + "|".join(NAMES["scale"]) + ")$")
        for flag in ("./test/...", "-tags=integration", "-timeout=20m", "-p=1"):
            self.assertIn(flag, runs[0])

    def test_chaos_delegates_exactly_the_five_families_to_check_chaos(self):
        result, calls = self.run_model("chaos")
        self.assertEqual(result.returncode, 0, result.stderr)
        delegated = [call for call in calls if call["args"][:1] == ["chaos"]]
        self.assertEqual(len(delegated), 1)
        self.assertEqual(delegated[0]["chaos"], ",".join(CHAOS_FAMILIES))

    def test_each_group_rejects_missing_unpassed_and_failed_tests(self):
        for group in GROUPS:
            for name in (NAMES[group][0], NAMES[group][-1]):
                with self.subTest(group=group, test=name, case="missing"):
                    result, calls = self.run_model(group, missing_list=name)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("matches zero tests", result.stderr)
                    self.assertFalse([c for c in calls if "-run" in c["args"] or c["args"][:1] == ["chaos"]])
                with self.subTest(group=group, test=name, case="no pass"):
                    result, _ = self.run_model(group, no_pass=name)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(name + " did not report --- PASS", result.stderr)
            with self.subTest(group=group, case="failed"):
                result, _ = self.run_model(group, fail=True)
                self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
