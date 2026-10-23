# 04-06 review repair

Status: in_progress; acceptance remains6/17.

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
