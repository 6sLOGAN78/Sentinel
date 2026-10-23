# Separate demo recovery — resource admission hold

Status: **HOLD before runtime mutation**. The approved separate demo has not
started. Phase4 plan07 remains pending acceptance; this report supplies no
all-nine-health, seed, detection, browser or full-phase acceptance claim.

Source inspected: `4f7d0028a88aced574ae17acc51f5ae31c332138`.
The complete STACK-RECOVERY-PLAN and independent STACK-RECOVERY-CHECK were read
before runtime work. Their binding start floor is **4 GiB = 4,294,967,296 bytes**.
The first disk observation had 4,052,328,448 bytes available. A separately
recorded read-only admission probe found **4,045,037,568 bytes** available,
249,929,728 bytes below that floor. Startup was refused.

## Recorded preflight

Private evidence directory:
`.local/phase4-acceptance/04-07/fresh-stack/` (0700; files0600).
`preflight.json` records exact argv, source SHA, working changes, UTC start/end,
monotonic elapsed and actual exits for every completed probe. No credential
was read, printed or written.

| Probe | Actual exit | Observation |
|---|---:|---|
| `git rev-parse HEAD` | 0 | Exact source SHA above |
| `git status --short` | 0 | Pre-existing modified `.planning/config.json`, untracked `.serena/` |
| `df -B1 /home/logan78/Desktop/Sentinel` | 0 | 4,045,037,568 bytes available |
| `free -b` | 0 | 7,453,655,040 bytes available RAM |
| `docker compose version` | 0 | v5.6.1; version lookup only |
| Private admission harness | **2** | `start_admitted=false` |

Recorded interval: 2026-10-23T22:50:20.730427Z to
2026-10-23T22:50:20.785209Z; monotonic elapsed0.054760s.
The initial private evidence harness exited1 because host Python lacks
`datetime.UTC`; it failed before any probe. That distinct failure is retained
in `failed-attempt-01.txt`; its timing was not captured. Only the private
harness was corrected for Python compatibility and repository ancestry.

## Runtime and preservation limits

No Compose config resolution, collision/port admission, snapshot, image build,
pull, container/network/volume creation, startup, migration, key provisioning,
ingestion, native store write or browser server launch was attempted.
No cleanup, pruning, deletion, reset, offset/group/fence change, restart,
teardown or old-runtime command occurred. The version lookup does not select
the existing project. Therefore no before/after Docker identity comparison
is claimed: the executor performed zero runtime mutations.

Existing containers, volumes, root `.env`, auth keys, source, config, Makefile,
Compose, Dockerfile, scripts, user planning config and `.serena/` were not
modified. No runtime resources were created that require stopping or removal.
The old missing Kafka range remains unresolved; this is not restoration or
proof that the old detector processed it. Its earlier fail-closed condition
remains documented in STACK-HOLD and was not re-probed here.

## Resume context and remaining gates

Repeat the read-only admission check with:

```sh
python3 -B .local/phase4-acceptance/04-07/fresh-stack/preflight.py
```

This command checks resources only and exits2 below the4GiB floor. It creates
no stack and grants no authority to clean up shared data. Its private JSON
records the latest run; preserve prior evidence separately before repeating.

No private launch context or overlay is provisioned yet. Future execution must
re-read the binding contract, require sufficient space, snapshot old identities
and protected file hashes before any mutation, then prove effective isolation
and repeat project/port collision guards. Use exact project
`sentinel-p4-demo-20261024`, absolute base Compose first, private override second,
and root env-file explicitly. Required NEW loopback ports remain Kafka39092,
CH28123/29000, PG25432, Redis26379, ingest18080, query18081,
ops19101..19105; optional Vite15173 must proxy18081. These are intended ports,
not observed listeners. Do not use default Make/seed/check-stack flows.

All startup, actual migrations/init, nine healthy services, topic/native/ops
checks, exact1000 current-run IDs with tenant exclusion, controlled real
detection through CH/PG/API, current07 browser workflow, preservation comparison
and repeatable NEW child credential/native context remain unperformed.
Root owns tracking/acceptance. No plan08, Phase5, publication or hostedCI work
was started. The binding contract requires returning a resource hold; no
prune/delete authority is inferred.

## Validation

Pinned gitleaks8.30.1, unchanged `.gitleaks.toml` and `.gitleaksignore`, redacted:

| Scope | Exit | Monotonic elapsed | Result |
|---|---:|---:|---|
| Private harness/evidence directory | 0 | 0.346727s | No leaks |
| New recovery report | 0 | 0.340626s | No leaks |
| Full existing git history, `--log-opts=--all` | 0 | 0.772688s | 572 commits, no leaks |

Each exact invocation and UTC interval is retained in private `*-scan.json`,
with redacted JSON reports in `*-report.json`. No credentials were provisioned
or forced into the scan. These scans do not supply absent runtime evidence.
The report is the sole owned tracked change; the private files are ignored.

Self-check: private evidence and report exist, directory/file modes checked,
probe exits inspected, and no source/config/scanner changes were made by this
executor. STATE/ROADMAP/requirements were left to root as explicitly assigned.
