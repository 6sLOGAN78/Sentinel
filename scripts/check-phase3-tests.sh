#!/usr/bin/env bash
# Each exact family must pass independently; one broad regex proves too little.
set -euo pipefail
group="${1-all}"
if (( $# > 1 )) || [[ "$group" != all && "$group" != core && "$group" != scenarios ]]; then
  echo "usage: $0 [all|core|scenarios]" >&2
  exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ "$group" != scenarios ]]; then
  for name in \
    TestDetectionPipelineAuth TestDetectionPipelineScan TestDetectionPipelineVolume \
    TestDetectorCheckpoint TestDetectorRetention TestDetectorConfigRestart TestDetectorCapacity \
    TestDetectorMidWindowRecovery TestDetectorThirdReplicaRebalance TestDetectorCrashBoundaries \
    TestAlertWriterPipeline TestAlertWriterConflict TestAlertWriterConcurrentReplay \
    TestAlertWriterPartialStorage TestAlertWriterPoison TestAlertWorkflowReplay TestDetectorPoison \
    TestConfigLedgerHandover TestDetectorPGDependency TestAlertsSchema TestDetectionDecisionsSchema \
    TestDetectorCooldownFixtureIsolation TestDetectorCooperativeAcquisition \
    TestAlertWriterLockLifetime TestAlertWriterDifferingPayloadLockLifetime \
    TestAlertWriterCancellationLockLifetime \
    TestAlertReservationsSchema TestAlertWriterReservation TestAlertWriterReservationAmbiguity \
    TestAlertWriterReservationRestart TestAlertWriterReservationNativeAckLoss; do
    bash scripts/go-test-nonempty.sh './test/...' "^${name}$" -tags=integration -timeout 6m -p 1
  done

  for suffix in Visible Expiry Replay Crash Outage Owners RestoreBehindCheckpoint; do
    bash scripts/go-test-nonempty.sh './test/...' "^TestDetectorCooldown${suffix}$" -tags=integration -timeout 8m -p 1
  done

  for name in TestConfigWindowEnlargement TestConfigLedgerHandover; do
    bash scripts/go-test-nonempty.sh './internal/detection/...' "^${name}$" -timeout 60s
  done
  bash scripts/go-test-nonempty.sh './internal/kafkax/...' '^TestDetectorCooperativeOffsetFetch$' -timeout 60s
  bash scripts/go-test-nonempty.sh './internal/config/...' '^TestLoadAlertWriterResourceAdmission$' -race -timeout 60s
  for name in TestAlertWriterReservationProtocol TestAlertWriterFailpointBuildBoundary; do
    bash scripts/go-test-nonempty.sh './internal/alertwriter/...' "^${name}$" -race -timeout 60s
  done
fi

# One original 15m orchestration per scenario; repeat covers two plus setup.
if [[ "$group" != core ]]; then
  bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$' -tags=integration -timeout 16m -p 1
  bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenariosPreservedRepeat$' -tags=integration -timeout 32m -p 1
fi
