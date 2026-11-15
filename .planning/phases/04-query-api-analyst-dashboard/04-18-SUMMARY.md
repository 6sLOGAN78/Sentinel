---
phase: 04-query-api-analyst-dashboard
plan: "18"
subsystem: acceptance
tags: [gap-closure, G1, phase4-harness, ci, playwright-seeder]
gap_closure: true
gaps_closed: [G1]
requires:
  - phase: 04-16
    provides: closed eight-family runner, phase4 CI jobs, test_phase4_guards.py
provides:
  - backend/testphase4/harness_env_test.go (phase4ConfigFrom/phase4Config helper, TestHarnessEnvBootstrap)
  - scripts/phase4_pg_env.py (keyctl_env)
  - go-job CI step "phase4 harness bootstraps from a scrubbed environment"
  - 11 new guard tests (15 -> 26) in scripts/test_phase4_guards.py
affects: [04-20 (e2e spec follow-up), 04-21, 04-23, 04-24]
tech-stack:
  added: []
  patterns: [one shared env bootstrap helper per test package, derive child env from repo .env instead of launcher env]
key-files:
  created: [backend/testphase4/harness_env_test.go, scripts/phase4_pg_env.py, .planning/phases/04-query-api-analyst-dashboard/deferred-items.md]
  modified: [backend/testphase4/auth_test.go, backend/testphase4/alerts_test.go, backend/testphase4/events_test.go, backend/testphase4/triage_test.go, backend/testphase4/stream_test.go, backend/testphase4/tenant_matrix_test.go, backend/testphase4/delivery_test.go, backend/testphase4/summary_rules_test.go, scripts/check-phase4-browser.sh, scripts/test_phase4_guards.py, .github/workflows/ci.yml]
key-decisions:
  - "startQueryAPI pins SENTINEL_OBSERVABILITY.LOGGING.FORMAT=json for its child (the repo .env opts into console logs; since 05-06 the logger honors that)"
  - "Seeder compose subprocesses pin cwd=ROOT so repo-relative PHASE4_COMPOSE_CMD paths work when Playwright runs burst from web/"
  - "e2e spec:730 conflict with 04-20 deferred to a G4 follow-up (D-04-18-01), not edited here"
requirements-targeted: [AUTH-03, API-05, API-06, API-07, LIVE-01, LIVE-02, LIVE-03, UI-02, UI-06]
metrics:
  started: 2026-11-14T23:30Z
  completed: 2026-11-15T00:20Z
  tasks: 3
status: G1 closed locally for all eight native families. Browser acceptance is 13/13 only with the 04-20 web change isolated; at HEAD it is 12/13 because of D-04-18-01.
completed: 2026-11-15
---

# Phase 4 Plan 18: Launcher-independent Phase 4 harness (G1) Summary

**All eight TestPhase4\* families now get their config from one `phase4Config` helper. The browser seeder derives keyctl's PG\* env from the repo `.env` and runs compose from the repo root. Both regressions are guarded. All eight families pass under `env -i` on an isolated stack with no launcher env. The browser acceptance is 13/13 launcher-free once the stale e2e step from 04-20 is isolated. No hosted-CI claim is made; 04-24 owns that.**

## Commits

| Role | SHA | Message |
|---|---|---|
| T1 RED | 80ebb28 | test(04-18): add failing scrubbed-env harness bootstrap test |
| T1 GREEN | 5f5fbed | feat(04-18): shared phase4Config harness helper for all Phase 4 families |
| T2 RED | 70fca62 | test(04-18): add failing launcher-independence guards |
| T2 GREEN | f34ff17 | feat(04-18): browser seeder derives keyctl PG env from the repo .env |
| T3 fix RED | ece0d25 | test(04-18): add failing guard that startQueryAPI pins JSON child logs |
| T3 fix GREEN | ac2118e | fix(04-18): startQueryAPI pins structured JSON logs for its child |
| T3 fix RED | 041c793 | test(04-18): add failing guard that seeder compose calls pin cwd=ROOT |
| T3 fix GREEN | eb01c1d | fix(04-18): seeder compose calls run from the repo root |

## Task 1: helper, scrubbed-env bootstrap, family migration

- `phase4ConfigFrom(t, envFile)` sets `SENTINEL_PRIMARY.ENV=local` and `SENTINEL_ENV_FILE`, requires the file to exist ("run make dev-up first"), and returns `config.Load(config.ServiceQueryAPI)`. `phase4Config(t, root)` uses `<root>/.env`.
- All eight families call `phase4Config(t, root)` once, after the JWT `t.Setenv` and before any `config.Load`, `database.Migrate` or `startQueryAPI`. In stream it comes after the `WRITE_TIMEOUT` setenv. The manual setenv pairs in auth, alerts and events were removed, and so were the now-unused `config` imports in auth, events, tenant and triage. In summary_rules, `_ = phase4Config(t, root)` comes right before the first `startQueryAPI`; the later in-subtest `config.Load` calls are kept. No assertions changed.
- How `TestHarnessEnvBootstrap` works:
  1. Negative control: with `PRIMARY.ENV` unset, `config.Load` must fail with `Primary.Env`.
  2. It runs the helper on a temp copy of `.env.example`.
  3. It asserts DB host/port/name, the CH native address and the Kafka brokers equal `godotenv.Read` of that file.
  4. It asserts `SENTINEL_DATABASE.HOST` is now in the process env, which is what a child inherits.
- Mutation check: commenting out the helper's two `t.Setenv` lines makes the test FAIL (`Primary.Env ... required`). Reverted.
- CI: one step added to the go job, after `go test -race ./...`. It runs the exact command from the plan.

Commands (backend/):
- `env -i PATH HOME GOCACHE GOMODCACHE GOPATH ../scripts/go-test-nonempty.sh './testphase4/...' '^TestHarnessEnvBootstrap$' -tags=phase4 -race -timeout 3m -p 1`: exit 1 at RED (undefined phase4ConfigFrom) and exit 0 at GREEN. Re-run at HEAD eb01c1d: exit 0.
- `go vet -tags=phase4 ./testphase4/` exit 0; `gofmt -l testphase4` was empty; the helper-use grep check passed (0 families missing).
- `golangci-lint run --build-tags=phase4 ./testphase4/...` reports issues only in tenant_matrix_test.go (lll, govet, musttag, perfsprint, nolintlint). The same set exists at d2fdce9, before this plan, so they are pre-existing. CI does not lint the phase4 tag. Out of scope.

## Task 2: seeder PG env and guards

- `scripts/phase4_pg_env.py` uses only the stdlib. `keyctl_env(env_file, base)` maps `SENTINEL_DATABASE.HOST/PORT/USER/PASSWORD/NAME` to PG\*, and `SSL_MODE` to `PGSSLMODE` when it is present.
  - `SENTINEL_DATABASE.*` values in base win over the file.
  - Missing or empty values, and ports outside 1..65535, raise `ValueError`. The message names the key, never the value.
  - It does not mutate base and prints nothing.
  - ruff check, ruff format --check and mypy --strict (generator venv) are clean.
- Seeder: `sys.path.insert(0, str(ROOT / "scripts"))` and `from phase4_pg_env import keyctl_env`. `ensure_key` passes `env=keyctl_env(ROOT / ".env", os.environ)`. `grep -n "env=keyctl_env"` returns exactly 1 line. The header documents the derivation.
- New guard class `Phase4HarnessEnvGuards`:
  - family helper-order guard, with mutations: helper dropped, startQueryAPI first, config.Load first
  - seeder keyctl env guard, with mutations: env dropped, import dropped
  - five keyctl_env unit tests: mapping and copy, quotes and precedence, rejection without leaking the password, scrubbed `os.environ`, and `.env.example`
  - Task 3 added a JSON-log pin guard and a compose cwd guard (with its mutation)
- `python3 -B -m unittest discover -s scripts -p test_phase4_guards.py`: exit 1 at RED (5 errors, 3 failures, all seeder/module). At HEAD: **26 tests OK**. The previous 15 are retained and 11 are new.
- `bash -n scripts/check-phase4-browser.sh` exit 0. The `env -i` keyctl_env check against `.env.example` resolved all six PG\* keys.

## Task 3: launcher-free local proof (isolated project sentinel-g1check)

Preflight:
- 10.68 GiB free.
- Published host ports from the compose and acceptance overlay with the root .env, plus the two proxy ports: 15432 16379 18090 18091 18123 19000 29092 8080 8081 9101-9105. All were free.
- sentinel-p4-demo-20261024 uses 18081/19105, so there was no conflict.
- HEAD at start: a826a2e (concurrent plans had committed).

Every command ran under `env -i PATH HOME DOCKER_CONFIG COMPOSE_PROJECT_NAME=sentinel-g1check GOCACHE GOMODCACHE GOPATH`. No SENTINEL\_\*, PG\* or P4\_\* variable was passed, and the launcher was not sourced. The JWT key came from `scripts/dev_auth_env.py`.

Evidence is in `.local/phase4-acceptance/04-18/` (mode 0700): `summary.txt` plus per-step logs. It holds exit codes, durations and test names only, with no compose config or env dump. Each browser artifact dir passed the script's own credential scan.

| # | Step | Start-end (UTC) | Exit |
|---|---|---|---|
| 1 | `timeout 16m make dev-up` | 23:55:16-23:57:53 (157s) | 0 |
| 2 | `bash scripts/check-stack.sh` | 23:57:53-23:58:07 | 0 |
| 3 | `bash scripts/check-phase4-tests.sh all` | 23:58:07-23:58:29 | **1**: TestPhase4Auth "missing actual structured security_audit event", the same failure as hosted run 43649750865 |
| 5 | auth after fix ac2118e | 23:59-00:00 | 0 (TestPhase4Auth PASS, 17s) |
| 6 | `bash scripts/check-phase4-tests.sh all` | 00:00:39-00:02:25 (106s) | **0**: Auth 17s, Alerts 6s, Events 6s, Triage 11s, SummaryRules 7s, Delivery 14s, Stream 38s, TenantMatrix 7s; all PASS and nonempty |
| 7 | `check-phase4-browser.sh` (PHASE4_COMPOSE_CMD `-p sentinel-g1check`, relative -f paths) | 00:02:25-00:03:44 | 1: seeding OK (8+8 detections, so keyctl worked with no launcher PG env); test 13 fails at spec:730 |
| 8 | the same from a temp worktree at ac2118e with the four 04-20 `web/src` files at bbd8d35^ | 00:04:54-00:06:02 | 1: test 13 passes the Refresh step but `burst` fails with CalledProcessError (relative compose paths with cwd=web/) |
| 9 | the same as 8 plus fix eb01c1d | ~00:07-00:08:27 | **0: 13/13 passed** |
| 10 | `check-phase4-browser.sh` in the main tree at HEAD eb01c1d | ~00:09-00:11 | 1: 12/13; test 13 still fails at spec:730 (D-04-18-01) |
| 11 | `make reset CONFIRM=1` (sentinel-g1check) | ~00:12 | 0 |

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 - Bug] The child query-api inherited console logging from the repo .env**
- **Found during:** Task 3, step 3.
- **Issue:** after the helper loads `.env`, the child inherits `SENTINEL_OBSERVABILITY.LOGGING.FORMAT=console`. Since 05-06 (55771a6) the logger honors this even under `PRIMARY.ENV=test`, so the auth audit assertions found no JSON line. Hosted auth failed in exactly this way.
- **Fix:** `startQueryAPI` overrides `SENTINEL_OBSERVABILITY.LOGGING.FORMAT=json` for its child, next to `PRIMARY.ENV=test`. A guard enforces it. No assertions were changed.
- **Files:** backend/testphase4/auth_test.go, scripts/test_phase4_guards.py. **Commits:** ece0d25 (RED), ac2118e (GREEN).

**2. [Rule 1 - Bug] Seeder compose calls depended on the launcher's absolute paths**
- **Found during:** Task 3, step 8.
- **Issue:** `pg_alerts` and the two `burst` SQL queries ran `PHASE4_COMPOSE_CMD` without a cwd. With the CI-default repo-relative `-f` paths, `burst` (spawned by Playwright from `web/`) fails. The launcher's absolute paths had masked this.
- **Fix:** `cwd=ROOT` on all three compose `subprocess.run` calls, with a guard and a mutation case.
- **Files:** scripts/check-phase4-browser.sh, scripts/test_phase4_guards.py. **Commits:** 041c793 (RED), eb01c1d (GREEN).

**3. Orchestrator override:** the plan says to leave sentinel-g1check running for 04-21, but the orchestrator told me to reset it. It was reset with `make reset CONFIRM=1` under the same project name. No sentinel-g1check volumes remain, and all root .env ports are free again.

## Deferred Issues

- **D-04-18-01** (in `deferred-items.md`): `web/e2e/phase4.spec.ts:730` clicks "Refresh alerts" to acknowledge an initial-catch-up banner that 04-20 (G4, a59f365) deliberately removed. Because of this, phase4-browser is 12/13 at HEAD. The isolation run proves it is the only remaining blocker (13/13 without the 04-20 change). Not edited here because it is outside this plan's files and must not be resolved by relaxing a selector. The suggested G4 follow-up is to assert that the banner is absent instead. **04-24 will see phase4-browser red until it is fixed.**

## Limits / not run

- No hosted CI (04-24).
- Steps 8-9 ran the browser from a temporary worktree (now removed). Its private dev JWT key differed from the root one, so step 10 recreated query-api with the root key, and the project was then reset.
- The project state checks (`docker compose ls`) show `sentinel` (exited 5) and `sentinel-p4-demo-20261024` (exited 4, restarting 5) unchanged from preflight.

## Threat Flags

None. The password only travels in the env= mapping, the error messages are key-only (tested), and the evidence contains no secrets.

## Self-Check: PASSED

- Files exist: harness_env_test.go, phase4_pg_env.py, deferred-items.md, this SUMMARY.
- Commits exist: 80ebb28 5f5fbed 70fca62 f34ff17 ece0d25 ac2118e 041c793 eb01c1d.
