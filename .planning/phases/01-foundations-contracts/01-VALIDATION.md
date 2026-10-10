---
phase: 1
slug: foundations-contracts
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-10
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `01-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` + testify + jsonschema/v6 + kin-openapi (integration tests under `//go:build integration`); Vitest 5 + ESLint 10 + `tsc` for `web/`; pytest 9 + ruff + mypy for `generator/` |
| **Config file** | none yet. Wave 0 creates `web/vite.config.ts`, `web/eslint.config.js`, `generator/pyproject.toml`, `backend/test/` harness |
| **Quick run command** | `cd backend && go test ./internal/... -short` |
| **Full suite command** | `make lint test && make test-integ` |
| **Estimated runtime** | ~30 s quick, ~180 s full (includes Compose/container start) |

---

## Sampling Rate

- **After every task commit:** Run `cd backend && go test ./internal/... -short` plus that task's own automated command
- **After every plan wave:** Run `make lint test`
- **Before `/gsd:verify-work`:** From a clean checkout, `make dev-up` twice, `make migrate` twice, `make test`, `make test-integ` and `make lint` all green
- **Max feedback latency:** 60 seconds for the quick loop

---

## Per-Task Verification Map

Task IDs are bound by the planner. Rows are keyed by requirement and behavior.

| Requirement | Behavior | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|------------|-----------------|-----------|-------------------|-------------|--------|
| FND-01 | All four services healthy, topics 6/3/1 with retention, auto-create off, idempotent re-run | — | Infra ports bound for local dev only | smoke | `make dev-up && make dev-up && scripts/check-stack.sh` | ❌ W0 | ⬜ pending |
| FND-01 | Host Kafka listener reachable | — | N/A | smoke | `bash -c '</dev/tcp/127.0.0.1/29092'` | ❌ W0 | ⬜ pending |
| FND-02 | `migrate` twice is a no-op; `reset` refuses without `CONFIRM=1` | — | Destructive op requires explicit confirmation | integration | `make migrate && make migrate`; `! make reset`; `make reset CONFIRM=1` | ❌ W0 | ⬜ pending |
| FND-02 | `seed`/`bench` placeholders exit non-zero | — | No fake success (spec §18) | script | `! make seed && ! make bench` | ❌ W0 | ⬜ pending |
| FND-03 | Five binaries build; no banned imports | — | Unused third-party SaaS clients removed | build+grep | `cd backend && go build ./cmd/... && ! go list -deps ./... \| grep -E 'newrelic\|clerk\|pkg/errors\|hibiken/asynq\|resend'` | ❌ W0 | ⬜ pending |
| FND-03 | Each binary starts, exits 0 on SIGTERM; fails fast with Redis down | — | Fail-fast on missing dependency | integration | `cd backend && go test -tags=integration ./test -run TestBinaryLifecycle` | ❌ W0 | ⬜ pending |
| FND-03 | Defects fixed: recover first, Shutdown nil-guard + Redis close, Redis ping fatal, DSN escaping, godotenv gated to local, resource close on error | — | DSN escaping prevents credential parsing bugs; dotenv not loaded outside local | unit | `cd backend && go test ./internal/... -run 'Recover\|Shutdown\|Redis\|DSN\|Dotenv\|Close'` | ❌ W0 | ⬜ pending |
| FND-04 | Schema and Go struct accept/reject identical fixtures (IPv4/IPv6, skew boundaries, unknown fields) | — | Strict input validation, unknown root fields rejected | contract | `cd backend && go test ./internal/contracts/... -run 'Schema\|Fixture\|Skew'` | ❌ W0 | ⬜ pending |
| FND-04 | DedupeKey golden vectors plus injectivity fuzz | — | No key collision via `\|` injection | unit+fuzz | `cd backend && go test ./internal/contracts -run DedupeKey && go test ./internal/contracts -fuzz=FuzzDedupeKey -fuzztime=10s` | ❌ W0 | ⬜ pending |
| FND-05 | OpenAPI valid 3.1; all spec §8 routes, status codes, envelope and pagination present; no drift vs JSON Schemas; TS client compiles | — | Security schemes declared (API key, bearer) | contract | `cd backend && go test ./test/... -run OpenAPI`; `cd web && npx openapi-typescript ../api/openapi.yaml -o src/api/schema.d.ts && npm run typecheck` | ❌ W0 | ⬜ pending |
| FND-06 | Images digest-pinned, lockfiles present, `.env.example` placeholders only, no secrets in history | — | No committed secrets | script+scan | `scripts/check-pins.sh && gitleaks git --redact` | ❌ W0 | ⬜ pending |
| FND-07 | Six ADRs exist with Status/Context/Decision/Consequences | — | N/A | script | `scripts/check-adrs.sh` | ❌ W0 | ⬜ pending |
| FND-03/06 | CI workflow has no `\|\| true` | — | Failures are never masked | script | `! grep -n '\|\| true' .github/workflows/*.yml` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `scripts/check-stack.sh`, `scripts/check-pins.sh`, `scripts/check-adrs.sh`
- [ ] `backend/internal/contracts/*_test.go` plus `testdata/` valid/invalid fixtures
- [ ] `backend/test/` build-tagged integration harness (container helpers for postgres/redis/clickhouse/kafka)
- [ ] OpenAPI validation test with expected-route table
- [ ] `web/` Vitest + jsdom setup; `generator/tests/test_events.py`
- [ ] Framework installs: `npm ci` (web), `uv sync --locked` (generator), `GOTOOLCHAIN=go1.26.9`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| CI skeleton green on push | FND-02, FND-05 | Requires a GitHub remote and Actions run | Push a branch and confirm every job in `.github/workflows/ci.yml` passes |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
