//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const (
	// gracefulExitLimit is the 30 s lifecycle budget plus scheduling slack.
	gracefulExitLimit = 35 * time.Second
	// shutdownCompleteMessage is the clean-exit log line from internal/app/app.go.
	shutdownCompleteMessage = "shutdown complete"
	chaosShutdownBatch      = 4 // recoveryWriterEnv sets SENTINEL_WRITER.BATCH_SIZE=4
)

// sigtermWithin sends SIGTERM and requires a clean exit inside the budget.
func sigtermWithin(t *testing.T, p *proc) {
	t.Helper()
	signaled := time.Now()
	require.NoError(t, p.cmd.Process.Signal(syscall.SIGTERM))
	code := p.requireExit(t, gracefulExitLimit)
	elapsed := time.Since(signaled)
	require.Zero(t, code, "exit code after SIGTERM; output:\n%s", p.output.String())
	require.Less(t, elapsed, gracefulExitLimit)
	require.Contains(t, p.output.String(), shutdownCompleteMessage)
	t.Logf("SIGTERM to clean exit in %s", elapsed)
}

func TestChaosGracefulShutdownEventWriter(t *testing.T) {
	kafka, topic, group := chaosWriterTopic(t, "chaos-shutdown-writer")
	clickhouse, conn := newRecoveryDatabase(t)
	store := storageclickhouse.NewEventStore(conn)
	first := canonicalWriterEvents(chaosShutdownBatch, time.Now().UTC().Add(-time.Minute))
	producer := newWriterProducer(t, kafka)
	require.NoError(t, producer.PublishBatch(t.Context(), first, uuid.NewString()))

	// Hold the batch after its insert and before its commit, then SIGTERM.
	barrier := filepath.Join(t.TempDir(), "after-insert")
	env := append(recoveryWriterEnv(t, kafka, clickhouse), writerBarrierEnv+"="+barrier)
	process := startRecoveryProc(t, buildIntegrationBinary(t, "event-writer"), env)
	requireFileEventually(t, barrier+".reached")
	var inserted uint64
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count() FROM events").Scan(&inserted))
	require.Equal(t, uint64(chaosShutdownBatch), inserted)
	_, committed := committedNextOffset(t, kafka, group, topic)
	require.False(t, committed, "offset advanced before the held batch completed")

	sigtermWithin(t, process)
	if next, ok := committedNextOffset(t, kafka, group, topic); ok {
		require.LessOrEqual(t, next, int64(inserted), "shutdown committed beyond the inserted prefix")
	}

	// A restart completes the remainder, including records published after the stop.
	second := canonicalWriterEvents(chaosShutdownBatch, time.Now().UTC().Add(-time.Minute))
	require.NoError(t, producer.PublishBatch(t.Context(), second, uuid.NewString()))
	restart := startRecoveryProc(t, filepath.Join(binDir, "event-writer"), recoveryWriterEnv(t, kafka, clickhouse))
	waitRecoveryOffset(t, kafka, group, topic, 2*chaosShutdownBatch, restart)
	sigtermWithin(t, restart)

	unique, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(2*chaosShutdownBatch), unique)
	for _, event := range append(first, second...) {
		got, getErr := store.Get(t.Context(), "demo", event.EventID)
		require.NoError(t, getErr, "event %s", event.EventID)
		require.Equal(t, event.EventID, got.EventID)
	}
}

// detectorProcessedNext reads the committed processed_next pin of partition 0.
func detectorProcessedNext(t *testing.T, f *cooldownFixture) (int64, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	offsets, err := f.admin.FetchOffsets(ctx, f.kafka.DetectorGroup)
	if err != nil {
		return 0, false
	}
	o, ok := offsets.Lookup(f.kafka.EventsTopic, 0)
	if !ok || o.Err != nil {
		return 0, false
	}
	m, err := detection.CheckpointMetadata(detection.Checkpoint{Offset: o.At, Metadata: o.Metadata})
	if err != nil {
		return 0, false
	}

	return m.ProcessedNext, true
}

func TestChaosGracefulShutdownDetector(t *testing.T) {
	f := newCooldownFixture(t, false)
	stopInitialDetector(t, f)
	events, canonical := recoveryAuthEvents(t, f, f.principal)
	var expected contracts.Alert
	require.NoError(t, json.Unmarshal(canonical, &expected))

	// Hold the alert-producing record (offset 9) before its publication.
	env, barrier := detectorBarrierEnv(t, f.env, "before-publish", 0, 9)
	env = append(without(env, "SENTINEL_DETECTOR.WRITER.BATCH_SIZE="), "SENTINEL_DETECTOR.WRITER.BATCH_SIZE=1")
	f.process = startRecoveryProc(t, buildIntegrationBinary(t, "detector"), env)
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
	reachedDetectorBarrier(t, barrier, "before-publish", f.kafka.EventsTopic, 0, 9)
	recoveryCheckpoint(t, f, 0, 9)

	sigtermWithin(t, f.process)
	next, ok := detectorProcessedNext(t, f)
	require.True(t, ok, "detector pin vanished on shutdown")
	require.GreaterOrEqual(t, next, int64(9), "shutdown regressed the processed pin")
	require.LessOrEqual(t, next, int64(10), "shutdown pinned beyond the published records")
	var decisions int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM detection_decisions WHERE alert_id=$1", expected.AlertID).Scan(&decisions))
	if next == 10 {
		// Pinned past the held record only if its decision completed during shutdown.
		require.Equal(t, 1, decisions, "pin advanced past an incomplete alert decision")
	}
	t.Logf("pin after SIGTERM processed_next=%d decisions=%d", next, decisions)

	f.process = startProc(t, "detector", "", f.env)
	requireRecoveredAlert(t, f, canonical)
	recoveryCheckpoint(t, f, 0, 10)
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM detection_decisions WHERE alert_id=$1", expected.AlertID).Scan(&decisions))
	require.Equal(t, 1, decisions, "alert decision must exist exactly once after restart")
	var workflows int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", expected.AlertID).Scan(&workflows))
	require.Equal(t, 1, workflows, "alert workflow must exist exactly once after restart")
	sigtermWithin(t, f.process)
}
