-- Keep the semantic manifest outside Kafka's 1024-byte checkpoint envelope.
-- Nullable columns preserve existing operational history on upgrade. The first
-- unchanged-config registration binds it; changed-config adoption fails closed.
ALTER TABLE detection_partition_fences
    ADD COLUMN rules_manifest jsonb,
    ADD COLUMN drain_checkpoint jsonb,
    ADD COLUMN handover_checkpoint jsonb;

ALTER TABLE detection_partition_fences
    ADD CONSTRAINT detection_rules_manifest_bound CHECK
        (rules_manifest IS NULL OR octet_length(rules_manifest::text) <= 8192),
    ADD CONSTRAINT detection_drain_checkpoint_bound CHECK
        (drain_checkpoint IS NULL OR octet_length(drain_checkpoint::text) <= 8192),
    ADD CONSTRAINT detection_handover_checkpoint_bound CHECK
        (handover_checkpoint IS NULL OR octet_length(handover_checkpoint::text) <= 8192);

---- create above / drop below ----

ALTER TABLE detection_partition_fences
    DROP COLUMN handover_checkpoint,
    DROP COLUMN drain_checkpoint,
    DROP COLUMN rules_manifest;
