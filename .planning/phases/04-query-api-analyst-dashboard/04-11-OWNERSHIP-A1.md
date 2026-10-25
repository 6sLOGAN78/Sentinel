# Plan11 wiring ownership amendment — proposed for independent check

The approved Overview increment requires two additional explicit source paths:
`backend/cmd/query-api/main.go` for actual CH/PG summary adapter construction and
authenticated route registration, and `web/src/api/client.ts` for a generated
summary GET through the existing guarded private transport. Original nine plus
these two yield eleven paths (ten tracked plus ignored generated schema).
No direct component fetch, hidden globals or query package store imports.

Prefer a separate summary constructor/register method within owned summary.go,
as Rules does; do not edit existing alerts.go Dependencies to conceal wiring.
Reuse existing real stores and their pools/connections explicitly from main.
Native tests may inject a frozen clock into the exported focused handler while
also checking production binary wiring with bounded assertions; no production
environment clock override or unowned fixture helper edit is authorized.

Original tasks/semantics remain: causal RED first; dedupe complete tenant event
identity before aggregation, PG authoritative alerts, one shared snapshot time,
5s query, event rate last60s/60, current open/severity counts, top20allalerts24h
with deterministicties and288aligned5minbuckets. Fill zeros only after success;
dependency failure503, no fake lag/System/performance values. Real typed UI/SVG
and accessible table/loading/error/empty states, no new dependency.

Independent checker must validate eleven paths sufficient for exact native
boundary/fault/tenant tests and production/browser path. Any extra causal path
needs another amendment before editing. Root must synchronize PLAN after PASS.
Runtime only private NEW context07-STACK-RECOVERY: PG25432/schema7,CH28123+29000,
Redis26379/Kafka39092. SharedAPI18081/ingest18080 prior07images; use current
ephemeral API/proxy. Stale006migrationcontainer EXIT1againstschema7; currenthost
tern+directrecordedNEWappIDsrecovery passed, ninehealthy. No implicitstale
Compose migration rerun/defaultoldstack/runtime/seed/reset/prune/remote.
Root owns shared tracking/reviews; executor eleven sources/private11evidence/
11SUMMARY only. Preserve userconfig/.serena untouchedunstaged.
