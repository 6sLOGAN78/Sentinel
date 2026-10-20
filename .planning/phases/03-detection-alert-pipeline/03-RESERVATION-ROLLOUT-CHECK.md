# Preserved-stack reservation rollout check

Root executed this check on 2026-10-20 before releasing the new writer startup.
It is a local compatibility check, not full Phase 3 acceptance.

## Old-process and server boundary

The canonical Compose project showed Kafka/CH/PG/Redis stopped. The old CH
container had exited 0, so no request from that server process remained executing.
Root stopped exactly ingest, detector, event-writer, alert-writer and query-api
with `stop --timeout 30`; subsequent `ps -a` confirmed all five exited.
No container, volume or retained data was deleted. Infrastructure-only
`up -d --wait --wait-timeout 150 kafka clickhouse postgres redis` exited 0;
all four dependencies became healthy and all five applications remained stopped.

## Complete retained-outcome reconciliation

With writers disabled, bounded read-only Compose `exec -T` calls obtained:

- Every complete PG workflow row, ordered by global alert ID.
- Every physical CH alert row's tenant, ID, immutable fingerprint and canonical
  payload, ordered by ID/payload, without FINAL or merge manipulation.

The check compared the entire global ID → (tenant, fingerprint) maps. For each
physical payload it verified exact canonical JSON round-trip, payload/header
identity agreement, and recomputed SHA-256 after clearing status and omitting
detected_at under the existing fingerprint policy. Only aggregate counts and
snapshot hashes were printed, not payloads or credentials.

Result: **91 physical rows, 91 global IDs, 91 workflows; zero conflicting global
variants, zero CH orphans, zero missing evidence, all 91 payload hashes verified.**
No known uncertain legacy identity remains outside this complete retained set;
confirmed old server termination excludes a later completion from that instance.
This does not reconstruct already-merged historical variants or certify external
writers/servers; none are configured in this single-node local Compose stack.

## Additive migration and preservation

Before migration: tern version 5, reservation table absent, 91 workflows.
`timeout 180s make migrate` exited 0 and applied only the new PG migration 006;
CH reported its schema up to date. After migration: version 6, zero reservations,
91 workflows. The permanent ledger is lazily adopted, not bulk backfilled.

Post-migration re-reads matched both complete pre-migration snapshot hashes:

| Snapshot | SHA-256 |
| --- | --- |
| All workflow fields, including sequence and mutable triage | `21630174a46900535e36e7b27a7c93f685e3e1b72792fe09e036315983f4da4e` |
| Physical alert identity/fingerprint/payload rows | `033dbf7d0ab2b19bccca5d981b20318b2afb38f3779328833df34a650ed7b69c` |

Root explicitly released the original 03-14 executor to build/start only the
new protocol images and run scoped, full and operator gates serially. No old/new
mixed writer deployment is authorized. A later disagreement remains a rollout
stop, never permission to delete data. Hosted CI and Phase 3 completion remain
unclaimed by this check. See the clean [R2 review](03-REVIEW-PRODUCTION-R2.md).
