CREATE TABLE IF NOT EXISTS events (
    tenant_id LowCardinality(String),
    event_id String,
    occurred_at DateTime64(3, 'UTC'),
    received_at DateTime64(3, 'UTC'),
    source LowCardinality(String),
    event_type LowCardinality(String),
    source_ip Nullable(String),
    destination_ip Nullable(String),
    destination_port Nullable(UInt16),
    protocol LowCardinality(Nullable(String)),
    principal Nullable(String),
    action LowCardinality(Nullable(String)),
    outcome LowCardinality(Nullable(String)),
    attributes_json String,
    INDEX idx_event_id event_id TYPE bloom_filter(0.01) GRANULARITY 4
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, occurred_at, event_id)
TTL toDateTime(occurred_at) + INTERVAL 7 DAY;
