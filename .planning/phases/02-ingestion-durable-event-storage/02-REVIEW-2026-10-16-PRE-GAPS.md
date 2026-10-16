---
phase: 02-ingestion-durable-event-storage
reviewed: 2026-10-16T08:30:20Z
depth: standard
files_reviewed: 93
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
  - backend/go.sum
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
  - backend/test/openapi_test.go
  - backend/test/ratelimit_test.go
  - backend/test/readyz_test.go
  - backend/test/recovery_test.go
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
  - generator/uv.lock
  - scripts/check-stack.sh
  - scripts/seed.py
  - scripts/test_seed.py
  - web/src/api/types.test.ts
findings:
  critical: 5
  warning: 4
  info: 0
  total: 9
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-10-16T08:30:20Z
**Depth:** standard
**Files Reviewed:** 93
**Status:** issues_found

## Summary

The Phase 2 implementation has strong test breadth, but five release-blocking defects remain at security and correctness boundaries: the supposedly loopback-only health probe follows unrestricted redirects; manifest output mutates arbitrary parent-directory permissions; the DLQ size cap can be bypassed with zero-length header floods; the published bearer-auth contract is not implemented; and the Kafka rebalance bound does not cover the writer's sequential poison-record work. Four additional robustness issues affect documented local operation and build secrecy.

## Critical Issues

### CR-01: Loopback health probe follows redirects to arbitrary hosts

**Classification:** BLOCKER

**File:** `backend/tools/healthprobe/main.go:34-52`

**Issue:** Only the initial URL is checked for a literal loopback host. The default `http.Client` used by `main` follows redirects, and no `CheckRedirect` callback revalidates or rejects the redirect target. A loopback endpoint returning `302 Location: http://169.254.169.254/...` therefore turns the probe into the SSRF primitive the validation is intended to prevent. A live reproduction against a loopback redirect returned exit code 0 after the redirected target answered 200.

**Fix:** Reject redirects for a health probe, or validate every redirect target with the same literal-loopback policy. For example, set `CheckRedirect` to return `http.ErrUseLastResponse` and treat every 3xx as failure. Add a test whose loopback server redirects to another host and assert no second request is sent.

### CR-02: Custom manifest output can chmod arbitrary existing directories

**Classification:** BLOCKER

**File:** `generator/src/sentinel_generator/cli.py:118-120`

**Issue:** `_write_manifest` unconditionally changes `path.parent` to mode `0700`, even when that directory already existed and was not created by the generator. `--output manifest.json` changes the current working directory's permissions; `--output /tmp/manifest.json` changes `/tmp`. This is destructive behavior outside the requested output file. A safe reproduction with a temporary mode-0755 directory changed it to 0700.

**Fix:** Track whether the parent was newly created. Set 0700 only on a newly created dedicated directory; for an existing directory, validate that it is a real directory (not a symlink) and leave its mode untouched. Continue creating the temporary file at 0600 and atomically replacing only the requested manifest path. Add a regression test asserting the parent mode is unchanged.

### CR-03: DLQ metadata cap is bypassable by empty-header floods

**Classification:** BLOCKER

**File:** `backend/internal/kafkax/dlq.go:152-177,196-201`

**Issue:** The claimed bounded DLQ record size sums only topic/key/value/header payload byte lengths. It does not count Kafka's per-header encoding overhead and does not cap header count. An untrusted source record containing thousands of empty-key/empty-value headers passes every size check because each candidate adds zero according to `dlqRecordSize`, yet produces a large encoded record. Publishing can then fail repeatedly and prevent the source partition from advancing. The same calculation also hardcodes `DefaultDLQTopic` even though `Publish` later replaces it with a configurable topic at line 93.

**Fix:** Stop copying arbitrary origin headers, or enforce a strict header-count cap plus a conservative encoded-size calculation that includes per-header varint/length overhead and the actual destination topic. Add tests with thousands of empty headers and with a long configured DLQ topic; assert the returned record remains within the real bound.

### CR-04: OpenAPI advertises bearer authentication that the query API cannot perform

**Classification:** BLOCKER

**File:** `api/openapi.yaml:10-13,101-108`; `backend/internal/eventquery/handler.go:41-52`

**Issue:** The public contract says query routes use bearer JWTs and explicitly declares `ApiKeyAuth` and `BearerAuth` as alternatives for `GET /api/v1/events/{event_id}`. The implementation reads only `X-Api-Key`; there is no bearer/JWT authentication in the query service or shared middleware. A conforming client that chooses the documented bearer alternative always receives 401.

**Fix:** Either implement and tenant-bind bearer verification before shipping this contract, or remove `BearerAuth` from this operation and revise the top-level authentication description until bearer support exists. Add an implementation-level contract test that exercises every advertised security alternative rather than only inspecting YAML.

### CR-05: Rebalance timeout omits sequential poison-record processing

**Classification:** BLOCKER

**File:** `backend/internal/kafkax/consumer.go:40-53,120-128`; `backend/internal/eventwriter/writer.go:229-252,291-320`

**Issue:** The consumer uses `BlockRebalanceOnPoll`, but `rebalanceTimeout` budgets only one retry sequence plus one commit. A batch may contain up to 5,000 poison records, and `durablePrefixes` publishes each DLQ record synchronously with its own full retry sequence. A rebalance that starts during such a batch can exceed the advertised rebalance timeout, causing Kafka to evict the member before its commit. The writer then replays already inserted events and already published DLQ records, and a poison-heavy partition can fail repeatedly.

**Fix:** Bound work per poll to a duration the rebalance timeout truly covers—for example, process/commit smaller per-partition prefixes or batch DLQ publication with a strict total deadline. Compute the group timeout from the actual maximum processing path, including poison effects, and add an integration test that triggers a rebalance while multiple slow DLQ records are in flight.

## Warnings

### WR-01: `make seed` can ignore the keys it just provisioned

**Classification:** WARNING

**File:** `scripts/seed.py:304-330`; `generator/src/sentinel_generator/cli.py:111-115`

**Issue:** The seed script invokes the generator without a sanitized environment. The generator deliberately prefers ambient `SENTINEL_API_KEY` over the explicit `--key-file`, so a developer who has that variable set can send with an unrelated key and make the documented one-command seed fail or target the wrong allowed tenant.

**Fix:** Pass a copy of the environment with `SENTINEL_API_KEY` removed for the generator subprocess, or make an explicitly supplied `--key-file` take precedence over the environment. Add a seed orchestration test with a conflicting ambient key.

### WR-02: Documented topic retention overrides are not repeatable

**Classification:** WARNING

**File:** `deployments/compose/docker-compose.yml:53-66`; `scripts/check-stack.sh:26-38`

**Issue:** `EVENTS_RETENTION_MS`, `ALERTS_RETENTION_MS`, and `DLQ_RETENTION_MS` are exposed as overrides, but topic initialization uses `--create --if-not-exists`, so changing retention after the first startup has no effect. The acceptance script also hardcodes default values and fails when valid overrides are used. This contradicts the documented repeatable local stack behavior.

**Fix:** After idempotent creation, explicitly alter each topic's retention to the configured value. Load the same effective values in `check-stack.sh` instead of hardcoding defaults. Keep partition-count changes reset-gated as already documented.

### WR-03: Docker build context includes git-ignored private key material

**Classification:** WARNING

**File:** `.dockerignore:1-13`; `.gitignore:52-55`; `deployments/docker/Dockerfile:12`

**Issue:** Git ignores `*.pem`, but Docker does not. Any local PEM under `backend/` is sent in the build context and copied into the builder layer by `COPY backend/ ./`, even though it is absent from the final distroless image. That exposes private material to the Docker daemon and build cache.

**Fix:** Add credential patterns such as `**/*.pem`, `**/*.key`, and project-specific certificate directories to `.dockerignore`; use BuildKit secret mounts for any build-time secret that is genuinely required. Add a context hygiene check alongside the existing pin checks.

### WR-04: Banned-module CI scan misses integration-tag-only dependencies

**Classification:** WARNING

**File:** `.github/workflows/ci.yml:42-49`

**Issue:** The prohibited-module guard runs `go list -deps -test ./...` only for the default build. A banned package imported exclusively by `//go:build integration` code is compiled by later integration-tag vet/lint steps but is invisible to this policy check.

**Fix:** Scan both default and integration dependency graphs, or inspect the complete module graph with a policy appropriate for direct and transitive modules. Preserve the current fail-closed `go list` behavior for both invocations.

---

_Reviewed: 2026-10-16T08:30:20Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
