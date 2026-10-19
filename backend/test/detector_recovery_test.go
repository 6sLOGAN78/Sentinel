//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const detectorControl = "SENTINEL_TEST_DETECTOR_"

type detectorObservation struct {
	Boundary   string                `json:"boundary"`
	Topic      string                `json:"topic"`
	Partition  int32                 `json:"partition"`
	Offset     int64                 `json:"offset"`
	Member     string                `json:"member"`
	Generation int32                 `json:"generation"`
	Token      uint64                `json:"token"`
	Checkpoint *detection.Checkpoint `json:"checkpoint,omitempty"`
}

func detectorBarrierEnv(t *testing.T, env []string, boundary string, partition int32, offset int64) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o700))
	path := filepath.Join(dir, "cut")
	return append(env, detectorControl+"BOUNDARY="+boundary, detectorControl+"BARRIER="+path,
		detectorControl+"PARTITION="+strconv.Itoa(int(partition)),
		detectorControl+"OFFSET="+strconv.FormatInt(offset, 10)), path
}

func reachedDetectorBarrier(t *testing.T, path, boundary, topic string, partition int32, offset int64) {
	t.Helper()
	var observation detectorObservation
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(path + ".reached")
		return err == nil && json.Unmarshal(raw, &observation) == nil
	}, 20*time.Second, 10*time.Millisecond, "exact causal barrier %s not reached", boundary)
	require.Equal(t, boundary, observation.Boundary)
	require.Equal(t, topic, observation.Topic)
	require.Equal(t, partition, observation.Partition)
	require.Equal(t, offset, observation.Offset)
	info, err := os.Stat(path + ".reached")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func recoveryAuthEvents(t *testing.T, f *cooldownFixture, principal string) ([]*contracts.Event, []byte) {
	t.Helper()
	events := canonicalWriterEvents(10, f.start.Add(time.Second))
	state := detection.NewPartitionState(f.rules)
	var canonical []byte
	for i, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &principal
		out, err := state.ApplySource(detection.TopicPartition{Topic: f.kafka.EventsTopic, Partition: 0}, int64(i), event)
		require.NoError(t, err)
		for _, candidate := range out.Candidates {
			canonical = candidate.CanonicalBytes()
		}
		require.NoError(t, state.Acknowledge(int64(i)))
	}
	require.NotEmpty(t, canonical)
	return events, canonical
}

func recoveryCheckpoint(t *testing.T, f *cooldownFixture, partition int32, next int64) detection.Checkpoint {
	t.Helper()
	var cp detection.Checkpoint
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		offsets, err := f.admin.FetchOffsets(ctx, f.kafka.DetectorGroup)
		o, ok := offsets.Lookup(f.kafka.EventsTopic, partition)
		if err != nil || !ok || o.Err != nil {
			return false
		}
		cp = detection.Checkpoint{Offset: o.At, Metadata: o.Metadata}
		m, err := detection.CheckpointMetadata(cp)
		return err == nil && m.ProcessedNext == next
	}, 70*time.Second, 25*time.Millisecond, "missing partition %d processed_next=%d; %s",
		partition, next, f.process.output.String())
	require.Zero(t, cp.Offset, "live AUTH retains the first source contributor")
	m, err := detection.CheckpointMetadata(cp)
	require.NoError(t, err)
	require.Equal(t, 2, m.Format)
	require.Nil(t, m.PrefixMaxEventTime)
	require.NotNil(t, m.SealedThrough)
	require.False(t, m.Drained)
	require.Len(t, m.RuleVersions, 4)
	require.Len(t, m.RuleEffectiveFrom, 4)
	require.LessOrEqual(t, len(cp.Metadata), detection.MaxMetadataBytes)
	if next < 10 {
		require.True(t, m.DecisionFrontier.None)
	} else {
		require.False(t, m.DecisionFrontier.None)
		require.Equal(t, int64(9), m.DecisionFrontier.Offset)
		var topic, fingerprint string
		var sourcePartition int32
		var sourceOffset int64
		require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT topic,partition_id,source_offset,immutable_fingerprint
			FROM detection_decisions WHERE alert_id=$1`, m.DecisionFrontier.AlertID).
			Scan(&topic, &sourcePartition, &sourceOffset, &fingerprint))
		require.Equal(t, f.kafka.EventsTopic, topic)
		require.Equal(t, partition, sourcePartition)
		require.Equal(t, m.DecisionFrontier.Offset, sourceOffset)
		require.Equal(t, m.DecisionFrontier.Fingerprint, fingerprint)
	}
	t.Logf("partition=%d broker pin=%d metadata=%s", partition, cp.Offset, cp.Metadata)
	return cp
}

func requireRecoveredAlert(t *testing.T, f *cooldownFixture, canonical []byte) {
	t.Helper()
	var expected contracts.Alert
	require.NoError(t, json.Unmarshal(canonical, &expected))
	readCtx, cancelRead := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancelRead()
	var records []*kgo.Record
	for len(records) == 0 && readCtx.Err() == nil {
		fetches := f.observer.PollRecords(readCtx, 100)
		if readCtx.Err() == nil {
			require.NoError(t, fetches.Err())
		}
		records = append(records, fetches.Records()...)
	}
	records = append(records, f.alerts(t)...)
	require.NotEmpty(t, records, "no actual Kafka publication: %s", f.process.output.String())
	for _, record := range records {
		require.Equal(t, "demo|"+expected.AlertID, string(record.Key))
		require.Equal(t, canonical, record.Value, "replay must preserve every canonical Kafka byte")
	}
	store := storageclickhouse.NewAlertStore(f.ch)
	require.Eventually(t, func() bool {
		var count int
		return f.db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM alert_workflows WHERE alert_id=$1", expected.AlertID).
			Scan(&count) == nil && count == 1
	}, 10*time.Second, 25*time.Millisecond)
	count, err := store.CountUnique(t.Context(), "demo", time.Time{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), count, "immediate visible CH identity count")
	stored, err := store.Get(t.Context(), "demo", expected.AlertID)
	require.NoError(t, err)
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	require.Equal(t, canonical, raw, "immediate CH detail retains immutable evidence")
	var decision []byte
	require.NoError(t, f.db.Pool.QueryRow(t.Context(),
		"SELECT canonical_payload FROM detection_decisions WHERE alert_id=$1", expected.AlertID).Scan(&decision))
	require.Equal(t, canonical, decision)
}

func killDetector(t *testing.T, process *proc) {
	t.Helper()
	require.NoError(t, process.cmd.Process.Signal(syscall.SIGKILL))
	require.NotZero(t, process.requireExit(t, failFastLimit))
}

func stopInitialDetector(t *testing.T, f *cooldownFixture) {
	t.Helper()
	requireStatusEventually(t, "http://127.0.0.1:"+opsPort(t, f.env)+"/readyz", http.StatusOK)
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

func TestDetectorMidWindowRecovery(t *testing.T) {
	f := newCooldownFixture(t, false)
	stopInitialDetector(t, f)
	events, canonical := recoveryAuthEvents(t, f, f.principal)
	env, barrier := detectorBarrierEnv(t, f.env, "after-record", 0, 8)
	// One-record batches make the durable predecessor independently inspectable.
	env = append(without(env, "SENTINEL_DETECTOR.WRITER.BATCH_SIZE="), "SENTINEL_DETECTOR.WRITER.BATCH_SIZE=1")
	f.process = startRecoveryProc(t, buildIntegrationBinary(t, "detector"), env)
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events[:9], uuid.NewString()))
	reachedDetectorBarrier(t, barrier, "after-record", f.kafka.EventsTopic, 0, 8)
	recoveryCheckpoint(t, f, 0, 8)
	killDetector(t, f.process)
	// Default binary receives fault controls too; completed detection must have no file effects.
	defaultEnv, defaultBarrier := detectorBarrierEnv(t, f.env, "before-publish", 0, 9)
	f.process = startProc(t, "detector", "", defaultEnv)
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events[9:], uuid.NewString()))
	recoveryCheckpoint(t, f, 0, 10)
	requireRecoveredAlert(t, f, canonical)
	_, err := os.Stat(defaultBarrier + ".reached")
	require.ErrorIs(t, err, os.ErrNotExist, "production binary must ignore fault settings")
	require.Zero(t, f.process.terminate(t), f.process.output.String())
}

func TestDetectorCrashBoundaries(t *testing.T) {
	binary := buildIntegrationBinary(t, "detector")
	for _, boundary := range []string{"before-publish", "after-ack-before-checkpoint", "after-successful-checkpoint"} {
		t.Run(boundary, func(t *testing.T) {
			f := newCooldownFixture(t, false)
			stopInitialDetector(t, f)
			events, canonical := recoveryAuthEvents(t, f, f.principal)
			env, barrier := detectorBarrierEnv(t, f.env, boundary, 0, 9)
			env = append(without(env, "SENTINEL_DETECTOR.WRITER.BATCH_SIZE="), "SENTINEL_DETECTOR.WRITER.BATCH_SIZE=1")
			f.process = startRecoveryProc(t, binary, env)
			require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
			reachedDetectorBarrier(t, barrier, boundary, f.kafka.EventsTopic, 0, 9)
			next := int64(9)
			if boundary == "after-successful-checkpoint" {
				next = 10
			}
			before := recoveryCheckpoint(t, f, 0, next)
			var expected contracts.Alert
			require.NoError(t, json.Unmarshal(canonical, &expected))
			workflow := ""
			if boundary != "before-publish" {
				requireRecoveredAlert(t, f, canonical)
				_, err := f.db.Pool.Exec(t.Context(), `UPDATE alert_workflows SET status='resolved',assignee='recovery-analyst',
					version=7,updated_at=created_at+interval '1 second' WHERE alert_id=$1`, expected.AlertID)
				require.NoError(t, err)
				require.NoError(t, f.db.Pool.QueryRow(t.Context(),
					"SELECT row_to_json(w)::text FROM alert_workflows w WHERE alert_id=$1", expected.AlertID).Scan(&workflow))
			} else {
				require.Empty(t, f.alerts(t), "before-publish must precede actual Kafka publication")
				var workflows int
				require.NoError(t, f.db.Pool.QueryRow(t.Context(),
					"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", expected.AlertID).Scan(&workflows))
				require.Zero(t, workflows)
				count, err := storageclickhouse.NewAlertStore(f.ch).CountUnique(t.Context(), "demo", time.Time{})
				require.NoError(t, err)
				require.Zero(t, count)
			}
			killDetector(t, f.process)
			f.process = startProc(t, "detector", "", f.env)
			// Require actual replay publication, even when the successful live pin remains zero.
			requireRecoveredAlert(t, f, canonical)
			after := recoveryCheckpoint(t, f, 0, 10)
			if next == 10 {
				require.Equal(t, before, after)
			}
			if workflow != "" {
				var afterWorkflow string
				require.NoError(t, f.db.Pool.QueryRow(t.Context(),
					"SELECT row_to_json(w)::text FROM alert_workflows w WHERE alert_id=$1", expected.AlertID).Scan(&afterWorkflow))
				require.Equal(t, workflow, afterWorkflow,
					"resolved workflow, assignee, version, timestamps and seq must survive replay")
			}
			t.Logf("cut=%s alert=%s canonical=%s", boundary, expected.AlertID, canonical)
			require.Zero(t, f.process.terminate(t), f.process.output.String())
		})
	}
}

func detectorObservations(t *testing.T, dir string) []detectorObservation {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var observations []detectorObservation
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, readErr)
		var observation detectorObservation
		// A concurrently created private observation may not yet be fully written.
		if json.Unmarshal(raw, &observation) == nil {
			observations = append(observations, observation)
		}
	}
	return observations
}

func observedDetector(t *testing.T, binary string, env []string) (*proc, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o700))
	env = append(without(env, "SENTINEL_OPS.PORT="), "SENTINEL_OPS.PORT="+opsPort(t, serviceEnv(t, "detector")))
	process := startRecoveryProc(t, binary, append(env, detectorControl+"OBSERVATIONS="+dir))
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("observed detector output: %s", process.output.String())
		for _, o := range detectorObservations(t, dir) {
			t.Logf("observed boundary=%s partition=%d member=%s generation=%d token=%d offset=%d",
				o.Boundary, o.Partition, o.Member, o.Generation, o.Token, o.Offset)
		}
	})
	return process, dir
}
