# Phase 3 hosted CI repair evidence

The earlier hosted run `39457945227` failed at an older remote checkpoint,
`19db519`; this document records local repair evidence, not a new hosted result.
The Python failure and historical secret-scan finding are repaired below. The
separate integration fixture repair and final live acceptance remain attributed
to their existing evidence. Hosted CI remains pending.

## Scope and regression sequence

The working tree initially had source HEAD
`a26e27ae1a089008b75dd8281758a3a4a58bfb60`; root's tracking-only commit
`60e3364` preceded the regression commit. The repair changes only
`.github/workflows/ci.yml`, `scripts/test_phase3_guards.py`, `.gitleaksignore`,
and this evidence document. No runtime, dependency, golden fixture, live-stack,
selector, timeout, or production fault-hook behavior changes.

The Python job already runs `uv sync --locked` in `generator`, but the scenario
unittests were subsequently launched with the system interpreter from the
repository root. Two tests import the generator CLI and sender, which need the
locked `httpx==0.28.1` dependency. The repaired step uses
`generator/.venv/bin/python`; no ambient package installation is needed.

1. Added a CI assertion requiring that interpreter and requiring the existing
   locked sync to precede the scenario step. The exact command
   `python3 -B -m unittest scripts.test_phase3_guards.Phase3GuardTests.test_ci_scenario_tests_use_the_synced_generator_environment`
   returned **1**, with the intended assertion failure against the old workflow.
   Committed the assertion RED as
   `59b3498a0b94cea43cf558ddaf28f75213b7c3a6` before editing the workflow.
2. Changed the one scenario-step command. The same regression command returned
   **0** (one test). The implementation and exact scanner exception were
   committed as `defe62ecda7f2ad191f67e48a4989f09bd1a4204`.
3. Reproduced the dependency boundary with the already available Python 3.13.11:
   `generator/.venv/bin/python -B -S -m unittest discover -s scripts -p test_seed_scenarios.py`
   returned **1**, including two `ModuleNotFoundError` errors for `httpx` and a
   downstream assertion failure. This environment reproduction is separate from
   the assertion RED. An earlier host `python3 -S` attempt used Python 3.10.12
   and failed on a standard-library compatibility issue, so it is not evidence
   of the missing dependency.
4. Executed the exact repaired workflow command,
   `generator/.venv/bin/python -B -m unittest discover -s scripts -p test_seed_scenarios.py`:
   **0**, all ten tests, 0.149 seconds.

The live `scripts/seed_scenarios.py` entry point already re-executes through
`uv run --locked --project generator python` before calling its main function.
The integration, Phase 3, and Compose smoke jobs already set up uv. No additional
prerequisite or live entry-point change was indicated by this failure.

## Historical scanner finding

CI pins gitleaks **8.30.1** and scans full history. The sole baseline finding
was `generic-api-key` at historical commit
`171497e5baeccbeb44d12b698be02ceec6b91d65`,
`.planning/phases/03-detection-alert-pipeline/03-08-PLAN.md`, line 148.
Private inspection of that line and the pinned default rule established that
the captured text consists of ordinary YAML-validation policy words describing
duplicated or absent fields. It is prose, not a credential or credential
assignment. No credential value is reproduced here.

`.gitleaksignore` adds only the scanner's exact historical
commit/path/rule/line fingerprint, following the existing fingerprint convention.
The default rules, full-history scan, and all other locations remain enabled.
No path allowlist, broad rule suppression, or history rewrite was introduced.

Root authorized the standalone existing CI scanner version in private ignored
`.local/phase3-acceptance/tools` after verifying its official release. The archive
and checksum file were downloaded from the
[official v8.30.1 release](https://github.com/gitleaks/gitleaks/releases/tag/v8.30.1).
Both SHA256 digests and the archive's matching checksum-file entry were verified
before execution:

- `gitleaks_8.30.1_linux_x64.tar.gz`:
  `551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb`
- `gitleaks_8.30.1_checksums.txt`:
  `061476c21adaf5441516f96f185c1a4706a83cd6329b9b38762271b3d4a52fae`

Archive inspection showed only `LICENSE`, `README.md`, and `gitleaks`; only the
binary was extracted. `gitleaks version` returned `8.30.1`. This is an ignored
standalone tool, not a project or ambient Python/Go/npm dependency installation.

The CI-equivalent full-history command, with private redacted JSON evidence,
was run before and after the exact fingerprint exception:

```sh
.local/phase3-acceptance/tools/gitleaks git --redact --no-banner -v \
  --report-format json --report-path .local/phase3-acceptance/gitleaks-before.json
.local/phase3-acceptance/tools/gitleaks git --redact --no-banner -v \
  --report-format json --report-path .local/phase3-acceptance/gitleaks-after.json
```

The baseline returned **1**, exactly one finding at the fingerprint above.
The repaired scan returned **0**, with zero findings. All scanner reports and
logs are private, mode 0600, beneath ignored `.local/phase3-acceptance`.

## Local regression verification

`generator/.venv/bin/python -B -m unittest discover -s scripts -p 'test_*.py'`
returned **0**, all **44** script tests, 77.382 seconds, including every existing
script guard and the added CI assertion. The test process used runner-normal
`umask 022`, while its parent created the redirected log under `umask 077`.

The pinned redacted full-history scan was repeated after implementation commit
`defe62ecda7f2ad191f67e48a4989f09bd1a4204`, using report
`.local/phase3-acceptance/gitleaks-post-repair.json`: **0**, zero findings.
`git diff --check` for the owned repair/evidence files also returned **0**.
These checks do not replace the independently owned live gates or establish a
hosted CI pass. No remote operation was performed by this repair worker.

An initial all-script run propagated the private log wrapper's `umask 077` into
test fixtures and returned **1** (44 tests, three failures): two directory-mode
assertions and one public-file rejection assertion. Those fixtures intentionally
assume the runner's ordinary `umask 022`. This was not the CI regression RED;
no fixture or assertion was changed. The corrected invocation creates its log
under private mode first, then restores `umask 022` only inside the test process.
