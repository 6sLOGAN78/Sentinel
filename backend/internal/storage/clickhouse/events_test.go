package clickhouse_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventsMigrationHasReadDedupeSchema(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../migrations/clickhouse/002_events.sql")
	require.NoError(t, err)
	ddl := string(raw)
	for _, clause := range []string{
		"ENGINE = ReplacingMergeTree",
		"PARTITION BY toYYYYMM(occurred_at)",
		"ORDER BY (tenant_id, occurred_at, event_id)",
		"TTL toDateTime(occurred_at) + INTERVAL 7 DAY",
		"INDEX idx_event_id event_id TYPE bloom_filter(0.01) GRANULARITY 4",
	} {
		require.Contains(t, ddl, clause)
	}
	require.NotContains(t, ddl, "non_replicated_deduplication_window")
	require.NotContains(t, ddl, "OPTIMIZE")
}
