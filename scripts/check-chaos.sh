#!/usr/bin/env bash
# Named spec §15 failure suite. Every family runs independently, nonempty
# (scripts/go-test-nonempty.sh) and under a finite ceiling, after the read-only
# resource gate. Only testcontainers-owned containers are used; this script
# never calls docker compose and never touches a running stack.
#
# Usage: bash scripts/check-chaos.sh            (all families)
#        CHAOS_FAMILIES=chaos-broker,dlq bash scripts/check-chaos.sh
#
# sse-reconnect runs backend/testphase4 (phase4 tag), which needs a healthy dev
# stack (make dev-up), exactly like scripts/check-phase4-tests.sh.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# Resource gate first: below the floor nothing is compiled or started.
bash scripts/check-resources.sh || exit $?

# name|packages|exact test names (each anchored alone)|build tag|timeout
families=(
  "chaos-broker|./test/|TestChaosKafkaBrokerRestart|integration|8m"
  "chaos-clickhouse|./test/|TestChaosClickHouseShortOutageAbsorbed TestChaosClickHouseLongOutageRestart|integration|10m"
  "chaos-shutdown|./test/|TestChaosGracefulShutdownEventWriter TestChaosGracefulShutdownDetector|integration|10m"
  "writer-restart|./test/|TestWriterCrashRestart TestWriterCommitAfterInsert TestWriterInsertFailureNoCommit TestDetectorCrashBoundaries|integration|12m"
  "dlq|./test/|TestWriterPoisonToDLQ TestWriterDLQFailureNoCommit TestDetectorPoison TestAlertWriterPoison|integration|10m"
  "rate-limit|./test/|TestRateLimit.* TestIngestShedsUnderLoad|integration|8m"
  "sse-reconnect|./testphase4/...|TestPhase4Stream TestPhase4Delivery|phase4|10m"
)

known=()
for entry in "${families[@]}"; do
  known+=("${entry%%|*}")
done

selected=()
if [[ -n "${CHAOS_FAMILIES:-}" ]]; then
  IFS=, read -r -a requested <<<"$CHAOS_FAMILIES"
  for name in "${requested[@]}"; do
    match=0
    for k in "${known[@]}"; do
      [[ "$name" == "$k" ]] && match=1
    done
    if (( ! match )); then
      echo "check-chaos: unknown family '$name'; known: ${known[*]}" >&2
      exit 2
    fi
    selected+=("$name")
  done
else
  selected=("${known[@]}")
fi

is_selected() {
  local s
  for s in "${selected[@]}"; do
    [[ "$s" == "$1" ]] && return 0
  done
  return 1
}

regex_for() {
  printf '^(%s)$' "$(tr ' ' '|' <<<"$1")"
}

# Per-name nonempty guard: every alternative must list at least one test.
# (backend/test's TestMain skips its fixtures under -list.)
cd "$root/backend"
for entry in "${families[@]}"; do
  IFS='|' read -r name packages names tag timeout <<<"$entry"
  is_selected "$name" || continue
  for test_name in $names; do
    # shellcheck disable=SC2086
    if ! listed="$(go test -tags="$tag" -list "^${test_name}\$" $packages 2>&1)"; then
      echo "check-chaos: listing $test_name in $packages failed:" >&2
      echo "$listed" >&2
      exit 1
    fi
    if ! grep -q '^Test' <<<"$listed"; then
      echo "check-chaos: family $name: '$test_name' matches zero tests in $packages" >&2
      exit 1
    fi
  done
done
cd "$root"

if [[ "${CHECK_RESOURCES_TEST:-}" == 1 ]]; then
  echo "check-chaos: resource-gate test seam is active; refusing to start containers" >&2
  exit 5
fi

started=$SECONDS
results=()
status=0
for entry in "${families[@]}"; do
  IFS='|' read -r name packages names tag timeout <<<"$entry"
  is_selected "$name" || continue
  regex="$(regex_for "$names")"
  mark=$SECONDS
  echo "[chaos] family $name started at $(date -u +%FT%TZ) regex=$regex timeout=$timeout"
  set +e
  bash scripts/go-test-nonempty.sh "$packages" "$regex" -tags="$tag" -timeout="$timeout" -p=1 -race
  code=$?
  set -e
  verdict=PASS
  if (( code != 0 )); then
    verdict=FAIL
    status=1
  fi
  results+=("$(printf '%-17s %-4s exit=%-3s %5ss  %s' "$name" "$verdict" "$code" "$((SECONDS - mark))" "$timeout")")
done

echo
echo "[chaos] family            result exit  secs  ceiling"
for line in "${results[@]}"; do
  echo "[chaos] $line"
done
echo "[chaos] total $((SECONDS - started))s finished $(date -u +%FT%TZ)"
exit "$status"
