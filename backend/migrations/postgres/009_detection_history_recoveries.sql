-- Append-only audit of operator-acknowledged detection loss: one row per
-- re-pinned partition of an applied detector-recover plan. Rows are inserted
-- pending before the Kafka offset commit and only ever move to applied.
-- Deliberately no foreign key to detection_partition_fences: the recovery tool
-- must never lock, reference-check or write fences.
CREATE TABLE detection_history_recoveries (
    recovery_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id text NOT NULL CHECK (length(group_id) BETWEEN 1 AND 255),
    topic text NOT NULL CHECK (length(topic) BETWEEN 1 AND 249),
    partition_id integer NOT NULL CHECK (partition_id >= 0),
    committed_offset bigint NOT NULL CHECK (committed_offset >= 0),
    log_start bigint NOT NULL CHECK (log_start >= 0),
    log_end bigint NOT NULL,
    never_evaluated_from bigint NOT NULL CHECK (never_evaluated_from >= 0),
    never_evaluated_to bigint NOT NULL,
    open_windows_discarded boolean NOT NULL,
    config_fingerprint text NOT NULL CHECK (config_fingerprint ~ '^[0-9a-f]{64}$'),
    plan_digest text NOT NULL CHECK (plan_digest ~ '^[0-9a-f]{64}$'),
    repinned_offset bigint NOT NULL,
    operator text NOT NULL CHECK (length(operator) BETWEEN 1 AND 128),
    reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024),
    status text NOT NULL CHECK (status IN ('pending', 'applied')),
    created_at timestamptz NOT NULL DEFAULT now(),
    applied_at timestamptz,
    CHECK (log_end >= log_start),
    CHECK (committed_offset < log_start),
    CHECK (never_evaluated_from <= never_evaluated_to),
    CHECK (repinned_offset >= log_start AND repinned_offset <= log_end),
    CHECK (never_evaluated_to = repinned_offset),
    CHECK ((status = 'applied') = (applied_at IS NOT NULL)),
    UNIQUE (group_id, topic, partition_id, plan_digest)
);

CREATE FUNCTION guard_detection_history_recovery_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status <> 'pending' OR NEW.status <> 'applied' OR NEW.applied_at IS NULL OR
       (NEW.recovery_id, NEW.group_id, NEW.topic, NEW.partition_id, NEW.committed_offset, NEW.log_start,
        NEW.log_end, NEW.never_evaluated_from, NEW.never_evaluated_to, NEW.open_windows_discarded,
        NEW.config_fingerprint, NEW.plan_digest, NEW.repinned_offset, NEW.operator, NEW.reason, NEW.created_at)
       IS DISTINCT FROM
       (OLD.recovery_id, OLD.group_id, OLD.topic, OLD.partition_id, OLD.committed_offset, OLD.log_start,
        OLD.log_end, OLD.never_evaluated_from, OLD.never_evaluated_to, OLD.open_windows_discarded,
        OLD.config_fingerprint, OLD.plan_digest, OLD.repinned_offset, OLD.operator, OLD.reason, OLD.created_at)
    THEN
        RAISE EXCEPTION 'detection history recoveries only move pending to applied';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER detection_history_recoveries_update BEFORE UPDATE ON detection_history_recoveries
    FOR EACH ROW EXECUTE FUNCTION guard_detection_history_recovery_update();

CREATE FUNCTION reject_detection_history_recovery_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'detection history recoveries are append-only';
END;
$$;
CREATE TRIGGER detection_history_recoveries_delete BEFORE DELETE ON detection_history_recoveries
    FOR EACH ROW EXECUTE FUNCTION reject_detection_history_recovery_delete();
CREATE TRIGGER detection_history_recoveries_truncate BEFORE TRUNCATE ON detection_history_recoveries
    FOR EACH STATEMENT EXECUTE FUNCTION reject_detection_history_recovery_delete();

---- create above / drop below ----

DROP TRIGGER detection_history_recoveries_truncate ON detection_history_recoveries;
DROP TRIGGER detection_history_recoveries_delete ON detection_history_recoveries;
DROP TRIGGER detection_history_recoveries_update ON detection_history_recoveries;
DROP FUNCTION reject_detection_history_recovery_delete();
DROP FUNCTION guard_detection_history_recovery_update();
DROP TABLE detection_history_recoveries;
