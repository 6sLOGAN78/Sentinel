#!/usr/bin/env bash
# The single list of integration tests that the aggregate `make test-integ`
# skips because exactly one other required CI job owns them:
#   - groups chaos and scale run in the separate `phase5` CI job (this script);
#   - the phase3 list is run one anchored, nonempty selector at a time by
#     scripts/check-phase3-tests.sh in the `phase3` CI job (core and scenarios).
#     It is not runnable here; it equals the ./test/... families pinned in
#     scripts/test_phase3_guards.py.
# The Makefile derives its `-skip` regex from --skip-regex, CI runs one phase5
# group per matrix entry, and scripts/test_phase5_guards.py cross-checks all of it.
#
# Usage: bash scripts/check-phase5-tests.sh <chaos|scale>   run one group
#        bash scripts/check-phase5-tests.sh --groups        list the phase5 groups
#        bash scripts/check-phase5-tests.sh --names <group> list a group's tests
#        bash scripts/check-phase5-tests.sh --phase3-owned  list the phase3-owned tests
#        bash scripts/check-phase5-tests.sh --skip-regex    anchored union regex
#
# Both groups use only backend/test's testcontainers (TestMain); no dev stack.
# chaos: the five testcontainers-only Phase 5 spec 15 families through
#        scripts/check-chaos.sh (resource gate, per-family nonempty, -race,
#        family ceilings 8m/10m/10m/10m/6m).
# scale: 1M-row ListEvents/SummarizeEvents and the detector startup race, one
#        anchored nonempty run under -timeout 20m (same flags as the aggregate).
# Every named test must exist before anything runs and must print its own
# "--- PASS:" line afterwards; a skipped or renamed test fails the group.
set -euo pipefail

readonly groups=(chaos scale)
readonly chaos_families="chaos-broker,chaos-clickhouse,chaos-shutdown,slow-consumer,overload"
readonly chaos_tests="TestChaosKafkaBrokerRestart TestChaosClickHouseShortOutageAbsorbed TestChaosClickHouseLongOutageRestart TestChaosGracefulShutdownEventWriter TestChaosGracefulShutdownDetector TestChaosSlowConsumerLag TestChaosLagAfterRebalance TestChaosOverloadBounded"
readonly scale_tests="TestListEventsScale TestListEventsOracleEquivalence TestListEventsTenantIsolation TestSummarizeEventsScale TestSummarizeEventsOracleEquivalence TestSummarizeEventsNoConflictsSkipsCorrection TestSummarizeEventsConflictCap TestDetectorConcurrentStartup TestDetectorRestoreRetryWithoutAckedCheckpoint TestDetectorManifestUpgradePath"
readonly scale_timeout=20m
# Phase 3 ./test/... families (31 ordinary, 7 cooldown, 2 scenarios), in runner order.
readonly phase3_owned="TestDetectionPipelineAuth TestDetectionPipelineScan TestDetectionPipelineVolume TestDetectorCheckpoint TestDetectorRetention TestDetectorConfigRestart TestDetectorCapacity TestDetectorMidWindowRecovery TestDetectorThirdReplicaRebalance TestDetectorCrashBoundaries TestAlertWriterPipeline TestAlertWriterConflict TestAlertWriterConcurrentReplay TestAlertWriterPartialStorage TestAlertWriterPoison TestAlertWorkflowReplay TestDetectorPoison TestConfigLedgerHandover TestDetectorPGDependency TestAlertsSchema TestDetectionDecisionsSchema TestDetectorCooldownFixtureIsolation TestDetectorCooperativeAcquisition TestAlertWriterLockLifetime TestAlertWriterDifferingPayloadLockLifetime TestAlertWriterCancellationLockLifetime TestAlertReservationsSchema TestAlertWriterReservation TestAlertWriterReservationAmbiguity TestAlertWriterReservationRestart TestAlertWriterReservationNativeAckLoss TestDetectorCooldownVisible TestDetectorCooldownExpiry TestDetectorCooldownReplay TestDetectorCooldownCrash TestDetectorCooldownOutage TestDetectorCooldownOwners TestDetectorCooldownRestoreBehindCheckpoint TestDetectionScenarios TestDetectionScenariosPreservedRepeat"

usage() {
  echo "usage: $0 <chaos|scale> | --groups | --names <group> | --phase3-owned | --skip-regex" >&2
  exit 2
}

names_for() {
  case "$1" in
    chaos) printf '%s\n' "$chaos_tests" ;;
    scale) printf '%s\n' "$scale_tests" ;;
    *) usage ;;
  esac
}

anchored() {
  printf '^(%s)$' "$(tr ' ' '|' <<<"$1")"
}

case "${1-}" in
  --groups)
    (( $# == 1 )) || usage
    printf '%s\n' "${groups[@]}"
    exit 0
    ;;
  --names)
    (( $# == 2 )) || usage
    names_for "$2" | tr ' ' '\n'
    exit 0
    ;;
  --phase3-owned)
    (( $# == 1 )) || usage
    tr ' ' '\n' <<<"$phase3_owned"
    exit 0
    ;;
  --skip-regex)
    (( $# == 1 )) || usage
    anchored "$chaos_tests $scale_tests $phase3_owned"
    echo
    exit 0
    ;;
  chaos | scale) (( $# == 1 )) || usage ;;
  *) usage ;;
esac

group="$1"
names="$(names_for "$group")"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# Per-name existence first (backend/test's TestMain skips fixtures under -list).
for test_name in $names; do
  if ! listed="$(cd backend && go test -tags=integration -list "^${test_name}\$" ./test/ 2>&1)"; then
    echo "phase5: listing $test_name failed:" >&2
    echo "$listed" >&2
    exit 1
  fi
  if ! grep -qx "$test_name" <<<"$listed"; then
    echo "phase5: group $group: '$test_name' matches zero tests in ./test/" >&2
    exit 1
  fi
done

out="$(mktemp)"
trap 'rm -f "$out"' EXIT

started=$SECONDS
echo "[phase5] group=$group started at $(date -u +%FT%TZ)"
if [[ "$group" == chaos ]]; then
  CHAOS_FAMILIES="$chaos_families" bash scripts/check-chaos.sh 2>&1 | tee "$out"
else
  bash scripts/go-test-nonempty.sh './test/...' "$(anchored "$names")" \
    -tags=integration -timeout="$scale_timeout" -p=1 2>&1 | tee "$out"
fi

missing=0
for test_name in $names; do
  if ! grep -q -- "--- PASS: ${test_name} (" "$out"; then
    echo "phase5: group $group: $test_name did not report --- PASS" >&2
    missing=1
  fi
done
if (( missing )); then
  exit 1
fi
echo "[phase5] group=$group passed in $((SECONDS - started))s"
