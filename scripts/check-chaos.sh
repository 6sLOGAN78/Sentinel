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
# stack (make dev-up), exactly like scripts/check-phase4-tests.sh. It must be an
# isolated compose project: COMPOSE_PROJECT_NAME=sentinel-chaoscheck make dev-up,
# then run this script with the same COMPOSE_PROJECT_NAME. The protected
# projects sentinel and sentinel-p4-demo-* are refused (exit 6).
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
  "slow-consumer|./test/|TestChaosSlowConsumerLag TestChaosLagAfterRebalance|integration|10m"
  "overload|./test/|TestChaosOverloadBounded|integration|6m"
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

# Compose-backed families reach the dev stack through the host ports in the root
# .env. The compose file is named "sentinel", so without an override they would
# hit the protected stack. Require an explicit, unprotected COMPOSE_PROJECT_NAME
# and prove every dependency port is published by that project's containers.
compose_families=(sse-reconnect)
readonly guard_exit=6

project_guard() {
  local project="${COMPOSE_PROJECT_NAME:-}"
  if [[ -z "$project" ]]; then
    echo "check-chaos: $1 needs a dev stack on an isolated project; set COMPOSE_PROJECT_NAME" \
      "(for example sentinel-chaoscheck) and run make dev-up with it" >&2
    exit "$guard_exit"
  fi
  if [[ "$project" == sentinel || "$project" == sentinel-p4-demo-* ]]; then
    echo "check-chaos: refusing protected compose project '$project'; use an isolated one" >&2
    exit "$guard_exit"
  fi
  local env_file="$root/.env"
  if [[ "${CHECK_RESOURCES_TEST:-}" == 1 && -n "${CHECK_CHAOS_ENV_FILE:-}" ]]; then
    env_file="$CHECK_CHAOS_ENV_FILE"
  fi
  if [[ ! -f "$env_file" ]]; then
    echo "check-chaos: $env_file missing; run make dev-up with COMPOSE_PROJECT_NAME=$project" >&2
    exit "$guard_exit"
  fi
  local key value port owner ports=()
  for key in SENTINEL_DATABASE.PORT SENTINEL_REDIS.ADDRESS SENTINEL_KAFKA.BROKERS \
    SENTINEL_CLICKHOUSE.NATIVE_ADDRESS; do
    value="$(grep -E "^${key//./\\.}=" "$env_file" | tail -n 1 | cut -d= -f2-)"
    port="${value##*:}"
    if ! [[ "$port" =~ ^[0-9]+$ ]]; then
      echo "check-chaos: cannot read a port for $key from $env_file" >&2
      exit "$guard_exit"
    fi
    ports+=("$port")
  done
  for port in "${ports[@]}"; do
    owner="$(docker ps --filter "publish=$port" --filter status=running \
      --format '{{.Label "com.docker.compose.project"}}' | sort -u)"
    if [[ -z "$owner" ]]; then
      echo "check-chaos: no running container publishes port $port;" \
        "run make dev-up with COMPOSE_PROJECT_NAME=$project" >&2
      exit "$guard_exit"
    fi
    if [[ "$owner" != "$project" ]]; then
      echo "check-chaos: port $port is published by project '${owner//$'\n'/,}', not '$project'" >&2
      exit "$guard_exit"
    fi
  done
  echo "check-chaos: $1 targets compose project '$project' (ports ${ports[*]})"
}

for name in "${compose_families[@]}"; do
  if is_selected "$name"; then
    project_guard "$name"
  fi
done

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
