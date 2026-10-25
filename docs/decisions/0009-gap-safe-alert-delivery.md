# 0009. Gap-safe alert delivery projection

Accepted engineering policy, 2026-10-25. Refines ADR 0008.

Workflow identity sequences describe allocation order, not commit order. A
writer can allocate an earlier sequence and commit after a later one has been
read. Neither max(workflow.seq) nor notifications alone provide safe recovery.

Migration 008 adds a durable pending ledger, a singleton transactional bigint
counter and a delivery log. An AFTER INSERT workflow trigger queues each new
identity in its writer transaction. The migration locks workflow writes before
installing the trigger and seeding preexisting rows in the same transaction;
writers already in progress must finish before the cutover proceeds. Workflow
rows, canonical detections, audit, reservations and equal replay stay intact.

Each query replica may project. It locks the counter row before discovering
at most 100 committed pending identities, then inserts unique delivery IDs,
updates the counter and removes pending rows in one five-second transaction.
The lock has a one-second acquisition timeout and is held through commit.
No identity sequence or external network operation occurs under that lock.
Rollback uses an independent one-second context. Uncertain acknowledgements
retry against the same unique log; overflow fails the managed service.
Idle projection polls every second, with failure backoff from 100ms to 5s.

Delivery IDs order projection commits. They do not claim to preserve the exact
commit order of original workflows or establish a distributed transaction.
The singleton lock serializes projection throughput, and a backlog introduces
delivery latency. Log/pending persistence grows with workload: this phase adds
no retention, garbage collection or fixed disk bound.

Bearer-only backfill reads the verified tenant from PostgreSQL, independently
of ClickHouse. It captures the committed counter before its page query and
restricts rows to that bound. A full page advances only to its last item; an
exhausted tenant interval may advance to the captured global bound. Global
IDs are sparse per tenant. Future cursors are rejected. Limits default to 50
and cap at 200; all cursors are canonical decimal strings through MaxInt64.

The browser retains progress privately in memory per authenticated client,
processes a page before advancing, and invalidates bounded queries without
appending an unbounded list or identity set. Arrivals offer an explicit refresh
while preserving current filters and pagination. At-least-once replay is
visibly idempotent; neither physical exactly-once delivery nor a persistent
browser cursor is promised. SSE/notification recovery is a later increment.
