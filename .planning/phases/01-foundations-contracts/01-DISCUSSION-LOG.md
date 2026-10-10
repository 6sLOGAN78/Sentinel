# Phase 1: Foundations & Contracts - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md. This log preserves the alternatives considered.

**Date:** 2026-10-10
**Phase:** 01-foundations-contracts
**Mode:** `--auto`. No user prompts; the recommended option was auto-selected for every question.
**Areas discussed:** Repo layout, Boilerplate removal scope, ClickHouse migration tooling, event_id policy, Atomic-batch semantics, Redis failure policy, OpenAPI validation approach, Frontend package manager, Phase-1 binary behavior, Local topic layout

---

## Repo layout

| Option | Description | Selected |
|--------|-------------|----------|
| Keep `backend/` module, binaries in `backend/cmd/*`, embedded assets inside the module | Matches existing code. Respects the `internal/` and `go:embed` boundaries | ✓ |
| Move the Go module to the repo root (spec §13 literal) | Large churn; no functional gain | |

**Choice:** [auto] Keep `backend/` (recommended in research/ARCHITECTURE.md)

## Boilerplate removal scope

| Option | Description | Selected |
|--------|-------------|----------|
| Remove all non-spec pieces (Clerk, asynq, Resend/emails, New Relic, TS packages, Turborepo/bun, Taskfile) | Spec §19.8: no unrelated services | ✓ |
| Keep Clerk for dashboard auth | Adds an external identity provider the spec doesn't mandate | |

**Choice:** [auto] Remove all

## ClickHouse migration tooling

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal in-repo Go runner with ordered SQL and a `schema_migrations` table | No unverified dependency | ✓ |
| golang-migrate ClickHouse driver | Compatibility with clickhouse-go v2.49 unverified | |

**Choice:** [auto] In-repo runner

## event_id policy

| Option | Description | Selected |
|--------|-------------|----------|
| Required, client-supplied UUID | Spec §6.1 lists event_id as required; makes retries idempotent | ✓ |
| Server-derived deterministic hash of the payload | Removes client burden but collapses legitimately identical events | |

**Choice:** [auto] Required, client-supplied

## Atomic-batch semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Validate whole batch first; 202 only after all acks; 503 on mid-batch Kafka failure with documented partial-publish plus retry | Honest at-least-once contract | ✓ |
| Kafka transactions for a truly atomic publish | Adds transactional producer complexity; consumers need read_committed | |

**Choice:** [auto] Response-level atomicity

## Redis failure policy

| Option | Description | Selected |
|--------|-------------|----------|
| Fail-closed (503) by default, configurable fail-open | Never silently bypasses controls (spec §7.4) | ✓ |
| Fail-open by default | Silently bypasses rate limits | |

**Choice:** [auto] Fail-closed default

## OpenAPI validation approach

| Option | Description | Selected |
|--------|-------------|----------|
| Hand-authored 3.1 YAML validated by a Go test (libopenapi or kin-openapi, per research) | Contract-first; tool choice research-flagged | ✓ |
| Generate OpenAPI from Go code | Spec asks to publish and validate a contract; code-first risks drift | |

**Choice:** [auto] Hand-authored plus Go validation test

## Frontend package manager

| Option | Description | Selected |
|--------|-------------|----------|
| npm with `web/package-lock.json`, no workspaces | Simplest; replaces bun/Turborepo | ✓ |
| Keep bun workspaces | Extra tooling for a single app | |

**Choice:** [auto] npm

## Phase-1 binary behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Load config, init logger, connect deps, wait for a signal, graceful shutdown; no routes | Builds and runs without fake features | ✓ |
| Add placeholder HTTP routes | Violates the spec §18 no-stub rule | |

**Choice:** [auto] Minimal runnable binaries

## Local topic layout

| Option | Description | Selected |
|--------|-------------|----------|
| events 6 / alerts 3 / dlq 1 partitions, RF=1, env-configurable | Enough partitions for multi-replica demos later | ✓ |
| Single partition everywhere | Can't demonstrate partitioned processing | |

**Choice:** [auto] 6/3/1

---

## Claude's Discretion

- Internal package naming, Makefile internals, Compose host ports, golangci-lint rule set, exact attribute and skew limits within the stated bounds, and the OpenAPI schema `$ref` vs generated copy (with a drift test).

## Deferred Ideas

- Hardened CI gates: Phase 6
- Health/readiness endpoints: Phase 2
- Metrics and OTel wiring: Phase 5
- Producer API keys: Phase 2; dashboard auth: Phase 4
