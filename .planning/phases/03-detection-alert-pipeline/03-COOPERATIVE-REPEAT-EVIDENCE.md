# Cooperative acquisition repeat repair

Source base: `eee632febdf601286d2ba8ec3de1e3f75febe2cd`.
Behavioral RED: `c8b4310`.
Test-only GREEN candidate: `e3423373a80d1bbcb8737be99fcb08d5cfd22a11`.
Root's intervening `9a08266` changes acceptance tracking only.

## Observed failure and causal boundary

Hosted repeat run39544892831 on eee632f failed its full integration step at
`TestDetectorCooperativeAcquisition` after2.85s: the retained map was empty at
`detector_rebalance_test.go:94`. All twelve broker checkpoints had
`processed_next=9`. Failure cleanup recorded one member's assignments and
committed-nine observations across all twelve partitions, while the other
process had started without any assignment observations. The32m suite budget
was not exhausted: package1684.173s, make exit2. Root retains the actual
failed log privately as `ci-repeat-39544892831-failed.log`. Historical run39515550386
on80d2d87 remains a separate all-nine-job success. Neither result overwrites the
other; the repeat's independent46-family and Compose jobs passed.

The test waited only for two group members before seeding. Its shared
`incorporatedOwners` helper kept an owners map across Eventually polls, filtered
each historical committed observation against that poll's group ownership,
and accepted once the accumulated map contained twelve partitions. That did
not establish that either process, especially the intended survivor, had any
incorporated partition in a complete current snapshot. It could accept the
observed all-twelve-on-one-member transition or combine ownership observations
from different polls.

Direct source tracing shows why broker checkpoints and group membership are
insufficient barriers. `DetectorConsumer.CommitCheckpoints` waits for the real
OffsetCommit callback, then updates assignments and writes `committed`
observations. Broker offsets can therefore be visible before those private
observation files. `detectorObservations` deliberately skips concurrently
incomplete JSON. Cooperative `assigned` callbacks contain additions, preserve
retained lifetime tokens, and update the generation; the worker restores or
re-fences state independently. `Stable` alone does not prove incorporation.

This establishes a test-readiness defect and reproduces its acceptance gap.
The failed hosted log does not contain an atomic group description at the
assertion, so no exact coordinator scheduling reconstruction is claimed. No
production defect was established or production file changed.

## Deterministic regression and repair

`TestDetectorIncorporatedOwnerSnapshot` obtains a real two-member, twelve-partition
Kafka group through the existing native fixture and kadm client. Controlled
observation schedules then exercise the actual extracted helper. The test uses
real public assignment accessors; no unsafe/private representation fabrication,
timing sleep, production hook change or mocked helper implementation is used.

At REDc8b4310 the extracted collector retains the original behavior. The native
command reached three intended assertion failures: two members with only one
incorporated owner, a previous poll supplying an absent current observation, and
an in-progress rebalance accepted as ready. Complete current coverage passed;
missing partition and wrong-owner controls were already rejected. This is
behavioral RED, not a compilation, prerequisite or empty-selector failure.

GREENe342337 clears the owner map each poll, requires a Stable group with exactly
two members and two process observation slices, requires nonempty current
committed-nine coverage in each process, and requires all twelve seeded
partitions0–11 with two current member IDs on the configured topic. Each accepted
observation still passes `groupOwns` against the same description. The existing
Eventually20s budget and25ms interval are unchanged. `Stable` is combined with
actual incorporation checks.

Both original transfer tests share this precondition. All existing retained-map,
old-owner/new-owner, acquired restore, retained lifetime token/generation,
PostgreSQL fence, two unique alert identities and complete canonical payload
assertions remain unchanged. The new negative controls continue to reject
incomplete or wrong-owner snapshots, so this readiness wait cannot itself
substitute for the subsequent ownership-transfer assertions.

## Executed checks

All commands below ran from `backend/`, with runner-normal umask022. Logs live
under ignored `.local/phase3-acceptance/` (directory0700; logs0600).

| Command | Actual result | Persistent log |
| --- | --- | --- |
| `go test -tags=integration -timeout 6m -p 1 ./test/... -run '^TestDetectorIncorporatedOwnerSnapshot$' -count=1 -v` at RED | Exit1; package24.804s; three intended subtest assertions failed; end2026-10-21T05:28:50Z | `cooperative-snapshot-red.log` |
| Same exact command at GREEN | Exit0; package25.098s; all six subtests passed; 05:29:15Z–05:29:42Z | `cooperative-snapshot-green.log` |
| `go test -tags=integration -timeout 6m -p 1 ./test/... -run '^TestDetector(CooperativeAcquisition\|ThirdReplicaRebalance)$' -count=3 -v` | Exit0; package70.937s; six actual top-level PASS results; 05:29:52Z–05:31:05Z | `cooperative-native-repeat.log` |
| `go tool -modfile=tools.mod golangci-lint run --build-tags integration ./...` and `go vet -tags=integration ./...` | Both exit0; zero lint issues; 05:29:43Z–05:29:46Z | `cooperative-static.log` |
| `go test -race ./...` | Exit0; cached package test results; 05:30:31Z–05:30:33Z | `cooperative-unit.log` |

The first RED shell wrapper used incorrect paths for its formatting command and
START marker before the correctly targeted Go invocation. Formatting and log
permissions were corrected separately; the actual RED log retains its full
native run, returned exit and END marker, but has no START marker. No missing
start timestamp is invented. Later wrappers use correct paths and private
precreated logs.

Third-replica repetitions took6.65s/8.63s/8.56s and cooperative repetitions
10.12s/10.24s/10.17s. Each test call created a fresh fixture/topic and processes;
the invocation shared its native TestMain dependency containers. These are
actual executions, not cached integration results.

Independent deep review at e342337 is clean across both source files, zero
findings; see03-REVIEW-COOPERATIVE-REPEAT.md (committed64fd7c8).
Fresh root static gates passed05:33:34Z–05:33:47Z: make test5.22s, make lint3.90s,
backendbuild/default+integrationvet/integrationlint3.43s, all exits0.
All44scriptcases passed75.114s (elapsed75.21s/exit0),05:33:47Z–05:35:02Z.
Pinnedfull-historyscanner exit0 atd157b5d,05:34:19Z. Persistent private logs:
cooperative-final-static.log,cooperative-final-scripts.log,cooperative-final-gitleaks.log
and redactedJSONreport. No new scanner exclusion was introduced.
Full aggregate started05:33:20Z at64fd7c8 with unchanged32mcap; result pending.
Complete new acceptance/hosted results remain pending. No full-suite,
new46-family matrix or fresh hosted success is implied by the focused checks.
Root owns acceptance tracking, remaining gates and any authorized remote push.
User `.planning/config.json` and `.serena/` were preserved. No volume deletion,
history rewrite, deadline increase, dependency installation or Phase4 work was
performed.
