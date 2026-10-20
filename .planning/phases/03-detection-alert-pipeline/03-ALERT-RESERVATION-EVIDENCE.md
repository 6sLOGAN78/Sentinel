# Alert reservation evidence and release boundary

Checkpoint: implementation Tasks 1–3 are committed and task-reviewed. Independent
whole-fix R2 review, CR-02-R1 disposition, final Phase 3 gates and hosted CI are
pending. This is evidence provenance, not Phase 3 completion or security certification.
The [approved design](03-ALERT-RESERVATION-DESIGN.md) defines the invariant and rollout.

## Durable invariant

A conforming writer acknowledges a permanent global-ID PG reservation before
attempting CH insertion. All subsequent effects use its validated canonical bytes.
Temporary transaction/connection loss cannot replace that winner. Workflow remains
post-CH and insert-only; reservation alone permits neither a workflow nor source
acknowledgement. Commit uncertainty is resolved by replay, not guessed success.
Physical identical duplicates remain possible: delivery is at-least-once.

Claims have no automatic GC, lease or transfer. Safety assumes intact PG durability
and all active writers upgraded. Direct privileged writes/schema destruction and
pre-upgrade uncertain writes are not made safe retroactively.

## Committed evidence and its limits

The full local reports are preserved under the intentionally ignored orchestration
directory `.superpowers/sdd/03-ALERT-RESERVATION-IMPLEMENTATION-PLAN/` as
`task-1-report.md`, `task-2-report.md`, and `task-3-report.md`. This durable record
summarizes their returned command results; it does not claim these commands were
rerun during Task 4's review hold.

| Proof class | Actual evidence | Boundary |
| --- | --- | --- |
| Native PG schema | `f08a060` assertion RED for absent relation; `ca7a4cd` additive migration GREEN; fresh and version-5 upgrades, 46 boundary/null cases, global uniqueness, immutable UPDATE/DELETE, workflow/sequence preservation and no-op rerun | Schema alone does not establish runtime ordering |
| Protocol/model + native admission | `59cb7ac` compiled assertion RED; `a5a564e` GREEN: independently visible committed native PG row at CH boundary, one-slot pool, saved mutable representation, legacy workflow/orphan reconciliation, corrupt-claim refusal, conflict/DLQ/source checks | Fake commit uncertainty models both committed-answer-lost and rejected outcomes; do not call the model a network fault |
| Real PG commit ambiguity | `3a26feb` ambiguity family uses actual pgx commit or rollback before returning uncertainty, then independently reacquires the same-ID lock | The wrapper controls the answer; it is not an observed PG network partition |
| Real process boundary | `3a26feb` kills/joins the integration child after committed reservation and before CH; independent stores/source show claim only; a fresh process recovers saved bytes despite mutable input changes | Integration-only barrier; default-binary fresh marker remains empty. No remote CH effect had begun at this cut |
| Native CH ACK loss | `3a26feb` unchanged native request through loopback proxy; independent physical CH read observes A before response suppression/socket close; actual send returns EOF, PG transaction ends/source remains uncommitted; independent B rejected before fresh-process A recovery | Proves result loss **after visible insertion**, not a native pre-visibility pause |
| Deferred visibility model | `3a26feb` controlled A request returns uncertainty while invisible, actual PG transaction ends, B attempts, then A is released into native CH | Establishes the selected protocol schedule as a model; exact stock-server pre-visibility delay remains unproved |
| Default build isolation | `TestAlertWriterFailpointBuildBoundary` selects actual default Go files/build, calls unconditional no-op with tempting controls and asserts no marker; default child replays normally | No public failure endpoint or default environment-controlled fault hook |

Task 3's final independent commands each used
`bash scripts/go-test-nonempty.sh './test/...' '^FAMILY$' -tags=integration -timeout 6m -p 1`:

| Family / invocation | Returned result |
| --- | --- |
| `TestAlertWriterReservationAmbiguity` | exit 0; five subcases, family 4.06s / package 21.900s |
| `TestAlertWriterReservationNativeAckLoss`, repeat 1 | exit 0; family 1.49s / package 18.778s; EOF, 2,784 response bytes suppressed |
| `TestAlertWriterReservationRestart`, repeat 1 | exit 0; family 92.83s / package 117.940s |
| `TestAlertWriterReservationNativeAckLoss`, repeat 2 | exit 0; family 1.48s / package 26.620s; EOF, 2,784 response bytes suppressed |
| `TestAlertWriterReservationRestart`, repeat 2 | exit 0; family 92.86s / package 111.081s |

Final default failpoint gate used
`bash scripts/go-test-nonempty.sh './internal/alertwriter/...' '^TestAlertWriterFailpointBuildBoundary$' -race -timeout 60s`:
exit 0, family 1.65s / package 2.676s. Task 2's expanded native reservation family
passed in 23.388s; its pure race family passed in 1.056s. Task 1's final schema
family after lint-only repair `cf9b879` passed in 26.517s with all 46 nested cases.
Reports also record default/full unit, build/vet/lint and integration scoped lint
passes. These are focused task results, not a final full integration-matrix pass.

Provenance limits are retained: Task 3 observed genuine restart RED but omitted a
tests-only RED commit; history was not reconstructed. Its first hook-enabled run
failed a 12s crash-offset observer, corrected to the existing finite 70s observer
for the killed group (Kafka's session timeout is 45s), not a production budget
increase. Task 2's interrupted session had missing logs; unreturned results were
not inferred and the remaining five native families were run afresh. Task 1's
seven integration-lint findings were reproduced and repaired test-only in
`cf9b879`, without dropping assertions.

## Exact-family guard extension

Task 4 extends the existing 39 package/name families to 46, retaining separate
nonempty invocations. Five new native families use 6m/integration/`-p 1`:
`TestAlertReservationsSchema`, `TestAlertWriterReservation`,
`TestAlertWriterReservationAmbiguity`, `TestAlertWriterReservationRestart`,
`TestAlertWriterReservationNativeAckLoss`. Two pure alertwriter families use race
and 60s: `TestAlertWriterReservationProtocol`, `TestAlertWriterFailpointBuildBoundary`.
Cooldown 8m, scheduled scenario 16m and preserved repeat 32m remain unchanged.

`python3 scripts/test_phase3_guards.py` before registry implementation returned
exit 1: five methods in 74.316s, 15 intended assertion failures (seven independent
empty cases, seven failed-command cases, exact registry mismatch). Existing
runner/CI characterization remained loadable. Tests-first commit: `0d6d82d`.
The subprocesses use a temporary fake Go executable; these are selector/failure/
budget proofs, **not** native reservation execution. The exact same command after
registration returned exit 0: five methods in 75.314s. Every one of the 46
package/name families was independently checked for an empty result and failing
Go command despite a misleading PASS line, plus exact selectors/tags/budgets and
race on both new pure families. Shell syntax and owned-file whitespace checks
also returned exit 0. Final native/full gates are still held, not implicitly run.

## Outstanding release gates and historical failures

The pre-reservation full aggregate on source `01cf7a6` failed, not timed out:
`make test-integ` with a 32m cap returned exit 2 after 974.03s (package 972.784s).
`TestDetectionScenarios/isolated-static-all` verifier exited 1; the preserved
repeat's first Make child exited 2. Those helpers originally discarded diagnostic
output, so the causal failures remain unexplained. A later exact static-only run
on source `cbc13f2` returned exit 0 (package 47.449s), which does not explain the
earlier failure or establish final-source acceptance. No final 46-family script,
full integration, preserved repeat or hosted CI pass is claimed here.

The earlier measured 10m aggregate returned exit 2 after 620.17s and exhausted
its budget before scenarios; this justified a finite 32m aggregate, not individual
budget inflation. Plan 12's earlier third target-recovery timeout also remains
historically unexplained despite subsequent passing repetitions.

Rollout additionally requires drained/stopped old writers and resolution of old
uncertain remote effects; stopped local processes alone are insufficient. At the
current parent preflight Kafka/PG/CH/Redis were stopped and all five old apps were
restart-looping. The parent stopped exactly those five applications with Compose
(`stop --timeout 30`, exit 0), without removing containers, data or volumes.
Dependencies remain down. No migration, new writer startup, healthy stack or
current preserved-stack acceptance is claimed. New live/full gates remain held
until root clears R2 and explicitly checks the rollout preconditions.
