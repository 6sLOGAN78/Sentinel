#!/usr/bin/env bash
# Each exact family must pass independently; one broad regex proves too little.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

for name in \
  TestDetectionPipelineAuth TestDetectionPipelineScan TestDetectionPipelineVolume \
  TestDetectorCheckpoint TestDetectorRetention TestDetectorConfigRestart TestDetectorCapacity \
  TestDetectorMidWindowRecovery TestDetectorThirdReplicaRebalance TestDetectorCrashBoundaries \
  TestAlertWriterPipeline TestAlertWriterConflict TestAlertWriterConcurrentReplay \
  TestAlertWriterPartialStorage TestAlertWriterPoison TestAlertWorkflowReplay TestDetectorPoison \
  TestConfigLedgerHandover TestDetectorPGDependency TestAlertsSchema TestDetectionDecisionsSchema; do
  bash scripts/go-test-nonempty.sh './test/...' "^${name}$" -tags=integration -timeout 6m -p 1
done

for suffix in Visible Expiry Replay Crash Outage Owners RestoreBehindCheckpoint; do
  bash scripts/go-test-nonempty.sh './test/...' "^TestDetectorCooldown${suffix}$" -tags=integration -timeout 8m -p 1
done

for name in TestConfigWindowEnlargement TestConfigLedgerHandover; do
  bash scripts/go-test-nonempty.sh './internal/detection/...' "^${name}$" -timeout 60s
done

# One original 15m orchestration per scenario; repeat covers two plus setup.
bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenarios$' -tags=integration -timeout 16m -p 1
bash scripts/go-test-nonempty.sh './test/...' '^TestDetectionScenariosPreservedRepeat$' -tags=integration -timeout 32m -p 1
