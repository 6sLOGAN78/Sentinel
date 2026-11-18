# Architecture decisions

Short MADR-style records. Each has Status, Context, Decision and Consequences sections, checked by `scripts/check-adrs.sh`.

| ADR | Title |
| --- | --- |
| [0001](0001-repo-layout-and-binaries.md) | Repo layout and binaries |
| [0002](0002-event-id-policy.md) | Event ID policy |
| [0003](0003-atomic-batch-semantics.md) | Atomic batch semantics |
| [0004](0004-metrics-label-policy.md) | Metrics label policy |
| [0005](0005-redis-failure-policy.md) | Redis failure policy |
| [0006](0006-alert-storage-split.md) | Alert storage split |
| [0007](0007-event-time-state-pin-recovery.md) | Event-time state pins and replay recovery |
| [0008](0008-alert-list-source-and-conflicts.md) | Alert list authority and immutable conflicts |
| [0009](0009-gap-safe-alert-delivery.md) | Gap-safe alert delivery projection |
| [0012](0012-detector-history-recovery.md) | Detector expired-history recovery |
| [0013](0013-non-idempotent-bounded-producers.md) | Non-idempotent bounded Kafka producers |
| [0014](0014-operator-tenant-allowlist.md) | Operator tenant allowlist for platform-wide views |
