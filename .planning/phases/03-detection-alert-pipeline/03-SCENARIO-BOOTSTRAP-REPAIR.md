# Python 3.10 scenario bootstrap repair

Base: `c668335f7cd5fd4f5856053a6da3667a25c74767`.
Permanent regression RED: `d9a8c76bc7d907c6eb7f9b979783c7e9534733d9`.
Compatibility GREEN: `2d446775ebb4d2ff2e8a0145dd66e28a06c769b9`.
Owned source scope: `scripts/seed_scenarios.py` and
`scripts/test_seed_scenarios.py` only.

## Concrete failure and scope

The replacement full integration gate actually failed: package1581.355s,
make exit2, external elapsed19688.54s, ending2026-10-21T11:01:29Z.
`TestDetectionScenarios/preserved-live-edges` reported an immediate unclassified
261-byte child failure after0.08s. Its sanitized diagnostic did not retain the
raw child traceback. A separate preserved-repeat failure reported Compose
startup; root retained migration logs establish a proxy.golang.org connection
reset while fetching pinned golang.org/x/textv0.41.0. These are distinct failures.
See [cooperative repeat evidence](03-COOPERATIVE-REPEAT-EVIDENCE.md).

This repair independently reproduces the scenario entry failure using the actual
system Python3.10.12: `/usr/bin/python3.10 -B scripts/seed_scenarios.py --help`
raises `ImportError: cannot import name 'UTC' from 'datetime'`, exit1. The
top-level import executes before the existing bottom-of-file handoff to the
locked generator interpreter. The immediate failure and traceback size match
root's bootstrap diagnosis; no raw historical child traceback is invented.

The generator remains locked to Python>=3.13,<3.14; the existing environment is
Python3.13.11. Only the standard-library launcher needs to load on system3.10
before handing off. All generator and httpx imports remain deferred, and the
direct `scripts.seed` dependency uses compatible imports. No generator runtime
requirement, CI interpreter selection, dependency pin, verifier, fixture,
deadline or canonical expectation changed.

## Causal regression and minimal change

The permanent guard runs the actual script entrypoint in an isolated subprocess.
On modern CI interpreters it deletes the optional `datetime.UTC` attribute to
model the precise Python3.10 stdlib boundary, forces a synthetic system prefix,
and intercepts only `os.execvpe`. It asserts the exact existing `uv run --locked
--project ... python -B ...` argument array, including forwarded CLI flags.
The interception exits before `main`, so no scenario traffic or external service
is invoked. Skipping the handoff cannot satisfy the expected JSON result.

At REDd9a8c76, the test module loads normally under the locked interpreter and
the new test reaches its intended assertion: subprocess exit1 instead of0,
caused by the reproduced UTC ImportError. This is assertion RED, not a failed
test collection or empty selector. The real3.10 RED is separate native evidence;
the portable guard is explicitly a stdlib model.

GREEN2d44677 imports `timezone` and binds `UTC = timezone.utc`, the same UTC
singleton used by modern `datetime.UTC`. The existing locked-runtime handoff
and execution order are unchanged. Both the guard and the real3.10 CLI now
reach the intended boundary. No earlier bootstrap refactor was necessary.

## Actual checks and evidence limits

Commands ran from the repository root with umask022. Private logs are under
ignored `.local/phase3-acceptance/` (directory0700, logs0600), each with actual
start/end and returned exits.

| Check | Actual result | Log |
| --- | --- | --- |
| Actual `/usr/bin/python3.10 -B scripts/seed_scenarios.py --help` before repair | Exit1; UTC ImportError; 11:12:42Z | `scenario-bootstrap-native-red.log` |
| Locked `python -B -m unittest scripts.test_seed_scenarios.ScenarioTests.test_bootstrap_reaches_locked_runtime_without_datetime_utc -v` before repair | Exit1; one intended assertion failure,0.067s; 11:12:43Z | `scenario-bootstrap-guard-red.log` |
| Same guard after repair | Exit0; one test,0.063s; 11:12:57Z–11:12:58Z | `scenario-bootstrap-guard-green.log` |
| Actual3.10 `--help`, then `--bootstrap-invalid-option`, both with `UV_OFFLINE=1` | Help exit0; invalid argument exits2 with normal argparse usage; wrapper exit0; 11:13:18Z | `scenario-bootstrap-native-green.log` |
| `generator/.venv/bin/python -B -m unittest discover -s scripts -p 'test_*.py' -v` | Exit0; all45tests passed in77.237s; 11:13:18Z–11:14:35Z | `scenario-bootstrap-scripts-green.log` |

The native CLI checks use safe parsing exits and do not load credentials, send
traffic or contact the stack. Offline mode allows the existing locked environment
handoff without a network dependency; no dependency or interpreter was installed.
The complete45-case script suite includes all44existing cases plus the new
bootstrap guard. Independent review remains pending at this draft. No replacement
full/native gate or new hosted success is claimed.

Root owns preserved-stack recovery, acceptance reruns, tracking and authorized
remote operations. This worker performed no stack startup, native integration
test, data reset, remote write, dependency installation or Phase4 work. The
external-versus-package time discrepancy in the failed aggregate is retained;
no exact historical scheduling cause is inferred from this bootstrap repair.
