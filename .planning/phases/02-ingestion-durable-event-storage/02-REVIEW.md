---
phase: 02-ingestion-durable-event-storage
reviewed: 2026-10-16T20:12:08Z
depth: standard
files_reviewed: 99
files_reviewed_list:
  - .dockerignore
  - .env.example
  - .github/workflows/ci.yml
  - .gitignore
  - Makefile
  - README.md
  - api/openapi.yaml
  - backend/cmd/alert-writer/main.go
  - backend/cmd/detector/main.go
  - backend/cmd/event-writer/main.go
  - backend/cmd/ingest/main.go
  - backend/cmd/query-api/main.go
  - backend/go.mod
  - backend/internal/apikeys/keys.go
  - backend/internal/apikeys/keys_test.go
  - backend/internal/apikeys/store.go
  - backend/internal/apikeys/verifier.go
  - backend/internal/apikeys/verifier_test.go
  - backend/internal/app/app.go
  - backend/internal/app/app_test.go
  - backend/internal/chmigrate/chmigrate.go
  - backend/internal/chmigrate/chmigrate_test.go
  - backend/internal/config/config.go
  - backend/internal/config/config_test.go
  - backend/internal/errs/errs_test.go
  - backend/internal/errs/http.go
  - backend/internal/eventquery/handler.go
  - backend/internal/eventquery/handler_test.go
  - backend/internal/eventwriter/failpoint.go
  - backend/internal/eventwriter/failpoint_integration.go
  - backend/internal/eventwriter/writer.go
  - backend/internal/eventwriter/writer_test.go
  - backend/internal/httpx/httpx.go
  - backend/internal/httpx/httpx_test.go
  - backend/internal/ingest/handler.go
  - backend/internal/ingest/handler_test.go
  - backend/internal/kafkax/consumer.go
  - backend/internal/kafkax/consumer_test.go
  - backend/internal/kafkax/dlq.go
  - backend/internal/kafkax/dlq_test.go
  - backend/internal/kafkax/failpoint.go
  - backend/internal/kafkax/failpoint_integration.go
  - backend/internal/kafkax/producer.go
  - backend/internal/kafkax/producer_test.go
  - backend/internal/middleware/global.go
  - backend/internal/middleware/middleware_test.go
  - backend/internal/opshttp/server.go
  - backend/internal/opshttp/server_test.go
  - backend/internal/ratelimit/ratelimit.go
  - backend/internal/ratelimit/ratelimit_test.go
  - backend/internal/redisx/redisx.go
  - backend/internal/redisx/redisx_test.go
  - backend/internal/storage/clickhouse/client.go
  - backend/internal/storage/clickhouse/events.go
  - backend/internal/storage/clickhouse/events_test.go
  - backend/internal/testing/containers.go
  - backend/migrations/clickhouse/002_events.sql
  - backend/migrations/postgres/002_producer_keys.sql
  - backend/test/dedupe_test.go
  - backend/test/ingest_test.go
  - backend/test/keyctl_test.go
  - backend/test/lifecycle_test.go
  - backend/test/main_test.go
  - backend/test/migrate_test.go
  - backend/test/openapi_drift_test.go
  - backend/test/openapi_runtime_auth_test.go
  - backend/test/openapi_test.go
  - backend/test/ratelimit_test.go
  - backend/test/readyz_test.go
  - backend/test/recovery_test.go
  - backend/test/topic_retention_test.go
  - backend/test/writer_rebalance_test.go
  - backend/test/writer_test.go
  - backend/tools/chmigrate/main.go
  - backend/tools/chmigrate/main_test.go
  - backend/tools/healthprobe/main.go
  - backend/tools/healthprobe/main_test.go
  - backend/tools/keyctl/main.go
  - backend/tools/keyctl/main_test.go
  - deployments/compose/docker-compose.yml
  - deployments/docker/Dockerfile
  - docs/decisions/0002-event-id-policy.md
  - docs/decisions/0003-atomic-batch-semantics.md
  - docs/event-storage.md
  - generator/README.md
  - generator/pyproject.toml
  - generator/requirements.txt
  - generator/src/sentinel_generator/cli.py
  - generator/src/sentinel_generator/send.py
  - generator/tests/test_send.py
  - scripts/check-docker-context.py
  - scripts/check-go-dependencies.sh
  - scripts/check-stack.sh
  - scripts/seed.py
  - scripts/test_docker_context.py
  - scripts/test_go_dependencies.py
  - scripts/test_seed.py
  - scripts/test_stack_config.py
  - web/src/api/types.test.ts
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 2: Code Review Report

**Reviewed:** 2026-10-16T20:12:08Z
**Depth:** standard
**Files Reviewed:** 99
**Status:** clean

## Narrative Findings (AI reviewer)

## Summary

Independent review of the existing Phase 2 changed-file scope from `95f30ab^..HEAD`, excluding planning artifacts, deleted files and dependency lock/checksum files. Manual tracing concentrated on repaired trust boundaries, their callers, and the assertions establishing real durable effects. No structural pre-pass was supplied.

No current findings remain after focused independent re-review of repair `428604a` and its regression tests `537e073`. The preceding independent assessment at 2026-10-16T20:09:03Z found one blocker and one warning; both are retained below as resolved historical findings. The original five critical and four warning findings remain preserved in the unchanged `02-REVIEW-2026-10-16-PRE-GAPS.md`.

The initial review checked the actual encoded-DLQ builder and 128-header/pre-copy limits, shared checked writer budgets, aggregate poison stage and independent commit reservation, per-partition durable prefixes, parent-only shutdown transfer, default-build exclusion of integration controls, real recovery barriers, redirect rejection, descriptor-anchored manifest output, sanitized generator environment, API-key-only event lookup contract, actual Docker fixture exporter, default/integration dependency enumeration, and repeated retention alteration/checking. The old manifest-output chmod defect is fixed in the generator; the separate seed-wrapper defect identified below is now also closed.

The 809-second local acceptance results in `02-GAP-ACCEPTANCE.md` were treated as implementer evidence, not as proof that untested edge cases are correct. This reviewer did not rerun the broad acceptance suite. Focused reproductions below use temporary synthetic directories and read-only requests to the running local ops listener. Hosted CI remains pending and is not classified as a source defect. Deferred Phase 4 JWT/dashboard work, metrics and benchmarks are outside this review.

## Focused re-review and evidence

The follow-up examined only `scripts/seed.py`, `scripts/test_seed.py` and `README.md`; the 99-file list preserves the preceding Phase 2 review scope, not a claim that all files were reread during this follow-up. No Go/service source changed, so the broad acceptance suite was not rerun.

- **CR-01 closed:** `_prepare_key_directory` now opens the root and each managed component with `O_DIRECTORY | O_NOFOLLOW`, creates children relative to opened descriptors, validates the full chain before permission repair, and applies `fchmod` to the opened directories. A symlink at either `.local` or `keys` therefore fails before permission mutation or traversal into the external target. Descriptors close on both success and failure.
- **Fresh reviewer check:** `python3 -B -m unittest scripts.test_seed.KeySafetyTests -v` passed all **4 tests** in 0.001s. The directory regression covers both previously reproduced symlink cases and asserts the external directory stays mode `0755` and empty. Repeated preparation of real directories retains the same inode and mode `0700`; existing key permission repair and final-file symlink rejection also pass.
- **WR-01 closed:** `README.md:129` now targets `/healthz`, consistent with the registered route and OpenAPI. A fresh bounded, read-only request returned HTTP **200**.
- **Additional implementer evidence:** `02-GAP-ACCEPTANCE.md` at `2af69fe` records all **27 script tests** passing and two further real seed runs, each with **sent=accepted=visible=1000**, API spotchecks **3**, tenant isolation **1**, retries **0**. These are supporting results; the independent checks above establish closure of the two reviewed failure cases.

No current BLOCKER or WARNING findings remain in the reviewed scope. Hosted CI is still pending; this verdict does not claim a hosted run or a new broad review.

## Previously reported critical issues — resolved

### CR-01: Seed directory preparation follows symlinks and chmods their external targets

**Classification:** BLOCKER

**Resolution:** CLOSED at `428604a`; independently rechecked 2026-10-16T20:12:08Z. The issue, original reproduction and suggested fix below are preserved as historical audit evidence. The separate key-file substitution hardening suggestion was not established as an independent finding and does not expand the two-case directory defect assessed here.

**File:** `/home/logan78/Desktop/Sentinel/scripts/seed.py:140-144`

**Issue:** `KEY_DIRECTORY.mkdir(..., parents=True, exist_ok=True)` accepts an existing directory symlink, and the following two path-based `chmod(0700)` calls follow symlinks. If either `<repo>/.local` or `<repo>/.local/keys` is a symlink to an existing shared directory, ordinary `make seed` changes that unrelated target's permissions. For a `keys` symlink, this happens before `keyctl ensure` can reject the unsafe directory. For a `.local` symlink, preparation can also create a `keys` directory in the external target. An eventual failure does not undo those mutations. This is an unintended filesystem modification of the same class as the original manifest-output chmod defect, in a different entry point.

**Evidence:** Independently invoked the actual `seed._prepare_key_directory()` with only `ROOT` and `KEY_DIRECTORY` redirected to temporary fixtures. Both a symlink at `.local` and a symlink at `.local/keys` changed the external target from mode `0755` to `0700`. No real repository credential, environment, volume or external shared directory was touched.

**Fix:** Traverse/create the managed directories through anchored directory descriptors with `O_DIRECTORY | O_NOFOLLOW`, refusing symlinks before creating children or changing permissions. Perform any intended permission changes through the validated descriptor, not a pathname. Preserve unrelated targets on all failure paths. Apply the same descriptor/no-follow discipline to `secure_key_file` at lines 128–133, whose separate `lstat` then path-based `chmod` otherwise leaves a substitution window. Add regressions for both linked directory components and component swaps, asserting failure with external permissions and contents unchanged; retain the ordinary idempotent seed path.

## Previously reported warnings — resolved

### WR-01: Documented liveness command targets an unregistered route

**Classification:** WARNING

**Resolution:** CLOSED at `428604a`; corrected command independently returned HTTP 200 on 2026-10-16T20:12:08Z.

**File:** `/home/logan78/Desktop/Sentinel/README.md:129`

**Issue:** The quickstart instructs operators to run `curl --fail ... http://127.0.0.1:9101/livez`, but the operations server registers only `GET /healthz` and `GET /readyz` (`backend/internal/opshttp/server.go:81-82`), consistent with the OpenAPI contract. The documented liveness command therefore reports failure even when the ingest process is healthy.

**Evidence:** Read-only requests against the running local listener returned HTTP **404** for `/livez` and HTTP **200** for `/healthz`.

**Fix:** Change the README command to `http://127.0.0.1:9101/healthz`.

---

_Reviewed: 2026-10-16T20:12:08Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
