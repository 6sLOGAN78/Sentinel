# Plan11 ownership A2 — explicit existing App fixture route

Independent A1check51c16b1 rejected eleven-path sufficiency because existing
`web/src/App.test.tsx` authenticates at implicit root `/` and expects Alerts,
while approved Overview belongs at `/`. Preserve that rejected report.

Add only App.test.tsx to the eleven A1paths: twelve total, eleven tracked and
ignored generated schema. Anchor existing Alert/session/privacy/retry tests
explicitly at `/alerts` in their existing setup before rendering. Preserve
every behavioral assertion and all negative/cache/token/disposal tests; do not
convert them into weak Overview smoke tests, alter global test setup or route
production differently under tests. New Overview tests must independently
exercise actual `/` via real App/session/client. No other source ownership.

All A1behavior/runtime/budget/guard requirements remain. Tests-first ordering:
this narrow old-fixture route adjustment is a tests-only compatibility change
in RED task, followed by independent causal Overview native/component RED.
Production wiring stays main+separatefocusedhandler and guardedclient method;
no extra backend/clock helper needed. Root synchronizes full PLAN after new
independent PASS; no source editing before that approval.
