# Plan 11 ownership amendment A3 — independent bounded check

**Result: PASS — 0 blockers, 0 warnings.**

Checked A3 at shared HEAD `1ecf173`, incomplete executor summary `13d2d50`,
and actual `web/src/pages/Alerts.test.tsx` setup, first parameterized case and
deep-link cases. This closes ownership for the reported routing-fixture
failure only. It does not approve the uncommitted implementation or claim
passing tests, browser proof, committed-source acceptance or phase completion.

## Necessary thirteenth path

Approve adding only `web/src/pages/Alerts.test.tsx`: thirteen owned paths,
twelve tracked plus ignored generated schema. Its `start()` helper renders
actual App without first establishing the Alerts route. The general cleanup
sets `/alerts` only after each test, so it cannot establish the first test's
route. The first parameterized case requires “First filtered page” after
calling `start()`. Actual production App now correctly renders Overview at
default `/`; its summary request cannot satisfy this Alerts fixture. The
reported full make-test failure, 138 web passes/one failure with generator
unreached, is consistent with the inspected source. The failed gate remains
preserved and is not converted into passing evidence by this static review.

A2 missed this separate existing fixture dependency. Its review and the
rejected A1 report remain unchanged; A3 supplies the precise additional scope.

## Sufficient repair while preserving explicit deep links

The shared `start()` helper must establish `/alerts` before rendering **only
when the current pathname is not already `/alerts`**. For example, conditional
`history.replaceState(null, "", "/alerts")` on that pathname check fixes the
initial `/` case without changing existing explicit Alerts navigation state.

An unconditional reset inside `start()` is not acceptable: the existing
“reads filter/cursor deep links” and “preserves server-frozen ranges” cases
set `/alerts?...cursor=deep-link...` and
`/alerts?cursor=frozen-server-cursor&limit=3` before calling this helper. Those
query parameters must survive unchanged so their original request/filter/
cache/cursor assertions still execute. This is the concrete implementation
of A3's explicit requirement to preserve every assertion and deep-link fixture,
not an expansion of source ownership or a new feature.

All existing minute-edit, filter, request, cache, privacy, expiry, cancellation
and history assertions remain. The separate malformed-time case sets its own
Alerts URL and renders directly; leave it intact. No global test setup change,
test-only production route exception, weakened Overview behavior or extra
fixture file is needed. App tests remain independently anchored per A2 and
new Overview tests continue exercising actual `/` through App/session/client.

The one added file is sufficient for this closure. Root must synchronize PLAN
and ownership before making the narrow fixture repair, then complete the
recorded bounded normal make-test and remaining focused/browser/source gates.
An ownership PASS is not evidence that any of those runtime gates passed.

## Preservation

Only this report was written and committed. Existing uncommitted implementation
paths, PLAN, shared tracking, runtime, scanner policy and global test setup
were neither edited nor staged. No application, build, test or scanner command
ran. User-owned `.planning/config.json` and `.serena/` remain untouched and
unstaged. Original NEW runtime isolation and all A1/A2 semantic/budget/review
requirements remain binding.

```yaml
issues: []
additional_source_paths_needed: []
owned_path_count: 13
approved_added_path: "web/src/pages/Alerts.test.tsx"
repair_constraint: "Default start() to /alerts only when pathname differs; preserve existing /alerts query/history fixtures and all assertions."
```
