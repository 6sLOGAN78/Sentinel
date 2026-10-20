//go:build integration

package test_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	sentineltesting "github.com/6sLOGAN78/sentinel/internal/testing"
)

const reservationBarrierEnv = "SENTINEL_TEST_ALERT_WRITER_AFTER_RESERVATION_BARRIER"
const reservationIDEnv = "SENTINEL_TEST_ALERT_WRITER_ALERT_ID"

// Keep physical variants observable throughout each fault schedule; merges in
// this fixture's UUID-named database must not erase evidence of a bad insert.
func preserveReservationPhysicalRows(t *testing.T, f *alertReplayFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, f.ch.Exec(ctx, "SYSTEM STOP MERGES "+f.chConfig.Database+".alerts"))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		require.NoError(t, f.ch.Exec(cleanupCtx, "SYSTEM START MERGES "+f.chConfig.Database+".alerts"))
	})
}

func reservationProcessEnv(t *testing.T, f *alertReplayFixture, barrier string) []string {
	t.Helper()
	env := serviceEnv(t, "alert-writer")
	for key, value := range map[string]string{
		"SENTINEL_KAFKA.ALERTS_TOPIC":       f.kafka.AlertsTopic,
		"SENTINEL_KAFKA.ALERT_WRITER_GROUP": f.kafka.AlertWriterGroup,
		"SENTINEL_KAFKA.DLQ_TOPIC":          f.kafka.DLQTopic,
		"SENTINEL_CLICKHOUSE.DATABASE":      f.chConfig.Database,
		"SENTINEL_WRITER.BATCH_SIZE":        "1",
		"SENTINEL_WRITER.FLUSH_INTERVAL":    "100ms",
		reservationBarrierEnv:               barrier,
		reservationIDEnv:                    f.alert.AlertID,
	} {
		env = append(without(env, key+"="), key+"="+value)
	}
	return env
}

// Own and join the child even when an assertion aborts the test.
func startReservationProcess(t *testing.T, binary string, env []string) *proc {
	t.Helper()
	out := &syncBuffer{}
	cmd := exec.CommandContext(t.Context(), binary)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = t.TempDir(), env, out, out
	require.NoError(t, cmd.Start())
	p := &proc{cmd: cmd, done: make(chan error, 1), output: out}
	go func() { p.done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case err := <-p.done:
			p.done <- err
		default:
			_ = cmd.Process.Kill()
			p.requireExit(t, 5*time.Second)
		}
	})
	return p
}

func independentReservationInput(t *testing.T, f *alertReplayFixture) *alertReplayFixture {
	t.Helper()
	other := *f
	other.kafka.AlertsTopic = sentineltesting.RequireUniqueTopic(t, f.kafka, "reservation-other")
	other.kafka.AlertWriterGroup = "reservation-other-" + uuid.NewString()
	return &other
}

func requireReservationState(t *testing.T, f *alertReplayFixture, want []byte, workflows int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var payload []byte
	var tenant, fingerprint string
	var count int
	require.NoError(t, f.db.Pool.QueryRow(ctx,
		`SELECT canonical_payload,tenant_id,immutable_fingerprint FROM alert_reservations WHERE alert_id=$1`,
		f.alert.AlertID).Scan(&payload, &tenant, &fingerprint))
	require.Equal(t, want, payload, "committed reservation must retain exact selected bytes")
	wantFingerprint, err := alertwriter.ImmutableFingerprint(f.alert)
	require.NoError(t, err)
	require.Equal(t, f.alert.TenantID, tenant)
	require.Equal(t, wantFingerprint, fingerprint)
	require.NoError(t, f.db.Pool.QueryRow(ctx,
		"SELECT count(*) FROM alert_reservations WHERE alert_id=$1", f.alert.AlertID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.db.Pool.QueryRow(ctx,
		"SELECT count(*) FROM alert_workflows WHERE alert_id=$1", f.alert.AlertID).Scan(&count))
	require.Equal(t, workflows, count)
}

func requireReservationPhysical(t *testing.T, f *alertReplayFixture, want []byte, count uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var rows, variants uint64
	var raw, fingerprint string
	require.NoError(t, f.ch.QueryRow(ctx,
		`SELECT count(),uniqExact(immutable_fingerprint),any(canonical_payload),any(immutable_fingerprint)
		FROM alerts WHERE alert_id={id:String}`,
		driverclickhouse.Named("id", f.alert.AlertID)).Scan(&rows, &variants, &raw, &fingerprint))
	require.Equal(t, count, rows)
	if count != 0 {
		require.Equal(t, uint64(1), variants)
		require.Equal(t, string(want), raw, "physical native payload must be the saved representation")
		wantFingerprint, err := alertwriter.ImmutableFingerprint(f.alert)
		require.NoError(t, err)
		require.Equal(t, wantFingerprint, fingerprint)
	}
}

// Removing the boundary call fails before the kill/restart, even when normal
// delivery succeeds. Matching mutable-only input must recover the saved bytes.
func TestAlertWriterReservationRestart(t *testing.T) {
	integrationBinary := buildIntegrationBinary(t, "alert-writer")
	for _, defaultReplay := range []bool{false, true} {
		name := "integration restart"
		if defaultReplay {
			name = "default binary ignores tempting fault controls"
		}
		t.Run(name, func(t *testing.T) {
			f := newAlertReplayFixture(t)
			preserveReservationPhysicalRows(t, f)
			raw, err := json.Marshal(f.alert)
			require.NoError(t, err)
			private := t.TempDir()
			require.NoError(t, os.Chmod(private, 0o700))
			barrier := filepath.Join(private, "reservation")
			p := startReservationProcess(t, integrationBinary, reservationProcessEnv(t, f, barrier))
			f.publish(t, f.alert)
			require.Eventually(t, func() bool {
				_, statErr := os.Stat(barrier + ".reached")
				return statErr == nil
			}, 10*time.Second, 10*time.Millisecond,
				"missing post-reservation/pre-CH boundary; child output:\n%s", p.output.String())
			info, err := os.Stat(barrier + ".reached")
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			requireReservationState(t, f, raw, 0)
			requireReservationPhysical(t, f, nil, 0)
			require.Zero(t, f.committed(t))
			require.NoError(t, p.cmd.Process.Kill())
			require.NotZero(t, p.requireExit(t, 5*time.Second))

			replay := *f.alert
			replay.Status = "resolved"
			detected := contracts.NewTimestamp(replay.CreatedAt.Add(time.Hour))
			replay.DetectedAt = &detected
			other := independentReservationInput(t, f)
			other.publish(t, &replay)
			binary, recoveryBarrier := integrationBinary, ""
			if defaultReplay {
				binary = filepath.Join(binDir, "alert-writer")
				freshPrivate := t.TempDir()
				require.NoError(t, os.Chmod(freshPrivate, 0o700))
				recoveryBarrier = filepath.Join(freshPrivate, "must-remain-inert")
			}
			recovered := startReservationProcess(t, binary, reservationProcessEnv(t, other, recoveryBarrier))
			other.waitOffset(t, 1)
			requireReservationState(t, f, raw, 1)
			requireReservationPhysical(t, f, raw, 1)
			require.Zero(t, f.committed(t), "independent recovery cannot acknowledge original group")
			require.Zero(t, recovered.terminate(t), recovered.output.String())
			if defaultReplay {
				entries, readErr := os.ReadDir(filepath.Dir(recoveryBarrier))
				require.NoError(t, readErr)
				require.Empty(t, entries, "healthy default binary must never access fault controls")
			}
			before := f.workflow(t)
			original := startReservationProcess(t, binary, reservationProcessEnv(t, f, ""))
			waitRecoveryOffset(t, f.kafka, f.kafka.AlertWriterGroup, f.kafka.AlertsTopic, 1, original)
			require.Equal(t, before, f.workflow(t))
			requireReservationPhysical(t, f, raw, 1)
			require.Zero(t, original.terminate(t), original.output.String())
		})
	}
}
