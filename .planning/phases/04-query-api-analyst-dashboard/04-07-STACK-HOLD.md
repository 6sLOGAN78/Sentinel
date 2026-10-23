# Preserved local detector history hold — read-only diagnosis

Status: unresolved_operator_recovery_choice; no reset or recovery mutation.
2026-10-23, sourcecandidatecc22566/metadata4259f4f. Separate from UI CR02.

After server interruption all four infrastructure containers were observed
exited. Executor normally started the existing containers, preserving volumes;
actualnative/browser checks then passed. Eight application/dependency services
are healthy, but sentinel-detector-1 restarts with exit1/OOMfalse and reports
"detector checkpoint history lost". No all-nine/pipeline PASS is claimed.

Root traced unchanged backend/internal/kafkax/detector_consumer.go:469-470:
restore refuses a committed checkpoint below retained start, beyond end, or
without metadata. Read-only actual Kafka group/bounds checks establish the
below-retained-start condition independently of metadata:

| security.events.v1 partition | Committed detector offset | Earliest retained | End |
|---|---:|---:|---:|
| 0 | 8339 | 8382 | 8382 |
| 1 | 8605 | 8643 | 8643 |
| 2 | 9249 | 9294 | 9294 |
| 3 | 7151 | 7197 | 7197 |
| 4 | 8776 | 8938 | 8938 |
| 5 | 8910 | 8945 | 8945 |

Group sentinel-detector has no active members. The369 offset-position gap is
not an invented event/identity/loss count; all six ranges are unavailable to
the consumer. Topic-init's checked-in default retention is24h, but exact live
policy/expiry chronology is not inferred from that default or wall-clock gaps.
No proof of lost ingested acknowledgements/canonical CH records is claimed.

Actualprivate evidence under04-07(dir0700/log0600):
- review-detector-readonly.log: bounded earlier errors/status.
- root-detector-group-readonly.log: EXIT0/1.688s,13:03:41–43Z; exact read-only
  consumer-groups --describe, no active member and committed offsets above.
- root-detector-bounds-readonly.log: EXIT0/3.205s,13:04:01–05Z; exact read-only
  get-offsets earliest then latest, all six partitions as above.

This is intentionally fail-closed recovery, not an07 frontend source defect.
Blind restart cannot restore trimmed source history. Do not reset offsets,
change consumer groups/config/state, clear PG fences/decisions/reservations,
weaken the guard, delete volumes/topics or claim current-source gates repaired
the historical pipeline. No such mutation was performed.

Remaining safe work in current turn: independentR2 of CR02, rootpostwave
build/defaulttests and metadata/scanner verification. Keep07pendingacceptance
and7/17accepted while this explicitly recorded stack hold remains unresolved.
Then request a concrete operator choice: restore the missing Kafka history
from a suitable backup, or authorize a separate fresh isolated synthetic demo
stack that leaves current containers/volumes/history intact. A fresh environment
is not restoration or proof that the old detector processed the missing range.
No recovery choice/publication/Phase5 authority is inferred from resume.
