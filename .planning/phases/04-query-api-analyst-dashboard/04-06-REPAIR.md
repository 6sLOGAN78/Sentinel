# 04-06 review repair

Status: implemented_pending_R1; acceptance remains6/17.

Independent R0 atbc27d83/source2436464 found CR01 storedIPv6 spelling false
negatives and WR01 invalid initial/history deep links stuck loading. Preserve
original04-REVIEW-06-R0. Earlier native/web/browser passes remain real but miss
these cases; candidate is not accepted. No new ownership required: native
events_test.go/query_events.go and Events.tsx/Events.test.tsx already owned.

Plan: causal native spelling/pagination and component initial/popstate RED,
tests-only commit; normalize IP equality only after whole-row dedupe without
changing stored event bytes, nullable semantics, tenant/bounds/parameterization;
derive actionable restored-filter validation and no loading for disabled
queries. Minimal GREEN, affected native/web/static/browser/default gates,
independentR1 and root postwave beforeacceptance. No changed deadline/fixture
weakening/dependency/migration/remote/data deletion. Rootowns tracking.

RED4619571: exactnativeEventsEXIT1/7.289s,05:01:07–05:01:14Z,
storedmixedIPv6returns5expected15; sixcomponentinitial/popstateURLcasesEXIT1/
1.648s,05:01:08–05:01:10Z,missingactionablealert. Setup succeeded in both.
GREEN2c6720e: nativeaddresscomparison `toIPv6OrNull` on both boundfilter and
selectedwhole-rowIP, only after dedupe. Existing storedbytes remain unchanged;
NULL/invalidIP does not match valid query. Official ClickHouse IP documentation
confirms IPv4/IPv6 conversion and NULL-on-error semantics:
https://clickhouse.com/docs/reference/functions/regular-functions/ip-address-functions#toipv6ornull
Actualnativefixtureproves mixedcompressed/expanded/uppercase data and each
equivalentqueryspellingsreturnexact15IDsacrossfive3-rowpagesinidenticalorder.
No performance/index guarantee. Restoredfiltervalidation derives from actual
appliedstate at render(initialandpopstate),shows existingactionablemessages;
loadingusesisLoading,notdisabledisPending. Clearfilters recovers normally.

Actual final gates(private04-06logs,allEXIT0):
- root-review-web-green:14.242s,END05:02:00Z,web90+type/lint/build/format.
- root-repair-native:28.371s,END05:03:10Z,Events+Alerts+Auth separately exact.
- root-repair-native-final:7.159s,END05:03:40Z,Events after mechanicalrange-looplintfix.
- root-repair-quality:12.455s,END05:03:31Z,queryrace/default+phase4+integrationvet/build/lint0issues.
- root-repair-openapi:2.813s,END05:03:43Z,11strictOpenAPI/driftcases.
- root-repair-default:12.059s,END05:03:31Z,normalmake test300souter,cachedGo/web90/generator121.
- root-repair-browser:7.213s,END05:02:50Z,actualcurrentbinaryPG+CH/nativehelperCLI,
  Chromium1440/375,mixedstoredIPv6allvisible,threeinvalidhistorylinksactionable,
  priorpagination/nav/focus/privacychecksretained. Rootvisuallyinspected375px
  screenshot:wrappedIDs/IPv6,clearfocus,active-onlynavigation,nohorizontaloverflow.
- root-repair-source-scan:EXIT0,pinned8.30.1stagedreport[].

The first repair static run failed only newtest integer-loopstyle; fixed using
range5 and repeated quality/native. HistoricalR0findings/RED/failedstaticremain
unaltered. No source changes after2c6720e. IndependentR1/rootpostwave stillneeded.
