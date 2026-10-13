CREATE TABLE producer_keys (
    key_id text PRIMARY KEY CHECK (key_id ~ '^[a-z2-7]{12}$'),
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
    secret_hash bytea NOT NULL CHECK (octet_length(secret_hash) = 32),
    scopes text[] NOT NULL CHECK (cardinality(scopes) > 0 AND scopes <@ ARRAY['events:read','events:write']::text[] AND array_position(scopes, NULL) IS NULL),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX producer_keys_tenant_idx ON producer_keys (tenant_id);
CREATE UNIQUE INDEX producer_keys_active_name_idx ON producer_keys (tenant_id, name) WHERE revoked_at IS NULL;

---- create above / drop below ----

DROP TABLE producer_keys;
