# CR-01: Cooperative pending-offset restoration repair

Date: 2026-10-20. Review: `03-REVIEW-PRODUCTION.md`, CR-01. RED: `56412ae`; GREEN: `f4e0485`.

## Cause and scoped repair

Pinned franz-go v1.22.1 builds cooperative `fetchOffsets(ctx, added)` requests from additions only (`consumer_group.go:1139-1143,1982+`). Completed retained partitions remain in the adapter's assignment map, but do not appear in that response. The previous all-assignment completeness check therefore terminated an otherwise healthy survivor acquiring a departed replica's partitions. Empty additions skip the fetch callback; no empty-callback runtime counterexample is claimed.

The repair captures the full assignment and validates response membership/completeness against its pending lifetimes before restoring any entry. Duplicate, failed, foreign/unowned, completed-retained and missing-pending response entries are fatal without partial mutation. Native retained-history checks and full-assignment configuration-transition authorization remain mandatory. Completion still atomically checks the captured token/member/generation and current driver identity; only captured pending entries are cleared. Completed retained checkpoint/state/token entries are neither fetched again nor overwritten. Worker ownership/PG fences, callback error propagation and public interfaces are unchanged.

Only `detector_consumer.go`, `detector_consumer_test.go` and the approved `detector_rebalance_test.go` extension changed. No original Plan 12 SUMMARY, STATE or ROADMAP was rewritten. No dependency installation, branch change, remote write, preserved-stack mutation or destructive cleanup occurred. Karpathy guidelines kept the production delta to response prevalidation and pending-only completion.

## Actual RED/GREEN evidence

Each row used an independent `scripts/go-test-nonempty.sh` invocation. Unit flags: `-race -timeout 60s`. Real flags: `-tags=integration -timeout 6m -p 1`. Package times include fixture lifecycle.

| Exact family / package | Result |
| --- | --- |
| `^TestDetectorCooperativeOffsetFetch$`, `./internal/kafkax/...`, before repair | Compiled assertion RED, exit 1, package 0.022s: the actual `fetched` callback completed native kadm history reads then rejected an additions-only response with `detector restore response incomplete`. Omission of a newly pending second partition remained correctly terminal. Test-only commit `56412ae`. |
| Same family, old production file from `56412ae` through a read-only Go overlay, corrected valid-rule fixture | Same assertion RED, exit 1, package 0.027s. Command: `GOFLAGS='-overlay=/tmp/sentinel-cr01-red.1Lmb6z/overlay.json' scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestDetectorCooperativeOffsetFetch$' -race -timeout 60s`. Repository production was never swapped/reset; the temporary overlay remains available. |
| Same family, repaired production | PASS, exit 0, package 1.025s: completed retained snapshot/current owner/token preserved; validated pending addition clears and accepts legitimate absent checkpoint metadata. |
| `^TestDetectorPendingOffsetFetch$`, `./internal/kafkax/...` | PASS, exit 0, package 1.015s: no early readiness/commit; revoke/reacquire, member and generation changes reject stale completion. |
| `^TestDetectorCommitMetadata$`, `./internal/kafkax/...` | PASS, exit 0, package 1.018s: complete authoritative callback success and unchanged ownership remain required. |
| `^TestDetectorCooperativeFetchValidation$`, `./internal/kafkax/...` | PASS, exit 0, package 1.021s: duplicate, foreign partition/topic, retained entry and partition errors leave all assignment state unchanged and cancel terminally. |
| `^TestDetectorCooperativeAcquisition$`, `./test/...`, strengthened `-count=2` | Both PASS, exit 0, tests 10.59s/10.15s, package 38.477s. |
| Same real family, final committed stricter full-checkpoint equality assertion | PASS, exit 0, test 10.48s/package 27.703s. |
| `^TestDetectorThirdReplicaRebalance$`, `./test/...` | PASS, exit 0, test 9.64s/package 32.352s. |
| `^TestDetectorCapacity$`, `./test/...` | PASS, exit 0, test 7.22s/package 25.068s, including joined-empty/retained-owner and late-counter subtests. |
| `^TestDetectorConfigRestart$`, `./test/...` | PASS, exit 0, test 22.56s/package 38.871s; full-assignment config authorization remains active. |
| Backend `timeout 120s go vet -tags=integration ./internal/kafkax/... ./test/...` | PASS, exit 0. |
| Backend `timeout 120s go tool -modfile=tools.mod golangci-lint run ./internal/kafkax/...` | PASS, exit 0, zero issues. |
| Backend integration-tag lint over `./test/...` | Exit 1 only for concurrent CR-02 fixture `alertwriter_lock_lifetime_test.go:139,164` line lengths; no CR-01 owned-file warning remained. The other fixer/parent owns that file and final cross-fix lint. |

The new real family seeds nine events on twelve partitions split between two actual detector processes. One leaves gracefully. Broker assignment, private per-process observations and actual PG fences prove the surviving member retains six completed tokens/checkpoints without re-restoration, registers the new generation, becomes ready and acquires/restores/replays additions through nine before either tenth event. One acquired and one retained partition then each receive event ten. Exact canonical Kafka keys/bytes, immediate CH detail/count (one identity then two total), one workflow per identity and exact PG decision bytes prove neither live window was lost. Live pin 0 is retained while processed_next advances 9→10; decision frontiers point to source offset 9.

Final real example: acquired partition 0 moved from `kgo-2889f526-9def-4829-80ce-9bfce69e6608` to survivor `kgo-7a45e969-01b3-4c22-bb8b-a812e9eca861`, generation 5; retained partition 2 kept its prior token/checkpoint. Both had sealed_through 1792470571008 / next 9 before their tenth, then 1792470571009 / next 10. Acquired alert ID `9d05002c0f998ac2cf60f02b2b0c1eff6fd5a1a0196fb41c37280204d3a92d99`; retained ID `244134b8b9c9a88f00e8ffd417209a3cfa1e70e008e914d3a45a514de5a9c414`.

## Fixture corrections and limits

- Initial in-memory broker used ListOffsets v0, whose old-style offsets are not the kadm field this fixture reads. Corrected the wire fixture to v1 before counting behavioral RED. The Kafka-version pin belongs only to this private transport fixture, not production.
- The retained checkpoint initially used an incomplete empty rule definition. Once GREEN reached full configuration validation, replaced it with all four explicit versioned definitions and asserted metadata validity. Re-running old production through the overlay preserved the intended RED independently of that fixture correction.
- First real acquisition fixture wrongly demanded a redundant new-generation Kafka commit for idle retained partitions (exit 1, test 28.50s/package 53.837s). Diagnostics showed actual additions restored/committed and retained checkpoint/token preserved; idle retained state correctly did not recommit. Replaced that invented expectation with real PG generation/token/frontier fences, current readiness, no retained re-restoration and exact retained tenth-event evidence. No production policy or assertion budget was weakened. An intermediate corrected acquired-only run also passed (8.47s/package 32.420s) before the final both-window proof.
- Owned fixture complexity/line-length/shadow lint findings were corrected; no suppression was added. One formatting invocation used a root-prefixed path from backend and failed before editing; corrected immediately.
- No attribution is made for the historical unexplained Plan 12 timeout. This independently confirmed review defect and its repair do not establish that timeout's cause.

## CI handoff and self-check

Add the exact independent real guard `scripts/go-test-nonempty.sh './test/...' '^TestDetectorCooperativeAcquisition$' -tags=integration -timeout 6m -p 1`; normal six-minute budget. The parent/03-14 owns CI and the final aggregate/preserved-repeat runs after all review fixes. This specialist did not run or claim the full 32-minute matrix.

Self-check: PASSED — RED and GREEN commits exist; all three changed artifacts exist; no tracked deletion or new public surface; focused unit and all requested real families actually passed. Final cross-fix/security/phase completion remains with the parent.
