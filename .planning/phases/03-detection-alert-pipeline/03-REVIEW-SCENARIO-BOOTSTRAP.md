---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-21T11:20:10Z
depth: deep
files_reviewed: 2
files_reviewed_list:
  - scripts/seed_scenarios.py
  - scripts/test_seed_scenarios.py
diff_base: c668335f7cd5fd4f5856053a6da3667a25c74767
red_commit: d9a8c76bc7d907c6eb7f9b979783c7e9534733d9
reviewed_head: 2d446775ebb4d2ff2e8a0145dd66e28a06c769b9
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Scenario bootstrap repair: scoped code review

## Narrative Findings (AI reviewer)

No actionable BLOCKER or WARNING found in this two-file repair. Both files were
read completely, the immutable RED/GREEN diff was inspected, and the direct
launcher dependency `scripts/seed.py`, generator runtime requirement, Make
entrypoint and CI test invocation were traced read-only. No structural pre-pass
was supplied. No project AGENTS.md, project-local skill directories or ignore
exclusion for the two source files was found.

### Compatibility and execution boundary

The change at `scripts/seed_scenarios.py:23` imports the Python-3.10-compatible
`timezone` type and binds `UTC = timezone.utc` at line 33. This is the same UTC
singleton represented by modern `datetime.UTC`; no timestamp arithmetic or
serialization policy changes. The direct seed-module dependency performs no
credential reads, stack calls or generator imports while loading. Generator
and httpx imports remain inside functions after the launcher boundary.

The existing bottom-of-file handoff at line 593 remains unchanged: outside the
generator virtual environment it invokes `uv run --locked --project ... python
-B` with the resolved script path and all original CLI arguments. The generator
still requires Python >=3.13,<3.14; the fix permits the system interpreter to
reach that boundary, not to run the generator on Python 3.10. No dependency,
verifier, deadline, payload assertion or runtime requirement was relaxed.

### Regression reliability and isolation

The new test at `scripts/test_seed_scenarios.py:16` uses an isolated subprocess,
removes the modern stdlib's UTC attribute, forces a non-generator prefix and
runs the actual script as `__main__`. Only the real `os.execvpe` boundary is
intercepted. A zero return code alone cannot pass: stdout must decode to the
exact program and argument array, including `--locked`, the generator project,
the actual script, `-B`, and both forwarded flags. An import error fails the
return-code assertion; skipping the handoff produces argparse help rather than
the required JSON and fails. Missing or changed handoff arguments fail exact
equality. The 15-second timeout fails rather than skips.

The stub exits before `main`, and its state changes are confined to the child
process. The selected help/invalid-option native checks exit during parsing,
before `execute`, credential reads, private manifest writes or scenario traffic.
The repair adds no shell interpolation, secret output or network operation.
The test module's own modern UTC import remains appropriate because these
script tests run under the locked generator interpreter, not the system
bootstrap interpreter.

### Actual evidence and limits

The following private logs were read directly:

- `scenario-bootstrap-native-red.log`: actual system Python 3.10 fails on the
  original top-level `from datetime import UTC` import, exit 1, at 11:12:42Z.
- `scenario-bootstrap-guard-red.log`: the new test at RED reaches its intended
  assertion failure, caused by the same missing UTC attribute in the controlled
  modern-interpreter subprocess; one test, 0.067s, exit 1.
- `scenario-bootstrap-guard-green.log`: the identical guard passes at GREEN;
  one test, 0.063s, exit 0, 11:12:57Z–11:12:58Z.
- `scenario-bootstrap-native-green.log`: actual Python-3.10 CLI help returns 0
  and an invalid option returns argparse status 2 at 11:13:18Z. The log records
  both child statuses and END, not an additional overall wrapper EXIT marker.
- `scenario-bootstrap-scripts-green.log`: all 45 script tests pass in 77.237s,
  exit 0, 11:13:18Z–11:14:35Z, including the new guard and all prior cases.

The permanent guard is a MODEL of the missing stdlib attribute, not a complete
Python-3.10 runtime emulation. Native Python-3.10 RED/GREEN evidence is separate.
The original full-suite scenario diagnostic did not preserve a raw child
traceback, so the independently reproduced import defect must not be described
as a recovered historical traceback. The separate Compose module-download
connection-reset failure is not repaired or explained by this source change.
No fresh full integration or hosted-CI success is implied by this review.

This reviewer ran no tests, builds, live-service commands or remote operations,
modified no source, and made no commit. Only this new review artifact was
created; prior review reports and user changes were left untouched.

_Reviewer: independent gsd-code-reviewer; deep source/call-chain review with
Karpathy scope and evidence discipline._
