CREATE TABLE IF NOT EXISTS alerts (
    tenant_id LowCardinality(String),
    alert_id String,
    created_at DateTime64(3, 'UTC'),
    rule_id LowCardinality(String),
    rule_version Int64,
    severity LowCardinality(String),
    window_start DateTime64(3, 'UTC'),
    window_end DateTime64(3, 'UTC'),
    entity_key String,
    summary String,
    event_count Int64,
    sample_event_ids Array(String),
    immutable_fingerprint String,
    canonical_payload String
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (tenant_id, alert_id);
