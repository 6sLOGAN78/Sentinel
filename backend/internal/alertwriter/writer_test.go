package alertwriter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storage "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

type orderConsumer struct {
	order     *[]string
	committed []*kgo.Record
	record    *kgo.Record
	cancel    context.CancelFunc
}

func (c *orderConsumer) PollRecords(ctx context.Context, _ int) ([]*kgo.Record, error) {
	if c.record != nil {
		record := c.record
		c.record = nil
		return []*kgo.Record{record}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*orderConsumer) AllowRebalance() {}

func (c *orderConsumer) CommitRecords(_ context.Context, records ...*kgo.Record) error {
	*c.order = append(*c.order, "source-commit")
	c.committed = records
	c.cancel()
	return nil
}

type orderTx struct {
	pgx.Tx
	t         *testing.T
	order     *[]string
	commitErr error
}

func (tx *orderTx) Commit(context.Context) error {
	*tx.order = append(*tx.order, "pg-commit")
	return tx.commitErr
}
func (tx *orderTx) Rollback(ctx context.Context) error {
	tx.t.Helper()
	require.NoError(tx.t, ctx.Err(), "cleanup must survive operation cancellation")
	deadline, ok := ctx.Deadline()
	require.True(tx.t, ok)
	require.LessOrEqual(tx.t, time.Until(deadline), time.Second)
	*tx.order = append(*tx.order, "rollback")
	return nil
}

type orderWorkflows struct {
	tx        *orderTx
	ensureErr error
	findErr   error
}

func (s *orderWorkflows) BeginLocked(context.Context, string) (pgx.Tx, error) {
	*s.tx.order = append(*s.tx.order, "lock")
	return s.tx, nil
}
func (s *orderWorkflows) Find(context.Context, pgx.Tx, string) (string, string, error) {
	*s.tx.order = append(*s.tx.order, "pg-preflight")
	if s.findErr != nil {
		return "", "", s.findErr
	}
	return "", "", pgx.ErrNoRows
}
func (s *orderWorkflows) Ensure(ctx context.Context, _ pgx.Tx, _ *contracts.Alert, _ string) error {
	*s.tx.order = append(*s.tx.order, "pg-ensure")
	if s.ensureErr != nil {
		<-ctx.Done()
	}
	return s.ensureErr
}

type orderDetections struct {
	order     *[]string
	insertErr error
}

func (s *orderDetections) Get(context.Context, string, string) (*contracts.Alert, error) {
	*s.order = append(*s.order, "ch-preflight")
	return nil, storage.ErrNotFound
}
func (s *orderDetections) InsertBatch(context.Context, []*contracts.Alert) error {
	*s.order = append(*s.order, "ch-ack")
	return s.insertErr
}

// Moving source commit before either store, or omitting bounded rollback, fails.
func TestAlertWriterOrdering(t *testing.T) {
	failure := errors.New("sink failure")
	for _, scenario := range []struct {
		name                             string
		chErr, pgErr, commitErr, findErr error
		want                             []string
	}{
		{"success", nil, nil, nil, nil,
			[]string{"lock", "pg-preflight", "ch-preflight", "ch-ack", "pg-ensure", "pg-commit", "rollback", "source-commit"}},
		{"CH failure", failure, nil, nil, nil, []string{"lock", "pg-preflight", "ch-preflight", "ch-ack", "rollback"}},
		{"PG failure after CH", nil, failure, nil, nil,
			[]string{"lock", "pg-preflight", "ch-preflight", "ch-ack", "pg-ensure", "rollback"}},
		{"PG commit failure", nil, nil, failure, nil,
			[]string{"lock", "pg-preflight", "ch-preflight", "ch-ack", "pg-ensure", "pg-commit", "rollback"}},
		{"preflight failure", nil, nil, nil, failure, []string{"lock", "pg-preflight", "rollback"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			order := []string{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tx := &orderTx{t: t, order: &order, commitErr: scenario.commitErr}
			record := orderingRecord(t)
			consumer := &orderConsumer{order: &order, record: record, cancel: cancel}
			settings := config.WriterConfig{BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
				RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
				OperationTimeout: 25 * time.Millisecond, ShutdownTimeout: time.Second}
			writer := alertwriter.New(consumer, &orderDetections{order: &order, insertErr: scenario.chErr},
				&orderWorkflows{tx: tx, ensureErr: scenario.pgErr, findErr: scenario.findErr}, settings, zerolog.Nop())
			err := writer.Run(ctx)
			if scenario.name == "success" {
				require.NoError(t, err)
				require.Equal(t, []*kgo.Record{record}, consumer.committed)
			} else {
				require.Error(t, err)
				require.Empty(t, consumer.committed)
			}
			require.Equal(t, scenario.want, order)
		})
	}
}

func orderingRecord(t *testing.T) *kgo.Record {
	t.Helper()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := contracts.NewTimestamp(start.Add(time.Minute))
	id := contracts.DedupeKey("tenant-a", "AUTH_BRUTE_FORCE", []string{"192.0.2.1", "alice"}, start)
	alert := &contracts.Alert{SchemaVersion: 1, AlertID: id, TenantID: "tenant-a",
		RuleID: "AUTH_BRUTE_FORCE", RuleVersion: 1,
		Severity: "high", Status: "open", CreatedAt: end, WindowStart: contracts.NewTimestamp(start), WindowEnd: end,
		EntityKey: "192.0.2.1|alice", Summary: "Failed logins", EventCount: 5,
		SampleEventIDs: []string{"00000000-0000-4000-8000-000000000001"}, DedupeKey: id}
	raw, err := json.Marshal(alert)
	require.NoError(t, err)
	return &kgo.Record{Topic: "alerts", Key: []byte("tenant-a|" + id), Value: raw, Offset: 4}
}
