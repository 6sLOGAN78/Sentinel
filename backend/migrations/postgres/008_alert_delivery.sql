-- This relation lock waits for pre-trigger writers and excludes new workflow
-- inserts until trigger installation and the preexisting seed commit together.
LOCK TABLE alert_workflows IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE alert_delivery_pending (
    alert_id text PRIMARY KEY REFERENCES alert_workflows(alert_id),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$')
);

-- next_seq is the last committed allocation (zero before the first delivery).
-- An ordinary transactional row, deliberately not an identity or sequence.
CREATE TABLE alert_delivery_counter (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    next_seq bigint NOT NULL CHECK (next_seq >= 0)
);
INSERT INTO alert_delivery_counter (singleton,next_seq) VALUES (true,0);

CREATE TABLE alert_deliveries (
    delivery_seq bigint PRIMARY KEY CHECK (delivery_seq > 0),
    alert_id text NOT NULL UNIQUE REFERENCES alert_workflows(alert_id),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$')
);
CREATE INDEX alert_deliveries_tenant_seq_idx ON alert_deliveries (tenant_id,delivery_seq);

CREATE FUNCTION queue_alert_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO alert_delivery_pending (alert_id,tenant_id)
    VALUES (NEW.alert_id,NEW.tenant_id) ON CONFLICT (alert_id) DO NOTHING;
    RETURN NEW;
END;
$$;
CREATE TRIGGER alert_workflow_delivery AFTER INSERT ON alert_workflows
FOR EACH ROW EXECUTE FUNCTION queue_alert_delivery();

INSERT INTO alert_delivery_pending (alert_id,tenant_id)
SELECT alert_id,tenant_id FROM alert_workflows ON CONFLICT (alert_id) DO NOTHING;

---- create above / drop below ----

DROP TRIGGER alert_workflow_delivery ON alert_workflows;
DROP FUNCTION queue_alert_delivery();
DROP TABLE alert_deliveries;
DROP TABLE alert_delivery_counter;
DROP TABLE alert_delivery_pending;
