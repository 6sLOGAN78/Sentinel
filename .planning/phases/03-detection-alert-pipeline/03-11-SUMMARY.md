---
phase: 03-detection-alert-pipeline
plan: "11"
subsystem: streaming
tags: [go, kafka, dlq, postgres, checkpoint, bounded-retries, tdd]
requires:
  - phase: 03-09
    provides: Aggregate reducer accounting and restored current-assignment readiness
  - phase: 03-10
    provides: Serialized immutable winner and native two-store conflict quarantine
provides:
  - Event-free acknowledged poison offsets preserving strict format-2 replay pins and event clocks
  - One aggregate effects deadline, retry sequence and independent final commit reservation per owned batch
  - Real worker DLQ outage, trusted envelope and 400-record slow-poison proofs
affects: [03-12, 03-20, 03-13, 03-14]
tech-stack:
  added: []
  patterns: [partition-contiguous durable effects, event-free poison acknowledgement, shared failed-attempt allowance]
key-files:
  created:
    - backend/internal/detector/worker_test.go
    - backend/internal/detector/poison.go
    - backend/internal/alertwriter/poison.go
    - backend/test/detector_dlq_test.go
    - backend/test/alertwriter_dlq_test.go
  modified:
    - backend/internal/detector/worker.go
    - backend/internal/detection/reducer.go
    - backend/cmd/detector/main.go
    - backend/internal/alertwriter/writer.go
    - backend/internal/alertwriter/writer_test.go
key-decisions:
  - "03-11: Acknowledge poison source bookkeeping only after durable DLQ; never synthesize an event or advance event clocks, windows or evidence."
  - "03-11: Share one failed-attempt/backoff sequence and effects deadline across an owned batch, reserving independent final commit time under the original parent."
requirements-completed: [ALRT-01, ALRT-02, ALRT-03, ALRT-04, DET-06]
duration: 15 min
completed: 2026-10-19
---

# Phase 3 Plan 11: Bounded Poison and Durable Prefixes Summary

Detector poison now reaches the existing trusted DLQ before strict replay-pin progress; both workers share one bounded effects/retry sequence while preserving independently safe partition prefixes.

## Performance

- Started: 2026-10-19T07:23:58Z; implementation and verification completed approximately 07:38:53Z.
- Duration: approximately 15 minutes; tasks: 3/3; application/test files changed: 10.
- The parent owns the Wave 10 cross-plan gate and subsequent execution. No global requirement or Phase 3 completion is claimed.

## Accomplishments

- Detector classification strictly decodes capped events, requires server received_at, validates replay relative to occurred_at and checks tenant/source Kafka keys. Decode/schema/key/missing-timestamp poison uses the existing three fixed DLQReason values. The default binary injects NewDLQPublisher with the configured topic/value cap and bounded lifecycle closer. Original source records reach the existing capped envelope builder; source dlq.* headers cannot override trusted topic/partition/offset.
- AcknowledgePoison reserves the existing aggregate prefix-ledger entry and advances only monotone source bookkeeping after DLQ acknowledgement. It preserves nullable maximum, sealed floor, rule/evidence state and decision frontier; invalid order, pending valid effects and capacity rejection cannot mutate state. Poison-only metadata restores strictly. A live AUTH contributor keeps pin zero while acknowledged poison advances processed_next; failed poison preserves the exact prior durable checkpoint. Detector never uses consumed-next CommitRecords.
- Both workers cap all source effects by one deadline, also capped at parent deadline minus OperationTimeout. Effects expiry leaves a separate final commit context alive under the original parent. Successful poison does not replenish failed-attempt allowance or reset backoff. The writer shares that retry sequence across sink and quarantine work. Parent cancellation prevents new source commits and both runtime loops release their rebalance gate on exit.
- Immediate failed poison stops only its partition. Previously durable records and safe other partitions can commit their own contiguous prefixes. Failed, unacknowledged poison is never skipped. Detector state sealing/retirement remains after successful effects; ownership validation, real PG fences, retained-assignment re-registration and restored-snapshot readiness remain intact.
- Writer persistence still serializes the global alert ID, checks the authoritative PG/retained CH winner before insertion, acknowledges CH before insert-only workflow creation, and commits Kafka last. persist returns and independently rolls back its lock before slow conflict quarantine. Invalid schema/key/hash/entity content never reaches CH. CH/PG failure and ambiguous native acknowledgements remain replayable operational failures with no DLQ side effect; resolved workflows remain unchanged.
- Real families run default binaries for trusted classification and real in-process workers/native stores for outage and latency controls. Each worker receives 400 poison records under a finite batch budget; delayed successful publications traverse the actual DLQ publisher. Broker progress equals only the acknowledged prefix. Detector additionally proves all 400 were fetched together and ownership released exactly once. Valid late events remain normal drops; poison does not advance the real maximum/sealed floor.

## Task Commits

1. Task 1 — compiled behavioral RED and real acceptance fixtures: `f37731d` (`test`). Includes only the minimal detector DLQ interface/binder declaration, without handling behavior.
2. Task 2 — detector poison, source-only acknowledgement and runtime wiring: `a552d03` (`feat`).
3. Additional Task 2 guard — pending-effect/order rejection and immutable accounting: `3acb983` (`test`).
4. Task 3 — shared writer sink/poison retries and deadline reservation: `27c3774` (`fix`).

No tracked file was deleted. Alert-writer main already injects the real DLQ and propagates worker failure through lc.Fail; it required no cosmetic change.

## Recorded Verification

Every named family below used its own scripts/go-test-nonempty.sh invocation and actual PASS. Unit runs used -timeout 60s. Ordinary real runs used -tags=integration -timeout 6m -p 1. Package times include TestMain/container lifecycle; they are not throughput measurements.

| Command / stage | Actual result |
| --- | --- |
| `scripts/go-test-nonempty.sh './internal/detector/...' '^TestDetectorPoisonPrefix$' -timeout 60s`, before behavior | Assertion RED, exit 1, package 0.010s: malformed/schema/key records return terminal errors without quarantine, zero DLQ calls and no safe prefix. |
| Separate detector `'^TestWorkerAggregateBudget$'`, before behavior | Assertion RED, exit 1, package 0.006s: no earlier poison acknowledgements/prefix. Parent cancellation was a passing guard. |
| Separate writer `'^TestWorkerAggregateBudget$'`, before behavior | Assertion RED, exit 1, package 0.207s: repeated per-record retry allowance acknowledges 83 records where only one should be acknowledged. Existing aggregate wall-time and cancellation cases passed. |
| Separate writer `'^TestAlertWriterPoisonPrefix$'`, baseline/final | Characterization PASS, exit 0, 0.007s / 0.010s. Schema/key/hash/entity rejection and lock-release behavior already existed. Initial Error expectation was corrected because the observing consumer cancels after commit; it is not production RED. |
| Final separate selectors `'^TestDetectorPoisonPrefix$'`, `'^TestAlertWriterPoisonPrefix$'`, `'^TestWorkerAggregateBudget$'` over `'./internal/detector/... ./internal/alertwriter/...'` | All PASS, exit 0. Detector prefix 0.014s; writer prefix 0.006s; aggregate detector 0.135s/writer 0.134s. Includes short shutdown deadlines preserving commit reservation. Nonmatching package warnings do not replace the actual matching family's PASS. |
| Detector-only independent Prefix/Aggregate selectors after implementation | PASS, exit 0, final focused prefix 0.013s and aggregate 0.138s. Strict metadata, live pin, capacity and failed-prefix guards pass. |
| Writer-only independent Prefix/Aggregate selectors after implementation | PASS, exit 0, prefix 0.010s and final aggregate 0.136s. |
| `scripts/go-test-nonempty.sh './test/...' '^TestDetectorPoison$' -tags=integration -timeout 6m -p 1` | Final three cases PASS, exit 0, test 2.34s/package 20.582s: default binary envelope, real unavailable DLQ, real delayed 400-record batch/release. Earlier corrected two-case run also passed, package 19.916s. |
| Separate `'^TestAlertWriterPoison$'` with the same real flags | PASS, exit 0, test 2.98s/package 21.236s: schema/identity, real unavailable DLQ and 400 delayed records. |
| Separate retained `'^TestWriterPoisonToDLQ$'` | PASS, exit 0, test 1.63s/package 23.279s. Phase 2 files unchanged. |
| Separate retained `'^TestWriterDLQFailureNoCommit$'` | PASS, exit 0, test 7.41s/package 26.065s. |
| Separate retained `'^TestWriterSlowPoisonRebalance$'` | PASS, exit 0, three cases/test 13.42s/package 33.437s, including parent cancellation and failed earliest poison. |
| Separate `'^TestAlertWriterOrdering$'` unit selector | PASS, exit 0, package 0.038s: CH/PG/preflight/commit failures and equal-winner replay. |
| Separate real `'^TestAlertWriterPartialStorage$'` | PASS, exit 0, test 2.46s/package 21.817s: native CH-success/PG-failure and ambiguous CH acknowledgement remain uncommitted with no DLQ. |
| Separate real `'^TestAlertWorkflowReplay$'` | PASS, exit 0, test 1.18s/package 19.955s: resolved workflow retained. |
| Separate real `'^TestDetectorCapacity$'` | PASS, exit 0, test 7.28s/package 24.670s: terminal capacity keeps exact prior metadata, runtime late counter, joined-empty and retained nine-before-join/ten-after-leave evidence. |
| Separate real `'^TestDetectionPipelineAuth$'` | PASS, exit 0, test 4.58s/package 24.580s: exact alert evidence, real PG decision and live format-2 pin. |
| `timeout 300s make test` | PASS, exit 0: backend Go race suite, web 6 tests and generator 121 tests. Detection package 46.884s. |
| `timeout 300s make lint` | PASS, exit 0: Go 0 issues, web lint/typecheck and generator checks. Owned complexity/line-length/setup-size findings were repaired without policy changes. |
| Final `cd backend && timeout 120s go tool -modfile=tools.mod golangci-lint run ./... && go test -race ./internal/detector/... ./internal/alertwriter/... -timeout 60s` | PASS, exit 0, Go 0 issues; final added ordering guard included, packages 1.192s/1.197s. |
| `cd backend && timeout 120s go build ./... && timeout 120s go vet -tags=integration ./...` | PASS, exit 0. |
| `cd backend && timeout 180s go tool -modfile=tools.mod golangci-lint run --build-tags=integration ./...` | Final exit 1, solely pre-existing test/recovery_test.go:162 unparam, explicitly deferred to 03-14. No owned finding remains. |
| `git diff --check` and changed-file stub scan | PASS, no unfinished runtime placeholder/TODO/FIXME or unexpected tracked deletion. |

## Decisions Made

Followed the selected durable cooldown, state-pin and native-store contracts. Poison acknowledgement needs an explicit event-free reducer operation; synthesizing a valid event would corrupt the nil maximum, watermark and rule state. Reused installed DLQ/client/store APIs and the Phase 2 aggregate budget pattern. No dependency installation or API/schema/identity change occurred.

## Deviations from Plan

1. **[Rule 2 - Required source-only acknowledgement]** Parent explicitly approved one additional owned file, backend/internal/detection/reducer.go. No existing method could advance an acknowledged malformed source position without fabricating event time. Added bounded monotone AcknowledgePoison with existing ledger accounting; pending-effect/order/capacity failures preserve state. Commits `a552d03`, `3acb983`; verified by exact metadata/pin and immutable accounting tests.
2. **[Rule 3 - Preserve existing wiring]** Alert-writer main already contains the configured real DLQ publisher, bounded closer and lc.Fail propagation. It was read and left unchanged; actual file count remains ten because the approved reducer extension replaces that unnecessary edit.

Karpathy guidance kept source changes tied to the two observed behavioral gaps, preserved passing native-store guards and avoided a new retry framework/package.

## Issues Encountered

- Initial real fixtures omitted producer buffer settings, called an assertion-bearing checkpoint helper before assignment restoration, and selected a 300ms Kafka record-delivery timeout rejected by pinned franz-go (minimum 1s). Corrected test producer settings, polled optional offsets before asserting, and kept the producer delivery timeout at least 1s while the independent operation context remains 300ms. Those fixture errors are not counted as RED. Initial default-binary detector expectation failed before behavior; the unit assertions are the definitive compiled RED evidence.
- A documentation lookup resolved /twmb/franz-go but its query transport failed. Verified the pinned official local v1.22.1 source for consumed-next CommitRecords, synchronous ProduceSync and ownership release; retained the separate detector checkpoint adapter.
- Two formatting calls used root-prefixed paths from backend and failed before edits. Corrected paths immediately. No failed lint/build was treated as successful verification.
- No authentication gate occurred. Test-owned containers/topics were managed by existing integration fixtures; user volumes, keys, environment, config edits and .serena were preserved.

## Deferred Issues and Remaining Dependencies

- Existing integration-lint recovery_test.go:162 warning and shared SCAN expected-summary wording mismatch remain 03-13/03-14 debt. No expected goldens were regenerated.
- 03-12 still owns the full mid-window crash/three-boundary/actual third-target ownership transfer matrix; 03-20 owns complete cooldown PG/outage/stale-owner/restore-behind cuts. Current retained-owner and native partial-write guards do not substitute for those matrices.
- Actual preserved-stack seed → scenario → repeat, full phase security audit and hosted CI remain later obligations. Seventeen of twenty-one Phase 3 summaries will exist after this closeout; global requirements and phase-completion checkboxes stay pending. Stop before Phase 4.

## Stub and Threat Surface Review

No goal-blocking stub or unwired runtime capability remains. Nil clocks/frontier-none on poison-only input and empty pending checkpoint maps are intentional state representations. No new public endpoint, auth path, schema or unplanned file-access boundary was introduced.

T-03-11-01 uses the existing capped envelope and trusted origin header handling, verified through default binaries and actual DLQ reads. T-03-11-02 is covered by strict format-2 poison-only/live-pin metadata, failed-prefix and two-store replay proofs. T-03-11-03 has aggregate retry/deadline, independent commit reservation, shutdown/cancel and real 400-record ownership-release evidence. No open high finding was identified for this slice; no phase-wide security or compliance verdict is claimed.

## User Setup Required

None.

## Self-Check: PASSED

- All ten changed application/test artifacts exist and all four listed proof/implementation commits exist in order.
- Assertion RED precedes new detector and writer behavior; existing passing guards are labeled characterization.
- Every required named unit and real family executed independently and passed; final standard tests/lint/build/vet pass, with only the explicitly retained integration-lint debt.
- No tracked deletion, unfinished runtime stub or unplanned trust boundary was found.
- This verified SUMMARY is committed before serialized STATE/ROADMAP tracking updates.
