# 04-04 ownership amendment A1 — retained cache-key assertions

Status: APPROVED by independent04-04-OWNERSHIP-A1-CHECK (PASS,0issues).
Root added the exact extra fixture path to PLAN/Task2 ownership. Runtime/source
acceptance remains pending; the three other component failures are not waived.

## Cause and bounded scope

At tests-only HEAD `91b1ead`, the ten owned paths contain the in-progress
04-04 implementation. Actual native Alerts and cursor tests pass, but the first
web run is 41/46: two retained App assertions still require the previous empty
filter object in the seven-part query key. The approved contract now requires
normalized filters, including the frozen default from/to range and limit.
Current production keys contain those values; keeping `{}` would contradict
the intended behavior or force production to omit its required cache boundary.

Evidence: private `04-04/web-first.log`, failures at App.test.tsx:395 and :476.
The other three failures are new owned Alerts component cases; this amendment
does not waive them. The deliberate post-limit SQL mutant separately failed
the full-page assertion (2 versus 3); its defect was restored immediately.

Proposed extra ownership: `web/src/App.test.tsx` ONLY, raising the declared
source envelope from ten paths (including derivative generated schema) to
eleven. Change only the two obsolete expected filter objects, with at most
one small local assertion helper to derive from the actual captured alert HTTP
request. Preserve the exact six identity/generation key parts and the exact
remaining normalized-filter shape; assert default limit 50, finite UTC
millisecond timestamps and exactly a 24-hour frozen interval. Do not replace
the key assertion with a partial matcher or blindly accept arbitrary values.

All existing 36 cases, cache clearing, old-tenant delayed-response suppression,
credential privacy, abort and CR-01 draft-clearing assertions remain intact.
No App/provider/client/CSS/backend production source beyond the original ten
paths is authorized, no dependency, framework or new feature is introduced,
and no existing test or budget may be removed, skipped or weakened.

## Approval and execution gates

The independent checker must verify the causal mismatch and this explicit
fixture-only eleven-file exception before application. Its scope is two
expectations plus a bounded local helper, not an additional subsystem; retain
the plan's <=45% context target. Requirements and dependency waves do not change.
After approval root adds App.test.tsx to PLAN ownership and Task 2 files; the
executor applies the correction in its scoped GREEN work, fixes the three new
component failures, verifies cursor-only deep links/frozen ranges, and reruns
all web, native Alerts/Auth, cursor, OpenAPI and affected static/default gates.
No independent source acceptance or whole-phase success follows from this
planning approval. User configuration and `.serena` remain untouched/unstaged.
