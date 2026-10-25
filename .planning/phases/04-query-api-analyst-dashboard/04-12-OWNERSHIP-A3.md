# Plan12 A3 — strict OpenAPI expected route fixture

Actual isolated OpenAPI gate at the twelve-path implementation passes10/11;
TestOpenAPIRoutes in `backend/test/openapi_test.go` has a closed expected route
set of13 and lacks the authorized GET `/api/v1/alerts/backfill` (actual14).
Executor did not edit this unowned file and corrected its premature passing
claim in committed incomplete12SUMMARY12e8a0b. Preserve that failure/provenance.

Add ONLY openapi_test.go as thirteenth path (twelve tracked plus ignoredschema).
Only add the authorized backfill GET to the strict expected route list; do not
weaken exact equality, drop any route/security/schema assertion, expand wildcard
matching or alter runtime/API behavior. Keep every earlier contract test.
All original runtime/quality/actual008/nativecounter/atleastonce/browserproof
requirements remain. Root must synchronize PLAN after independent PASS before
the fixture edit; no further feature or dependency is introduced.

SourceGREEN921dbb2 is committed; nativeDelivery9 and browser2widths at that HEAD
pass, but plan stays incomplete until OpenAPI/rereview/rootgate. NEW detector
historyloss remains separate heldruntime; no fullpipeline/ninehealthyclaim.
No extra reset/prune/remote/Phase5authority. Userconfig/.serena preserved.
