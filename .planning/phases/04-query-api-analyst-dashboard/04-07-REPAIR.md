# 04-07 review repair — persistent detail identity vs transient generation

Status: pending_causal_RED; acceptance remains7/17.

IndependentR0at3738d7a/source242b162foundoneBLOCKER: persisted event detail
history compares sentinelEventsOwner including transient session generation.
ActualStrictMode App reproduction failed login -> successful retry -> open
detail -> fresh page/sign-in sameverifiedidentity drops event_id (generation4
vs2), no dialog or detail request. First-try happy-path browser proof missed it.
Other13-pathboundaries/scannerexceptionreview established no furtherfinding;
original04-REVIEW-07-R0retained. PassingtestsdoNOTsupersedecounterexample.

Repairownership is existing web/src/pages/Events.tsx and Events.test.tsx only,
plus ignored private browser harness/evidence and07summary. Causal tests-only
REDbeforeminimalGREEN. Separate persistent verifiedsubject/tenant/role identity
from per-page session generation used for cache/request isolation. A valid
detail ID may restore on a fresh page for the same verified identity, but
differentidentity and same-page stale-generation entries must remain rejected.
Keep explicit logout/expiry/identity-change selectedURL cleanup, cache clear,
credential/callerabort and actual server tenant checks. Do not simply delete
ownership validation or infer identity by parsing ambiguous colon strings.
Choose the smallest robust representation; reviewer's page-instance-marker
example is an option, not permission for a broader framework/abstraction.

Repeat allpriorweb106+newregressions/type/lint/build/format, actualbrowser
failed-login/success/reload/sameidentityat1440/375, affectednative/contract/
static/default/scanner gates; retain previous failures/diagnosticlimits. No
unownedsource,session.tsx/Appchange,dependency,fixtureweakening/deadlinechange,
remote/destructive action, broadscannerignore orPhase5 authority. Independent
R1 and rootpostwave required before accepting07/starting08. Rootowns tracking.
