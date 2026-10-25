# Plan11 A3 — explicit Alerts component fixture route

Full normal make test at the twelve-path implementation found one existing
test-routing dependency missed by A2: `web/src/pages/Alerts.test.tsx` start()
renders App without establishing `/alerts`; only afterEach sets that route.
The first parameterized minute-edit case starts at default `/`, correctly
renders the new approved Overview, and fails its existing filtered-page check.
Evidence in incomplete11SUMMARY13d2d50: web138/139, generator not reached.
Preserve this failed gate and prior A1rejection/A2approval unchanged.

Add ONLY Alerts.test.tsx as thirteenth path (twelve tracked plus ignored
generated schema). Its shared start helper sets `/alerts` before rendering,
matching its existing afterEach route. Preserve every assertion/request/cache/
filter/minute-edit test and global setup; no production route exception or
Overview weakening. App.test.tsx remains separately anchored per A2; new
Overview tests continue checking actual root `/`.

All original behavior, runtime, test budgets and review gates remain. No new
feature/dependency/fixture file. Root synchronizes PLAN after independent PASS,
then applies the one fixture setup line and completes normalmake/browser/
committed-source proofs. Existing source remains uncommitted; no edit to this
new path before independent check. Userconfig/.serena unchangedunstaged.
