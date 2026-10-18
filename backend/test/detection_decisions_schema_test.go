//go:build integration

package test_test

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/database"
)

func TestDetectionDecisionsSchema(t *testing.T) {
	require.NoError(t, database.Migrate(t.Context(), pgCfg, zerolog.Nop()))
	db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
	require.NoError(t, err)
	defer db.Close()
	for _, table := range []string{"detection_partition_fences", "detection_cooldown_cursors", "detection_decisions"} {
		var relation *string
		require.NoError(t, db.Pool.QueryRow(t.Context(), "SELECT to_regclass($1)::text", table).Scan(&relation))
		require.NotNil(t, relation, "durable cooldown operational table %s is absent", table)
	}
	var unique bool
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_indexes
		WHERE tablename='detection_decisions' AND indexdef LIKE '%UNIQUE% (alert_id)%')`).Scan(&unique))
	require.True(t, unique, "alert_id must be globally unique in immutable decision ledger")
	for _, column := range []string{"generation", "member_id", "assignment_token", "incarnation",
		"frontier_offset", "frontier_ordinal"} {
		var count int
		require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM information_schema.columns
			WHERE table_name='detection_partition_fences' AND column_name=$1`, column).Scan(&count))
		require.Equal(t, 1, count, "missing durable fence column %s", column)
	}
	for table, fragments := range map[string][]string{
		"detection_partition_fences": {"UNIQUE INDEX", "(group_id, topic, partition_id)"},
		"detection_cooldown_cursors": {"UNIQUE INDEX",
			"(group_id, config_fingerprint, tenant_id, rule_id, entity_components)"},
		"detection_decisions": {"UNIQUE INDEX", "(group_id, topic, partition_id, source_offset, rule_ordinal)"},
	} {
		rows, queryErr := db.Pool.Query(t.Context(), "SELECT indexdef FROM pg_indexes WHERE tablename=$1", table)
		require.NoError(t, queryErr)
		var definitions []string
		for rows.Next() {
			var definition string
			require.NoError(t, rows.Scan(&definition))
			definitions = append(definitions, definition)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		for _, fragment := range fragments {
			require.Contains(t, strings.Join(definitions, "\n"), fragment)
		}
	}
	var immutable bool
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_trigger
		WHERE tgrelid='detection_decisions'::regclass AND tgname='detection_decisions_immutable' AND tgenabled='O')`).
		Scan(&immutable))
	require.True(t, immutable, "immutable rows must reject mutation")
}
