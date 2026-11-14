from __future__ import annotations

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
PACKAGE = "./testphase4/..."
# group -> (exact family, Go timeout). Ordinary families get 6m; delivery and
# stream carry real reconnect/replay waits and get 10m.
FAMILIES = {
    "auth": ("TestPhase4Auth", "6m"),
    "alerts": ("TestPhase4Alerts", "6m"),
    "events": ("TestPhase4Events", "6m"),
    "triage": ("TestPhase4Triage", "6m"),
    "summary": ("TestPhase4SummaryRules", "6m"),
    "delivery": ("TestPhase4Delivery", "10m"),
    "stream": ("TestPhase4Stream", "10m"),
    "tenant": ("TestPhase4TenantMatrix", "6m"),
}
ORDER = tuple(FAMILIES)
EXPECTED = [(PACKAGE, name, timeout) for name, timeout in FAMILIES.values()]

SETUP = """      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
      - uses: astral-sh/setup-uv@1c37ad07a6a961277cf70c0d37d6f313000f5884 # v10.3.0
"""
PHASE4_JOB = """  phase4:
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    needs: [go]
    strategy:
      fail-fast: false
      matrix:
        group: [auth, alerts, events, triage, summary, delivery, stream, tenant]
    steps:
""" + SETUP + """      - run: make dev-up
      - run: scripts/check-stack.sh
      - name: independently nonempty Phase 4 native family
        run: bash scripts/check-phase4-tests.sh '${{ matrix.group }}'
      - name: tear down
        if: always()
        run: make dev-down"""
BROWSER_JOB = """  phase4-browser:
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    needs: [go]
    steps:
""" + SETUP + """      - uses: actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1 # v7.1.0
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: make dev-up
      - run: scripts/check-stack.sh
      - name: dashboard dependencies
        working-directory: web
        run: npm ci
      - name: pinned Chromium for Playwright
        working-directory: web
        run: npx playwright install --with-deps chromium
      - name: generator environment for the seeder
        working-directory: generator
        run: uv sync --locked
      - name: real browser acceptance through the proxy and two replicas
        run: bash scripts/check-phase4-browser.sh
      - name: tear down
        if: always()
        run: make dev-down"""
GUARD_STEP = """      - name: phase 4 selector and CI guards
        working-directory: .
        run: python3 -B -m unittest discover -s scripts -p test_phase4_guards.py"""


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


def assert_phase4_workflow(case: unittest.TestCase, workflow: str) -> None:
    case.assertEqual(job_block(workflow, "phase4"), PHASE4_JOB)
    case.assertEqual(job_block(workflow, "phase4-browser"), BROWSER_JOB)
    case.assertEqual(step_block(job_block(workflow, "python"), "phase 4 selector and CI guards"), GUARD_STEP)


class Phase4GuardTests(unittest.TestCase):
    def run_guard(self, *, empty: str = "", failed: str = "", arguments: tuple[str, ...] = (),
                  script: str | None = None):
        """Run the real runner and nonempty helper against a controlled fake Go.

        This is an orchestration MODEL: it proves selection, ordering and
        exit propagation only; it is never evidence that a native family passes.
        """
        with tempfile.TemporaryDirectory(prefix="sentinel-phase4-guard-") as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "backend").mkdir()
            shutil.copy2(ROOT / "scripts" / "go-test-nonempty.sh", root / "scripts")
            runner = root / "scripts" / "check-phase4-tests.sh"
            if script is None:
                shutil.copy2(ROOT / "scripts" / "check-phase4-tests.sh", runner)
            else:
                runner.write_text(script, encoding="utf-8")
            fake = root / "go"
            fake.write_text(
                f"#!{sys.executable}\n"
                "import json, os, sys\n"
                "args = sys.argv[1:]\n"
                "with open(os.environ['PHASE4_CALLS'], 'a') as log:\n"
                "    log.write(json.dumps({'args': args, 'cwd': os.getcwd()}) + '\\n')\n"
                "selector = args[args.index('-run') + 1]\n"
                "if selector == os.environ['PHASE4_FAILED']:\n"
                "    print('--- PASS: misleading_prior_success (0.00s)')\n"
                "    sys.exit(7)\n"
                "if selector != os.environ['PHASE4_EMPTY']:\n"
                "    print('--- PASS: ' + selector.strip('^$') + ' (0.00s)')\n"
                "print('ok synthetic')\n",
                encoding="utf-8",
            )
            fake.chmod(0o700)
            log = root / "calls.jsonl"
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ.get("PATH", ""),
                       PHASE4_CALLS=str(log), PHASE4_EMPTY=empty, PHASE4_FAILED=failed)
            result = subprocess.run(["bash", str(runner), *arguments], cwd="/tmp", env=env, text=True,
                                    capture_output=True, timeout=20)
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            for call in calls:
                self.assertEqual(call["cwd"], str(root / "backend"))
            return result, [call["args"] for call in calls]

    def assert_exact_calls(self, calls, expected):
        actual = []
        for args in calls:
            self.assertEqual(args[0], "test")
            self.assertIn("-count=1", args)
            self.assertIn("-v", args)
            selector = args[args.index("-run") + 1]
            self.assertRegex(selector, r"^\^TestPhase4[A-Za-z]+\$$", "broad regex cannot prove each family")
            self.assertIn(PACKAGE, args)
            self.assertIn("-tags=phase4", args, "new acceptance is isolated behind the phase4 tag")
            self.assertNotIn("-tags=integration", args)
            self.assertIn("-race", args)
            self.assertIn("-p=1", args)
            timeouts = [arg for arg in args if arg.startswith("-timeout=")]
            self.assertEqual(len(timeouts), 1, "every family needs exactly one finite Go timeout")
            self.assertNotIn("-short", args)
            self.assertFalse([arg for arg in args if arg.startswith("-skip")])
            actual.append((PACKAGE, selector[1:-1], timeouts[0].split("=", 1)[1]))
        self.assertEqual(actual, expected, "exact family union, order and budgets")

    # --- runner contract -------------------------------------------------

    def test_all_runs_the_eight_exact_families_with_budgets_and_phase4_tag(self):
        for arguments in ((), ("all",)):
            with self.subTest(arguments=arguments):
                result, calls = self.run_guard(arguments=arguments)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assert_exact_calls(calls, EXPECTED)

    def test_each_group_is_one_family_and_groups_are_a_disjoint_union_of_all(self):
        grouped = []
        for group, (name, timeout) in FAMILIES.items():
            with self.subTest(group=group):
                result, calls = self.run_guard(arguments=(group,))
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assert_exact_calls(calls, [(PACKAGE, name, timeout)])
                grouped.extend(calls)
        default, default_calls = self.run_guard()
        self.assertEqual(default.returncode, 0)
        self.assertEqual(grouped, default_calls)
        self.assertEqual(len({name for name, _ in FAMILIES.values()}), 8)

    def test_unknown_empty_or_extra_groups_are_rejected_before_go(self):
        for arguments in (("unknown",), ("",), ("--group",), ("auth", "stream"), ("all", "extra"),
                          ("browser",), ("Auth",), (" auth",), ("core",), ("auth;true",)):
            with self.subTest(arguments=arguments):
                result, calls = self.run_guard(arguments=arguments)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn("usage:", result.stderr)
                self.assertEqual(calls, [], "invalid group must not run any Go command")

    def test_every_group_rejects_empty_and_failed_families_despite_prior_passes(self):
        for group, (name, _) in FAMILIES.items():
            for condition in ("empty", "failed"):
                with self.subTest(group=group, condition=condition):
                    result, calls = self.run_guard(arguments=(group,), **{condition: f"^{name}$"})
                    self.assertNotEqual(result.returncode, 0, "family was accepted: " + name)
                    diagnostic = "no passing tests matched" if condition == "empty" else "go test failed"
                    self.assertIn(diagnostic, result.stderr)
                    self.assertTrue(any(f"^{name}$" in args for args in calls))

    def test_all_fails_fast_and_a_later_pass_never_masks_an_earlier_failure(self):
        for index, group in enumerate(ORDER):
            name = FAMILIES[group][0]
            for condition in ("empty", "failed"):
                with self.subTest(family=name, condition=condition):
                    result, calls = self.run_guard(**{condition: f"^{name}$"})
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(len(calls), index + 1, "later families ran after a failure")
                    self.assertIn(f"^{name}$", calls[-1])

    def test_runner_cannot_skip_continue_or_mask_failures(self):
        text = (ROOT / "scripts" / "check-phase4-tests.sh").read_text(encoding="utf-8")
        code = "\n".join(line.split(" #", 1)[0] for line in text.splitlines() if not line.lstrip().startswith("#"))
        self.assertIn("set -euo pipefail", code)
        for forbidden in ("|| true", "||true", "set +e", "continue", "-short", "-skip", "t.Skip", "|| :", "exit 0"):
            self.assertNotIn(forbidden, code)

    def test_guards_catch_mutated_runners(self):
        text = (ROOT / "scripts" / "check-phase4-tests.sh").read_text(encoding="utf-8")
        masked = text.replace("bash scripts/go-test-nonempty.sh", "! bash scripts/go-test-nonempty.sh || true; true #", 1)
        mutants = {
            "family dropped": text.replace("TestPhase4Delivery", "TestPhase4Dropped"),
            "family renamed broadly": text.replace("^TestPhase4Stream$", "^TestPhase4"),
            "race removed": text.replace("-race", ""),
            "tag removed": text.replace("-tags=phase4", "-tags=integration"),
            "budget relaxed": text.replace("-timeout=6m", "-timeout=60m"),
            "failure masked": masked,
        }
        for label, mutant in mutants.items():
            with self.subTest(mutation=label):
                self.assertNotEqual(mutant, text, "mutation must change the runner")
                with self.assertRaises(AssertionError, msg="mutant accepted: " + label):
                    result, calls = self.run_guard(script=mutant)
                    self.assertEqual(result.returncode, 0)
                    self.assert_exact_calls(calls, EXPECTED)
                    failed, _ = self.run_guard(script=mutant, arguments=("auth",), failed="^TestPhase4Auth$")
                    self.assertNotEqual(failed.returncode, 0)

    # --- family source ---------------------------------------------------

    def test_exact_family_union_matches_the_native_test_sources(self):
        found = set()
        for path in sorted((ROOT / "backend" / "testphase4").glob("*_test.go")):
            source = path.read_text(encoding="utf-8")
            self.assertTrue(source.startswith("//go:build phase4\n"), f"{path.name} must stay phase4-only")
            self.assertNotIn("t.Skip", source, f"{path.name} must not skip")
            self.assertNotIn("testing.Short", source)
            found.update(re.findall(r"^func (TestPhase4[A-Za-z]+)\(t \*testing\.T\)", source, re.M))
        self.assertEqual(found, {name for name, _ in FAMILIES.values()})

    def test_legacy_integration_package_is_not_polluted_by_phase4(self):
        for path in (ROOT / "backend" / "test").glob("*.go"):
            self.assertNotIn("phase4", path.read_text(encoding="utf-8"), path.name)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn(
            "\tcd backend && go test -tags=integration -timeout 32m -p 1 ./test/...\n", makefile)
        self.assertNotIn("-tags=phase4", makefile.split("test-integ:", 1)[1].split("\n\n", 1)[0])

    # --- CI --------------------------------------------------------------

    def test_ci_runs_phase4_jobs_and_the_unskippable_script_guard_exactly(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        assert_phase4_workflow(self, workflow)

    def test_ci_guard_rejects_skips_masks_and_incomplete_matrices(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        selector = "        run: bash scripts/check-phase4-tests.sh '${{ matrix.group }}'"
        browser = "        run: bash scripts/check-phase4-browser.sh"
        guard = "        run: python3 -B -m unittest discover -s scripts -p test_phase4_guards.py"
        mutations = {
            "job skipped": ("    strategy:\n      fail-fast: false\n      matrix:\n        group: [auth",
                            "    if: false\n    strategy:\n      fail-fast: false\n      matrix:\n        group: [auth"),
            "selector skipped": (selector, "        if: false\n" + selector),
            "selector masked": (selector, selector + " || true"),
            "selector continues": (selector, "        continue-on-error: true\n" + selector),
            "family missing": ("group: [auth, alerts, events, triage, summary, delivery, stream, tenant]",
                               "group: [auth, alerts, events, triage, summary, delivery, stream]"),
            "family excluded": ("group: [auth, alerts, events, triage, summary, delivery, stream, tenant]\n",
                                "group: [auth, alerts, events, triage, summary, delivery, stream, tenant]\n"
                                "        exclude:\n          - group: tenant\n"),
            "fail fast": ("fail-fast: false\n      matrix:\n        group: [auth",
                          "fail-fast: true\n      matrix:\n        group: [auth"),
            "larger budget": ("timeout-minutes: 45\n    needs: [go]\n    strategy:\n      fail-fast: false\n"
                              "      matrix:\n        group: [auth",
                              "timeout-minutes: 60\n    needs: [go]\n    strategy:\n      fail-fast: false\n"
                              "      matrix:\n        group: [auth"),
            "browser skipped": (browser, "        if: false\n" + browser),
            "browser masked": (browser, browser + " || true"),
            "browser continues": (browser, "        continue-on-error: true\n" + browser),
            "guard skipped": (guard, "        if: false\n" + guard),
            "guard masked": (guard, guard + " || true"),
            "guard continues": (guard, "        continue-on-error: true\n" + guard),
            "guard removed": (GUARD_STEP + "\n", ""),
        }
        for label, (before, after) in mutations.items():
            with self.subTest(mutation=label):
                self.assertEqual(workflow.count(before), 1, "mutation must target one actual field")
                with self.assertRaises(AssertionError, msg="CI bypass accepted: " + label):
                    assert_phase4_workflow(self, workflow.replace(before, after, 1))

    def test_ci_guard_checks_job_fields_that_follow_separator_comments(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        for name in ("phase4", "phase4-browser"):
            lines = workflow.split("\n")
            start = lines.index(f"  {name}:")
            end = next(i for i in range(start + 1, len(lines)) if re.fullmatch(r"  [A-Za-z0-9_-]+:\s*", lines[i]))
            # Insert after the last step, before the next comment/job: YAML
            # comments do not end the mapping, so the field still belongs here.
            comment_end = end
            while lines[comment_end - 1].lstrip().startswith("#") or not lines[comment_end - 1].strip():
                comment_end -= 1
            for field in ("    if: false", "    continue-on-error: true"):
                with self.subTest(job=name, field=field):
                    mutant = lines[:end] + [field] + lines[end:]
                    # field placed after the separator comments, directly before the next job key
                    with self.assertRaises(AssertionError, msg="post-comment field accepted"):
                        assert_phase4_workflow(self, "\n".join(mutant))
                    self.assertGreaterEqual(end, comment_end)

    def test_ci_retains_every_legacy_family_job_and_time_limit(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
        for job in ("integration", "phase3", "compose-smoke"):
            self.assertIn("timeout-minutes: 45", job_block(workflow, job))
        self.assertIn("      matrix:\n        group: [core, scenarios]", job_block(workflow, "phase3"))
        self.assertIn("run: bash scripts/check-phase3-tests.sh '${{ matrix.group }}'", job_block(workflow, "phase3"))
        integration = job_block(workflow, "integration")
        self.assertIn("run: make test-integ", integration)
        for group in ("API key", "ingest", "dedupe schema and TTL", "writer and DLQ",
                      "recovery", "rate limit", "readiness"):
            self.assertIn(group + " integration group is nonempty", integration)
        self.assertIn("timeout 32m scripts/go-test-nonempty.sh", job_block(workflow, "compose-smoke"))
        python = job_block(workflow, "python")
        self.assertIn("-p test_phase3_guards.py", python)
        self.assertLess(python.index("test_phase3_guards.py"), python.index("test_phase4_guards.py"))
        for job in ("phase4", "phase4-browser"):
            self.assertLessEqual(int(re.search(r"timeout-minutes: (\d+)", job_block(workflow, job)).group(1)), 45)

    # --- Make and docs ---------------------------------------------------

    def test_make_alias_runs_only_the_phase4_runner(self):
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(makefile, r"(?m)^\.PHONY:.*\btest-phase4\b")
        self.assertRegex(makefile, r"(?m)^test-phase4:.*##")
        result = subprocess.run(["make", "-n", "test-phase4"], cwd=ROOT, text=True, capture_output=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "bash scripts/check-phase4-tests.sh")

    def test_docs_describe_the_gate_without_claiming_phase_completion(self):
        operations = (ROOT / "docs/operations.md").read_text(encoding="utf-8")
        self.assertIn("scripts/check-phase4-tests.sh", operations)
        self.assertIn("make test-phase4", operations)
        self.assertIn("scripts/check-phase4-browser.sh", operations)
        threat = (ROOT / "docs/threat-model.md").read_text(encoding="utf-8")
        self.assertIn("T04-35", threat)
        self.assertIn("T04-36", threat)
        architecture = (ROOT / "docs/architecture.md").read_text(encoding="utf-8")
        self.assertIn("check-phase4-tests.sh", architecture)
        for text in (operations, threat, architecture):
            self.assertNotRegex(text.lower(), r"phase 4 (is )?(complete|accepted|closed)")


# --- launcher independence (04-18 / G1) -----------------------------------

HELPER_CALL = "phase4Config(t, root)"
FAMILY_FUNC = re.compile(r"^func (TestPhase4[A-Za-z]+)\(t \*testing\.T\)", re.M)
KEYCTL_CALL = 'subprocess.run(["go", "run", "./tools/keyctl"'
PASSWORD = "pw-NEVER-in-messages-7c1e"


def family_bodies(sources: dict[str, str]) -> dict[str, str]:
    """Each TestPhase4* body up to the next top-level func."""
    bodies = {}
    for source in sources.values():
        for match in FAMILY_FUNC.finditer(source):
            rest = source[match.end():]
            following = re.search(r"^func ", rest, re.M)
            bodies[match.group(1)] = rest[:following.start()] if following else rest
    return bodies


def family_helper_violations(sources: dict[str, str]) -> list[str]:
    problems = []
    for name, body in sorted(family_bodies(sources).items()):
        helper = body.find(HELPER_CALL)
        if helper < 0:
            problems.append(f"{name}: does not call {HELPER_CALL}")
            continue
        for later in ("config.Load(", "startQueryAPI("):
            index = body.find(later)
            if 0 <= index < helper:
                problems.append(f"{name}: {later} runs before {HELPER_CALL}")
    return problems


def call_text(text: str, start: int) -> str:
    """The full call expression starting at start (balanced parentheses)."""
    depth = 0
    for index in range(text.index("(", start), len(text)):
        if text[index] == "(":
            depth += 1
        elif text[index] == ")":
            depth -= 1
            if depth == 0:
                return text[start:index + 1]
    raise AssertionError("unterminated call")


def seeder_env_violations(script: str) -> list[str]:
    problems = []
    if "from phase4_pg_env import keyctl_env" not in script:
        problems.append("seeder does not import keyctl_env from phase4_pg_env")
    if 'sys.path.insert(0, str(ROOT / "scripts"))' not in script:
        problems.append("seeder does not put the repo scripts dir on sys.path")
    calls = [call_text(script, m.start()) for m in re.finditer(re.escape(KEYCTL_CALL), script)]
    if len(calls) != 1:
        problems.append(f"expected exactly one keyctl subprocess call, found {len(calls)}")
    for call in calls:
        if not re.search(r"\benv=keyctl_env\(", call):
            problems.append("keyctl subprocess does not pass env=keyctl_env(...)")
    return problems


def phase4_sources() -> dict[str, str]:
    return {path.name: path.read_text(encoding="utf-8")
            for path in sorted((ROOT / "backend" / "testphase4").glob("*_test.go"))}


class Phase4HarnessEnvGuards(unittest.TestCase):
    # --- static guards ---------------------------------------------------

    def test_every_family_uses_the_shared_helper_before_config_and_children(self):
        sources = phase4_sources()
        self.assertEqual(set(family_bodies(sources)), {name for name, _ in FAMILIES.values()})
        self.assertEqual(family_helper_violations(sources), [])
        helper = (ROOT / "backend/testphase4/harness_env_test.go").read_text(encoding="utf-8")
        self.assertIn("func phase4Config(", helper)
        self.assertIn('t.Setenv("SENTINEL_PRIMARY.ENV", "local")', helper)
        self.assertIn('t.Setenv("SENTINEL_ENV_FILE", envFile)', helper)
        self.assertIn("config.Load(config.ServiceQueryAPI)", helper)

    def test_family_guard_catches_a_dropped_or_late_helper(self):
        sources = phase4_sources()
        triage = sources["triage_test.go"]
        dropped = dict(sources, **{"triage_test.go": triage.replace(HELPER_CALL, "nil", 1)})
        late_child = dict(sources, **{"summary_rules_test.go": sources["summary_rules_test.go"].replace(
            "\t_ = " + HELPER_CALL + "\n\tbase, _ := startQueryAPI(t, binary, root, secret, \"\")\n",
            "\tbase, _ := startQueryAPI(t, binary, root, secret, \"\")\n\t_ = " + HELPER_CALL + "\n", 1)})
        late_load = dict(sources, **{"stream_test.go": sources["stream_test.go"].replace(
            "\tcfg := " + HELPER_CALL + "\n",
            "\t_, _ = config.Load(config.ServiceQueryAPI)\n\tcfg := " + HELPER_CALL + "\n", 1)})
        for label, mutant in (("helper dropped", dropped), ("startQueryAPI first", late_child),
                              ("config.Load first", late_load)):
            with self.subTest(mutation=label):
                self.assertNotEqual(mutant, sources, "mutation must change a family")
                self.assertNotEqual(family_helper_violations(mutant), [], "mutant accepted: " + label)

    def test_browser_seeder_derives_keyctl_env_from_the_repo_env(self):
        script = (ROOT / "scripts/check-phase4-browser.sh").read_text(encoding="utf-8")
        self.assertEqual(seeder_env_violations(script), [])
        self.assertEqual(script.count("env=keyctl_env("), 1)

    def test_seeder_guard_catches_a_dropped_keyctl_env(self):
        script = (ROOT / "scripts/check-phase4-browser.sh").read_text(encoding="utf-8")
        mutants = {
            "env dropped": re.sub(r",?\s*env=keyctl_env\([^)]*\)", "", script, count=1),
            "import dropped": script.replace("from phase4_pg_env import keyctl_env", "keyctl_env = dict", 1),
        }
        for label, mutant in mutants.items():
            with self.subTest(mutation=label):
                self.assertNotEqual(mutant, script, "mutation must change the seeder")
                self.assertNotEqual(seeder_env_violations(mutant), [], "mutant accepted: " + label)

    # --- phase4_pg_env.keyctl_env ----------------------------------------

    def write_env(self, directory: str, values: dict[str, str], extra: str = "") -> Path:
        path = Path(directory) / ".env"
        lines = ["# comment line", ""] + [f"{key}={value}" for key, value in values.items()]
        path.write_text("\n".join(lines) + "\n" + extra, encoding="utf-8")
        return path

    def full(self) -> dict[str, str]:
        return {"SENTINEL_DATABASE.HOST": "127.0.0.1", "SENTINEL_DATABASE.PORT": "15432",
                "SENTINEL_DATABASE.USER": "sentinel", "SENTINEL_DATABASE.PASSWORD": PASSWORD,
                "SENTINEL_DATABASE.NAME": "sentinel", "SENTINEL_DATABASE.SSL_MODE": "disable"}

    def expected(self) -> dict[str, str]:
        return {"PGHOST": "127.0.0.1", "PGPORT": "15432", "PGUSER": "sentinel", "PGPASSWORD": PASSWORD,
                "PGDATABASE": "sentinel", "PGSSLMODE": "disable"}

    def test_keyctl_env_maps_database_settings_and_copies_base(self):
        import contextlib
        import io
        import phase4_pg_env
        with tempfile.TemporaryDirectory() as directory:
            path = self.write_env(directory, self.full())
            base = {"PATH": "/bin", "PGHOST": "ambient-host"}
            before = dict(base)
            printed = io.StringIO()
            with contextlib.redirect_stdout(printed), contextlib.redirect_stderr(printed):
                result = phase4_pg_env.keyctl_env(path, base)
            self.assertEqual(printed.getvalue(), "", "keyctl_env must print nothing")
            self.assertEqual(base, before, "base must not be mutated")
            self.assertIsNot(result, base)
            self.assertEqual(result, dict({"PATH": "/bin"}, **self.expected()))

    def test_keyctl_env_strips_quotes_and_lets_process_env_win(self):
        import phase4_pg_env
        values = dict(self.full(), **{"SENTINEL_DATABASE.HOST": '"db.local"', "SENTINEL_DATABASE.NAME": "'quoted'"})
        with tempfile.TemporaryDirectory() as directory:
            path = self.write_env(directory, values)
            result = phase4_pg_env.keyctl_env(path, {"SENTINEL_DATABASE.PORT": "25432"})
        self.assertEqual(result["PGHOST"], "db.local")
        self.assertEqual(result["PGDATABASE"], "quoted")
        self.assertEqual(result["PGPORT"], "25432", "process env wins, as with godotenv")

    def test_keyctl_env_rejects_missing_empty_or_invalid_values_without_leaking(self):
        import phase4_pg_env
        required = ("HOST", "PORT", "USER", "PASSWORD", "NAME")
        cases = {}
        for key in required:
            missing = self.full()
            del missing["SENTINEL_DATABASE." + key]
            cases["missing " + key] = (missing, "SENTINEL_DATABASE." + key)
            cases["empty " + key] = (dict(self.full(), **{"SENTINEL_DATABASE." + key: ""}),
                                     "SENTINEL_DATABASE." + key)
        for port in ("abc", "0", "65536", "-1", "15432x"):
            cases["port " + port] = (dict(self.full(), **{"SENTINEL_DATABASE.PORT": port}), "SENTINEL_DATABASE.PORT")
        for label, (values, key) in cases.items():
            with self.subTest(case=label), tempfile.TemporaryDirectory() as directory:
                path = self.write_env(directory, values)
                with self.assertRaises(ValueError) as raised:
                    phase4_pg_env.keyctl_env(path, {})
                self.assertIn(key, str(raised.exception))
                self.assertNotIn(PASSWORD, str(raised.exception), "error message leaks the password")

    def test_keyctl_env_needs_no_ambient_environment(self):
        from unittest import mock
        import phase4_pg_env
        with tempfile.TemporaryDirectory() as directory:
            path = self.write_env(directory, self.full())
            scrubbed = {"PATH": os.environ.get("PATH", "/usr/bin"), "HOME": os.environ.get("HOME", "/")}
            with mock.patch.dict(os.environ, scrubbed, clear=True):
                result = phase4_pg_env.keyctl_env(path, os.environ)
        for key, value in self.expected().items():
            self.assertEqual(result[key], value)

    def test_keyctl_env_resolves_all_keys_from_the_committed_example(self):
        import phase4_pg_env
        result = phase4_pg_env.keyctl_env(ROOT / ".env.example", {})
        self.assertLessEqual({"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE"}, set(result))


if __name__ == "__main__":
    unittest.main()
