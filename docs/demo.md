# Preserved-state detection demo

Local Phase 3 acceptance gates passed; phase-wide goal verification and hosted CI
remain pending. Scoped reservation R2 review is clean; existing installations must complete the
[stopped-writer rollout](operations.md#reservation-protocol-rollout) before these
commands. Earlier
actual preserved-stack, exact static/live and repeat results are recorded in
[Plan 13](../.planning/phases/03-detection-alert-pipeline/03-13-SUMMARY.md).
The later repaired full integration, exact-family matrix with affected-fixture
delta verification, seed and preserved-repeat results are recorded in the
[local acceptance evidence](../.planning/phases/03-detection-alert-pipeline/03-ALERT-RESERVATION-EVIDENCE.md).

Run from repository root using the prerequisites and private local `.env` setup
in [README](../README.md). Examples use synthetic documentation-range addresses
and pseudonymous principals. Pause other producers until every scenario's final
marker is acknowledged. Keep the same Kafka topics/groups and existing volumes.

## Legacy seed, then two exact live runs

```sh
make dev-up
scripts/check-stack.sh
make seed
timeout 16m make seed-scenarios \
  SCENARIO_OUTPUT="$PWD/.local/demo-first.json" \
  SCENARIO_PRESERVE="$PWD/.local/last-send.json"
timeout 16m make seed-scenarios \
  SCENARIO_OUTPUT="$PWD/.local/demo-second.json" \
  SCENARIO_PRESERVE="$PWD/.local/demo-first.json"
```

`make seed` keeps its 1,000-current-event contract, API spot checks and other-tenant
404 check. Each scenario command chooses fresh synthetic entities/event IDs,
expects eight exact alerts, and verifies Kafka key/value bytes, immediate-dedupe
CH content and PG immutable/list/workflow fields. It retains full prior event,
CH and workflow snapshots, including workflow seq and status. Historical rows
cannot satisfy current acknowledged event IDs: their `received_at` must be at or
after that run's original UTC-millisecond `run_start`.

Success requires `verified=true`, `prior_rows_preserved=true`, `drained=true`,
and `new_late_drops=0` in each private output. A nonzero exit or false flag is
failure. Acknowledged output is first written with false flags so a failed final
reconciliation does not become a success claim. Do not print full manifests or
keys. Files use private 0600 permissions, new private directories use 0700, and
symlink/oversized/ambiguous inputs refuse. Keys enter children through the
environment/private files, never argv.

The two normal late-counter baselines are captured after startup/legacy seed.
Legacy random historical seed can itself be late; zero delta applies to each
normal scenario run. A detector restart or counter reset during a run refuses
acceptance rather than hiding a changed baseline.

## What the scenarios mean

| Generator scenario | Predicate / expected behavior |
| --- | --- |
| `auth-brute-force` | Ten unique failures by tenant/source/principal cross AUTH's threshold |
| `port-scan` | Twenty exact distinct destination ports on one destination cross SCAN's threshold |
| `request-flood` | Three hundred unique requests by tenant/source cross FLOOD's threshold |
| `deny-spike` | Thirty deny decisions by tenant/source cross DENY's threshold |
| `benign` | Explicit negative fixture; no alert |
| `boundary` | Events straddling the tumbling edge stay separate; the qualifying window fires once |
| `lateness` | Watermark equality contributes; one millisecond older is dropped |
| `all` | Two eligible windows per rule, eight exact alerts, with benign traffic included |

The ordinary operator command uses `all`. The edge acceptance additionally uses
boundary/lateness expectations: ten exact alerts and exactly one intentional new
late drop. AUTH crossings at 299s and 301s are a different adjacent-window
cooldown case: only the first is visible under the 60s cooldown. Equality at the
next eligible time allows a fresh window. Suppressed windows remain frozen;
waiting for cooldown cannot make that same window retry.

## Why a live run may wait or refuse

The private read-only `detection-state` helper snapshots all six source-partition
ends and waits for compatible format-2 `processed_next` to catch up. An active
replay pin may remain behind. It distinguishes empty partitions explicitly and
derives processed maximum from `sealed_through + lateness`, not the pre-pin
maximum. A capped retained scan can reconstruct a clock but cannot fabricate
completed detector effects; missing/corrupt/trimmed metadata still refuses.

The live base is at least `max(now, every caught-up maximum + 1ms)`. Actual keyed
partition mapping is obtained before scheduling. Same-millisecond contributors
precede nonmatching `auth.success` markers on every required partition at the
shared latest relevant window end plus 30s. Each batch waits until ingest's
five-minute future-skew allowance permits it, with a two-second margin.
Wall-clock idleness itself never advances the detector watermark.

Snapshot/scan, schedule construction, legal waits and delivery share one original
15m deadline. Each actual batch retains the unchanged five-attempt/120s sender
wait allowance; the final 60s is reserved inside the original deadline for
reconciliation. Preflight rejects an impossible budget before injection. One
Make invocation has a 16m outer cap; the two-run acceptance has 32m. These are
ceilings, not promised demo latency. Moving producers, excessive prior future
time, source retention loss, process restarts and inconsistent PG/Kafka recovery
cause bounded refusal. Do not reset watermarks/groups or substitute an arbitrary
past anchor to force a pass.

## Exact acceptance and recovery commands

After the healthy stack prerequisite, the finite selectors are:

```sh
scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$' -tags=integration -timeout 16m -p 1
timeout 32m scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenariosPreservedRepeat$' -tags=integration -timeout 32m -p 1
scripts/go-test-nonempty.sh './test/...' '^TestDetectorMidWindowRecovery$' -tags=integration -timeout 6m -p 1
scripts/go-test-nonempty.sh './test/...' '^TestDetectorThirdReplicaRebalance$' -tags=integration -timeout 6m -p 1
scripts/go-test-nonempty.sh './test/...' '^TestDetectorCrashBoundaries$' -tags=integration -timeout 6m -p 1
scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooldownCrash$' -tags=integration -timeout 8m -p 1
```

The scenario parent runs isolated complete fixed canonical fixtures through
default binaries, then preserved live edges. Repeat runs the ordinary legacy
seed and both actual Make commands, preserving prior rows. Historical fixed
fixtures are isolated test data; they are never injected as a live preserved
group timebase. The private verifier caps event batches at 5,000 IDs/rows and
32 MiB and keeps the existing earliest received-row policy; it does not perform
an N+1 lookup per event or weaken full-row checks.

Recovery tests use private integration-only causal barriers and real Kafka/CH/PG.
They observe nine incorporated failures before killing the detector, restore
before the tenth, prove a seeded target's actual move to the third owner, and
cover publication/acknowledgement/checkpoint and PG-decision cuts. Resolved
workflow fields and complete canonical bytes must survive replay. An earlier
03-12 third-target timeout remains historically unexplained; later passing
repeats do not establish its cause.

Run the complete [operations regression gates](operations.md#regression-commands)
for final proof. Hosted CI remains pending; no benchmark, exactly-once claim,
security certification, dashboard or SSE result is implied by this demo.
