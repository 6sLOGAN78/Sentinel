# Phase 3 production review scope

Explicit production-only scope for deep review while Plan 03-14 validates test/CI changes. This is not the complete phase review: final test/CI/docs review and aggregate report remain pending after 03-14.

Baseline: 7b77246e52412b3ca331f726d1163875daa5a609 (before first 03-01 RED). Production frozen after 03-13; Plan 03-14 has no production ownership.

- backend/cmd/alert-writer/main.go
- backend/cmd/detector/main.go
- backend/internal/alertwriter/identity.go
- backend/internal/alertwriter/poison.go
- backend/internal/alertwriter/writer.go
- backend/internal/config/config.go
- backend/internal/detection/bounds.go
- backend/internal/detection/candidate.go
- backend/internal/detection/checkpoint.go
- backend/internal/detection/config.go
- backend/internal/detection/cooldown.go
- backend/internal/detection/evidence.go
- backend/internal/detection/reducer.go
- backend/internal/detection/scan.go
- backend/internal/detection/transition.go
- backend/internal/detection/volume.go
- backend/internal/detector/failpoint.go
- backend/internal/detector/failpoint_integration.go
- backend/internal/detector/poison.go
- backend/internal/detector/worker.go
- backend/internal/kafkax/alert_consumer.go
- backend/internal/kafkax/alerts.go
- backend/internal/kafkax/detector_consumer.go
- backend/internal/opshttp/metrics.go
- backend/internal/opshttp/server.go
- backend/internal/storage/clickhouse/alerts.go
- backend/internal/storage/postgres/alerts.go
- backend/internal/storage/postgres/detection_decisions.go
- backend/tools/check-alerts/main.go
- backend/tools/detection-state/catchup.go
- backend/tools/detection-state/main.go
- backend/migrations/postgres/003_alert_workflows.sql
- backend/migrations/postgres/004_detection_decisions.sql
- backend/migrations/postgres/005_detection_config.sql
- backend/migrations/clickhouse/003_alerts.sql
- config/rules.yaml
- deployments/compose/docker-compose.yml
- generator/src/sentinel_generator/cli.py
- generator/src/sentinel_generator/live_schedule.py
- generator/src/sentinel_generator/scenarios.py
- scripts/seed_scenarios.py

