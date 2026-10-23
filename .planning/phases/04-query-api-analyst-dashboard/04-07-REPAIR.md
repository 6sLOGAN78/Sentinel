# 04-07 review repair — persistent detail identity vs transient generation

Status: CR01_and_CR02_closed_by_clean_R2_root_gate_passed_operator_stack_hold;
acceptance remains7/17.

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

## Current disposition — independent R2 and root postwave passed

R2at2059335/sourcecc22566cleanzeroissues,resolvesCR01+CR02;preserved
04-REVIEW-07-R2. Rootpostwavebackend/webbuild+make testEXIT0/13.857s,
END2026-10-23T13:08:56.404072Z,cachedGo/web114/gen121,private root-postwave.log.
Allimplementation/reviewrepairlocalgatescomplete;earlierpendingsectionsbelow
arehistoricalexecutionprovenance. Acceptance stillheld7/17becauseactualKafka
retainedstart=endbeyondall6detectorcheckpoints. See04-07-STACK-HOLD. No offset/
group/PG/volume reset or historyguardweakening. Await operatorbackuprestoration
or explicitapprovalforfreshseparateisolatedsyntheticstackpreservingoldstate.
No08/Phase5/remoteauthorityorwholepipelinePASSinferred.

## Executor results — 2026-10-23, pending independent R1

Tests-onlyRED3260ab6 at rootdocumentationbaseline738f8f5, minimalGREENec14c34.
Only Events.tsx/Events.test.tsx changed. Actual StrictMode failed401->success->
open->save actualURL/history->fresh modules/provider restoration lost event_id
before repair: review-reload-red EXIT1/1.971s,12:19:22–24Z; four negative identity/
samepage cases passed. Structured stable verifiedidentity, documentUUID and
per-document first-generation ownership now restore newpage sameidentity while
rejecting differentidentity and samepage stale generations, including olddocument
history after restored-session sign-out. Request/cache/client authority unchanged;
no App/session/dependency/focus/ignore edit. Six new regression cases,112totalweb.

Final actual gates: focusedGREEN/type/lint0/7.115s; fullweb112/type/lint/build/
format0/14.067s; normalmake test0/8.812s(Go race/web112/gen121); quickrace+
three-modevet/build/lint0/16.129s; exactcontract/runtime60s0/2.333s,0/2.418s;
OpenAPI11/race60s0/5.506s; nativeEvents/Auth/Alerts6m0/8.177s,0/13.334s,0/8.271s.
Actual currentrace-built queryAPI+devCLI/helper/nativePGCH/Chromium1440+375
review-browser-ready0/9.156s ends12:24:50Z; actualfailedlogin->retry->selected
page.reload->sameverifiedidentity generation4→2 preservesURLandcanonicalJSON.
Priorfocus/Escape/history/longJSON/expandedIPv6/missing/privacychecks retained.
All exactcommands/SHA/start/end/exits private04-07review-*logs; summarytablelinks
their meaning. Source/generatedpinned8.30.1reports[]; fullhistoryatec14c34
EXIT0/562commits/0.871s ends12:27:27Z, existingexactrootexceptionunchanged.

Initialnative3familiesEXIT1werePG15432connection-refused SETUP afterserverrestart;
browserpreflightEXIT1missingpassingfixture. Preserved,notbehaviorRED/PASS.
Normalstartofexisting4infracontainerswithpreservedvolumesEXIT0/7.417s/all4healthy
before independentnative/browserreruns. No recreate/pull/reset/teardown/datachange.
Eightstackserviceshealthyatboundedread-onlycloseout; detectorstillrestarting,
exit1/OOMfalse/33restarts. Capturedprivate25-line readonlylog; no unownedfix or
all-nine/full-pipelinePASSclaim. Rootassessesstackreadinessafterhandoff.

Sourceandmetadata self-check passed; review/rootpostwave remains pending.
OriginalR0findings and all failed/successful attempts remain distinct evidence.

## R1 counterexample — Events popstate rewrites destination Alerts filters

IndependentR1at1dd2997/sourceec14c34closesCR01butfindsnewCR02BLOCKER:
Events popstate nowalwayswriteLocation evenwhenactualBacknavigatesto/alerts.
ActualStrictModeApp/history.back differentialreproduction: startingAlerts
severity=critical/status=open/search=needle,Events,Back; pre-repairR0retains
allfiltersandfilteredrequest,ec14c34dropsfiltersandsendsunfilteredrequest.
All112existingwebtestsPASSbutmissthisinterpagehistorycase. PreserveR1report
04-REVIEW-07-R1; noacceptanceuntilnewrepairR2/rootgate/stackholdresolved.

BoundedrepairONLYexistingEvents.tsx/Events.test.tsx: pinactualbackfromEvents
tofilteredAlertsURLandrealclientrequest,causaltests-onlyREDthenminimalowned
pathnameguardbeforeEventsadopts/rewriteshistory. Retainrewritesforaccepted/
rejectedEventsentries,CR01freshpageidentityandstale/foreignguards. No App/
Alerts/session source edits,unownedfiles,framework/deps or otherrefactor.
Repeataffectedweb/native/contracts/browser/default/static/scanner,independent
R2androotgate. Separatestackdetectorrestartremainsunresolved, nofullstackPASS.

## CR02 executor results — pending independent R2

RED843ba72, GREENcc22566 at rootbaselinebe7ef9b. ProductiondeltaONLYone
pathnameguardatstartofEvents popstatebeforeanyread/stateupdate/URLwrite.
ExistingCR01ownership/adoption/privacymechanism unchanged. TwonewactualStrictMode
Appcases(realhistory.back/forward)assertdestinationAlertsURL/controls/alltyped
queryfilters,selectedalert/detailrequestvariant,andForwardEventsURL/results.
BothREDfailnullseverityvscritical; all112priorcasesremain,114final.

Private07cr02-*logs2026-10-23UTC: RED1/1.364s12:56:33–35; focusedGREEN0/
1.404s12:56:49–50; finalweb114/type/lint/build/format0/15.480sends12:59:14;
normalmake test0/18.122s(Go race/web114/gen121); quickrace+3modevet/build/lint
0/20.601s; nativeEvents/Auth/Alerts6m0/15.103s,20.972s,15.260s; OpenAPI11/
race60s0/6.152s; exactcontract/runtime60s0/1.330s,1.244s. ActualcurrentraceAPI+
CLI/helper/nativePGCH/Chromium1440+3750/9.854sends12:58:45: realfilteredAlerts
->Events->BackpreservesURL/controls/APIquery->Forward,alongsideactualfailed401/
retry/detailhardreload/sameidentity4→2andsafeJSON/focus/IPv6/missing/privacy.
Browserresults/screenshotsprivatecr02/. Generatedschemahashunchanged.

Initialcombinedwebgate runtime114PASSbuttypecheckEXIT2unsupportednewtest
ByRoleOptions.exact;removedfourunsupportedoptionswithoutweakeningassertions,
thenfinalweballqualityPASS. Earlierfailedgate preserved. Pinnedsource/generated
reports[]; fullhistoryatcc22566EXIT0/567commits/0.942sends12:59:59Z,approved
rootexceptionunchanged. Fullcommands/actualSHA/start/end/exitsineachprivatelog.
Nootherpaths/deps/ignore/config/sharedtracking/session/App/Alertsedit;noextra
stackaction/detectorinvestigation/fullstackPASS. Self-checkPASS; pendingR2,
rootpostwaveandseparatedetector/readinesshold. No08/Phase5/remoteauthority.
