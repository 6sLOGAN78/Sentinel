# 04-07 separate-stack recovery — independent preflight

Decision: **APPROVE / PASS**. Zero blockers; zero warnings.

Planning-only review at `e789f005446a288ce3b3944e386dc660f14bd1e6`, before
new runtime work. User choice1 authorizes a fresh isolated local synthetic
demo while preserving the existing stack. This check approves the bounded
recovery contract; it does not assert current ports/resources, runtime health,
data verification or recovery completion.

## Scope and isolation

Read the complete07STACK-RECOVERY-PLAN, preserved STACK-HOLD and current STATE;
inspected the actual base Compose, Makefile, private JWT helper, keyctl and
migration interfaces, check-stack, generator CLI/scenario helpers and current
Phase4 native fixture launch/config handling. Applied Docker Compose patterns
guidance for dependency ordering, project-scoped persistence and secret-safe
configuration validation. No AGENTS.md or project skill directory was found.

The plan gives the executor exactly two output areas: ignored artifacts beneath
`.local/phase4-acceptance/04-07/fresh-stack/` and the new
04-07-STACK-RECOVERY.md evidence report. Tracked source/test/config/Compose/
Makefile/script files, root tracking and userconfig/.serena are excluded. The
accepted07sourcecc22566 and its114web/root gates are preserved. No new feature,
dependency, image-version pin, schema or implementation rewrite is required.

The base Compose has project name sentinel, project-scoped kafka/clickhouse/
postgres named volumes, default networking and no fixed container names.
Explicit `-p sentinel-p4-demo-20261024` plus the absolute base file first and
private override second can create independent resources while preserving its
relative build/rules paths. The new effective configuration must prove the
project/network/volume/container/port invariants before startup, and the project
name collision guard prevents adopting an unrelated prior attempt.

The planned full port-list replacement avoids retaining old publications:
Kafka39092->29092, CH28123->8123 and29000->9000, PG25432->5432,
Redis26379->6379, ingest18080->8080, query18081->8081, ops19101..19105 to
their existing9101..9105 targets. All bindings are loopback. Kafka's external
advertised endpoint changes to localhost39092 while its internal kafka:9092
remains on the new network. App healthchecks continue using unchanged internal
ports. Optional private Vite15173 must proxy the new query endpoint.

New project image tags/builds avoid replacing the old application's image tags;
reusing pinned bases/cache does not authorize pruning them. Only the existing
read-only rules bind is permitted. Old identities/settings and relevant file
hashes are captured before mutation and compared afterward; old detector
self-restarts do not grant permission to recreate/reconfigure it. Missing old
Kafka history remains missing; no offset/group/fence/data reset is approved.

## Interfaces and execution feasibility

The existing JWT helper accepts an explicit root for its private .local/auth
credential. The contract requires a separate new key: the launch wrapper must
remove inherited normal/dotted JWT values before provisioning rather than let
the helper select an ambient old key. Its CLI otherwise roots at the repository,
so the private launcher must call child_environment with the fresh directory as
specified. Preserve0700directories/0600credentials/evidence and child-only
environment changes; never print resolved configuration, tokens or Config.Env.

The private repeatable context must carry both native dotted endpoints and
tool-specific environment/arguments. Existing Phase4 tests set root.env but
config uses non-overriding dotenv loading, so inherited explicit dotted NEW
PG/CH/Redis/Kafka endpoints survive and flow to their child query processes.
Existing keyctl instead reads PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE/
PGSSLMODE; use its ensure --out private-path flow, not create-to-terminal.
The existing PG migration tool accepts explicit new host/port and child
PGPASSWORD; chmigrate accepts explicit new HTTP address and its plain password/
TTL environment names. No source modification is needed for these distinctions.

The generator CLI accepts explicit URL, tenant, key-file and manifest output,
so seed42/count1000/batch100 can target new ingest and new private files.
Remove ambient SENTINEL_API_KEY when selecting a new key file because the
generator otherwise prioritizes that variable. Exact current-run identity and
tenant verification can be implemented in the private harness against NEW
query/native endpoints. Existing seed.py and seed_scenarios.py top-level flows
hardcode old endpoints/project/private paths and are correctly excluded; pure
contracts/helpers remain reusable without monkeypatching their globals.

Base topic-init and postgres-migrate already express healthy/completed
dependencies. The plan orders new infrastructure, topic initialization and
actual PG/CH migrations before the five current-source applications. A private
checker can reproduce check-stack's nine health states, topic partitions/RF/
retention, disabled auto-create, ports/readiness and native probes with explicit
NEW project/ports and secret-safe client calls. Its result must be described as
adapted new-stack evidence, never default check-stack/dev-up acceptance.

Controlled detection is feasible with existing rules and pure scenario helpers
without running the expensive aggregate: use a unique second tenant/reserved
source, actual ingest ACKs, appropriate same-key watermark/drain events, then
bounded observation of canonical CH alert/evidence, PG workflow and authenticated
API visibility. Existing scenario helpers encode window/threshold/marker
semantics; health or acknowledged event storage alone cannot prove detection.
The plan requires actual rule/version/sample IDs tied to the sent fixture and
fails honestly on missing detection or timeout.

These interface notes spell out the approved plan's existing separate-key,
NEW-context and actual-pipeline requirements. They add no owned source path or
acceptance relaxation.

## Required runtime proof and limits

Before mutation, repeat the plan's port/project/resource admission checks.
The reported installedCompose5.6.1,5.8GBdiskfree and approximately6.9GBavailable
RAM are supplied preflight observations, not fresh measurements by this review.
Require>=4GiBdiskfree before starting and>=2GiB during verification; monitor
build/start and stop only owned producers on unsafe pressure. No old/shared
cache, image, container, topic, volume or data deletion is authorized.

Recovery completion still requires all9NEWservices healthy, actual init and
migrations EXIT0, exact1000 current-run IDs/tenant exclusion, real controlled
ingest->detector->alert-writer->CH/PG/API proof, current07workflow verification
as scoped, final detector health, old-resource preservation and final unchanged
source/generated/history scanner gates. Keep all failed attempts and limits
distinct, with exact source/command/start/end/monotonicelapsed/exit privately.
Only root may reconcile tracking and accept07/continue08 using the documented
NEW context. The old hold remains historical; no restoration, Phase5 or remote
publication follows from this approval.

No Docker/runtime/config validation commands, tests, services, migrations or
secret reads were performed for this check. Only this check file was written;
source, configuration, root tracking, userconfig and .serena were untouched.

```yaml
status: passed
decision: approve
scope: planning_preflight_only
blockers: 0
warnings: 0
issues: []
```
