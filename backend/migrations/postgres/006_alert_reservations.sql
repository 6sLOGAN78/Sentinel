-- Permanent winner authority survives uncertain sink outcomes and source replay.
-- It is independent of workflow creation and has no safe automatic expiry.
CREATE TABLE alert_reservations (
    alert_id text PRIMARY KEY CHECK (alert_id ~ '^[0-9a-f]{64}$'),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    immutable_fingerprint text NOT NULL CHECK (immutable_fingerprint ~ '^[0-9a-f]{64}$'),
    canonical_payload bytea NOT NULL CHECK (octet_length(canonical_payload) BETWEEN 1 AND 1048576),
    reserved_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION reject_alert_reservation_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'alert reservations are immutable';
END;
$$;
CREATE TRIGGER alert_reservations_immutable BEFORE UPDATE OR DELETE ON alert_reservations
    FOR EACH ROW EXECUTE FUNCTION reject_alert_reservation_mutation();

---- create above / drop below ----

-- Destructive maintenance only; never use rollback for writer failure recovery.
DROP TABLE alert_reservations;
DROP FUNCTION reject_alert_reservation_mutation();
