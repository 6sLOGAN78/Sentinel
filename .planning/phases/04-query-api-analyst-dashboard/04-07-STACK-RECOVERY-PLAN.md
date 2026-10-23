# Approved separate demo recovery — bounded execution contract

Status: user_choice_1_approved; independent preflight review pending.
User selected1 after being offered a fresh separate demo stack preserving
existing containers/volumes versus restoring Kafka history. This authorizes
new isolated local synthetic resources, NOT reset/restore/mutation of old state.
Continue Phase4 only after this recovery gate; no Phase5/remote authority.

## Ownership

Executor may create only ignored runtime/evidence artifacts under
`.local/phase4-acceptance/04-07/fresh-stack/` and write the new evidence report
`04-07-STACK-RECOVERY.md`. Root owns STATE/ROADMAP/requirements/validation/reviews
and this contract. No tracked application, test, Makefile, Compose, Dockerfile,
script, configuration, .env, userconfig or .serena edits. No new library/tool/
image version/pin, source rewrite or hidden fixture/acceptance relaxation.
Existing approved07sourcecc22566/R2clean and localgates are preserved, not redone
as a new feature. Runtime orchestration files are explicit private test assets.

## Isolation and resource preflight

- New exact Compose project `sentinel-p4-demo-20261024`; refuse to touch an
  existing project of that name unless its own recorded manifest proves this
  attempt created it. Never select/recreate/remove/restart old `sentinel`.
- First file stays deployments/compose/docker-compose.yml, second a private
  override; always pass explicit -p and both absolute -f paths and rootenvfile.
  Project name overrides base name. No default make dev-up/dev-down/reset/seed
  or check-stack.sh: they target the old project/hardcoded ports.
- Override published port arrays with !override (installedCompose5.6.1 supports
  it), not merge/append. Only127.0.0.1 bindings. Kafka39092->container29092,
  CH28123->8123/29000->9000,PG25432->5432,Redis26379->6379,
  ingest18080->8080/query18081->8081,ops19101..19105->9101..9105.
  Optional private Vite15173 must proxy only new18081; no change to tracked Vite
  settings. Assert every intended port free immediately before startup.
- Kafka external advertised listener must use localhost39092, internal kafka:
  9092 unchanged on new project network. Preserve topic partitions/retention,
  no-reset detector guard, rules and all service resource/time caps.
- Assert effective Compose JSON IN MEMORY has exact new project/network and
  fresh project-scoped named volumes, no external resources, no old volume/
  network names, no fixed old container names and exactly the intended ports.
  Only accepted readonly rules bind is allowed. Do not print/store resolved
  secret-bearing configuration or .Config.Env. Quiet config validation first.
- New app images must use new project tags/builds, not retag/replace old images;
  reuse pinned base images/build cache where possible. Build current source,
  do not claim old container images prove current event-detail implementation.
- Before any mutation privately snapshot old project container IDs/images/
  mount-volume names/networks/ports plus volume IDs/names and root.env/private
  auth key/config/.serena content hashes as appropriate, never secrets. Check
  identities after; old detector may self-restart but no operator mutation.
- Current disk5.8GBfree and memory~6.9GBavailable. Require>=4GiBfree before
  starting, monitor build/start and leave>=2GiBfree for verification. If unsafe,
  stop only owned producers and return hold; no prune/delete/reset authorized.

## Credentials and child context

Reuse validated existing dotenv parsing for DB/CH passwords in child memory,
no shell interpolation, echo, argv or global env rewrite. Provision a separate
private JWT via the existing dev_auth_env.child_environment helper rooted in
the fresh private runtime directory (its private .local/auth), not old auth.
Explicitly scope child local/test mode; avoid inherited JWT override conflicts.
No credential in tracked files, URLs, captures or stdout; dirs0700/files0600.
Child build/test umask022; evidence/credential creation umask077.

Provide a private repeatable command/launch context that points host native
fixtures and tools to NEW PG25432/CH28123+29000/Redis26379/Kafka39092 and new
HTTP/ops ports with explicit dotted config overrides. Root.env/old contexts
remain unchanged. Record exact use for subsequent Phase4 agents; never silently
run their native writes, migrations or scenario tools against old project.
No rewrite/monkeypatch of production script globals to manufacture acceptance.

## Startup and actual verification

1. Start only new4infra; run exact new topic init and actual PG/CH migrations
   through existing tools against the new endpoints; then build/start current
   five apps with declared prerequisites. Every command logs exactHEAD/args
   (no credential)/start/end/monotonicelapsed/actualexit privately. No skip or
   continue-through failure. Preserve actual failed attempts distinctly.
2. Require all9 new services healthy, migration/initEXIT0, native PG/CH/Redis
   probes, all intended loopback ports, all5ops ready responses, explicit
   6/3/1topics/RF1/retention and auto-create-off proof. Private adapted checker
   may mirror existing obligations at explicit new ports/project, but must NOT
   claim original hardcoded check-stack.sh/default make dev-up passed.
3. Use existing keyctl with NEW DB child context and private NEW key files;
   generator CLI send seed42/count1000/batch100 to NEW ingest, unique synthetic
   tenant, manifest in fresh runtime directory. Verify exact1000 current-run
   IDs, successful storage and tenant exclusion through new query API/native
   store, bounded failure semantics; never .local/keys or old manifest overwrite.
4. Prove fresh detector->alert-writer pipeline, not health alone: bounded
   controlled synthetic built-in-rule fixture on reserved IP/unique second
   tenant, actual ingestion ACK, real detector, canonical CH alert/evidence and
   PG workflow/API visibility. Use existing scenario/contracts helpers when
   feasible; actual recorded rule/version/sample IDs tied to fixture. Honest
   no-detection/timeout is FAIL, not inferred PASS or invented throughput.
   No need expensive full03aggregate/all4scheduledscenario replay for recovery.
5. Verify current07 dual-credential detail/tenant/escapedbrowser workflow on
   fresh APIs when feasible and ensure fresh detector stays healthy afterward.
   No mock-only demo acceptance. Focused browser localhost1440/375 can reuse
   cached tooling, no new browser dependency; never print tokens/privatecaptures.
6. Compare old resource identities/settings again and report preservation.
   Final source/generated/history scans unchanged rules/pins. All new runtime
   resources remain for operator use; no teardown/volume cleanup authorization.

## Evidence and handoff

Write04-07-STACK-RECOVERY.md with exact source/project/ports/isolation checks,
commands/SHA/exits/limits, old identity preservation and repeatable private
launch instructions. No restoration claim: missing old Kafka range remains
missing and old detector remains fail-closed. No old-data processed claim.
Root independently verifies actual new health/pipeline and accepts07onlywhen
all gatesclear, preserving old STACK-HOLD as history/new separate-environment
disposition. Then continue08withinPhase4/newcontext; no Phase5 or publication.
