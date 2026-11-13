---
phase: 04-query-api-analyst-dashboard
plan: "15"
subsystem: acceptance
tags: [playwright, nginx, sse, tenant-matrix, two-replicas]
requires:
  - phase: 04-14
    provides: Reconnecting LISTEN links, bounded stream resources, durable replay
provides:
  - TestPhase4TenantMatrix (surfaces x 2 tenants x 3 roles, real PG/CH, two production query-api children)
  - Real-browser Playwright acceptance through a pinned nginx proxy and two query replicas
  - check-phase4-browser.sh wrapper (pipeline seeding, 10 minute budget, artifact credential scan)
affects: [04-16]
tech-stack:
  added: ["@playwright/test 1.64.1 (exact, lockfile integrity matches registry)", "nginx 1.30.6-alpine pinned by digest"]
  patterns: [additive Compose overlay, envsubst proxy template with causal negative control]
key-files:
  created:
    - backend/testphase4/tenant_matrix_test.go
    - web/e2e/phase4.spec.ts
    - web/playwright.config.ts
    - scripts/check-phase4-browser.sh
    - deployments/compose/phase4-acceptance.yml
    - deployments/proxy/phase4.conf
  modified: [web/package.json, web/package-lock.json, docs/demo.md]
requirements-targeted: [AUTH-01, AUTH-02, AUTH-03, AUTH-04, API-01, API-02, API-03, API-04, API-05, API-06, API-07, API-08, LIVE-01, LIVE-02, LIVE-03, UI-01, UI-02, UI-03, UI-04, UI-06]
completed: 2026-11-13
status: complete
---

# Phase 4 Plan 15: Tenant matrix and real-proxy browser acceptance Summary

**Two tenants investigate and triage actual pipeline-seeded detections in Chromium through a pinned nginx proxy in front of two query replicas, and live reconnect recovers the exact missed alert IDs through each replica. Incremental slice evidence, not whole-phase acceptance.**

## Commits

| Role | SHA | Content |
|---|---|---|
| RED (tests only) | `880f379` | tenant_matrix_test.go, phase4.spec.ts |
| GREEN | `a56074c` | proxy/overlay/wrapper/config, package.json, docs |

## Honest RED record

- Native `TestPhase4TenantMatrix` **passed on unchanged production** (exit 0, UTC 2026-10-30 ~16:53). It is a characterization test; there is no causal native RED. My earlier failing runs were wrong expectations in the test (summary/rules ignore query strings rather than 400; reopen is 400 not 409; fixture needed CH detections), not production defects. No mutation check was run.
- Causal RED: with the Playwright spec committed, `npm test` (`vitest run`) exited 1 because Vitest collected `e2e/phase4.spec.ts` (evidence `.local/phase4-acceptance/04-15/red/npm-test.txt`). The spec failure itself ended at a missing fixture env and the wrapper at exit 127 (absent script): both are fixture/absence failures, not behavioral RED.
- Evidence is private (git-ignored) under `.local/phase4-acceptance/04-15/` (browser-run1..15; run15 is the passing run).

## What was delivered

- `phase4-acceptance.yml`: additive overlay with `query-api-b` (no host port), pinned `nginx:1.30.6-alpine@sha256:0e52bd05...97949` (index digest verified through `docker buildx imagetools inspect` for both `stable-alpine` and `1.30.6-alpine`), and profile-gated `proxy-buffered`.
- `phase4.conf`: SSE not buffered, read timeout 45s (> 15s heartbeat), `X-Forwarded-For` overwritten, identity headers blanked, no CORS grant, per-request replica choice with fallback, `X-Phase4-Replica` acceptance header.
- Negative control: nginx `proxy_buffering on` + HTTP/1.0 upstream (verified experimentally to hold the `: connected` frame; plain `proxy_buffering on` over HTTP/1.1 chunked did NOT delay it, and gzip did not engage). The test uses a tenant with no alerts so a replay cannot fill nginx's buffer and mask buffering; real proxy gets the first frame inside 5s, control misses it.
- `npm test` is `vitest run src`; `test:e2e` is `playwright test`; the wrapper asserts `vitest list --filesOnly src` has no `e2e/` and that unfiltered discovery still sees the spec.
- Playwright 1.64.1 / Chrome headless shell 156.0.8078.4 (build 1248) already installed; no download was needed.

## Verification (all real, exit codes)

| Command | UTC | Exit |
|---|---|---|
| `bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4TenantMatrix$' -tags=phase4 -race -timeout=6m -p=1` (launcher env) | 2026-11-13 18:20:22-18:20:30 | 0 (12 subtests pass) |
| `(cd web && npm test)` | 18:20 | 0 (9 files, 170 tests, no e2e) |
| `bash scripts/check-phase4-browser.sh` via launcher (NEW project overlay) | 18:13:28-18:16:09 (run15, 162.7s) | 0 (13/13 Playwright) |
| Same families repeated: Stream, Triage, Alerts, Auth, Events, SummaryRules, Delivery (phase4, race, p1) | 18:21-18:23 | all 0 |
| `tsc --noEmit`, `eslint .`, `prettier --check .`, `go vet -tags=phase4`, `check-pins.sh images`, `test_stack_config.py`+`test_docker_context.py`, `check-docker-context.py` | 18:20-18:23 | all 0 |

Run15 happened at the working-tree content committed as `a56074c`; the gates after it ran at that HEAD. Wrapper command used `PHASE4_COMPOSE_CMD` = the launcher compose argv (project `sentinel-p4-demo-20261024`, private overlay, `compose.detector-group-r3.yaml`, acceptance overlay).

Browser coverage: both tenants x all four pages with exact alert-ID equality (8 real detections each: benign + four threat families through ingest->Kafka->detector->writers), foreign alert/event hidden, safe-JSON injection event, viewer 403 / foreign 404 / stale 409, resolve dialog focus/Escape, keyboard focus, Back/Forward, 1440/375/200% zoom (720px, DPR 2), reduced motion, loading/empty/error+retry, cursor/tenant-switch/logout/expiry, and reconnect: both replicas stopped, real detections committed while no stream exists, recovery via one replica then the other with exact IDs and `X-Phase4-Replica` proof. Screenshots were reviewed visually (375 alerts, 1440 recovered): no overflow or overlap observed.

## Deviations / Findings

1. **[Rule 3] Base Compose cannot start the current query-api.** Since 04-10 the binary needs the rules file (`SENTINEL_DETECTOR.RULES_PATH`); `deployments/compose/docker-compose.yml` wires neither, and the NEW stack's query-api image predated it. Fixed only in the owned overlay (mount + env for both replicas); the wrapper rebuilds and recreates `query-api` (never infra). The base file should be fixed in a later plan.
2. Spec/wrapper corrections made while going GREEN (documented in the commit): JWT max lifetime is 15 minutes (`exp - iat`), alert deep links pin a wide range because built-in window ends can be ahead of wall clock, modal focus may leave the document, state test needs a new filter value to avoid a cached query, build output needs readable permissions for the nginx user (artifacts stay 0700/0600).
3. **Detector clock constraint.** The detector clock is monotone event time and cooldown is event time. The seeder therefore builds at now-5s, never sends the scenario's future drain markers (they advance the clock past later runs; earlier attempts lost alerts as late events), and bursts wait out the previous alert's cooldown and window end. Two earlier seeding failures (random missing DENY_SPIKE/PORT_SCAN alerts) were this, not a detector bug; `detection_late_events_total` and `allowed=false` decisions confirmed it.
4. Observations, unfixed (Phase 4 semantics unchanged): `/metrics/summary` and `/rules` ignore query strings (tenant tampering has no effect; OpenAPI lists 400 for summary); the Alerts page shows "New alerts available" immediately on first load because the initial backfill is treated as arrivals; OpenAPI text still says resolved returns 409 though resolve works.

## Limits

- Native matrix has no causal RED and no mutation check. Hosted CI untouched. Single Chromium project only. Cold Go module cache needed warm-up (`go mod download` flaked on unrelated test-only modules; `go build` succeeded). Delivery is at-least-once; no exactly-once or performance claim. Consumer lag / System remain Phase 5. No STATE/ROADMAP updates (orchestrator-owned).

## Self-Check: PASSED

Created files exist; commits `880f379` and `a56074c` exist; only `.planning/config.json` and `.serena/` remain dirty and were never staged.
