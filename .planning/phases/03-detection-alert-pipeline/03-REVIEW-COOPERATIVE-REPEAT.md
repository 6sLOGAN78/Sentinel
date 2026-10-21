---
phase: 03-detection-alert-pipeline
reviewed: 2026-10-21T05:32:00Z
depth: deep
files_reviewed: 2
files_reviewed_list:
  - backend/test/detector_owner_snapshot_test.go
  - backend/test/detector_rebalance_test.go
diff_base: eee632febdf601286d2ba8ec3de1e3f75febe2cd
reviewed_head: e3423373a80d1bbcb8737be99fcb08d5cfd22a11
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Cooperative ownership repeat repair: scoped code review

## Narrative Findings (AI reviewer)

No actionable BLOCKER or WARNING found in the two-file repair. This is a scoped
source review, not acceptance of the entire phase or a fresh hosted-CI result.
Both source files were read completely at the immutable reviewed head; the
diff range includes intervening tracking-only edits, which are outside this
source scope. No structural pre-pass was supplied. No project AGENTS.md,
project-local skill directories, or source-ignore exclusion was found.

### Ownership and observation tracing

The collector at `backend/test/detector_rebalance_test.go:306` now clears poll
history and gates success on a Stable two-member description, both process
observation streams contributing currently owned committed-nine checkpoints,
all twelve configured-topic partitions, and two current member IDs. Wrong
topics, negative/out-of-range partitions, absent/malformed checkpoint metadata,
and observations belonging to the wrong current owner cannot supply coverage.
This rejects the observed all-twelve-on-one-process readiness state instead of
weakening the subsequent retained-map assertion.

Read-only contract tracing covered `detectorObservations`, `observedDetector`,
`groupOwns`, `observationAtNine`, the Kafka adapter's assignment/revoke and
successful OffsetCommit observation paths, and the worker's state restore,
commit and rebalance ordering. Kafka's successful commit precedes the private
observation file; partially written JSON is deliberately skipped. Requiring
per-process observed coverage is therefore material, not redundant with broker
offsets or member count. Private observation streams are process-specific;
the native member ownership filter prevents another process's historical member
ID from substituting for the current member.

The original assertions remain intact: distinct old/new owner, actual acquired
restore, checkpoint equality, readiness, retained lifetime token and current
generation, PostgreSQL fence, canonical Kafka/ClickHouse/decision bytes, and
the two immutable acquired/retained alert identities. No deadline or polling
interval was increased. No production code, hooks, goldens or payload checks
were changed.

### Regression fixture and evidence

The new fixture obtains public decoded assignments from a native Kafka group.
Its assignment mutation is confined to a decoded admin response, restored by
defer, and occurs in sequential subtests; live consumers do not share that
decoded object. Consumer cleanup cancels polling, closes the consumer, and joins
the polling goroutine. The complete-snapshot positive case and incomplete /
wrong-owner negative cases remain asserted.

Actual private logs were read, not merely the worker summary:

- `cooperative-snapshot-red.log`: RED `c8b4310` reaches the three intended
  assertion failures (one incorporated owner, stale prior-poll coverage, and
  in-progress rebalance), package 24.804s, exit 1, end 05:28:50Z. The extracted
  RED collector preserves the old matching/accumulation behavior. The log has
  no START marker; no start time is inferred.
- `cooperative-snapshot-green.log`: candidate passes all six cases, package
  25.098s, exit 0, 05:29:15Z–05:29:42Z.
- `cooperative-native-repeat.log`: both original native tests execute three
  times each, all six top-level results pass, package 70.937s, exit 0,
  05:29:52Z–05:31:05Z. These repetitions use fresh fixtures/topics/processes
  while sharing invocation-level native dependency containers.
- The retained failed-run log for hosted run `39544892831` shows the actual
  empty retained-map assertion and one process with all twelve incorporated
  partitions while the other has no assignment observations.

The controlled helper schedules are a MODEL of the readiness acceptance gap,
not a native reproduction of the original coordinator timing. The hosted
failure lacks an atomic group description at the assertion; its exact schedule
cannot be reconstructed from these logs. Likewise, the helper reads historical
files against one broker description, not an atomic cross-system generation
snapshot. The unchanged downstream token/generation/fence checks remain the
native proof and can reject later ownership churn. Focused repetitions do not
prove absence of every possible timing failure. Full aggregate acceptance and
new hosted CI remain separate pending gates, owned by the orchestrator.

No tests, builds, live-service commands, or remote operations were run by this
reviewer. No source files were modified and no commit was created.

_Reviewer: independent gsd-code-reviewer; GSD deep review with Karpathy scope and evidence discipline._
