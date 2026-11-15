//go:build integration

package test_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const recoverToolTimeout = 90 * time.Second

type recoverOutput struct {
	Group             string                    `json:"group"`
	Topic             string                    `json:"topic"`
	ConfigFingerprint string                    `json:"config_fingerprint"`
	Entries           []detection.RecoveryEntry `json:"entries"`
	PlanDigest        string                    `json:"plan_digest"`
	NothingToRecover  bool                      `json:"nothing_to_recover"`
	Applied           bool                      `json:"applied"`
}

type recoverResult struct {
	stdout, stderr string
	code           int
}

func buildRecoverTool(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "detector-recover")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", path, "./tools/detector-recover")
	cmd.Dir = ".."
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return path
}

func runRecoverTool(t *testing.T, binary string, env []string, args ...string) recoverResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), recoverToolTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "tool did not run: %v", err)
		code = exitErr.ExitCode()
	}
	return recoverResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func parseRecoverOutput(t *testing.T, result recoverResult) recoverOutput {
	t.Helper()
	require.Zero(t, result.code, "stdout=%s stderr=%s", result.stdout, result.stderr)
	var out recoverOutput
	decoder := json.NewDecoder(strings.NewReader(result.stdout))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&out), result.stdout)
	return out
}

func recoveryRowCount(t *testing.T, db *database.Database, group string) int {
	t.Helper()
	var count int
	require.NoError(t, db.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM detection_history_recoveries WHERE group_id=$1", group).Scan(&count))
	return count
}

// fenceSnapshot is the full row image of every fence for the group.
func fenceSnapshot(t *testing.T, db *database.Database, group string) string {
	t.Helper()
	var snapshot string
	require.NoError(t, db.Pool.QueryRow(t.Context(), `SELECT coalesce(json_agg(f ORDER BY topic, partition_id)::text, '')
		FROM detection_partition_fences f WHERE group_id=$1`, group).Scan(&snapshot))
	return snapshot
}

func requireRefusal(t *testing.T, result recoverResult, phrase string) {
	t.Helper()
	require.NotZero(t, result.code, "stdout=%s stderr=%s", result.stdout, result.stderr)
	require.Contains(t, result.stderr, phrase)
	require.Empty(t, result.stdout, "refusals print no plan")
}

func historyBounds(t *testing.T, admin *kadm.Client, topic string) (int64, int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	starts, err := admin.ListStartOffsets(ctx, topic)
	require.NoError(t, err)
	ends, err := admin.ListEndOffsets(ctx, topic)
	require.NoError(t, err)
	start, ok := starts.Lookup(topic, 0)
	require.True(t, ok)
	require.NoError(t, start.Err)
	end, ok := ends.Lookup(topic, 0)
	require.True(t, ok)
	require.NoError(t, end.Err)
	return start.Offset, end.Offset
}

func deleteHistory(t *testing.T, admin *kadm.Client, topic string, end int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	// Test-owned unique topic only; never a shared topic.
	deleted, err := admin.DeleteRecords(ctx, kadm.Offsets{topic: {0: {Topic: topic, Partition: 0, At: end}}})
	require.NoError(t, err)
	require.NoError(t, deleted.Error())
}

func groupMembers(t *testing.T, admin *kadm.Client, group string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	groups, err := admin.DescribeGroups(ctx, group)
	require.NoError(t, err)
	described, ok := groups[group]
	require.True(t, ok)
	require.NoError(t, described.Err)
	return len(described.Members)
}

// joinPlainMember joins a non-committing kgo member to the group; the returned
// stop function leaves the group and waits until it is empty again.
func joinPlainMember(t *testing.T, admin *kadm.Client, kafka config.KafkaConfig) func() {
	t.Helper()
	member, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(kafka.Brokers, ",")...),
		kgo.ConsumerGroup(kafka.DetectorGroup), kgo.ConsumeTopics(kafka.EventsTopic), kgo.DisableAutoCommit())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			pollCtx, cancelPoll := context.WithTimeout(ctx, 200*time.Millisecond)
			member.PollRecords(pollCtx, 10)
			cancelPoll()
		}
	}()
	require.Eventually(t, func() bool { return groupMembers(t, admin, kafka.DetectorGroup) == 1 },
		30*time.Second, 100*time.Millisecond, "plain member never joined")
	return func() {
		cancel()
		<-done
		member.Close()
		require.Eventually(t, func() bool { return groupMembers(t, admin, kafka.DetectorGroup) == 0 },
			30*time.Second, 100*time.Millisecond, "plain member never left")
	}
}

func publishNeutral(t *testing.T, f *cooldownFixture, count int, at time.Time) {
	t.Helper()
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(),
		canonicalWriterEvents(count, at), uuid.NewString()))
}

// requireSingleFreshAlert publishes a fresh ten-failure sequence and requires
// exactly one alert on Kafka, in the PG workflow and in ClickHouse.
func requireSingleFreshAlert(t *testing.T, f *cooldownFixture, at time.Time) {
	t.Helper()
	events := canonicalWriterEvents(10, at)
	for _, event := range events {
		event.EventType, event.SourceIP, event.Principal = "auth.failure", &f.ip, &f.principal
	}
	require.NoError(t, newWriterProducer(t, f.kafka).PublishBatch(t.Context(), events, uuid.NewString()))
	readCtx, cancelRead := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancelRead()
	var records []*kgo.Record
	for len(records) == 0 && readCtx.Err() == nil {
		records = append(records, f.observer.PollRecords(readCtx, 100).Records()...)
	}
	records = append(records, f.alerts(t)...)
	require.Len(t, records, 1, "exactly one alert after recovery: %s", f.process.output.String())
	var alert contracts.Alert
	require.NoError(t, json.Unmarshal(records[0].Value, &alert))
	require.Equal(t, "AUTH_BRUTE_FORCE", alert.RuleID)
	require.Equal(t, "demo", alert.TenantID)
	require.Eventually(t, func() bool {
		var count int
		return f.db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM alert_workflows WHERE alert_id=$1", alert.AlertID).
			Scan(&count) == nil && count == 1
	}, 15*time.Second, 25*time.Millisecond)
	var decisions int
	require.NoError(t, f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM detection_decisions
		WHERE group_id=$1 AND topic=$2 AND allowed`, f.kafka.DetectorGroup, f.kafka.EventsTopic).Scan(&decisions))
	require.Equal(t, 1, decisions)
	store := storageclickhouse.NewAlertStore(f.ch)
	require.Eventually(t, func() bool {
		count, err := store.CountUnique(t.Context(), "demo", time.Time{})
		return err == nil && count == 1
	}, 15*time.Second, 25*time.Millisecond)
	stored, err := store.Get(t.Context(), "demo", alert.AlertID)
	require.NoError(t, err)
	require.Equal(t, alert.AlertID, stored.AlertID)
}

type auditRow struct {
	topic                        string
	partition                    int32
	committed, start, end        int64
	neverFrom, neverTo, repinned int64
	discarded                    bool
	fingerprint, digest          string
	operator, reason, status     string
	appliedAt                    *time.Time
}

func auditRows(t *testing.T, db *database.Database, group string) []auditRow {
	t.Helper()
	rows, err := db.Pool.Query(t.Context(), `SELECT topic, partition_id, committed_offset, log_start, log_end,
		never_evaluated_from, never_evaluated_to, repinned_offset, open_windows_discarded, config_fingerprint,
		plan_digest, operator, reason, status, applied_at
		FROM detection_history_recoveries WHERE group_id=$1 ORDER BY topic, partition_id`, group)
	require.NoError(t, err)
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		require.NoError(t, rows.Scan(&r.topic, &r.partition, &r.committed, &r.start, &r.end, &r.neverFrom, &r.neverTo,
			&r.repinned, &r.discarded, &r.fingerprint, &r.digest, &r.operator, &r.reason, &r.status, &r.appliedAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func requireAuditMatches(t *testing.T, rows []auditRow, plan recoverOutput, reason string) {
	t.Helper()
	require.Len(t, rows, len(plan.Entries))
	for i, e := range plan.Entries {
		r := rows[i]
		require.Equal(t, e.Topic, r.topic)
		require.Equal(t, e.Partition, r.partition)
		require.Equal(t, e.Committed, r.committed)
		require.Equal(t, e.Start, r.start)
		require.Equal(t, e.End, r.end)
		require.Equal(t, e.NeverEvaluatedFrom, r.neverFrom)
		require.Equal(t, e.NeverEvaluatedTo, r.neverTo)
		require.Equal(t, e.Repinned.Offset, r.repinned)
		require.Equal(t, e.OpenWindowsDiscarded, r.discarded)
		require.Equal(t, plan.ConfigFingerprint, r.fingerprint)
		require.Equal(t, plan.PlanDigest, r.digest)
		require.Equal(t, "test-operator", r.operator)
		require.Equal(t, reason, r.reason)
		require.Equal(t, "applied", r.status)
		require.NotNil(t, r.appliedAt)
	}
}

//nolint:funlen // One executable operator workflow; the steps are the specification.
func TestDetectorHistoryRecovery(t *testing.T) {
	tool := buildRecoverTool(t)
	t.Run("idle retention recovery", func(t *testing.T) {
		f := newCooldownFixture(t, false)
		group, topic := f.kafka.DetectorGroup, f.kafka.EventsTopic
		// (1) Processed and committed history, then a graceful stop.
		publishNeutral(t, f, 3, f.start.Add(time.Second))
		f.offset = 3
		f.waitSource(t)
		require.Zero(t, f.process.terminate(t), f.process.output.String())
		committed := brokerCheckpoint(t, f.admin, f.kafka)
		// (2) History produced while the detector is stopped.
		publishNeutral(t, f, 4, f.start.Add(2*time.Second))
		_, end := historyBounds(t, f.admin, topic)
		require.Equal(t, int64(7), end)
		require.Less(t, committed.At, end)
		// (3) Simulated retention removes every record.
		deleteHistory(t, f.admin, topic, end)
		start, end := historyBounds(t, f.admin, topic)
		require.Equal(t, end, start)
		require.Greater(t, start, committed.At)
		// (4) The detector restore path is unchanged: it fails closed.
		p := startProc(t, "detector", "", f.env)
		require.NotZero(t, p.requireExit(t, 20*time.Second), p.output.String())
		require.Contains(t, p.output.String(), "detector checkpoint history lost")
		retained := brokerCheckpoint(t, f.admin, f.kafka)
		require.Equal(t, committed.At, retained.At)
		require.Equal(t, committed.Metadata, retained.Metadata)
		fencesBefore := fenceSnapshot(t, f.db, group)
		require.NotEmpty(t, fencesBefore)

		// (5) Dry-run prints the exact plan and writes nothing.
		expected, err := detection.PlanHistoryRecovery(f.rules, group, []detection.PartitionBounds{{
			Topic: topic, Partition: 0, Committed: committed.At, HasCommitted: true,
			Metadata: committed.Metadata, Start: start, End: end}})
		require.NoError(t, err)
		require.Len(t, expected.Entries, 1)
		plan := parseRecoverOutput(t, runRecoverTool(t, tool, f.env))
		require.Equal(t, group, plan.Group)
		require.Equal(t, topic, plan.Topic)
		require.Equal(t, f.rules.Fingerprint(), plan.ConfigFingerprint)
		require.Equal(t, expected.Entries, plan.Entries)
		require.Equal(t, expected.Digest(), plan.PlanDigest)
		require.False(t, plan.NothingToRecover)
		require.False(t, plan.Applied)
		entry := plan.Entries[0]
		committedMeta, err := detection.CheckpointMetadata(detection.Checkpoint{Offset: committed.At,
			Metadata: committed.Metadata})
		require.NoError(t, err)
		require.Equal(t, committed.At, entry.Committed)
		require.Equal(t, start, entry.Start)
		require.Equal(t, end, entry.End)
		require.Equal(t, committedMeta.ProcessedNext, entry.NeverEvaluatedFrom)
		require.Equal(t, end, entry.NeverEvaluatedTo)
		require.Equal(t, end, entry.Repinned.Offset)
		require.Zero(t, recoveryRowCount(t, f.db, group))
		unchanged := func() {
			t.Helper()
			current := brokerCheckpoint(t, f.admin, f.kafka)
			require.Equal(t, committed.At, current.At)
			require.Equal(t, committed.Metadata, current.Metadata)
			require.Zero(t, recoveryRowCount(t, f.db, group))
			require.Equal(t, fencesBefore, fenceSnapshot(t, f.db, group))
		}
		unchanged()
		const reason = "idle dev stack history expired; acknowledged loss"
		apply := func(digest, operator, why string) recoverResult {
			return runRecoverTool(t, tool, f.env, "--apply", "--acknowledge-loss="+digest,
				"--operator="+operator, "--reason="+why)
		}

		// (6) A wrong digest refuses with zero writes.
		requireRefusal(t, apply(strings.Repeat("0", 64), "test-operator", reason), "plan digest mismatch")
		unchanged()
		// (7) Any group member refuses with zero writes.
		leave := joinPlainMember(t, f.admin, f.kafka)
		requireRefusal(t, apply(plan.PlanDigest, "test-operator", reason), "detector group has active members")
		leave()
		unchanged()
		// (8) An empty reason is a usage error.
		requireRefusal(t, apply(plan.PlanDigest, "test-operator", ""), "--reason")
		unchanged()

		// (9) The acknowledged apply re-pins the same group and audits it.
		appliedResult := apply(plan.PlanDigest, "test-operator", reason)
		applied := parseRecoverOutput(t, appliedResult)
		require.True(t, applied.Applied)
		require.Equal(t, plan.PlanDigest, applied.PlanDigest)
		require.Equal(t, plan.Entries, applied.Entries)
		requireAuditMatches(t, auditRows(t, f.db, group), plan, reason)
		repinned := brokerCheckpoint(t, f.admin, f.kafka)
		require.Equal(t, entry.Repinned.Offset, repinned.At)
		require.Equal(t, entry.Repinned.Metadata, repinned.Metadata)
		require.Equal(t, fencesBefore, fenceSnapshot(t, f.db, group), "fences are never written")
		require.Contains(t, appliedResult.stderr, "detector history recovery applied")
		require.NotContains(t, appliedResult.stderr, "test-operator", "operator is never logged")
		require.NotContains(t, appliedResult.stderr, reason, "reason is never logged")

		// (10) The restarted detector is ready and raises exactly one alert.
		f.process = startProc(t, "detector", "", f.env)
		requireStatusEventually(t, "http://127.0.0.1:"+opsPort(t, f.env)+"/readyz", http.StatusOK)
		requireSingleFreshAlert(t, f, f.start.Add(10*time.Second))
		require.Zero(t, f.process.terminate(t), f.process.output.String())

		// (11) A second apply has nothing to recover and adds no rows.
		second := runRecoverTool(t, tool, f.env, "--apply", "--acknowledge-loss="+plan.PlanDigest,
			"--operator=test-operator", "--reason="+reason)
		again := parseRecoverOutput(t, second)
		require.True(t, again.NothingToRecover)
		require.Empty(t, again.Entries)
		require.False(t, again.Applied)
		require.Contains(t, second.stderr, "nothing to recover")
		require.Len(t, auditRows(t, f.db, group), 1)
	})
	t.Run("audit table is append-only", func(t *testing.T) {
		db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
		require.NoError(t, err)
		t.Cleanup(db.Close)
		group, digest := "audit-"+uuid.NewString(), strings.Repeat("b", 64)
		var id int64
		require.NoError(t, db.Pool.QueryRow(t.Context(), `INSERT INTO detection_history_recoveries
			(group_id, topic, partition_id, committed_offset, log_start, log_end, never_evaluated_from,
			 never_evaluated_to, open_windows_discarded, config_fingerprint, plan_digest, repinned_offset,
			 operator, reason, status)
			VALUES ($1,'t',0,1,5,9,3,5,true,$2,$3,5,'op','why','pending') RETURNING recovery_id`,
			group, strings.Repeat("c", 64), digest).Scan(&id))
		for _, statement := range []string{
			"UPDATE detection_history_recoveries SET operator='other' WHERE recovery_id=$1",
			"UPDATE detection_history_recoveries SET reason='other', status='applied', applied_at=now() WHERE recovery_id=$1",
			"UPDATE detection_history_recoveries SET status='applied' WHERE recovery_id=$1",
			"DELETE FROM detection_history_recoveries WHERE recovery_id=$1",
		} {
			_, err = db.Pool.Exec(t.Context(), statement, id)
			require.Error(t, err, statement)
		}
		_, err = db.Pool.Exec(t.Context(),
			"UPDATE detection_history_recoveries SET status='applied', applied_at=now() WHERE recovery_id=$1", id)
		require.NoError(t, err, "pending to applied is the only permitted update")
		for _, statement := range []string{
			"UPDATE detection_history_recoveries SET status='pending', applied_at=NULL WHERE recovery_id=$1",
			"UPDATE detection_history_recoveries SET applied_at=now() + interval '1 hour' WHERE recovery_id=$1",
			"DELETE FROM detection_history_recoveries WHERE recovery_id=$1",
		} {
			_, err = db.Pool.Exec(t.Context(), statement, id)
			require.Error(t, err, statement)
		}
		_, err = db.Pool.Exec(t.Context(), "TRUNCATE detection_history_recoveries")
		require.Error(t, err, "truncate is rejected")
		require.Equal(t, 1, recoveryRowCount(t, db, group))
	})
	t.Run("missing group metadata with fences", func(t *testing.T) {
		env, kafka := detectorPGEnv(t, pgCfg)
		db, err := database.New(t.Context(), pgCfg, zerolog.Nop())
		require.NoError(t, err)
		t.Cleanup(db.Close)
		admin := checkpointAdmin(t)
		require.NoError(t, newWriterProducer(t, kafka).PublishBatch(t.Context(),
			canonicalWriterEvents(2, time.Now().UTC()), uuid.NewString()))
		p := startProc(t, "detector", "", env)
		require.Eventually(t, func() bool { return fenceSnapshot(t, db, kafka.DetectorGroup) != "" },
			30*time.Second, 50*time.Millisecond, "detector created no fence: %s", p.output.String())
		require.Eventually(t, func() bool {
			offsets, fetchErr := admin.FetchOffsets(t.Context(), kafka.DetectorGroup)
			o, ok := offsets.Lookup(kafka.EventsTopic, 0)
			return fetchErr == nil && ok && o.Err == nil && o.At >= 0
		}, 30*time.Second, 50*time.Millisecond)
		require.Zero(t, p.terminate(t), p.output.String())
		fences := fenceSnapshot(t, db, kafka.DetectorGroup)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		// Test-owned unique group only.
		deleted, err := admin.DeleteGroups(ctx, kafka.DetectorGroup)
		require.NoError(t, err)
		require.NoError(t, deleted.Error())
		for _, args := range [][]string{nil, {"--apply", "--acknowledge-loss=" + strings.Repeat("a", 64),
			"--operator=test-operator", "--reason=missing group"}} {
			requireRefusal(t, runRecoverTool(t, tool, env, args...), "group metadata missing while PG fences exist")
			require.Zero(t, recoveryRowCount(t, db, kafka.DetectorGroup))
			require.Equal(t, fences, fenceSnapshot(t, db, kafka.DetectorGroup))
			offsets, fetchErr := admin.FetchOffsets(t.Context(), kafka.DetectorGroup)
			require.NoError(t, fetchErr)
			_, present := offsets.Lookup(kafka.EventsTopic, 0)
			require.False(t, present, "refusal committed no offsets")
		}
	})
}
