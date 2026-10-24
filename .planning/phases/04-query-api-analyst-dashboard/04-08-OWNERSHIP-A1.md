# Plan08 ownership amendment — approved, execution authorized after PLAN synchronization

Independent check0dea674 PASS0 approves two additional source paths for the already approved acknowledge
increment. Original nine owned paths and behavior remain; no new feature or
dependency is introduced. Execution must wait for an independent PASS and an
updated PLAN before editing these additions.

1. `backend/cmd/query-api/main.go`: inject the real PostgreSQL triage adapter
   explicitly into the production query handler. Current Dependencies supplies
   read-only Alerts/Workflows/Details, and main is the actual constructor site.
   Do not use globals, runtime type assertions or hidden store imports to avoid
   declaring this wiring change. The native fixture launches the actual binary.
2. `web/src/api/client.ts`: expose one generated, guarded PATCH method through
   the existing private transport. AlertDetails needs this method to submit the
   current version and desired acknowledged state. Retain generation/lifetime,
   caller-abort, post-body and401 revocation guards; no direct fetch in the UI,
   new credential path, automatic mutation retry or optimistic fake success.

Resulting source review envelope: eleven declared paths, including the existing
ignored generated schema. Original tasks remain tests-only causal RED then
minimal GREEN. Component tests can exercise the real typed method with the
existing transport fixture; no shared helper edit is preauthorized.

Actual migration007, race/audit rollback/replay/tenant/viewer/body tests, CH-down
PG-supported successful PATCH and browser acknowledge/conflict behavior remain
required. Only open→acknowledged is delivered in08; resolve remains09.

Runtime: use only the private NEW context documented in07-STACK-RECOVERY:
`.local/phase4-acceptance/04-07/fresh-stack/run-c2ba399-01/context.py`.
PG25432/CH28123+29000/Redis26379/Kafka39092, API18081, Vite15173. The launcher
supports `--cwd backend --timeout N -- COMMAND` and `-- compose ...` with exact
project/override/env. Actual native tests launch current source with that private
environment; migration must target NEW PG explicitly through the existing
migrator. No default old-stack Make/runtime/seed operation, old-data reset,
prune, scanner exception, new global setup, remote push/CI or Phase5 authority.

Root owns STATE/ROADMAP/REQUIREMENTS/VALIDATION/reviews. Executor owns only the
eleven source paths, ignored private08 evidence and08-SUMMARY. Preserve user
`.planning/config.json` and `.serena/` untouched/unstaged. Any additional required
path requires a new causal ownership amendment before editing.
