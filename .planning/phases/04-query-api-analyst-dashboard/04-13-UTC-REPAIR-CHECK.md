# Plan13 UTC prerequisite — independent bounded check

**Result: PASS — 0 blockers, 0 warnings.**

Checked proposed `04-13-UTC-REPAIR-PLAN.md` at `e4045ec`, with current SSE
source `2c88d18`. This approves the separate two-path prerequisite plan before
source edits. It is not implementation acceptance, a native/browser pass, or
completion of Plan13 or Phase4.

## Cause and sufficient repair

The actual private record
`.local/phase4-acceptance/04-13/browser-1792968021932943288/delivery-shape.json`
contains the exact fifteen WorkflowAlert keys and exact decimal delivery cursor
`9007199254740993`. Its four timestamp strings, in created/updated/window-start/
window-end order, are:

```text
2026-10-26T04:09:22.208343+05:30
2026-10-26T04:10:22.208343+05:30
2026-10-26T04:08:22.208343+05:30
2026-10-26T04:10:22.208343+05:30
```

All four violate OpenAPI `WorkflowAlert` references to `UTCTimestamp`, whose
pattern requires `Z`. The current live client's `validDelivery` requires that
suffix for each field. `receiveLiveAlerts` validates the REST page before
processing, advancing its cursor, or attaching SSE; rejection therefore
explains the observed successful-HTTP-backfill retry before stream attachment.
The validator must remain strict.

`Store.Read` in `backend/internal/alertstream/delivery.go` scans the four fields
directly into `query.WorkflowAlert` (`time.Time`) and appends without UTC
normalization. Both REST backfill and the SSE handler consume this same read
path. Existing `backend/internal/storage/postgres/query_alerts.go` and
`triage.go` already normalize these four fields using `.UTC()`.

Normalizing CreatedAt, UpdatedAt, WindowStart and WindowEnd after a successful
scan and before appending is sufficient for this identified contract defect:
it preserves each instant while making standard JSON time serialization end
in `Z` for both consumers. No SQL, transaction, counter, bound capture, cursor,
tenant, projection, pagination, canonical schema or client change is needed.
This makes no claim about other pending browser acceptance assertions.

## Native regression is feasible within the two paths

The planned regression must inspect actual raw JSON strings as well as their
parsed instants; decoding only into `deliveryPage` and comparing `time.Time`
values cannot prove the wire suffix. Use explicit fixture instants at PostgreSQL
microsecond precision, retrieve the matching workflow through the actual
current query-api child, assert all four suffixes are `Z`, and parse/compare
each with its own inserted instant. Include distinct values so field swaps or
replacing values with current time cannot pass. Before production changes the
same successfully built/running process and real migrated PG must return200
with offset-bearing strings and fail the wire assertion: compilation,
connection or setup failures are not causal RED.

`startQueryAPI` in `backend/testphase4/auth_test.go` copies inherited environment
entries except its explicit overrides; it does not override `TZ`. A scoped,
sequential test environment containing `TZ=Asia/Kolkata` before child creation
can therefore exercise the actual process without editing that shared helper.
The Go test process's global `time.Local` must not be replaced. Record the
explicit child timezone with the command/evidence so a passing UTC-default
runner cannot conceal the regression.

Use the existing `deliveryDatabase` owned-database/migration/cleanup pattern
for the added UTC fixture, with its own child and connection. Isolating it
avoids altering existing exact tenant-page counts, cursor expectations,
status/version/audit assertions or synthetic-counter fixtures. Native children
must join before their newly owned database cleanup. The shared NEW database,
history and counter are outside this repair. These choices require no third
source path: test additions remain in `backend/testphase4/delivery_test.go`.

## Scope, sequence and acceptance gates

Approve exactly:

- `backend/testphase4/delivery_test.go` — actual non-UTC-child wire regression,
  tests-only causal RED commit and retained prior assertions.
- `backend/internal/alertstream/delivery.go` — the four UTC conversions and
  minimal GREEN commit.

This is a recorded separate prerequisite to browser continuation. The original
Plan13 fourteen-artifact ownership remains unchanged; delivery.go is not a
concealed fifteenth feature path. Execute the repair sequentially, obtain its
independent source review, then resume browser13 against the actual committed
API at1440/375 with the original strict checks. Retain the failed browser
attempts and stale/incomplete summary provenance.

The plan retains exact nonempty `TestPhase4Delivery` and `TestPhase4Stream`
phase4/race/10m gates, affected unchanged OpenAPI11/schema and three-mode quality
checks, sequential ordinary `make test` and web gates, and actual SHA/command/
UTC-start/end/exit evidence. No acceptance result is inferred here. The
unchanged scanner policy remains root's execution responsibility.

Only NEWPG25432/schema8 and newly owned fixture databases are authorized for
native proof. The separate detector checkpoint-history hold stays separate;
PG/API/browser proof cannot establish full pipeline recovery. No timezone
workaround, server configuration change, contract relaxation, counter repair,
runtime reset, tracking update or source/test/build/scanner execution occurred
during this check. User `.planning/config.json` and `.serena/` remain untouched.
No root AGENTS.md or project `.codex/skills`/`.agents/skills` directory exists.

```yaml
issues: []
approved_source_paths:
  - backend/internal/alertstream/delivery.go
  - backend/testphase4/delivery_test.go
additional_source_paths_needed: []
original_plan13_owned_artifacts: 14
separate_sequential_prerequisite: true
implementation_acceptance: false
```
