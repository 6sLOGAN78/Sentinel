---
phase: 02-ingestion-durable-event-storage
plan: "14"
subsystem: event-storage
tags: [kafka, dlq, bounded-memory, encoding, gap-closure]
requires:
  - phase: 02-ingestion-durable-event-storage
    provides: executed 02-13 ingestion-to-storage slice and existing acknowledged DLQ publisher
provides:
  - destination-aware encoded quarantine bounds with checked arithmetic
  - 128-header output and source-inspection caps
  - independent record-v2 encoding regressions for empty floods and configured topics
affects: [02-15, event-writer, poison-quarantine, phase-verification]
tech-stack:
  added: []
  patterns: [pre-copy length validation, checked encoded-size accounting, independent wire-encoding oracle]
key-files:
  created: []
  modified:
    - backend/internal/kafkax/dlq.go
    - backend/internal/kafkax/dlq_test.go
key-decisions:
  - "Preserve BuildDLQRecord as a default-topic wrapper; Publish sizes its actual configured topic before producing."
  - "Limit inspected source headers as well as output headers to 128, so spoof-only metadata cannot force an unbounded scan."
  - "Reserve redaction context and check lengths before cloning untrusted bytes; keep the original 8 KiB allowance."
patterns-established:
  - "DLQ bounds include signed-varint lengths/counts, worst-case record fields, batch/request framing and destination topic bytes."
  - "Source dlq.* prefixes are rejected case-insensitively using only the fixed prefix, without allocating a lowercased source key."
requirements-completed: [STOR-04, ING-07]
duration: 18min
completed: 2026-10-16
---

# Phase 02 Plan 14: Encoded DLQ Bounds Summary

Quarantine records now fit the configured value allowance plus 8 KiB under independent Kafka record-v2 encoding, including adversarial empty headers and 249-byte destinations.

## Performance

- Recorded start: 2026-10-16T09:10:56Z
- Completed: 2026-10-16T09:29:02Z
- Duration: 18 min
- Tasks: 2/2
- Implementation/test files modified: 2

## Accomplishments

- Preserved the existing builder signature and all-ISR synchronous publication. `Publish` now calls `BuildDLQRecordForTopic` with its actual destination before publication rather than replacing the topic afterward.
- Added Kafka topic validation for destination and origin (1..249 ASCII topic bytes, excluding `.` and `..`), checked size arithmetic, and mandatory/redaction envelope checks.
- Accounted for nullable key/value and header signed-varint lengths, header count, worst-case timestamp/offset deltas, record length, batch and Produce request framing.
- Bounded copied headers and inspected source headers to 128; stopped at exhausted byte capacity. Oversized keys/values/header data are checked before cloning. Trusted identity/reason/UTC metadata and small source payload behavior remain intact.
- Added an independent test serializer using `encoding/binary.PutVarint`; covered nil/empty floods, spoofing, varint boundaries, maximum destinations, overflow rejection, nil-versus-empty preservation, and source-copy independence.

## Task Commits

1. Task 1, expose encoded DLQ overflow: `1132fc4` — `test(02-14): expose encoded DLQ header overflow`.
2. Task 2, enforce destination-aware pre-copy limits: `de0f2e6` — `fix(02-14): bound encoded quarantine before copying`.

## Verification Evidence

All commands ran from `backend/` unless otherwise stated. This evidence is local only.

| Gate | Observed result |
|------|-----------------|
| RED: `go test ./internal/kafkax -run '^TestBuildDLQRecordEncodedHeaderFlood$' -count=1 -timeout 30s` | Exit 1, package 0.197s. All four subcases failed behavioral encoded-size assertions against the old production implementation. Nil/empty floods encoded 20,368 bytes, mixed spoofing 10,367, and varint-boundary flood 9,203; limit was 8,256. No missing-symbol or setup failure. |
| GREEN: same named regression after repair | Exit 0, package 0.013s. |
| `go test -race ./internal/kafkax -count=1 -timeout 60s` | Exit 0, package 1.068s; measured command 1.91s. All old and new cases passed. |
| `go vet ./internal/kafkax` | Exit 0; measured command 0.13s. |
| `go tool -modfile=tools.mod golangci-lint run ./internal/kafkax/...` | Exit 0, zero issues; measured command 0.86s. |
| `git diff --check` | Exit 0. |

Acceptance checks additionally established that the maximum-topic empty flood yields exactly 128 total headers, invalid source/destination topics and overflowing caps fail, small-origin metadata remains exact, and spoof-only input inspection stops before a late source header.

The wire layout was cross-checked against the pinned franz-go v1.22.1 implementation's `recordNumbersAt` and Produce request serializer in `pkg/kgo/sink.go`. The test oracle serializes fields independently and does not invoke production sizing helpers.

## Decisions Made

- A fixed framing allowance conservatively covers the pinned producer's request/batch envelope, while variable lengths are checked independently. The 8 KiB metadata allowance was not increased.
- A bounded scan can omit later source headers after spoofing or capacity exhaustion; required trusted origin identity always remains present.
- Rejected payloads are not copied before redaction, and admitted key/value/header bytes are cloned so the returned record does not alias mutable source bytes.

## Deviations from Plan

None. Shared STATE, ROADMAP, REQUIREMENTS and configuration updates remain owned by the orchestrator, as directed for concurrent execution.

## Issues Encountered

- The first targeted lint attempt identified a 122-character condition. Wrapping the condition resolved the sole finding; the final pinned lint run passed.
- A globally installed `golangci-lint` command was unavailable. The repository's existing pinned `go tool -modfile=tools.mod golangci-lint` command was used without installing or changing dependencies.

## Known Stubs

None in either modified implementation/test file.

## Threat Surface

No new endpoint, authentication path, file access or schema was added. T-02-14-1/2/3 mitigations are covered by the bounded encoding, trusted-header and redaction tests. No dependency manifests or lockfiles changed; the dual-build dependency guard remains part of the final phase gate.

## Next Phase Readiness

- Ready for 02-15 to consume the destination-aware builder and prove aggregate poison/rebalance durability with real Kafka and ClickHouse.
- This helper-level gate does not establish the complete storage boundary. Real poison/DLQ integration and final independent phase verification remain pending.
- No Docker resources or volumes were changed. Hosted GitHub Actions remains pending.

## Self-Check: PASSED

- Both implementation/test files exist.
- Commits `1132fc4` and `de0f2e6` exist and contain only the assigned implementation/test paths; neither deletes tracked files.
- Task acceptance regressions and the complete targeted race/vet/lint gate passed.
- Original verification/review reports and user-owned `.planning/config.json`/`.serena/` were preserved.
