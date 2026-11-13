#!/usr/bin/env bash
# Each exact Phase 4 native family must pass independently and nonempty (run
# through go-test-nonempty.sh). A group is one family; "all" runs the union of
# the eight in a fixed order and stops at the first failure. Families live in
# backend/testphase4 behind the phase4 build tag, so `make test-integ` never
# sees them. They need a healthy dev stack (make dev-up). Budgets: ordinary 6m,
# delivery and stream 10m. The real-browser acceptance is separate:
# scripts/check-phase4-browser.sh.
set -euo pipefail
group="${1-all}"
case "$group" in
  all | auth | alerts | events | triage | summary | delivery | stream | tenant) ;;
  *) group=invalid ;;
esac
if (( $# > 1 )) || [[ "$group" == invalid ]]; then
  echo "usage: $0 [all|auth|alerts|events|triage|summary|delivery|stream|tenant]" >&2
  exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

started=$SECONDS
echo "[phase4] group=$group started"

# family <label> runs the exact family and reports its wall-clock duration.
family() {
  local mark=$SECONDS
  echo "[phase4] family $1 started at +$((mark - started))s"
}
finished() {
  echo "[phase4] family $1 passed in $((SECONDS - $2))s"
}

if [[ "$group" == all || "$group" == auth ]]; then
  mark=$SECONDS; family TestPhase4Auth
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Auth$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4Auth "$mark"
fi
if [[ "$group" == all || "$group" == alerts ]]; then
  mark=$SECONDS; family TestPhase4Alerts
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Alerts$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4Alerts "$mark"
fi
if [[ "$group" == all || "$group" == events ]]; then
  mark=$SECONDS; family TestPhase4Events
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Events$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4Events "$mark"
fi
if [[ "$group" == all || "$group" == triage ]]; then
  mark=$SECONDS; family TestPhase4Triage
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Triage$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4Triage "$mark"
fi
if [[ "$group" == all || "$group" == summary ]]; then
  mark=$SECONDS; family TestPhase4SummaryRules
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4SummaryRules$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4SummaryRules "$mark"
fi
if [[ "$group" == all || "$group" == delivery ]]; then
  mark=$SECONDS; family TestPhase4Delivery
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Delivery$' -tags=phase4 -race -timeout=10m -p=1
  finished TestPhase4Delivery "$mark"
fi
if [[ "$group" == all || "$group" == stream ]]; then
  mark=$SECONDS; family TestPhase4Stream
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4Stream$' -tags=phase4 -race -timeout=10m -p=1
  finished TestPhase4Stream "$mark"
fi
if [[ "$group" == all || "$group" == tenant ]]; then
  mark=$SECONDS; family TestPhase4TenantMatrix
  bash scripts/go-test-nonempty.sh './testphase4/...' '^TestPhase4TenantMatrix$' -tags=phase4 -race -timeout=6m -p=1
  finished TestPhase4TenantMatrix "$mark"
fi
echo "[phase4] group=$group passed in $((SECONDS - started))s"
