---
phase: 02-ingestion-durable-event-storage
verified: 2026-10-16T08:36:32Z
status: gaps_found
score: 37/43 must-haves verified
overrides_applied: 0
gaps:
  - truth: "Phase 2 MVP goal is a canonical user story"
    status: failed
    reason: "ROADMAP.md marks Phase 2 as mode mvp, but its goal fails the canonical user-story validator because it lacks the required 'As a ..., I want to ..., so that ... .' structure."
    artifacts:
      - path: ".planning/ROADMAP.md"
        issue: "gsd-sdk user-story.validate returned valid=false."
    missing:
      - "Run $gsd-mvp-phase 2 (or equivalently update the roadmap goal through that workflow) before MVP user-flow verification."
  - truth: "Poison records have bounded DLQ effects and remain safely committable through rebalances"
    status: failed
    reason: "The DLQ size calculation ignores per-header encoding overhead and header count, and the rebalance timeout covers one retry sequence rather than the sequential retry path for up to 5,000 poison records."
    artifacts:
      - path: "backend/internal/kafkax/dlq.go"
        issue: "Empty-header floods add zero to dlqRecordSize; the configured destination topic is applied after sizing."
      - path: "backend/internal/kafkax/consumer.go"
        issue: "rebalanceTimeout budgets one operation/retry/commit sequence."
      - path: "backend/internal/eventwriter/writer.go"
        issue: "durablePrefixes synchronously retries every poison record in sequence."
    missing:
      - "Cap header count and conservatively account for encoded overhead and the actual destination topic."
      - "Bound poison work per poll/commit prefix and test a rebalance during multiple slow DLQ effects."
  - truth: "The published getEvent authentication alternatives are implemented"
    status: failed
    reason: "OpenAPI advertises ApiKeyAuth OR BearerAuth, while the query handler reads only X-Api-Key and has no bearer verifier."
    artifacts:
      - path: "api/openapi.yaml"
        issue: "GET /api/v1/events/{event_id} advertises BearerAuth."
      - path: "backend/internal/eventquery/handler.go"
        issue: "GetEvent authenticates only X-Api-Key."
    missing:
      - "Implement tenant-bound bearer verification, or remove the bearer alternative until Phase 4 and add an implementation-level contract test."
  - truth: "The container health probe cannot be redirected away from loopback"
    status: failed
    reason: "The initial URL is loopback-validated, but the default http.Client follows redirects without revalidating the target."
    artifacts:
      - path: "backend/tools/healthprobe/main.go"
        issue: "No CheckRedirect policy; a loopback 3xx can redirect to a non-loopback host."
    missing:
      - "Reject redirects or validate every redirect target; add a no-second-request regression test."
  - truth: "Generator output handling cannot mutate unrelated existing directories"
    status: failed
    reason: "The manifest writer chmods path.parent to 0700 even when the directory already existed."
    artifacts:
      - path: "generator/src/sentinel_generator/cli.py"
        issue: "_write_manifest unconditionally executes path.parent.chmod(0o700)."
    missing:
      - "Only chmod a newly created dedicated directory and preserve existing parent permissions."
  - truth: "No local credential material enters the Docker build context"
    status: failed
    reason: "Git ignores *.pem, but .dockerignore does not; Dockerfile COPY backend/ can send and copy ignored private material into the build layer/cache."
    artifacts:
      - path: ".dockerignore"
        issue: "Credential extensions such as **/*.pem and **/*.key are not excluded."
      - path: "deployments/docker/Dockerfile"
        issue: "COPY backend/ ./ includes any such local files."
    missing:
      - "Exclude credential patterns and add a build-context hygiene regression."
  - truth: "make seed and CI policy guards are deterministic under supported environments"
    status: failed
    reason: "make seed inherits SENTINEL_API_KEY, which overrides its freshly ensured --key-file, and the banned-module guard scans only the default Go build graph."
    artifacts:
      - path: "scripts/seed.py"
        issue: "Generator subprocess inherits ambient SENTINEL_API_KEY."
      - path: "generator/src/sentinel_generator/cli.py"
        issue: "Environment key takes precedence over an explicit key file."
      - path: ".github/workflows/ci.yml"
        issue: "Prohibited-module scan omits -tags=integration."
    missing:
      - "Remove SENTINEL_API_KEY from the seed generator environment or prioritize explicit --key-file."
      - "Scan both default and integration dependency graphs, preserving fail-closed go list behavior."
human_verification:
  - test: "Push a branch and confirm every GitHub Actions job, including the Docker-backed integration job, passes."
    expected: "All hosted jobs are green and every nonempty Phase 2 integration group executes."
    why_human: "No branch was pushed; only the equivalent local gates have run."
---

# Phase 2: Ingestion & Durable Event Storage Verification Report

**Phase Goal:** A producer with a scoped API key can submit event batches that are durably acknowledged only after Kafka accepts them, and the events end up queryable and duplicate-safe in ClickHouse.
**Verified:** 2026-10-16T08:36:32Z
**Status:** gaps_found
**Re-verification:** No — initial verification.

## User Flow Coverage

Phase 2 is marked `Mode: mvp`, but its ROADMAP goal is not a valid MVP user story. The canonical validator returned `false` because the goal lacks `As a ..., I want to ..., so that ... .`. Per the MVP verification contract, a canonical user-flow verdict cannot be issued until `$gsd-mvp-phase 2` repairs the goal. The plan files consistently contain an inferred story (producer submits acknowledged batches so events become queryable and duplicate-safe), and the technical path for that story was audited below; this does not waive the format gate.

| Step | Expected | Evidence | Status |
|------|----------|----------|--------|
| Story contract | ROADMAP goal parses into role, capability, and outcome | `gsd-sdk query user-story.validate ...` returned `valid=false` | ✗ BLOCKED |
| Technical path (supplemental) | Scoped producer → ingest → Kafka ack → writer → ClickHouse → tenant lookup | Real implementation is wired and the local integration/seed gates passed | ✓, with blockers below |
| Outcome (supplemental) | Current-run IDs are visible once and cross-tenant reads are hidden | Two fresh 1,000-event seed runs reported exact visibility and tenant isolation | ✓ |

## Goal Achievement

### Roadmap Success Criteria

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Valid scoped batches receive 202 only after all Kafka acknowledgements; invalid/auth/size/unavailable cases use the JSON envelope | ✓ VERIFIED | `ingest/handler.go` authenticates and validates before `PublishBatch`; `kafkax/producer.go` uses `AllISRAcks`, bounded buffers and `ProduceSync`; fresh full integration gate passed. |
| 2 | `make seed` submits 1,000 demo events and verifies ClickHouse plus event lookup | ✓ VERIFIED | Two consecutive live runs reported sent/accepted/visible=1000, three lookup checks, tenant_isolation=1 and retries=0. Ambient-key behavior is a separate reliability gap. |
| 3 | Retry/replay never exposes duplicate rows or counts, immediately and without OPTIMIZE | ✓ VERIFIED | `events.go` uses tenant-bound `uniqExact`; real double-insert and crash/ambiguous-publish tests passed; no `OPTIMIZE` is used. |
| 4 | Quota returns 429/Retry-After; Redis outage policy is explicit; load and producer buffers are bounded | ✓ VERIFIED | Redis TIME Lua bucket, fail-closed default, in-flight semaphore, bounded franz-go buffers; real rate-limit/outage/load tests passed. |
| 5 | Poison reaches DLQ before source commit; all services expose dependency-aware readiness | ✗ FAILED | Normal poison and readiness tests pass, but empty-header floods evade DLQ bounds and poison-heavy sequential retries can exceed the rebalance budget. |

### Plan-Specific Truth Audit

All 38 PLAN frontmatter truths were inspected against implementation, tests and wiring rather than SUMMARY claims.

| Plan | Result | Details |
|------|--------|---------|
| 02-00 | 2/2 VERIFIED | CORS/log sanitization and bind configuration are implemented and tested. |
| 02-01 | 3/3 VERIFIED | Hash-only persistence, constant-time secret verification and private keyctl flows are substantive. |
| 02-02 | 4/4 VERIFIED | Scoped bounded intake, synchronous all-ISR acknowledgement and public-route controls are wired. |
| 02-03 | 3/3 VERIFIED literally | Atomic rejection/error details and the documented auth alternatives exist; runtime bearer mismatch is a separate broken contract link. |
| 02-04 | 3/3 VERIFIED | Native schema, tenant lookup and immediate read-time dedupe are implemented. |
| 02-05 | 2/2 VERIFIED | TTL override is validated and applied after every migration run. |
| 02-06 | 2/3 VERIFIED | Commit-after-insert and terminal storage failure are correct; the claimed bound does not include sequential poison work. |
| 02-07 | 1/2 VERIFIED | Commit-after-effect ordering exists; the claimed bounded DLQ context is bypassable by empty-header count/encoding overhead. |
| 02-08 | 3/3 VERIFIED | Atomic quotas, precedence and fail-closed behavior are wired. |
| 02-09 | 2/2 VERIFIED | Five binaries expose separate bounded liveness/readiness with real checks and draining. |
| 02-10 | 2/2 VERIFIED | Deterministic identical-byte retry and private key loading exist; manifest parent chmod remains a blocker anti-pattern. |
| 02-11 | 3/3 VERIFIED | Crash and ambiguous-publish recovery tests are causal and failure controls are integration-only. |
| 02-12 | 2/3 VERIFIED | Ordered healthy stack and nonroot pinned image exist; generic credential files can enter build context. |
| 02-13 | 1/3 VERIFIED | Actual local gates are recorded; seed key selection and integration-tag dependency policy are not deterministic/complete. |

**Score:** 37/43 truths verified (5 roadmap criteria + 38 plan truths).

## Required Artifacts

`verify.artifacts` found all 39 declared artifacts present and syntactically substantive. Semantic inspection changes the result for the groups below.

| Artifact group | Expected | Status | Details |
|----------------|----------|--------|---------|
| 02-00 through 02-06 declared artifacts | Authenticated bounded intake, ClickHouse storage, retention and writer | ✓ VERIFIED | Exists, substantive and wired into service binaries/tests. |
| `backend/internal/kafkax/dlq.go` | Bounded DLQ encoding and acknowledged publish | ✗ DEFECTIVE | Header count/encoding and configured topic length are not safely bounded. |
| `backend/internal/opshttp/server.go` + five commands | Real health/readiness and draining | ✓ VERIFIED | Live ingest `/healthz` and `/readyz` returned live/ready with Kafka/Postgres/Redis ok. |
| Generator send/CLI | Deterministic send and private manifest | ⚠️ PARTIAL | Send behavior is substantive; existing output parent permissions can be destructively changed. |
| Recovery tests/failpoints | Real crash/ambiguous retry proof, integration-only controls | ✓ VERIFIED | Default build excludes integration controls; causal tests passed in the fresh gate. |
| Dockerfile/Compose/healthprobe | Pinned nonroot local stack and safe static probe | ✗ DEFECTIVE | Stack works, but redirects escape loopback validation and credential extensions are absent from `.dockerignore`. |
| Seed/CI artifacts | Exact 1,000-ID acceptance and policy guards | ✗ PARTIAL | Live acceptance passes in a clean environment; ambient key precedence and integration-only dependency scanning remain gaps. |

## Key Link Verification

The SDK reported 31/32 declared links directly. The one reported miss is a false negative: `verifier.go` calls `VerifyHash`, and `keys.go` implements that helper with `subtle.ConstantTimeCompare`, so all 32 declared links are manually verified. An additional cross-artifact contract link is broken.

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| Ingest handler | API-key verifier / rate limiter / Kafka producer | Authenticate → Allow → PublishBatch | ✓ WIRED | Tenant and scope come from verified principal; 202 follows `ProduceSync`. |
| Event writer | ClickHouse / DLQ / Kafka offsets | Insert or Publish before CommitRecords | ✓ WIRED | Ordinary effect ordering is explicit. |
| Query handler | ClickHouse EventStore | Tenant-bound `Get` | ✓ WIRED | Authenticated tenant and canonical UUID are passed to parameterized storage. |
| Seed Make target | keyctl → generator → manifest → ClickHouse/query API | `scripts/seed.py` | ✓ WIRED | Exact-ID and cross-tenant checks are real; environment precedence weakens determinism. |
| OpenAPI BearerAuth | Query handler | Advertised security alternative | ✗ NOT WIRED | No bearer/JWT verification path exists. |

## Data-Flow Trace (Level 4)

| Artifact | Data | Source | Produces Real Data | Status |
|----------|------|--------|--------------------|--------|
| Ingest handler | Canonical event batch | Authenticated JSON request | Kafka records acknowledged with all-ISR `ProduceSync` | ✓ FLOWING |
| Event writer | Kafka records | Manual group consumer | Native ClickHouse batch then manual offset commit | ✓ FLOWING |
| Event query | Event object | Tenant/id parameterized ClickHouse query | Real stored event or tenant-hidden 404 | ✓ FLOWING |
| Seed verifier | 1,000 intended IDs | Generator manifest | Exact tenant/run-bound ClickHouse membership plus API reads | ✓ FLOWING |
| DLQ path | Poison record/context | Kafka source record | Ordinary poison flows, but adversarial header cardinality can make the record unbounded | ✗ PARTIAL |

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Running ingest liveness | `curl --max-time 3 http://127.0.0.1:9101/healthz` | `{"status":"live"}` | ✓ PASS |
| Running ingest dependency readiness | `curl --max-time 3 http://127.0.0.1:9101/readyz` | ready; Kafka/Postgres/Redis all `ok` | ✓ PASS |
| Core unit behavior | `go test ./internal/apikeys ./internal/ingest ./internal/eventquery ./internal/kafkax ./tools/healthprobe -count=1 -timeout 10s` | all five packages passed | ✓ PASS |
| Full local gate | `make lint test && make test-integ` | Fresh execution passed; integration package 204.715s | ✓ PASS |
| Exact end-to-end seed | `make seed` twice | Each run verified exactly 1,000 visible IDs and tenant isolation | ✓ PASS |

## Probe Execution

No `scripts/*/tests/probe-*.sh` files or phase-declared probes exist, so Step 7c is not applicable.

## Requirements Coverage

All 14 IDs mapped to Phase 2 appear in PLAN frontmatter; there are no orphaned Phase 2 requirements.

| Requirement | Source plan(s) | Status | Evidence |
|-------------|----------------|--------|----------|
| ING-01 | 02-01, 02-02, 02-13 | ✓ SATISFIED | Hash-only scoped key store and tenant-derived intake. |
| ING-02 | 02-03, 02-13 | ✓ SATISFIED | Atomic strict validation with bounded indexed details and zero-publish integration cases. |
| ING-03 | 02-02, 02-13 | ✓ SATISFIED | Ordered 202 follows all-ISR `ProduceSync` results. |
| ING-04 | 02-02, 02-04, 02-06, 02-11, 02-13 | ✓ SATISFIED | Stable IDs plus immediate read-time dedupe and retry/crash tests. |
| ING-05 | 02-00, 02-01, 02-02, 02-03, 02-08, 02-13 | ✓ SATISFIED | 401/403/413/503 envelope matrix is implemented/tested. |
| ING-06 | 02-08, 02-13 | ✓ SATISFIED | Redis TIME token bucket, 429/Retry-After, explicit closed/open policy. |
| ING-07 | 02-00, 02-02, 02-08, 02-11, 02-12, 02-13 | ✓ SATISFIED | Nonblocking HTTP admission and capped producer/fetch buffers. |
| STOR-01 | 02-06, 02-07, 02-11, 02-13 | ✓ SATISFIED with risk | Insert-before-commit is explicit; poison-heavy rebalance budgeting remains a robustness blocker. |
| STOR-02 | 02-04, 02-05, 02-13 | ✓ SATISFIED | Required partition/order/index/default TTL and validated override. |
| STOR-03 | 02-04, 02-11, 02-13 | ✓ SATISFIED | `uniqExact` and lookup dedupe immediately without OPTIMIZE. |
| STOR-04 | 02-07, 02-13 | ✗ BLOCKED | Ordinary DLQ behavior passes, but unbounded empty-header metadata can prevent the promised poison quarantine. |
| GEN-01 | 02-10, 02-13 | ✓ SATISFIED with safety gap | Reproducible documentation-range events work; manifest parent chmod is unsafe. |
| GEN-03 | 02-01, 02-12, 02-13 | ✓ SATISFIED with reliability gap | Live 1,000-event seed passes; ambient key override can invalidate the one-command path. |
| OBS-01 | 02-09, 02-12, 02-13 | ✓ SATISFIED | All five binaries wire real dependency checks; live ingest endpoints passed. |

## Anti-Patterns and Review Findings

The standard-depth code review's findings were independently checked against the source. They are not dismissed by green tests.

| Finding | Evidence | Severity | Effect |
|---------|----------|----------|--------|
| Healthprobe follows redirects | `http.Client` has no `CheckRedirect` | 🛑 BLOCKER | Breaks the claimed loopback-only safety boundary. |
| Manifest chmods existing parent | unconditional `path.parent.chmod(0o700)` | 🛑 BLOCKER | Destructive behavior outside requested output. |
| DLQ header-count/encoding bypass | size sums only payload bytes; no count cap | 🛑 BLOCKER | STOR-04 / bounded-writer truth can fail. |
| OpenAPI bearer mismatch | contract has BearerAuth; handler only X-Api-Key | 🛑 BLOCKER | Public contract alternative is unusable. |
| Poison work omitted from rebalance budget | one-sequence timeout vs sequential effects | 🛑 BLOCKER | Poison-heavy partitions can replay/stall. |
| Seed inherits ambient key | env takes precedence over explicit key file | ⚠️ WARNING → truth failure | One-command seed is not deterministic in a supported shell environment. |
| Retention overrides are creation-only | `--create --if-not-exists`; checker hardcodes defaults | ⚠️ WARNING | Repeat startup does not apply documented retention changes. |
| Docker context omits credential extensions | `.dockerignore` lacks PEM/key patterns | ⚠️ WARNING → truth failure | Contradicts plan 02-12's secret-free context truth. |
| CI banned-module scan is default-tag only | no integration graph scan | ⚠️ WARNING → truth failure | Contradicts plan 02-13's policy truth. |

No unreferenced `TBD`, `FIXME`, or `XXX` markers were found in the Phase 2 implementation. The dashboard and benchmark placeholders are explicitly scheduled for later phases and are not Phase 2 stubs.

## Disconfirmation Pass

- **Partially met requirement:** STOR-04 works for ordinary malformed records but is not robust to an empty-header cardinality flood.
- **Misleading passing test:** `TestBuildDLQRecordRedactsOversizeValueAndBoundsHeaderFlood` uses 128-byte header values and computes size with the same payload-only model as production; it does not test thousands of empty headers or Kafka encoding overhead.
- **Uncovered error paths:** healthprobe redirects, an existing manifest parent directory's mode, conflicting ambient `SENTINEL_API_KEY`, integration-tag-only banned imports, and rebalancing during multiple slow poison publishes are not covered.

## Human Verification Required

### 1. Hosted GitHub Actions

**Test:** Push a branch and observe every GitHub Actions job, especially the Docker-backed integration job and all seven nonempty Phase 2 groups.
**Expected:** Every hosted job is green and no behavioral group is empty.
**Why human:** No push occurred. Local equivalents passed, but hosted runner behavior is not established.

## Gaps Summary

The central happy-path slice is real: scoped batches are synchronously acknowledged by Kafka, written to ClickHouse, queried with tenant isolation, and immediately deduplicated; the full local gate and two exact 1,000-event runs passed. The phase still cannot pass verification because one roadmap truth and five plan truths fail under adversarial or supported-environment conditions, and five independently confirmed blocker findings remain. The most direct requirement impact is STOR-04: poison quarantine is not actually bounded for arbitrary Kafka headers, and poison-heavy work is not covered by the rebalance timeout. The MVP roadmap goal must also be normalized before canonical user-flow verification.

---

_Verified: 2026-10-16T08:36:32Z_
_Verifier: the agent (gsd-verifier)_
