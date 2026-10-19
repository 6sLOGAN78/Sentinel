//go:build integration

package test_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/detection"
)

func TestDetectorConfigRestart(t *testing.T) {
	f := newCooldownFixture(t, false)
	f.start = f.start.Truncate(10 * time.Minute)
	f.send(t, 299)
	original := f.alerts(t)
	require.Len(t, original, 1)
	require.Zero(t, f.process.terminate(t))
	f.process = startProc(t, "detector", "", f.env)
	replayed := f.alerts(t)
	if len(replayed) == 0 {
		replayed = f.alerts(t)
	}
	require.Len(t, replayed, 1)
	require.Equal(t, original[0].Value, replayed[0].Value)
	f.waitSource(t)
	require.Zero(t, f.process.terminate(t))
	raw, err := os.ReadFile("../../config/rules.yaml")
	require.NoError(t, err)
	changed := strings.Replace(string(raw), "version: 1", "version: 2", 1)
	changed = strings.Replace(changed, "window: 300s", "window: 600s", 1)
	path := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	env := append(without(f.env, "SENTINEL_DETECTOR.RULES_PATH="), "SENTINEL_DETECTOR.RULES_PATH="+path)
	active := startProc(t, "detector", "", env)
	require.NotZero(t, active.requireExit(t, 10*time.Second), "increased version cannot adopt active old windows")
	f.process = startProc(t, "detector", "", f.env)
	f.send(t, 301) // durable old suppression must also survive the handover.
	marker := canonicalWriterEvents(1, f.start.Add(630*time.Second))
	marker[0].EventType, marker[0].SourceIP = "auth.success", &f.ip
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), marker, uuid.NewString()))
	f.offset++
	f.waitSource(t)
	before := brokerCheckpoint(t, f.admin, f.kafka)
	var old detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(before.Metadata), &old))
	require.True(t, old.Drained)
	require.Equal(t, old.ProcessedNext, before.At)
	require.Zero(t, f.process.terminate(t))
	// Equal-version drift fails even when the source is drained.
	drift := strings.Replace(changed, "version: 2", "version: 1", 1)
	require.NoError(t, os.WriteFile(path, []byte(drift), 0o600))
	bad := startProc(t, "detector", "", env)
	require.NotZero(t, bad.requireExit(t, 10*time.Second))
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	f.process = startProc(t, "detector", "", env)
	require.Eventually(t, func() bool {
		cp := brokerCheckpoint(t, f.admin, f.kafka)
		var m detection.Metadata
		return json.Unmarshal([]byte(cp.Metadata), &m) == nil && m.RuleVersions["AUTH_BRUTE_FORCE"] == 2 &&
			m.RuleEffectiveFrom["AUTH_BRUTE_FORCE"] == f.start.Add(600*time.Second).UnixMilli()
	}, 12*time.Second, 100*time.Millisecond, "real drained restart must publish authorized gates: %s", f.process.output.String())
	var fingerprint string
	var drained bool
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT config_fingerprint,drained FROM detection_partition_fences
		WHERE group_id=$1 AND topic=$2 AND partition_id=0`, f.kafka.DetectorGroup, f.kafka.EventsTopic).
		Scan(&fingerprint, &drained))
	var current detection.Metadata
	require.NoError(t, json.Unmarshal([]byte(brokerCheckpoint(t, f.admin, f.kafka).Metadata), &current))
	require.Equal(t, current.ConfigFingerprint, fingerprint)
	require.True(t, drained)
	require.Len(t, current.RuleEffectiveFrom, 4)
	f.stored(t, 1, 1)
	require.Zero(t, f.process.terminate(t))
	f.process = startProc(t, "detector", "", env)
	// The gate and config survive an unchanged, idle new-generation restart.
	requireOutputEventually(t, f.process, "service started")
	require.Zero(t, f.process.terminate(t))
}
