# Phase 02 gap-closure coverage audit

Planning date: 2026-10-16. Gap closure only; no implementation/test success is claimed. Original 02-00..13 plans, their SUMMARYs, 02-VERIFICATION.md and 02-REVIEW.md are preserved.

## Scope and root causes

All seven structured verification gap groups and all nine review findings are planned. Failures came from incomplete safety oracles (encoded bytes, redirect hops, existing directory permissions, environment/build-tag variation), a single-operation budget applied to many sequential effects, a published future auth alternative, creation-only operational policy, and summary-count-based completion metadata.

Gap closure resolves D-28's premature getEvent BearerAuth alternative using the established Phase 2 boundary and D-03/D-13: keep the live operation API-key-only; JWT/RBAC remains Phase 4. This is a repair choice derived from the existing scope, not a new explicit user decision. Other D-28 work (error types and literal-colon ADR) remains delivered. No deferred feature is included. GEN-03's threat scenarios remain assigned to Phase 3 per CONTEXT; this phase retains 1000 benign events.

Discovery: gap mode skips new external research. Actual source and tests were inspected for all defects. Existing package pins/APIs are reused; no package install or new dependency is planned. CLAUDE/codebase boilerplate maps are stale; use current five commands and explicit dependency constructors. Research/PATTERNS are historical context, not evidence of current safety.

## Wave and ownership graph

| Wave | Plans | Dependencies | Ownership |
|------|-------|--------------|-----------|
| 1..10 | 02-00..13 | Historical executed graph | Preserved |
| 11 | 02-14, 02-16, 02-17, 02-18, 02-20 | 02-13 | Disjoint DLQ / OpenAPI / probe / generator+seed / Compose+checker paths |
| 12 | 02-15 | 02-14 | Writer, consumer, config, tests, storage docs |
| 13 | 02-19 | 02-13, 02-15 | Docker context and CI, after required rebalance test exists |
| 14 | 02-21 | 02-15..20 | ROADMAP/STATE via orchestrator, validation and additive acceptance evidence |

22 total plans; 14 historically executed, 8 repairs pending in four additional waves; 18 new concrete tasks. No same-wave files_modified overlap. Wave numbers continue after existing wave 10 instead of restarting. No additional SKELETON.md.

## Verification/review closure map

| Source | Item | Repair task(s) | Regression/evidence |
|--------|------|----------------|---------------------|
| VERIFICATION gap 1 | Canonical MVP story | 02-21 Task 1 | user-story.validate valid=true and persisted roadmap goal |
| VERIFICATION gap 2 | Encoded DLQ bound | 02-14 Tasks 1..2 | TestBuildDLQRecordEncodedHeaderFlood, configured-topic boundary cases |
| VERIFICATION gap 2 | Aggregate poison/rebalance bound | 02-15 Tasks 1..3 | TestWriterAggregatePoisonBudget, TestWriterSlowPoisonRebalance |
| VERIFICATION gap 3 | Runtime authentication alternatives | 02-16 Tasks 1..2 | TestOpenAPIGetEventRuntimeSecurity, TestEventLookupTenantScope |
| VERIFICATION gap 4 | Probe redirect escape | 02-17 Tasks 1..2 | TestRunRejectsRedirects, exactly one request |
| VERIFICATION gap 5 | Existing manifest parent modes | 02-18 Task 1 | parent-mode and symlink/output regressions |
| VERIFICATION gap 6 | Credential build-context leakage | 02-19 Task 1 | real Docker COPY fixture and exclusion-removal regression |
| VERIFICATION gap 7 | Ambient seed key | 02-18 Task 2 | test_generator_environment_ignores_ambient_key |
| VERIFICATION gap 7 | Integration-only banned dependency | 02-19 Task 2 | test_go_dependencies.py default/integration/failure cases |
| REVIEW CR-01 | Probe redirects | 02-17 | No second request for 301/302/303/307/308 |
| REVIEW CR-02 | Manifest chmod | 02-18 Task 1 | Existing parents unchanged on success/failure |
| REVIEW CR-03 | Header count / actual topic | 02-14 | Independent encoded oracle, cap128, <=value cap+8192 |
| REVIEW CR-04 | getEvent bearer claim | 02-16 | API-key-only published runtime contract |
| REVIEW CR-05 | Poison aggregate budget | 02-15 | Real delayed multi-poison rebalance and durable prefixes |
| REVIEW WR-01 | Ambient seed key | 02-18 Task 2 | Explicit child env, parent env unchanged |
| REVIEW WR-02 | Retention overrides | 02-20 Tasks 1..2 | TestTopicRetentionOverridesReapply, test_stack_config.py |
| REVIEW WR-03 | Docker private keys | 02-19 Task 1 | Actual matcher/export tests, no real secret fixture |
| REVIEW WR-04 | Tag-only modules | 02-19 Task 2 | Both real go-list graphs scanned, both failures propagate |
| Historical metadata | Premature complete from SUMMARY count | 02-21 Tasks 1/3 | Verifying/pending until independent assessment |
| Final acceptance | Full gate, exact two seeds, preserve volumes | 02-21 Task 2 | Fresh sanitized exits/totals in additive GAP-ACCEPTANCE |
| External evidence | Hosted CI | Pending, outside push scope | No push or invented result; retain explicit pending |

## Multi-source audit

COVERED means assigned within old+new plans, not newly verified.

| SOURCE | ID | Required outcome/constraint | Plan(s) | Status |
|--------|----|-----------------------------|---------|--------|
| GOAL | Phase 2 | Scoped batches Kafka-acknowledged, queryable and duplicate-safe | 02-01..13; repairs14..21 | COVERED |
| REQ | ING-01 | Scoped/hash-only key and bounded batch | 02-01/02/13; 02-16/21 | COVERED |
| REQ | ING-02 | Atomic strict rejection | 02-03/13; 02-21 regression gate | COVERED |
| REQ | ING-03 | Kafka-only 202 acknowledgement | 02-02/13; 02-21 gate | COVERED |
| REQ | ING-04 | Retry/replay duplicate safety | 02-04/06/11/13; 02-15/21 | COVERED |
| REQ | ING-05 | Uniform public error envelopes | 02-00/01/02/03/08/13; 02-16/21 | COVERED |
| REQ | ING-06 | Redis quota and explicit outage policy | 02-08/13; 02-21 gate | COVERED |
| REQ | ING-07 | Bounded work/buffers | 02-00/02/08/11/12/13; 02-14/15/19/21 | COVERED |
| REQ | STOR-01 | Insert before source commit | 02-06/07/11/13; 02-15/21 | COVERED |
| REQ | STOR-02 | Monthly schema/default/configurable ClickHouse TTL | 02-04/05/13; 02-21 gate | COVERED |
| REQ | STOR-03 | Immediate read-time uniqueness | 02-04/11/13; 02-15/16/21 | COVERED |
| REQ | STOR-04 | Bounded acknowledged poison quarantine | 02-07/13; 02-14/15/20/21 | COVERED |
| REQ | GEN-01 | Seeded reproducible benign producer | 02-10/13; 02-18/21 | COVERED |
| REQ | GEN-03 | One-command exact 1000 benign run | 02-01/12/13; 02-17..21 | COVERED |
| REQ | OBS-01 | Five real readiness/liveness endpoints | 02-09/12/13; 02-17/21 | COVERED |
| CONTEXT | D-01 | Key format/hash/constant-time | 02-01; 02-21 existing regression | COVERED |
| CONTEXT | D-02 | PostgreSQL keys/scopes | 02-01; 02-16/21 | COVERED |
| CONTEXT | D-03 | X-API-Key/uniform401 | 02-01/02; 02-16 | COVERED |
| CONTEXT | D-04 | Bounded positive cache30s | 02-01; 02-21 gate | COVERED |
| CONTEXT | D-05 | CLI/private ensured demo keys | 02-01/13; 02-18 | COVERED |
| CONTEXT | D-06 | Literal batch route/caps | 02-02/03; 02-21 gate | COVERED |
| CONTEXT | D-07 | Whole-batch validation/server timestamp | 02-03; 02-21 gate | COVERED |
| CONTEXT | D-08 | Tenant/scope from key | 02-02/03; 02-16/21 | COVERED |
| CONTEXT | D-09 | Exact202/error envelope | 02-02/03; 02-21 gate | COVERED |
| CONTEXT | D-10 | Nonqueued load shedding | 02-02/08; 02-21 gate | COVERED |
| CONTEXT | D-11 | All-ISR bounded idempotent producer | 02-02/11; 02-21 gate | COVERED |
| CONTEXT | D-12 | Shared manual consumer helper | 02-06/07; 02-15 | COVERED |
| CONTEXT | D-13 | Read-key-only tenant event lookup | 02-04; 02-16 | COVERED |
| CONTEXT | D-14 | Events schema/7day configurable TTL | 02-04/05; 02-21 gate | COVERED |
| CONTEXT | D-15 | Read-time dedupe/noOPTIMIZE | 02-04/11; 02-15/21 | COVERED |
| CONTEXT | D-16 | Batch/retries/commit/shutdown | 02-06; 02-15 | COVERED |
| CONTEXT | D-17 | Native ClickHouse pinned client | 02-04/06; 02-15/21 | COVERED |
| CONTEXT | D-18 | Reason/origin/ack-before-offset DLQ | 02-07; 02-14/15 | COVERED |
| CONTEXT | D-19 | RedisTIME per-key cost/outage policy | 02-08; 02-21 gate | COVERED |
| CONTEXT | D-20 | Private ops/readiness listener | 02-09/12; 02-17/21 | COVERED |
| CONTEXT | D-21 | Bounded deterministic generator | 02-10; 02-18/21 | COVERED |
| CONTEXT | D-22 | Exact seed and shortfall failure | 02-13; 02-18/20/21 | COVERED |
| CONTEXT | D-23 | Pinned nonroot Compose apps/order | 02-12; 02-19/20/21 | COVERED |
| CONTEXT | D-24 | PublicHTTP/log/CORS defaults | 02-00; 02-19/21 | COVERED |
| CONTEXT | D-25 | Prohibited-module CI check | 02-13; 02-19 | COVERED |
| CONTEXT | D-26 | Insert failure fatal, never poison | 02-06/07; 02-15/21 | COVERED |
| CONTEXT | D-27 | Exact error precedence/details50 | 02-03/08; 02-21 gate | COVERED |
| CONTEXT | D-28 | Error/types/colon; original auth alternative superseded by established Phase 2 boundary | 02-03; 02-16 | COVERED |
| CONTEXT | D-29 | Explicit host/container binds | 02-00/09/12; 02-21 gate | COVERED |
| CONTEXT | D-30 | No write-dedupe window, cancellation, static probe, private context | 02-02/04/12; 02-14/17/19 | COVERED |
| RESEARCH | Patterns1/2 | Echo literal colon, tenant-first bounded validation | 02-02/03/08; 02-21 gate | COVERED |
| RESEARCH | Pattern3 | Cancellation-aware bounded producer | 02-02/11; 02-14/21 | COVERED |
| RESEARCH | Pattern4 | Scoped key persistence/private CLI | 02-01; 02-18/21 | COVERED |
| RESEARCH | Pattern5 | RedisTIME Lua and bounded outage | 02-08; 02-21 gate | COVERED |
| RESEARCH | Pattern6 | Native inserts, configured TTL, uniqExact | 02-04/05; 02-15/21 | COVERED |
| RESEARCH | Pattern7 | In-flight semaphore/memory caps | 02-02/08; 02-21 gate | COVERED |
| RESEARCH | Pattern8/Pitfall13 | Manual effects/offset/rebalance discipline | 02-06/07; 02-14/15 | COVERED |
| RESEARCH | Pattern9 | Shared private ops, drain/LIFO | 02-09; 02-17/21 | COVERED |
| RESEARCH | Pattern10 | Anchored identical-byte retry/private manifest | 02-10; 02-18 | COVERED |
| RESEARCH | Pattern11 | Digest build/loopback Compose/context hygiene | 02-12; 02-17/19/20 | COVERED |
| RESEARCH | Pattern12 | Service-specific bounds/defaults/config | 02-00..09; 02-15/21 | COVERED |
| RESEARCH | Pitfalls1/2/9 | Merge-independent reads and replay-safe validation | 02-04/06/11; 02-15/21 | COVERED |
| RESEARCH | Pitfalls3..8/10..12 | Producer cancellation, route/CORS/logs, wire cap, pins, port/key/seed safety | 02-00..13; 02-14/17/18/19/21 | COVERED |
| RESEARCH | Security L1 | Tenant/auth/input/log/resource/config boundaries | Historical threat models; new14..21 threat models | COVERED |
| RESEARCH | Package audit | Reuse approved pins; no new install | 02-19 guard; 02-21 gate | COVERED |
| RESEARCH | Validation | Real dependencies, nonempty groups, bounded gates | 02-15/19/20/21 | COVERED |
| RESEARCH | Assumptions A2/A7/A8 | No new unmeasured memory/query/cache claims | Existing caps/read semantics; final real gate | COVERED |

All source items are covered across original+repair plans. Exclusions: CONTEXT deferred items; future research list APIs/benchmark optimizations; alert/scenario work assigned to Phase3; JWT/RBAC Phase4; metrics/scale Phase5; deployment hardening Phase6.

