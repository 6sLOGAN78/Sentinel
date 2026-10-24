CREATE TABLE alert_status_audit (
    alert_id text NOT NULL REFERENCES alert_workflows(alert_id),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    version bigint NOT NULL CHECK (version >= 2),
    actor text NOT NULL CHECK (length(actor) > 0),
    changed_at timestamptz NOT NULL,
    from_status text NOT NULL,
    to_status text NOT NULL,
    PRIMARY KEY (alert_id, version),
    CHECK ((from_status = 'open' AND to_status = 'acknowledged') OR
           (from_status = 'acknowledged' AND to_status = 'resolved'))
);
CREATE INDEX alert_status_audit_tenant_alert_version_idx
    ON alert_status_audit (tenant_id, alert_id, version);

---- create above / drop below ----

DROP TABLE alert_status_audit;
