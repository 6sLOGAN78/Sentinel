---
phase: 04-query-api-analyst-dashboard
plan: "14"
reviewed: 2026-10-30T05:15:00Z
depth: standard
review_iteration: R1
reviewed_head: 0547c9c
repair_red_commit: 62f09a0
repair_green_commit: 66a3b35
diff_base: db0fa224cb1015ae50bd0a99f02e6d496ae8cf86
prior_review: 04-REVIEW-14-R0.md
files_reviewed: 2
files_reviewed_list:
  - backend/testphase4/stream_test.go
  - backend/internal/alertstream/listener.go (unchanged; traced for the repair)
findings:
  critical: 0
  warning: 0
  info: 2
  total: 2
status: clean
r0_findings_resolved: [WR-01]
runtime_verification: native_stream_pass_2026-10-30
whole_phase_acceptance: false
---

# Phase 4 Plan 14: Code Review Report — R1

**Reviewed:** 2026-10-30
**Scope:** repair delta `db0fa22..0547c9c` (only Go change: `backend/testphase4/stream_test.go`)
**Result:** 0 BLOCKERS, 0 WARNINGS, 2 INFO. R0 WR-01 is resolved in source.

## WR-01 (R0) — resolved

R0 asked for a held barrier instead of a single post-kill readiness sample.
The repair adds `streamPGGate`, a loopback startup proxy per API child:

- Each child reaches the owned database only through its own gate
  (`SENTINEL_DATABASE.PORT` is set per child before `startQueryAPI`). The parent
  pool and observer still connect directly.
- `hold()` is taken only after both initial listeners exist in
  `pg_stat_activity`, and before the listener backends are terminated.
- The test waits for a blocked `sentinel-alert-listener` startup from **each**
  gate. Then it asserts that the PG listener set is empty and that **both**
  replicas report `unavailable`. It commits a workflow, reads it on both
  streams, and checks again that both replicas are still `unavailable` before
  releasing. This makes the commit and delivery during the loss deterministic,
  and it checks both replicas, not just one.
- Release is deferred, and t.Cleanup closes any held channel, so failure paths
  cannot leave a gate closed.

Tracing against `listener.go`: `listen` stores `connected=false` in its defer
before `Run` sleeps and reconnects. So `unavailable` is guaranteed by the time
a reconnect reaches the gate. A held attempt is bounded by pgx
`ConnectTimeout` (5s); later attempts back off from 200ms. After release, the
next attempt passes the gate immediately, which leaves margin inside the 8s
`Eventually` for new PIDs plus `ok` on both replicas. The later
re-LISTEN notification assertion is unchanged.

## Info

### IN-01: Held startups do not notice client timeouts
`awaitStartup` waits only on `held`/`ctx`. When pgx gives up a held attempt
after 5s, the proxy goroutine keeps waiting. On release it dials PG and sends
the startup for a client that has already gone, which then fails and is cleaned
up. This only adds harmless extra goroutines/connections during the hold
window. No change needed.

### IN-02: Gate B port persists for later children
`t.Setenv` leaves `SENTINEL_DATABASE.PORT` pointing at gate B for the rest of
`TestPhase4Stream`. The later child at line ~350 inherits it and connects
through gate B, which passes traffic straight through when not held. This is
correct but indirect. A comment or explicit reset would make it clearer.

## Checks run (2026-10-30)

| Check | Result |
|---|---|
| `go vet -tags phase4 ./testphase4/` | pass |
| `go test -tags phase4 -run XXX_none ./testphase4/` (compile) | pass |
| `go test -race -count=1 ./internal/alertstream/` | pass (1.16s) |
| golangci-lint | not run — not installed locally |
| Native `TestPhase4Stream` | pass (11/11 subtests, 55s) — see Runtime verification |

## Runtime verification (added 2026-10-30)

After the NEW stack infra (`sentinel-p4-demo-20261024`) was recreated on its existing volumes, the native test ran through the recorded launcher (`context.py`, evidence `20261030T162734875641Z-operator-0aed4d59.json`, source 00f67aa):

- `go-test-nonempty.sh ./testphase4/... ^TestPhase4Stream$ -tags=phase4 -race -timeout=10m -p=1`: exit 0, all 11 subtests PASS in 55s, including `owned_listener_loss_readiness_relisten_and_both_replicas_replay` (2.03s).
- `web`: `npm test` 170 passed, `npm run typecheck` clean, `npm run build` ok.

WR-01 is resolved at runtime as well as in source. The detector in the NEW stack is still in the separate "checkpoint history lost" hold; this run did not depend on it and changed no offsets, groups or fences.
