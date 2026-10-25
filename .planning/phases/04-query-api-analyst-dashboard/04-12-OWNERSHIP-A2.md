# Plan12 A2 — explicit existing backfill fixture dispatch

Independent A1checkba0d114 identifies two concrete missing paths; preserve its
rejection unchanged. Add `web/src/App.test.tsx` and
`web/src/components/AlertDetails.test.tsx` to the ten A1paths, yielding twelve
declared paths (eleven tracked plus ignored generated schema).

Both additions are bounded fixture compatibility: explicitly dispatch the new
backfill endpoint and keep list/detail/session observations separate. Existing
sequential-response mocks/exact-call assertions must retain their original
behavior checks on relevant requests, while separately asserting a legitimate
backfill response. Do not drop tenant/cache/abort/post-body/focus/history/401/
version assertions or replace real App/session/client with weaker mocks.
Owned Alerts.test.tsx requires the same endpoint-aware fixture handling.
Tests-only RED task owns these fixture adjustments before new causal delivery
tests; native real reversecommit/counter rollback/migration/cutover proof remains.

No necessary Events.test.tsx expansion established; do not edit speculatively.
Any further concrete path needs another checked amendment before editing.
All original behavior, runtime, security/budgets and A1client guards remain.
Root must synchronize PLAN after independent PASS before source execution.
