-- Operational recovery metadata: retain replayable rows, including suppressions.
-- No wall-clock TTL or automatic cleanup is safe for source replay.
CREATE TABLE detection_partition_fences (
    group_id text NOT NULL CHECK (length(group_id) BETWEEN 1 AND 255),
    topic text NOT NULL CHECK (length(topic) BETWEEN 1 AND 249),
    partition_id integer NOT NULL CHECK (partition_id >= 0),
    incarnation text NOT NULL CHECK (incarnation ~ '^[0-9a-f]{64}$'),
    member_id text NOT NULL CHECK (length(member_id) BETWEEN 1 AND 1024),
    generation integer NOT NULL CHECK (generation >= 0),
    assignment_token bigint NOT NULL CHECK (assignment_token > 0),
    config_fingerprint text NOT NULL CHECK (config_fingerprint ~ '^[0-9a-f]{64}$'),
    frontier_offset bigint NOT NULL DEFAULT -1 CHECK (frontier_offset >= -1),
    frontier_ordinal integer NOT NULL DEFAULT -1 CHECK (frontier_ordinal BETWEEN -1 AND 3),
    drained boolean NOT NULL DEFAULT false,
    sealed_through timestamptz,
    PRIMARY KEY (group_id, topic, partition_id),
    CHECK ((frontier_offset = -1) = (frontier_ordinal = -1))
);

CREATE TABLE detection_cooldown_cursors (
    group_id text NOT NULL,
    config_fingerprint text NOT NULL CHECK (config_fingerprint ~ '^[0-9a-f]{64}$'),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    rule_id text NOT NULL CHECK (length(rule_id) BETWEEN 1 AND 64),
    entity_components text NOT NULL CHECK (length(entity_components) BETWEEN 2 AND 4096),
    topic text NOT NULL,
    partition_id integer NOT NULL,
    max_candidate_time timestamptz,
    next_eligible_at timestamptz,
    PRIMARY KEY (group_id, config_fingerprint, tenant_id, rule_id, entity_components),
    FOREIGN KEY (group_id, topic, partition_id)
        REFERENCES detection_partition_fences (group_id, topic, partition_id),
    CHECK ((max_candidate_time IS NULL) = (next_eligible_at IS NULL))
);

CREATE TABLE detection_decisions (
    alert_id text PRIMARY KEY CHECK (alert_id ~ '^[0-9a-f]{64}$'),
    group_id text NOT NULL,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    rule_id text NOT NULL CHECK (length(rule_id) BETWEEN 1 AND 64),
    rule_version integer NOT NULL CHECK (rule_version >= 1),
    entity_components text NOT NULL CHECK (length(entity_components) BETWEEN 2 AND 4096),
    config_fingerprint text NOT NULL CHECK (config_fingerprint ~ '^[0-9a-f]{64}$'),
    allowed boolean NOT NULL,
    trigger_occurred_at timestamptz NOT NULL,
    effective_clock timestamptz NOT NULL CHECK (effective_clock >= trigger_occurred_at),
    topic text NOT NULL,
    partition_id integer NOT NULL,
    source_offset bigint NOT NULL CHECK (source_offset >= 0),
    rule_ordinal integer NOT NULL CHECK (rule_ordinal BETWEEN 0 AND 3),
    canonical_payload bytea NOT NULL CHECK (octet_length(canonical_payload) BETWEEN 1 AND 65536),
    immutable_fingerprint text NOT NULL CHECK (immutable_fingerprint ~ '^[0-9a-f]{64}$'),
    decision_generation integer NOT NULL CHECK (decision_generation >= 0),
    UNIQUE (group_id, topic, partition_id, source_offset, rule_ordinal),
    FOREIGN KEY (group_id, topic, partition_id)
        REFERENCES detection_partition_fences (group_id, topic, partition_id)
);

CREATE FUNCTION reject_detection_decision_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'detection decisions are immutable';
END;
$$;
CREATE TRIGGER detection_decisions_immutable BEFORE UPDATE ON detection_decisions
    FOR EACH ROW EXECUTE FUNCTION reject_detection_decision_update();

---- create above / drop below ----

DROP TABLE detection_decisions;
DROP FUNCTION reject_detection_decision_update();
DROP TABLE detection_cooldown_cursors;
DROP TABLE detection_partition_fences;
