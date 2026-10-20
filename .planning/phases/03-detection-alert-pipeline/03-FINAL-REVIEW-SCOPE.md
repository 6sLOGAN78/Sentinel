# Phase 3 remaining source review scope

Frozen head: `62099b20c2a98a1966a0d6f90e5b1d653fc1e855`.
Reliable phase baseline: `7b77246e52412b3ca331f726d1163875daa5a609`.

This explicit standard-depth scope contains every changed non-planning source,
test, CI, configuration, dependency or documentation artifact not listed in the
original 41-file production review or the complete 16-file reservation R2.
Those historical reviews and R1 repair dispositions supply complementary coverage;
do not claim they reviewed this remaining scope. Planning/user .serena files are
excluded. Large scope requires truthful coverage and narrower follow-up if needed.
No optional structural pre-pass is enabled.

Output: new `03-REVIEW-FINAL-DELTA.md`; preserve historical reports. Review source
read-only at the frozen head using git show/diff. No tests, compilation, live
operations, staging, commits or edits outside that new report. Root handles the
canonical aggregate report after this review. Final execution proof remains
separate and pending; later acceptance evidence updates are not source defects.

## Exact files (73)

- .env.example
- .github/workflows/ci.yml
- Makefile
- README.md
- backend/go.mod
- backend/internal/alertwriter/identity_test.go
- backend/internal/app/app_test.go
- backend/internal/config/config_test.go
- backend/internal/config/detection_pg_test.go
- backend/internal/config/detection_test.go
- backend/internal/contracts/scenario_identity_test.go
- backend/internal/detection/auth_test.go
- backend/internal/detection/bounds_test.go
- backend/internal/detection/candidate_test.go
- backend/internal/detection/checkpoint_test.go
- backend/internal/detection/config_test.go
- backend/internal/detection/evidence_test.go
- backend/internal/detection/loader_test.go
- backend/internal/detection/replay_test.go
- backend/internal/detection/scan_test.go
- backend/internal/detection/transition_test.go
- backend/internal/detection/volume_test.go
- backend/internal/detection/window_test.go
- backend/internal/detector/decision_test.go
- backend/internal/detector/failpoint_test.go
- backend/internal/detector/readiness_test.go
- backend/internal/detector/worker_test.go
- backend/internal/httpx/httpx_test.go
- backend/internal/kafkax/alerts_test.go
- backend/internal/kafkax/detector_consumer_test.go
- backend/internal/opshttp/metrics_test.go
- backend/internal/storage/clickhouse/alerts_test.go
- backend/internal/storage/postgres/detection_decisions_test.go
- backend/test/alerts_schema_test.go
- backend/test/alertwriter_conflict_test.go
- backend/test/alertwriter_dlq_test.go
- backend/test/alertwriter_partial_test.go
- backend/test/alertwriter_pipeline_test.go
- backend/test/detection_decisions_schema_test.go
- backend/test/detector_capacity_test.go
- backend/test/detector_checkpoint_test.go
- backend/test/detector_config_test.go
- backend/test/detector_cooldown_test.go
- backend/test/detector_dlq_test.go
- backend/test/detector_pg_test.go
- backend/test/detector_pipeline_test.go
- backend/test/detector_rebalance_test.go
- backend/test/detector_recovery_test.go
- backend/test/detector_scan_test.go
- backend/test/detector_volume_test.go
- backend/test/lifecycle_test.go
- backend/test/main_test.go
- backend/test/readyz_test.go
- backend/test/recovery_test.go
- backend/test/scenarios_pipeline_test.go
- backend/test/scenarios_preserved_test.go
- backend/tools/check-alerts/main_test.go
- backend/tools/detection-state/main_test.go
- docs/architecture.md
- docs/decisions/0007-event-time-state-pin-recovery.md
- docs/decisions/0008-alert-list-source-and-conflicts.md
- docs/demo.md
- docs/operations.md
- docs/rules.md
- docs/threat-model.md
- generator/tests/test_live_schedule.py
- generator/tests/test_scenarios.py
- scripts/check-phase3-tests.sh
- scripts/check-stack.sh
- scripts/test_phase3_guards.py
- scripts/test_seed_scenarios.py
- scripts/test_stack_config.py
- testdata/detection/scenarios.json
