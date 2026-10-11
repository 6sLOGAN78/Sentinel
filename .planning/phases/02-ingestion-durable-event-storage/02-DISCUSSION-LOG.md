# Phase 2: Ingestion & Durable Event Storage - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md. This log preserves the alternatives considered.

**Date:** 2026-10-12
**Phase:** 02-ingestion-durable-event-storage
**Mode:** `--auto`. No user prompts; the recommended option was auto-selected for every question.
**Areas discussed:** API key format and storage, Key provisioning, Minimal event lookup, ClickHouse dedup read path, Event-writer batching, Rate-limit cost model, Load shedding, Health listeners, Running apps locally, Generator send path

---

## API key format and storage

| Option | Description | Selected |
|--------|-------------|----------|
| `snt_<key_id>_<secret>`, SHA-256(secret) in Postgres, constant-time compare, 30 s bounded cache | Fast lookup by public id; high-entropy secret makes a fast hash sufficient | ✓ |
| bcrypt/argon2 hashed opaque keys | Slow hash on every request without an id prefix; needless for random 256-bit secrets | |
| JWT for producers | Adds signing-key management; spec allows either, keys are simpler | |

**Choice:** [auto] Prefixed keys with SHA-256 at rest

## Key provisioning

| Option | Description | Selected |
|--------|-------------|----------|
| `backend/tools/keyctl` CLI (create/revoke/list), used by `make seed` | No admin route needed in v1 (TRI-02 is v2) | ✓ |
| Admin HTTP API | Out of v1 scope; adds an auth surface | |

**Choice:** [auto] CLI tool

## Minimal event lookup

| Option | Description | Selected |
|--------|-------------|----------|
| query-api `GET /api/v1/events/{event_id}` with an `events:read` API-key scope | Proves tenant-scoped lookup without pulling Phase 4 JWT/RBAC forward | ✓ |
| SQL-only verification script | Not an endpoint; weaker proof of the read path | |
| Full events list API now | Phase 4 scope (API-01) | |

**Choice:** [auto] Single lookup route on query-api

## ClickHouse dedup read path

| Option | Description | Selected |
|--------|-------------|----------|
| ReplacingMergeTree plus read-time `LIMIT 1 BY event_id` and `uniqExact` counts, tested right after a double insert | Correct before merges; matches research | ✓ |
| Rely on `insert_deduplication_token` | Not sufficient across re-batching or single-node tables | |
| `FINAL` everywhere | Costly at scale; allowed only for small bounded lookups | |

**Choice:** [auto] Read-time dedup

## Event-writer batching

| Option | Description | Selected |
|--------|-------------|----------|
| 5,000 records or 1 s, insert then commit, bounded retry with backoff | Avoids "too many parts" and keeps commit-after-effects | ✓ |
| Per-record inserts | ClickHouse small-insert pitfall | |

**Choice:** [auto] Batched inserts

## Rate-limit cost model

| Option | Description | Selected |
|--------|-------------|----------|
| Token bucket per key_id, cost = events per batch, 10k capacity / 2k per s | Quota matches actual load; Retry-After computable | ✓ |
| Cost = 1 per request | 500-event batches would bypass meaningful limits | |

**Choice:** [auto] Per-event cost

## Load shedding

| Option | Description | Selected |
|--------|-------------|----------|
| Bounded in-flight limiter (64), immediate 503 with Retry-After | No unbounded buffering (ING-07) | ✓ |
| Request queue | Unbounded or long latency under load | |

**Choice:** [auto] In-flight limiter

## Health listeners

| Option | Description | Selected |
|--------|-------------|----------|
| Separate ops listener per binary with `/healthz` and `/readyz` doing real dependency checks | Workers have no public HTTP; ops port also hosts `/metrics` in Phase 5 | ✓ |
| Health only on public ports | Workers would need public listeners | |

**Choice:** [auto] Ops listener per binary

## Running apps locally

| Option | Description | Selected |
|--------|-------------|----------|
| Multi-stage Dockerfile plus Compose app services; `make dev-up` starts infra, migrations, then apps | Single-command setup (FR-13) and a reproducible seed | ✓ |
| Run binaries from source only | Not a single-command setup | |

**Choice:** [auto] Compose app services

## Generator send path

| Option | Description | Selected |
|--------|-------------|----------|
| httpx `send` subcommand, key from env/.local file, honors Retry-After, prints totals | Real traffic through ingest; secrets never on the command line | ✓ |
| Direct Kafka producer | Bypasses ingest auth and validation | |

**Choice:** [auto] HTTP send

---

## Claude's Discretion

- Internal package layout, config key names, timeout and backoff constants, readiness body detail, and Dockerfile base image (pinned by digest).

## Deferred Ideas

- Admin key API: v2 TRI-02
- JWT/RBAC and the events list API: Phase 4
- Prometheus counters: Phase 5
- Multi-replica writer load test: Phase 5
