# Plan10 guarded client ownership — proposed for independent check

The approved read-only startup Rules increment needs one additional source
path: `web/src/api/client.ts`. Its generated transport is private to the factory;
the new Rules page needs a typed rules GET method through that transport.
Retain session generation/lifetime/caller cancellation, post-body checks and
current401 disposal. No direct UI fetch, credential path or persistent token.

Original eight paths plus client.ts yield nine declared paths (eight tracked
and ignored generated schema). Both original tasks and all behavior remain:
tests-only causal native/component RED, minimal GREEN, existing validated
startup loader, read-only all-role API, real Rules page and failclosed startup.
Actual `detection.LoadRules` is reusable. Production query main already owns
config+PG pool; existing query config carries Detector.RulesPath. Do not change
detector configuration, loader behavior, reservation registry or mutable CRUD.
Current definitions must come from validated query startup state; distinguish
retained version registry from current definitions and verify exact versions.

The independent checker must verify nine paths sufficient using actual source,
including startup lifecycle, retained version access and tests that launch the
real query binary. Any additional path requires another amendment before edit.
No source work until independent PASS and PLAN synchronization.

Runtime stays the private NEW context documented in07-STACK-RECOVERY, PG25432,
CH28123+29000/Redis26379/Kafka39092. Current-source ephemeral API/browser for
acceptance; shared18081/18080 retain prior07images. No default old-stack runtime,
seed/migration/reset/prune/remote operation. Preserve user config/.serena.
Executor owns nine sources/private10 evidence/10SUMMARY; root owns tracking and
reviews. This is scope approval only, not implementation or phase acceptance.
