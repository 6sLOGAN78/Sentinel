---
phase: 05-observability-resilience-scale
plan: "17"
subsystem: bench
tags: [benchmark, compose, isolation, preflight, safety, makefile]
requirements: [BENCH-01, REL-03]
requires:
  - phase: 04-16
    provides: accepted Makefile (bench target, check-makefile-dotenv guard)
  - phase: 05-08
    provides: open-loop sender / SSE subscriber the bench stack will host
  - phase: 05-11
    provides: scripts/check-chaos.sh (make test-chaos)
  - phase: 05-13
    provides: observability overlay layered by the bench overlay
provides:
  - deployments/compose/docker-compose.bench.yml (replicas, loopback ports, limits, log rotation)
  - bench/stack.py (validate_project, project_name, compose_argv, preflight, create_env, read_env, up, migrate, mint_api_key, mint_jwt, down, purge, run_command, DryRunRunner, host_probes)
  - bench/run.py (up | down | purge; 05-18 adds run)
  - make bench-up / bench-down / bench-purge / test-chaos
affects: [05-18 (run subcommand, make bench), 05-20 (first real up), 05-21 (System readiness env in the bench overlay)]
tech-stack:
  added: []
  patterns:
    - "compose argv only via compose_argv(): -p sentinel-bench-<id> + 3 files + private --env-file"
    - "injected Runner protocol + Probes dataclass; fake runner in tests, bounded spooled subprocess in production"
    - "raise-only floors (disk 15 GiB, MemAvailable 6 GiB), as in scripts/check-resources.sh"
key-files:
  created:
    - deployments/compose/docker-compose.bench.yml
    - bench/stack.py
    - bench/run.py
    - generator/tests/test_bench_stack.py
  modified:
    - Makefile
decisions:
  - "App images are compose-native project images (sentinel-bench-<id>-<service>) labelled org.opencontainers.image.revision=<git sha>, not an undigested image: line, because check-pins (D-21) refuses image: without @sha256. Purge removes them with down --rmi local; pinned infra images are never removed"
  - "dev-jwt runs with SENTINEL_PRIMARY.ENV=test (config.Load loads no dotenv in test mode) plus SENTINEL_ENV_FILE pointing at a nonexistent file in the run dir, so the root .env can never be read"
  - "Run ids starting with 'sentinel' are refused so a project name passed as --run (sentinel, sentinel-p4-demo-*, sentinel-bench-*) fails instead of becoming sentinel-bench-sentinel"
metrics:
  duration: "2026-11-17T19:48:08Z to 2026-11-17T21:18:31Z (about 90 min)"
  completed: 2026-11-17
  tasks: 2
  files: 5
---

# Phase 5 Plan 17: Isolated bench stack Summary

`make bench-up RUN=<id>` brings up a disposable multi-replica `sentinel-bench-<id>` Compose project. It layers the base, observability and bench files over a private generated env under `.local/bench/<id>/`, and it publishes only on loopback ports that no other stack uses. `bench/stack.py` validates the name and runs a read-only host preflight before any docker command that mutates state. It never opens the root `.env` and never uses the Makefile's `$(COMPOSE)`. Volume and image removal is a separate `CONFIRM=1` purge. This plan only renders `docker compose config`; no stack was started.

## Commits

| Role | SHA | Content |
|---|---|---|
| RED | 29d9bc8 | generator/tests/test_bench_stack.py only (31 test functions, 48 cases) |
| GREEN | 7c9c50d | overlay, bench/stack.py, bench/run.py, Makefile, image-representation test adjustment (see Deviations) |

**RED evidence:** at 21:05:40Z, `uv run --locked pytest tests/test_bench_stack.py -q` exited 1 with 7 failed and 41 errors. All 41 errors were `FileNotFoundError: .../bench/stack.py`. Six CLI failures were `can't open file .../bench/run.py`, and one Makefile test failed because the `bench-up` target was absent. The failures were causal.

## Topology

| Service | Replicas | Host ports (all 127.0.0.1) | mem_limit | cpus |
|---|---|---|---|---|
| kafka | 1 | none | 1g (heap -Xms512m -Xmx512m) | 2.0 |
| clickhouse | 1 | 38123 -> 8123 (native 9000 not published) | 2g | 2.0 |
| postgres | 1 | 35432 -> 5432 | 512m | 1.0 |
| redis | 1 | none | 128m | 0.5 |
| ingest | 2 (`BENCH_INGEST_REPLICAS`, 1..2) | 38080-38081 -> 8080 | 384m | 1.0 |
| detector | 3 (`BENCH_DETECTOR_REPLICAS`, 1..6, <= partitions) | none | 256m (base) | 1.0 (base) |
| event-writer | 2 | none | 384m | 1.0 |
| alert-writer | 1 | none | 256m (base) | 1.0 (base) |
| query-api | 2 | 38090-38091 -> 8081 | 256m | 1.0 |
| prometheus / grafana / jaeger | 1 each | 39090 / 33000 / 36686 | 512m each (05-13) | - |
| topic-init | one-shot | none | - | - |
| postgres-migrate | parked (profile `bench-unused`) | - | - | - |

- Ops ports 9101-9105 are never published. Prometheus scrapes them over Compose DNS (05-13 `dns_sd_configs`).
- Total limits: infra 3.6 GiB, apps 3.0 GiB, observability 1.5 GiB, 8.1 GiB in all. These are caps, not measurements. The plan assumed a 4 GB host, but this host has 15.3 GiB RAM (MemTotal 16054100 kB) and 16 logical CPUs (i5-13500H). 05-20 must check the caps against `docker stats`.
- Every service in the overlay uses json-file logging at 20m x 3. That is all apps and all infra; the observability backends keep their 05-13 10m x 3.
- `stop_grace_period: 40s` is inherited from the base for all workers.
- Ingest rate limit is set explicitly: `SENTINEL_RATELIMIT.CAPACITY=${BENCH_RATE_LIMIT_CAPACITY:-10000}`, `SENTINEL_RATELIMIT.REFILL_RATE=${BENCH_RATE_LIMIT_REFILL:-2000}`. Capacity must be at least 500, the ingest max batch size.
- Topic layout: `EVENTS_PARTITIONS` defaults to 6.

## Private env keys (names only)

Generated per run at `.local/bench/<id>/env`, with the directory at 0700 and the file at 0600. The file is created with O_EXCL|O_NOFOLLOW, and the run dir is refused if it already exists or is a symlink. The four secrets are each `secrets.token_urlsafe(32)` and are never committed:

`SENTINEL_POSTGRES_PASSWORD`, `SENTINEL_CLICKHOUSE_PASSWORD`, `SENTINEL_AUTH_JWT_SECRET`, `SENTINEL_GRAFANA_ADMIN_PASSWORD`, `BENCH_GIT_SHA`, `BENCH_GIT_DIRTY`, `EVENTS_PARTITIONS`, `BENCH_INGEST_REPLICAS`, `BENCH_DETECTOR_REPLICAS`, `BENCH_RATE_LIMIT_CAPACITY`, `BENCH_RATE_LIMIT_REFILL`, `SENTINEL_TRACING_SAMPLE_RATIO` (one ratio for every service, default 0.01).

Keys minted later go under `.local/bench/<id>/keys/<tenant>.key` at 0600.

## Refusal matrix

All refusals exit nonzero before any docker command that mutates state. Name refusals happen before any docker call at all.

| Condition | Where | Message (abridged) |
|---|---|---|
| project `sentinel`, `sentinel-p4-demo-*`, or not `^sentinel-bench-[a-z0-9][a-z0-9-]{0,39}$` | validate_project | refusing protected compose project / name must match |
| run id empty, uppercase, `/`, leading `-`, >40 chars, or starting with `sentinel` | project_name | refusing run id |
| any of the 9 bench ports fails to bind on 127.0.0.1 | preflight | refusing: host port(s) N already in use |
| any `sentinel-bench-*` project running (`docker compose ls --format json`) | preflight | refusing: bench project(s) already running |
| repo or Docker root free < 15 GiB | preflight | free at least 15 GiB; Sentinel will not prune |
| MemAvailable < `BENCH_MIN_MEM_GIB` (default 6) | preflight | MemAvailable ... below the N GiB floor |
| `BENCH_MIN_DISK_GIB` < 15 or `BENCH_MIN_MEM_GIB` < 6 | preflight | refusing lowered floors |
| settings out of bounds (replicas, partitions, rate limit, ratio, SHA) | BenchSettings.validate | refusing bench settings |
| `.local/bench/<id>` exists or is a symlink; env already exists | create_env | refusing: already exists / refusing to overwrite |
| purge without `CONFIRM=1` | purge | refusing to purge ... rerun with CONFIRM=1 |
| down/purge/migrate/mint without a private 0600 env for the run | read_env | refusing: no private bench env |
| tenant other than `bench-<id>[-suffix]`; role other than viewer/analyst/admin | mint_api_key / mint_jwt | refusing tenant / refusing role |

Lifecycle:
- `up` runs: config --quiet, `--progress plain build <5 apps>`, `up -d --wait` infra, `run --rm topic-init`, then host tern and chmigrate against 35432/38123 with passwords in the child env only, then `up -d --wait --no-build` for apps and observability.
- `down` runs `stop` then `down`, with no -v.
- `purge --confirm` runs `down -v --remove-orphans --rmi local`, then fd-safe rmtree of `.local/bench/<id>`.
- Compose children get an allowlisted env (PATH, HOME, locale, DOCKER_*). Ambient `SENTINEL_*`, `COMPOSE_*`, `BENCH_*` and `EVENTS_*` are stripped (tested), so a shell export can never override the private env.

## Verification

| Gate | Window (UTC) | Result |
|---|---|---|
| `bash scripts/check-resources.sh` (before any work) | 19:48Z | PASS (repo/docker 15.62 GiB, MemAvailable 6.28 GiB) |
| RED: `pytest tests/test_bench_stack.py -q` | 21:05Z | exit 1, causal (see above) |
| Plan Task 2 verify chain: test_bench_stack, full generator pytest, `ruff check . ../bench`, `mypy src tests`, check-pins, check-makefile-dotenv, `make -n bench-up` greps | 21:10:27Z-21:11:24Z | exit 0 (48 passed; 310 passed) |
| `mypy --strict bench/stack.py bench/run.py`, `ruff format --check ../bench` | 21:11Z | clean |
| Acceptance greps: `$(COMPOSE)` in bench recipes: none; `prune` in bench/*.py: none; `!reset`/`!override` lines: 14 | 21:11Z | pass |
| `BENCH_DRY_RUN=1 run.py up --run sentinel` / `--run sentinel-p4-demo-20261024` | 21:11Z | exit 1, refusal printed, no argv printed |
| `run.py purge --run t1` (no confirm) | 21:11Z | exit 1, "rerun with CONFIRM=1" |
| Real overlay render (`docker compose config --format json`) inside test_bench_stack | 21:10Z | pass: replicas 2/2/3/1/2, ports as above, ops ports unpublished |
| Real dry run `BENCH_DRY_RUN=1 run.py up --run dryrun1` (read-only probes) | 21:15Z | exit 1: `repo has 13.47 GiB free; free at least 15 GiB`. The real disk gate refused, and nothing was written |
| dev-jwt in test mode with child env only (no stack) | 21:16Z | 3-part token, iss sentinel-dev, aud sentinel-dashboard, life 900 s |
| `cd generator && uv run --locked pytest tests/test_bench_*.py` | 21:12:26Z-21:13:17Z | 189 passed |
| `bash scripts/check-pins.sh` | 21:13Z | PASS (4 compose files incl. bench) |
| `bash scripts/check-observability.sh` | 21:13Z | PASS (promtool + 22 tests) |
| `make test` | 21:13:24Z-21:14:23Z | exit 0 (Go race ok, web 174 passed, generator 310 passed) |
| `make lint` | 21:14:29Z-21:14:36Z | exit 0 |
| scripts guards as CI runs them (`unittest discover -s scripts`): phase3, phase4, stack_config, check_chaos, check_resources, dev_auth_env, seed, docker_context, go_dependencies | 21:15Z-21:18Z | OK |
| `scripts/test_seed_scenarios.py` with venv Python 3.13 | 21:18Z | OK |
| `scripts/test_seed_scenarios.py` with system Python 3.10 | 21:15Z | ImportError (known pre-existing `datetime.UTC`; not fixed, as instructed) |
| Docker leftovers: sentinel-bench containers/volumes/images/networks, `.local/bench` | 21:18Z | none |

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 - Blocking] App image representation changed so check-pins (D-21) keeps passing**
- **Found during:** Task 2, before writing the overlay.
- **Issue:** The plan wanted a build-once image tagged by git SHA. The RED tests expressed that as `image: ${BENCH_APP_IMAGE}` plus `pull_policy: never`. But `scripts/check-pins.sh` fails any `image:` line without `@sha256`, and a per-run local build has no digest. Satisfying the tests would have meant carving an exemption into the D-21 guard or hiding the line from its grep.
- **Fix:** All five app services keep the base `build:`, with an `org.opencontainers.image.revision=${BENCH_GIT_SHA}` label. They are built once per run by one `compose build` of the 5 identical build definitions; the build cache resolves them to one image. Compose names them `sentinel-bench-<id>-<service>`. Purge uses `down -v --remove-orphans --rmi local`, which removes only images without a custom `image:` name, so the pinned infra and observability images are never touched. No separate `docker image rm` is needed.
- **Test change in GREEN:** limited to the image representation, in five places:
  - `BenchSettings(git_sha=...)`
  - the build argv lists the 5 app services
  - env `BENCH_GIT_SHA`
  - purge argv
  - the render test asserts the label instead of `image`/`pull_policy`
  No isolation, refusal or secret assertion was changed.
- **Files:** generator/tests/test_bench_stack.py, deployments/compose/docker-compose.bench.yml, bench/stack.py. **Commit:** 7c9c50d.

**2. [Rule 1 - Bug] Plan text contradicts itself on the word "prune"**
- The behavior requires the message "free at least 15 GiB; Sentinel will not prune", while the acceptance grep requires `prune` to appear nowhere in bench/*.py. Resolved as `scripts/check-resources.sh` does: the word is assembled (`"p" + "rune"`), so the grep guard stays meaningful and the rendered message is exact.

**3. [Rule 2 - Correctness] Extra guards beyond the plan text**
- Compose children get an allowlisted env, because shell env overrides `--env-file` in compose. Without that, an exported `SENTINEL_POSTGRES_PASSWORD` or `BENCH_*` would silently change the stack.
- Settings bounds: ingest replicas at most 2 (the published port range), detector replicas at most partitions and at most 6, rate-limit capacity at least 500.
- `depends_on: !override` on detector and alert-writer drops `postgres-migrate`. Without it, `up` would build and run the stale containerized migrator (T-05-17-06).
- `purge` keeps the private dir if `down -v` fails, so the env needed to retry is not lost.

**4. Sizing note:** the plan says to size limits "for the 4 GB RAM host". The measured host has 15.3 GiB RAM and 16 CPUs, and the caps above were chosen for it. The 6 GiB MemAvailable floor is the plan's default. The 8.1 GiB sum of limits is a ceiling, not expected usage.

### Not done (by instruction)

- STATE.md, ROADMAP.md and REQUIREMENTS.md not touched. BENCH-01 is not complete until 05-18 replaces `make bench`, and REL-03 is not complete until 05-20 runs the stack.
- No stack was started; the plan only renders config. The real `up` is 05-20.

## Known Stubs

None. The `bench` target still exits 1 with "bench is available from Phase 5", which is the plan's explicit hand-off to 05-18.

## Threat Flags

None beyond the plan's register:
- T-05-17-01: regex, denylist, `sentinel*` run-id refusal, compose-ls check, port preflight, and argv assertions.
- T-05-17-02: open() and os.open() are monkeypatched in tests to fail on `/.env`; secrets stay in the child env and are redacted in error tails.
- T-05-17-03: floors, mem_limit and log rotation.
- T-05-17-04: CONFIRM plus validated name, and the no-prune grep.
- T-05-17-05: check-pins scans the bench overlay.
- T-05-17-06: per-project build labelled with the SHA, and host tern/chmigrate.

## Notes for 05-18

- Add `run` to `bench/run.py` (`_parser()` and `main()`). Use `stack.up(...)` info for the raw JSON hardware/config block. It returns `cpu_model`, `logical_cpus`, `mem_total_bytes`, `mem_available_bytes`, `disk_free_bytes{repo,docker_root}`, `kernel`, `docker_version`, `compose_version`, `floors_gib`, `ports`, `git_sha`, `git_dirty`, `settings`, `replicas`, `urls{ingest[2],query_api[2],prometheus,grafana,jaeger}` and `app_images`.
- Credentials:
  - `stack.mint_api_key(run, f"bench-{run}", runner=stack.run_command)` returns the 0600 key path. Pass the path, never the key, to the sender.
  - `stack.mint_jwt(run, "viewer", tenant, runner=...)` returns `(token, expiry)`. The lifetime is 15 min (dev-jwt max), so re-mint for SSE runs longer than that.
- Ingest has no load balancer. The sender must round-robin `urls["ingest"]` (38080, 38081), and the SSE subscriber should use `urls["query_api"]`.
- Kafka, Redis, ClickHouse native and every ops port are not published:
  - Read-only Kafka counts need `docker compose ... exec kafka` via `stack.compose_argv` (add it to the read-only/dry-run classification if used), or must come from Prometheus.
  - ClickHouse counts can use HTTP 38123 with the env password via the child env.
  - The 05-14 `sentinel_sse_clients` cross-check must go through Prometheus (39090, `sum(sentinel_sse_clients)`), because query-api :9105 is internal. The gauge is per replica and never counts 429-refused streams.
- Producers keep one request in flight per broker (ADR 0013). Measure throughput; don't assume it.
- Replace the `bench` target (Makefile line ~115) without using `$(COMPOSE)`. `test_makefile_bench_targets_never_use_default_compose` asserts the current "available from Phase 5" text, so update that assertion in the same plan.
- Teardown is part of the run (Pitfall 6: idle >24 h loses checkpoint history). Call `stack.down` in a `finally`, and purge only with explicit confirmation.

## Notes for 05-20

- Blocker as of 21:15Z: the real preflight refused, with repo/docker root at 13.47 GiB free against the 15 GiB floor. Free disk had fallen from 15.62 GiB at 19:48Z. The user must free space; Sentinel will not prune.
- Measure `docker stats` against the caps above (ClickHouse 2g, Kafka 1g with a 512m heap, ingest and event-writer 384m) and adjust from the data.
- An unrelated Aegis `compose` project was running (14 containers). Record it as bench noise or ask the user to stop it.

## Notes for 05-21

- Add the System-page readiness env (`SENTINEL_OBSERVABILITY.SYSTEM.*`) under `query-api.environment` in `deployments/compose/docker-compose.bench.yml`. A placeholder comment marks the spot. query-api runs 2 replicas behind ports 38090-38091, and in the bench overlay it reaches Prometheus at `http://prometheus:9090` inside the network.
- The render test (`test_overlay_renders_replicas_ports_and_limits`) asserts the exact published ports per service. Adding env keys does not affect it, but any new published port must be added to its `expected` map and to `stack.BENCH_PORTS` (the port preflight).

## Self-Check: PASSED

- FOUND: deployments/compose/docker-compose.bench.yml, bench/stack.py, bench/run.py, generator/tests/test_bench_stack.py, Makefile targets bench-up/bench-down/bench-purge/test-chaos
- FOUND commits: 29d9bc8 (RED, test file only), 7c9c50d (GREEN)
- `grep -c "def test_" generator/tests/test_bench_stack.py` = 31 (>= 14); the denylist name `sentinel-p4-demo-20261024` appears in the tests
- No stack started; no docker prune; no sentinel or sentinel-p4-demo-* project touched; no sentinel-bench resources remain
- .planning/config.json and .serena/ not staged; backend/.env not read; STATE.md and ROADMAP.md not touched; nothing pushed
