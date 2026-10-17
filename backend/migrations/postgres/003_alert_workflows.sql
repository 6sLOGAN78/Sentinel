CREATE TABLE alert_workflows (
    alert_id text PRIMARY KEY CHECK (alert_id ~ '^[0-9a-f]{64}$'),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved')),
    assignee text,
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    seq bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    rule_id text NOT NULL,
    rule_version bigint NOT NULL CHECK (rule_version >= 1),
    severity text NOT NULL CHECK (severity IN ('low','medium','high','critical')),
    entity_key text NOT NULL,
    summary text NOT NULL,
    event_count bigint NOT NULL CHECK (event_count >= 1),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL CHECK (window_end > window_start),
    immutable_fingerprint text NOT NULL CHECK (immutable_fingerprint ~ '^[0-9a-f]{64}$')
);
CREATE INDEX alert_workflows_tenant_created_idx ON alert_workflows (tenant_id, created_at, alert_id);
CREATE INDEX alert_workflows_tenant_status_created_idx ON alert_workflows (tenant_id, status, created_at, alert_id);
CREATE INDEX alert_workflows_tenant_seq_idx ON alert_workflows (tenant_id, seq);

---- create above / drop below ----

DROP TABLE alert_workflows;
