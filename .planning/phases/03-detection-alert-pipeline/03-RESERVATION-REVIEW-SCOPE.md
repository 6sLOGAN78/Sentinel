# Reservation Whole-Fix R2 Review Scope

Prepared: 2026-10-20T11:39:41Z
Frozen backend head: `3a26feb6b5d095dafff47711a293513e13f77c0c`
Repair base: `fa33ed0eee76e7b3ed11c5aeda3683125a7675be`
Depth: deep, scoped to the complete durable-reservation repair and its direct contracts.
Output: `03-REVIEW-PRODUCTION-R2.md`, preserving original and R1 reports.

This review judges whether CR-02-R1 is resolved by committed immutable authority before every conforming CH insertion, including unknown results and process boundaries. Earlier R1 already addressed CR-01/WR-01 and the reproduced five-second local lease defect; do not repeat the unchanged 41-file baseline review. This is not full Phase3 CI/docs/test-matrix/security verification.

Complete change package: `.superpowers/sdd/03-ALERT-RESERVATION-IMPLEMENTATION-PLAN/review-reservation-whole-fix.diff` (16 backend files, full diff through EOF). Per-task reports/review snapshots are in that same plan-scoped directory; the approved design and implementation plan remain phase artifacts. Actual native lost-ack evidence is distinct from modeled delayed visibility; exact stock-server pre-visibility pause is not claimed.

## Explicit file list

- backend/internal/alertwriter/failpoint.go
- backend/internal/alertwriter/failpoint_integration.go
- backend/internal/alertwriter/failpoint_test.go
- backend/internal/alertwriter/reservation.go
- backend/internal/alertwriter/reservation_test.go
- backend/internal/alertwriter/writer.go
- backend/internal/alertwriter/writer_test.go
- backend/internal/storage/postgres/alert_reservations.go
- backend/migrations/migrations_test.go
- backend/migrations/postgres/006_alert_reservations.sql
- backend/test/alert_reservations_schema_test.go
- backend/test/alertwriter_ackloss_proxy_test.go
- backend/test/alertwriter_lock_lifetime_test.go
- backend/test/alertwriter_reservation_fault_test.go
- backend/test/alertwriter_reservation_restart_test.go
- backend/test/alertwriter_reservation_test.go

## Required disposition

Judge global immutable identity/claim retention, pre-CH commit ambiguity, stored-payload corruption, existing workflow/orphan compatibility, finite context/pool/cleanup ownership, source/DLQ/workflow effects, real process/remote-result proof and direct protocol callers. Explain any limitation rather than downgrading an open integrity defect. No critical risk may be silently accepted.

Task2 minor must be triaged: `writer_test.go:90` labels tx phases by odd/even BeginLocked counts and can desynchronize after a preparation failure; task review found no demonstrated runtime defect. Do not silently discard or presume severity.

Task3 provenance deviation: genuine compiled behavior RED is recorded but no separate tests-only RED commit exists; history has not been reconstructed. Task3 review accepted it as an explicit evidence limitation, not a code defect.

The two recorded execution rulings preserve actual assertions: cancellation observer ignores only deferred cleanup after acknowledged successful reservation COMMIT; original SIGKILL group uses the existing crash-recovery offset wait for the configured 45s Kafka session expiry. No runtime retry/operation/session settings were increased.

CI/docs/guard edits may proceed outside this frozen backend scope while review is active. Reviewer is read-only except its new R2 artifact, runs no test suites/live mutations and commits nothing. Root will validate and commit the report. Separate final phase review covers those CI/docs deltas later.
