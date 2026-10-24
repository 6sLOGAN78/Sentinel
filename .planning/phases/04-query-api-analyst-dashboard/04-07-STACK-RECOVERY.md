# Separate demo recovery — actual isolated runtime evidence

Current status: **bounded recovery runtime checks and independent root
verification PASS;07accepted on2026-10-24**. The original disk hold below remains
historical. The separate stack is running; the missing old Kafka history has
not been restored. No fullPhase4, performance, Phase5 or publication claim.

## Current source, isolation and resources

Root independently verified sourcebb8fa1e on2026-10-24. Private04-07 logs:
`root-fresh-data-verification` EXIT0/1.923s ending00:31:48Z verifies the existing
exact1000-ID manifest and controlled canonical detections without resending;
`root-fresh-native-events` EXIT0/8.379s ending00:31:55Z executes the nonempty
Events family with phase4/race/6m against NEW ports. `root-fresh-health-verification`
EXIT0/15.128s ending00:36:00Z repeats all-nine health, actual bindings, topic
settings/absence, init/migration exits, five readiness and native probes.
`root-fresh-postwave` EXIT0/15.082s ending00:36:00Z runs backend/web builds and
normal make test (cached Go,114web,121generator). Source files remain unchanged
from reviewedcc22566; the generated schema retains its recorded SHA256.

`root-fresh-preservation-v2` EXIT0/0.926s ending00:36:47Z independently compares
old container IDs/images/mounts/ports/networks/environment hashes, old volumes,
non-new networks and protected file hashes against before.json. It inspects
ALL containers and the pinned image declaration to prove the exact five new
anonymous volumes are mounted exclusively by owned new Kafka/init containers.
The first root preservation harness attempt EXIT1 used full inspect IDs against
the manifest's12-character IDs. No preservation comparison failed; the harness
was corrected to resolve the recorded IDs through Docker inspect, then rerun.
The failed attempt remains preserved. No runtime repair or data change followed.

Acceptance authorizes continuing04-08 using the private NEW context below;
default runtime commands still point at the old held stack and must not be used
for new native/migration acceptance. No old-history restoration, wholePhase4
completion or remote authority is inferred.

Execution base `c2ba3993af424ac8f10727d27b1f8dd330cbb8b3`; unchanged backend source
commit `242b162abdb0f16708743bfbde016439954807fa`. No source/test/build/default
configuration/scanner rules or exceptions were edited. The only tracked
executor output is this report; user `.planning/config.json` and `.serena/`
remain unstaged and their captured hashes are unchanged.

Exact new Compose project: `sentinel-p4-demo-20261024`. Private run directory:
`.local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/` (0700; evidence,
code, override and credentials0600). Original hold evidence was not overwritten.
New evidence names contain UTC time/run identifiers and each subprocess record
contains exact argv/cwd/sourceSHA/workingchanges/start/end/monotonicelapsed/exit.
Credentials and resolved Compose configuration are suppressed, never printed.

Read-only preflight at23:16:53ZOct23 recorded7,713,529,856free bytes, above4GiB.
Repeated project/volume/network collision and port-free checks passed before
startup. Quiet Compose validation passed; effective JSON was checked only in
memory. It proves the exact project/default network, three fresh project-scoped
named volumes, no external resource/fixed container name, exact loopback port
replacement via `!override`, only the existing readonly rules bind, unchanged
topic settings and Kafka advertised external `localhost:39092`/internal
`kafka:9092`. No default Make/seed/check-stack flow was executed.

All Compose commands used `docker compose -p sentinel-p4-demo-20261024`, the
absolute `deployments/compose/docker-compose.yml` first, absolute private
`compose.private.yaml` second and absolute root `.env` via `--env-file`.
New application tags are `sentinel-p4-demo-20261024-{service}` built from current
source; existing tags were not retagged or replaced. Pinned infrastructure and
Dockerfile image versions were preserved. No further cache cleanup occurred.
Build/start commands sampled free space every2seconds; minimum recorded during
monitored commands5,600,952,320bytes, above2GiB. Final preservation check retained
5,614,809,088bytes. Resource checks are observations, not throughput/RSS claims.

| Service | Actual new container ID prefix | Published127.0.0.1 ports | Final health |
|---|---|---|---|
| kafka | `7d416788b66a` | 39092→29092 | healthy |
| clickhouse | `314d91a3fd20` | 28123→8123,29000→9000 | healthy |
| postgres | `ba7066e89537` | 25432→5432 | healthy |
| redis | `8ba7c82241e1` | 26379→6379 | healthy |
| ingest | `945a53143ffd` | 18080→8080,19101→9101 | healthy |
| detector | `4b334e51b773` | 19102→9102 | healthy after traffic/browser |
| event-writer | `4cb39af9cc4e` | 19103→9103 | healthy |
| alert-writer | `0948bc361b91` | 19104→9104 | healthy |
| query-api | `7d9255800794` | 18081→8081,19105→9105 | healthy |

Full IDs/image IDs/tags/bindings and readiness bodies are in timestamped
`nine-healthy-*.json`; exact initial ownership is in `creation-intent.json`
and current IDs in `owned-resources.json`. Five `/readyz` responses reported
HTTP200/ready. Native PG `SELECT1`, CH `SELECT1`, RedisPONG and all TCP dials
passed. Adapted obligations were checked again after traffic/browser, EXIT0.
This is private adapted NEW-stack evidence, not original `check-stack.sh` or
default `make dev-up` acceptance.

## Actual startup and pipeline verification

| Command scope, exact new context above | UTC interval Oct23 | Exit | Monotonic seconds |
|---|---|---:|---:|
| Four infra `up -d --wait` | 23:17:27.575–38.910 | 0 | 11.335 |
| Existing topic-init, no dependencies | 23:17:47.010–58.380 | 0 | 11.370 |
| Existing tern, explicit PG25432 | 23:17:58.437–58.660 | 0 | 0.224 |
| Existing chmigrate, explicit CH28123 | 23:17:58.667–58.959 | 0 | 0.292 |
| Current-source build, five apps+migration | 23:18:08.018–23:20:06.644 | 0 | 118.625 |
| Five apps `up -d --no-build --wait` | 23:20:25.713–51.581 | 0 | 25.867 |
| Actual generator console CLI | 23:27:19.314–19.656 | 0 | 0.341 |
| Actual current-UI Chromium checks | 23:29:08.984–11.655 | 0 | 2.670 |

Existing topic-init and postgres-migrate containers are exited0, with declared
dependency gates preserved. All three topics have exact6/3/1 partitions, RF1,
retention86400000/604800000/604800000ms. The undeclared-topic producer returned0;
its exit is not treated as proof. The subsequent actual listing proves the
undeclared topic absent, consistent with disabled auto-create. No topic reset,
offset/group/fence/cooldown reset, retention change or old-project command ran.

Existing keyctl `ensure --out` created two private keys in the NEW DB/private
run directory. The existing generator console CLI sent seed42/count1000/
batch100 to18080 as `recovery-seed-c2ba399-01`: accepted1000/rejected0/retries0.
`seed-manifest.json` records the sole real run at23:27:19.378Z. Native CH proves
the exact1000current-run manifest IDs, not count alone; five authenticated API
pages prove the same full IDset without duplicates. Own key/JWT samples return
200; foreign key/JWT samples404 and foreign listempty. InvalidBearer+validkey
returns401, foreignBearer+ownkey404, proving no fallback/tenant confusion.
Malformed ID400 and unauthenticated401 were also observed.

Controlled detection used the existing pure `build_scenario(name="port-scan",
seed=42)` helper with unique second tenant `recovery-rule-c2ba399-01`, reserved
documentation IPs and its normal duplicate/threshold/drain semantics. NEW
ingest acknowledged69records. Same-key nonmatching markers at windowend+30s
advance event-time watermarks within the existing future-skew allowance; no
clock reset or wall-clock-expiry shortcut was used. The two exact expected
PORT_SCANv1 alerts have event_count21/sample20 and window23:28:00–23:29:00Z:

- `f2360a63794d739af38db36eebba02fe8c739c37bd3ae314f9715792b327ec18`
- `070d0eb9452d356e414e4683537c51c648d6b710170b7eee811df7be73837268`

Actual native CH canonical payloads match every expected helper field and
ordered sample ID. Native PG proves both workflows open/version1. Authenticated
API detail proves the same detections, retained ordered evidence/missing=[] and
foreign404. `rule-fixture.json`, `rule-ingest-ack.json`, `detection-proof*.json`
and timestamped native/API probes retain the exact source/evidence identities.
Only this bounded rule family was replayed; no expensive Phase3 aggregate or
all-four scheduled scenario acceptance is claimed. Verify-only seed/detection
commands were repeated successfully without resending or replacing manifests.

## Current07 browser workflow

Private Vite remains at `http://127.0.0.1:15173`, proxying only NEW18081, using
the current tracked UI/config without tracked edits. Browser plugin unavailable;
cached Playwright/Chromium was used without dependency installation. A separate
single synthetic auth.success event with reservedIP192.0.2.254 and2500-character
plus malicious-looking text attributes was acknowledged through NEW ingest.
Tokens were minted by existing dev-jwt under the separate JWT context, passed
to the browser via private stdin, and never recorded in headers/logs/captures.

At1440×1000 and375×1000, actual API canonical JSON displays as text, creates no
script/image node or payload side effect, and wraps without horizontal overflow.
Actual foreign404, hard reload/same identity, Escape/exact-trigger focus,
Back/reopened detail, logout URL/input privacy, empty local/session storage,
meaningful content/page identity and no framework overlay/unexpected console
errors passed. `browser-results.json` and `detail-1440.png`/`detail-375.png`
retain private evidence; the mobile screenshot was visually inspected. No mock
API or fabricated detection was used. No browser remains as an active producer.

## Preservation and explicit resource naming limit

`before.json` and `after-v2.json` prove exact preservation of all11old container
IDs/images/names/mounts/networkIDs/portbindings/environment hashes, every old
volume name, existing network identities, and protected `.env`, old auth/key,
userconfig and `.serena/` file hashes. Old detector self-restart counters are
not identity changes; its missing-history hold was not repaired or re-probed.

The new network is `sentinel-p4-demo-20261024_default`; named volumes are
`sentinel-p4-demo-20261024_{kafka,clickhouse,postgres}-data`. In addition, the
unchanged pinned Kafka image declares anonymous volumes: two on NEW kafka
(`/etc/kafka/secrets`, `/mnt/shared/config`) and three on NEW topic-init (those
two plus `/var/lib/kafka/data`). These five actual new IDs are recorded in
`anonymous-volume-proof.json`, were absent before, and are mounted exclusively
by owned NEW containers. Read-only inspection of every container proves that
each destination is declared by the same pinned Kafka image ID/digest. No
old/external volume is reused; no arbitrary addition is ignored. This ownership
proof scopes the five anonymous names; their names lack the project prefix.
No cleanup/recreation was attempted to rename them.

## Repeatable private NEW context and safe verification

Run from `/home/logan78/Desktop/Sentinel`. The private launcher injects NEW
dotted PG25432/CH28123+29000/Redis26379/Kafka39092 settings, plain PG tool
variables, local mode and fresh JWT via existing `child_environment(root=RUN)`.
It removes inherited Sentinel/PG credentials, including ambient API key, and
keeps old rootenv/defaults untouched. Child build/test umask022; evidence077.

```sh
python3 -B .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py -- compose ps -a
python3 -B .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py --timeout 120 -- generator/.venv/bin/python -B .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/data.py verify-seed
python3 -B .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py --timeout 120 -- generator/.venv/bin/python -B .local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/data.py verify-detection
```

These verify-only commands preserve traffic manifests and create uniquely named
private evidence. For subsequent env-aware native tools, use the same launcher
with `--cwd backend -- <exact-command>` and explicit CLI endpoints where tools
have address flags. The launcher cannot redirect a tool that hardcodes old
ports; inspect its interface first. Never use default Make/seed/check-stack or
unqualified migrations/scenario tools as though they target the NEW stack.
The raw JWT helper CLI defaults to the old root and must not replace this
private child context. No production globals were monkeypatched.

The9services and optional Vite remain running for operator use. No teardown,
volume cleanup, further prune, Phase5, hostedCI or publication is authorized.
Root independently checks fresh health/pipeline/preservation and owns tracking
and07acceptance before08.

## Failed attempts retained

In addition to the historical disk/Python hold below, current private records
retain: module-only generator invocation exit0/no CLI/no manifest and enclosing
verification exit1; first real-seed list query exit400 for noncanonical UTC
timestamp and enclosing exit1; first detection verifier exit1 before asynchronous
event-writer evidence flush; first strict preservation checker exit1 because it
had not accounted for the exact five new image-declared anonymous volumes.
Only private harnesses were corrected. Subsequent verification passed without
resending either real traffic manifest, ignoring arbitrary volume additions,
relaxing application acceptance or modifying tracked source.

## Current scans and self-check

Pinned gitleaks8.30.1, unchanged rules and exact existing exceptions, redacted:
new private harness EXIT0/no findings (0.382s); generated contract EXIT0/no
findings (0.386s); current report EXIT0/no findings (0.365s); normal full Git
history `--log-opts=--all` EXIT0/575commits/no findings (0.890s). Exact commands,
timing and redacted results are private timestamped `*scan*.json` records.
Actual private credential files were excluded by selecting code/config inputs,
not by changing scanner policy or adding exceptions. Source and generated
tracked files are unchanged; normal history scanning covers current source.

A separate copied-source directory scan correctly remains **EXIT1**, one
unchanged synthetic malformed-key fixture at
`backend/internal/apikeys/keys_test.go:35`. Git blame is
`996d2d610df8eb020423de3caa980bab3332fce6`; `.gitleaksignore` already contains
that exact commit/path/rule/line fingerprint. A copied-directory scan has no
commit identity and a different path, so the existing history-scoped exception
cannot match. No secret value is reproduced here; no exception was added or
finding silently removed. Root explicitly accepted the unchanged source
identity plus normal full-history policy context and independently owns final
classification. This copied-directory failure is not called a passing scan.

Self-check: source/config/scanner working changes inspected; report and private
isolation/ownership/seed/detection/browser/preservation evidence exist. Original
hold commit02942a8 remains in history. All failed attempts are retained. Only
this report is staged for the recovery evidence commit; root tracking remains
untouched. Final staged and post-commit history checks are recorded privately
at handoff. Historical scan results below describe the earlier admission hold.

---

## Historical admission hold — preserved original attempt

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
