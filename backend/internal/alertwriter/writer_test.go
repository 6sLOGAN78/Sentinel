package alertwriter_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storage "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
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
	t           *testing.T
	order       *[]string
	commitErr   error
	store       *orderWorkflows
	reservation *protocolReservation
	preparing   bool
}

func (tx *orderTx) Commit(context.Context) error {
	if tx.preparing {
		*tx.order = append(*tx.order, "reservation-commit")
		tx.store.reservation = tx.reservation
		return nil
	}
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
	tx          *orderTx
	ensureErr   error
	findErr     error
	winner      *contracts.Alert
	reservation *protocolReservation
	begins      int
}

func (s *orderWorkflows) BeginLocked(context.Context, string) (pgx.Tx, error) {
	*s.tx.order = append(*s.tx.order, "lock")
	s.begins++
	tx := *s.tx
	tx.store, tx.preparing = s, s.begins%2 == 1
	tx.reservation = s.reservation
	return &tx, nil
}
func (s *orderWorkflows) FindReservation(_ context.Context, tx pgx.Tx, _ string) (string, string, []byte, error) {
	r := tx.(*orderTx).reservation
	if r == nil {
		return "", "", nil, pgx.ErrNoRows
	}
	return r.tenant, r.fingerprint, r.payload, nil
}
func (*orderWorkflows) InsertReservation(_ context.Context, tx pgx.Tx, _, tenant, fp string, payload []byte) error {
	tx.(*orderTx).reservation = &protocolReservation{tenant, fp, payload}
	return nil
}
func (s *orderWorkflows) Find(context.Context, pgx.Tx, string) (string, string, error) {
	*s.tx.order = append(*s.tx.order, "pg-preflight")
	if s.findErr != nil {
		return "", "", s.findErr
	}
	if s.winner != nil {
		fingerprint, err := alertwriter.ImmutableFingerprint(s.winner)
		return s.winner.TenantID, fingerprint, err
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
	retained  *contracts.Alert
}

func (s *orderDetections) Get(context.Context, string, string) (*contracts.Alert, error) {
	*s.order = append(*s.order, "ch-preflight")
	if s.retained != nil {
		return s.retained, nil
	}
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
			[]string{"lock", "pg-preflight", "ch-preflight", "reservation-commit", "rollback",
				"lock", "pg-preflight", "ch-ack", "pg-ensure", "pg-commit", "rollback", "source-commit"}},
		{"CH failure", failure, nil, nil, nil, []string{"lock", "pg-preflight", "ch-preflight",
			"reservation-commit", "rollback", "lock", "pg-preflight", "ch-ack", "rollback"}},
		{"PG failure after CH", nil, failure, nil, nil,
			[]string{"lock", "pg-preflight", "ch-preflight", "reservation-commit", "rollback",
				"lock", "pg-preflight", "ch-ack", "pg-ensure", "rollback"}},
		{"PG commit failure", nil, nil, failure, nil,
			[]string{"lock", "pg-preflight", "ch-preflight", "reservation-commit", "rollback",
				"lock", "pg-preflight", "ch-ack", "pg-ensure", "pg-commit", "rollback"}},
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
	t.Run("equal winner skips workflow INSERT", func(t *testing.T) {
		order := []string{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		winner := identityAlert(t)
		tx := &orderTx{t: t, order: &order}
		consumer := &orderConsumer{order: &order, record: orderingRecord(t), cancel: cancel}
		settings := config.WriterConfig{BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
			RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
			OperationTimeout: 25 * time.Millisecond, ShutdownTimeout: time.Second}
		writer := alertwriter.New(consumer, &orderDetections{order: &order, retained: winner},
			&orderWorkflows{tx: tx, winner: winner, ensureErr: errors.New("replay must not INSERT")}, settings, zerolog.Nop())
		require.NoError(t, writer.Run(ctx), "an authoritative identical winner needs no new workflow INSERT")
		require.Equal(t, []string{"lock", "pg-preflight", "ch-preflight", "reservation-commit", "rollback",
			"lock", "pg-preflight", "pg-commit", "rollback", "source-commit"}, order)
	})
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

type poisonBatchConsumer struct {
	records   []*kgo.Record
	committed []*kgo.Record
	released  int
	commitCtx error
	cancel    context.CancelFunc
}

func (c *poisonBatchConsumer) PollRecords(ctx context.Context, _ int) ([]*kgo.Record, error) {
	if len(c.records) > 0 {
		records := c.records
		c.records = nil
		return records, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *poisonBatchConsumer) CommitRecords(ctx context.Context, records ...*kgo.Record) error {
	c.committed, c.commitCtx = records, ctx.Err()
	c.cancel()
	return ctx.Err()
}

func (c *poisonBatchConsumer) AllowRebalance() { c.released++ }

type poisonDLQ struct {
	publish func(context.Context, *kgo.Record, kafkax.DLQReason) error
}

func (p poisonDLQ) Publish(ctx context.Context, r *kgo.Record, reason kafkax.DLQReason) error {
	return p.publish(ctx, r, reason)
}

func poisonAlertWriter(t *testing.T, records []*kgo.Record,
	publish func(context.Context, *kgo.Record, kafkax.DLQReason) error,
) (*alertwriter.Writer, *poisonBatchConsumer, context.Context, *[]string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	c := &poisonBatchConsumer{records: records, cancel: cancel}
	order := []string{}
	tx := &orderTx{t: t, order: &order}
	settings := config.WriterConfig{BatchSize: 500, FlushInterval: time.Millisecond, MaxAttempts: 2,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}
	w := alertwriter.New(c, &orderDetections{order: &order}, &orderWorkflows{tx: tx}, settings, zerolog.Nop()).
		WithQuarantine(poisonDLQ{publish})
	return w, c, ctx, &order
}

func TestAlertWriterPoisonPrefix(t *testing.T) {
	t.Run("failed poison partition and safe two-store prefix", func(t *testing.T) {
		good := orderingRecord(t)
		good.Offset = 0
		bad := &kgo.Record{Topic: "alerts", Offset: 1, Value: []byte("{")}
		later := *good
		later.Offset = 2
		safe := *good
		safe.Partition = 1
		calls := 0
		w, c, ctx, order := poisonAlertWriter(t, []*kgo.Record{good, bad, &later, &safe},
			func(context.Context, *kgo.Record, kafkax.DLQReason) error {
				calls++
				return errors.New("secret driver details")
			})
		_ = w.Run(ctx) // the commit observer cancels only after recording the durable prefixes
		require.Equal(t, 2, calls)
		require.Len(t, c.committed, 2)
		for _, r := range c.committed {
			require.Equal(t, int64(0), r.Offset)
		}
		require.Equal(t, 2, countOrder(*order, "ch-ack"))
		require.Equal(t, 2, countOrder(*order, "pg-commit"))
		require.Equal(t, 2, countOrder(*order, "reservation-commit"))
		require.Equal(t, 1, c.released)
	})
	t.Run("reject before sink and release winner lock before quarantine", testAlertPoisonClassification)
}

func testAlertPoisonClassification(t *testing.T) {
	for _, kind := range []string{"decode", "schema", "key", "hash", "entity", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			r := orderingRecord(t)
			var a contracts.Alert
			require.NoError(t, json.Unmarshal(r.Value, &a))
			switch kind {
			case "decode":
				r.Value = []byte("{")
			case "schema":
				a.SchemaVersion = 9
			case "key":
				r.Key = []byte("other|" + a.AlertID)
			case "hash":
				a.AlertID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				r.Key = []byte(a.TenantID + "|" + a.AlertID)
			case "entity":
				a.EntityKey = "broken"
			}
			if kind != "decode" {
				r.Value, _ = json.Marshal(a)
			}
			var order *[]string
			w, c, ctx, observed := poisonAlertWriter(t, []*kgo.Record{r},
				func(_ context.Context, _ *kgo.Record, reason kafkax.DLQReason) error {
					if kind == "conflict" {
						require.Contains(t, *order, "rollback", "release global ID lock before delayed DLQ")
					}
					if kind == "decode" {
						require.Equal(t, kafkax.DLQReasonDecode, reason)
					} else {
						require.Equal(t, kafkax.DLQReasonValidation, reason)
					}
					return nil
				})
			order = observed
			if kind == "conflict" {
				// Existing identity is authoritative; differing content may not reach CH.
				winner := identityAlert(t)
				winner.Summary = "original"
				tx := &orderTx{t: t, order: order}
				w = alertwriter.New(c, &orderDetections{order: order},
					&orderWorkflows{tx: tx, winner: winner}, config.WriterConfig{
						BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
						RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
						OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}, zerolog.Nop()).
					WithQuarantine(poisonDLQ{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
						require.Contains(t, *order, "rollback")
						return nil
					}})
			}
			require.NoError(t, w.Run(ctx))
			require.Len(t, c.committed, 1)
			require.Zero(t, countOrder(*order, "ch-ack"))
		})
	}
}

func countOrder(order []string, value string) int {
	count := 0
	for _, step := range order {
		if step == value {
			count++
		}
	}
	return count
}

func TestWorkerAggregateBudget(t *testing.T) {
	for _, mode := range []string{"slow", "shared retries", "parent cancellation", "shutdown deadline"} {
		t.Run(mode, func(t *testing.T) { testAlertAggregateBudget(t, mode) })
	}
}

func testAlertAggregateBudget(t *testing.T, mode string) {
	var records []*kgo.Record
	for n := range int64(400) {
		records = append(records, &kgo.Record{Topic: "alerts", Offset: n, Value: []byte("{")})
	}
	attempts := make(map[int64]int)
	acks := 0
	var c *poisonBatchConsumer
	w, consumer, ctx, _ := poisonAlertWriter(t, records,
		func(op context.Context, r *kgo.Record, _ kafkax.DLQReason) error {
			attempts[r.Offset]++
			if mode == "shared retries" && attempts[r.Offset] == 1 {
				return errors.New("temporary failure")
			}
			if mode == "parent cancellation" {
				c.cancel()
				return op.Err()
			}
			if mode == "slow" || mode == "shutdown deadline" {
				timer := time.NewTimer(20 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-op.Done():
					return op.Err()
				case <-timer.C:
				}
			}
			acks++
			return nil
		})
	c = consumer
	if mode == "shutdown deadline" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 75*time.Millisecond)
		defer cancel()
	}
	start := time.Now()
	_ = w.Run(ctx)
	require.Less(t, time.Since(start), 400*time.Millisecond)
	require.Equal(t, 1, c.released)
	if mode == "parent cancellation" {
		require.Empty(t, c.committed)
		return
	}
	require.Len(t, c.committed, 1)
	require.NoError(t, c.commitCtx)
	require.Equal(t, int64(acks-1), c.committed[0].Offset)
	if mode == "shared retries" {
		require.Equal(t, 1, acks, "poison success must not reset the aggregate retry allowance")
	}
}

type recordingWaiter struct {
	ceilings []time.Duration
	fail     error
}

func (r *recordingWaiter) wait(_ context.Context, ceiling time.Duration) error {
	r.ceilings = append(r.ceilings, ceiling)
	return r.fail
}

func jitterAlertWriter(t *testing.T, records []*kgo.Record, waiter *recordingWaiter,
	publish func(context.Context, *kgo.Record, kafkax.DLQReason) error,
) (*alertwriter.Writer, *poisonBatchConsumer, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	c := &poisonBatchConsumer{records: records, cancel: cancel}
	order := []string{}
	settings := config.WriterConfig{BatchSize: 500, FlushInterval: time.Millisecond, MaxAttempts: 3,
		RetryMinBackoff: 100 * time.Millisecond, RetryMaxBackoff: 150 * time.Millisecond,
		OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}
	w := alertwriter.New(c, &orderDetections{order: &order}, &orderWorkflows{tx: &orderTx{t: t, order: &order}},
		settings, zerolog.Nop()).WithQuarantine(poisonDLQ{publish}).WithRetryWaiter(waiter.wait)
	return w, c, ctx
}

func TestAlertWriterRetryUsesJitteredWaiter(t *testing.T) {
	t.Run("transient failures recover within ceilings", func(t *testing.T) {
		waiter := &recordingWaiter{}
		calls := 0
		w, c, ctx := jitterAlertWriter(t, []*kgo.Record{{Topic: "alerts", Offset: 0, Value: []byte("{")}}, waiter,
			func(context.Context, *kgo.Record, kafkax.DLQReason) error {
				calls++
				if calls <= 2 {
					return errors.New("dlq recovering")
				}
				return nil
			})
		require.NoError(t, w.Run(ctx))
		require.Equal(t, 3, calls)
		require.Equal(t, []time.Duration{100 * time.Millisecond, 150 * time.Millisecond}, waiter.ceilings,
			"waits receive the capped exponential ceiling, not a fixed timer")
		require.Len(t, c.committed, 1)
	})
	t.Run("shared budget is not reset by success", func(t *testing.T) {
		waiter := &recordingWaiter{}
		attempts := make(map[int64]int)
		records := []*kgo.Record{
			{Topic: "alerts", Offset: 0, Value: []byte("{")}, {Topic: "alerts", Offset: 1, Value: []byte("{")},
		}
		w, c, ctx := jitterAlertWriter(t, records, waiter,
			func(_ context.Context, r *kgo.Record, _ kafkax.DLQReason) error {
				attempts[r.Offset]++
				if r.Offset == 0 && attempts[0] > 2 {
					return nil
				}
				return errors.New("dlq outage")
			})
		_ = w.Run(ctx)
		require.Equal(t, map[int64]int{0: 3, 1: 1}, attempts)
		require.Len(t, waiter.ceilings, 2, "remaining allowance is shared across the batch")
		require.Len(t, c.committed, 1)
		require.Equal(t, int64(0), c.committed[0].Offset)
	})
	t.Run("wait failure is terminal without commit", func(t *testing.T) {
		waiter := &recordingWaiter{fail: errors.New("wait aborted")}
		calls := 0
		w, c, ctx := jitterAlertWriter(t, []*kgo.Record{{Topic: "alerts", Offset: 0, Value: []byte("{")}}, waiter,
			func(context.Context, *kgo.Record, kafkax.DLQReason) error {
				calls++
				return errors.New("dlq outage")
			})
		require.Error(t, w.Run(ctx))
		require.Equal(t, 1, calls)
		require.Len(t, waiter.ceilings, 1)
		require.Empty(t, c.committed)
	})
}

const (
	alertTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	alertSpanID  = "00f067aa0ba902b7"
)

func alertTracing(t *testing.T) (*telemetry.Tracing, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	// Ratio 1: a root span would be sampled, so "no span" proves the writer
	// starts none without a sampled alert record.
	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: 1, OTLPEndpoint: "http://127.0.0.1:4318"},
		config.ServiceAlertWriter, telemetry.WithExporter(exporter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })
	return tr, exporter
}

func tracedAlertRecord(t *testing.T, flags string) *kgo.Record {
	t.Helper()
	record := orderingRecord(t)
	if flags != "" {
		record.Headers = []kgo.RecordHeader{{Key: "traceparent",
			Value: []byte("00-" + alertTraceID + "-" + alertSpanID + "-" + flags)}}
	}
	return record
}

func runTracedAlertWriter(t *testing.T, record *kgo.Record) []tracetest.SpanStub {
	t.Helper()
	tr, exporter := alertTracing(t)
	order := []string{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	consumer := &orderConsumer{order: &order, record: record, cancel: cancel}
	settings := config.WriterConfig{BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		OperationTimeout: 25 * time.Millisecond, ShutdownTimeout: time.Second}
	writer := alertwriter.New(consumer, &orderDetections{order: &order},
		&orderWorkflows{tx: &orderTx{t: t, order: &order}}, settings, zerolog.Nop())
	require.Same(t, writer, writer.WithTracing(tr.Provider(), tr.Propagator()))
	require.NoError(t, writer.Run(ctx))
	require.Equal(t, []*kgo.Record{record}, consumer.committed)
	require.NoError(t, tr.ForceFlush(t.Context()))
	return exporter.GetSpans()
}

// The alert record's header (injected under the detect span) makes
// store_alert a child in the same trace.
func TestAlertWriterStoreAlertSpanFollowsSampledRecord(t *testing.T) {
	spans := runTracedAlertWriter(t, tracedAlertRecord(t, "01"))
	require.Len(t, spans, 1)
	span := spans[0]
	require.Equal(t, "store_alert", span.Name)
	require.Equal(t, alertTraceID, span.SpanContext.TraceID().String())
	require.Equal(t, alertSpanID, span.Parent.SpanID().String())
	attrs := map[attribute.Key]int64{}
	for _, kv := range span.Attributes {
		attrs[kv.Key] = kv.Value.AsInt64()
	}
	require.Equal(t, map[attribute.Key]int64{"partition": 0, "offset": 4}, attrs)
	require.Equal(t, codes.Unset, span.Status.Code)
}

func TestAlertWriterStoreAlertSpanSkipsUnsampledRecord(t *testing.T) {
	for _, flags := range []string{"00", ""} {
		t.Run("flags="+flags, func(t *testing.T) {
			require.Empty(t, runTracedAlertWriter(t, tracedAlertRecord(t, flags)))
		})
	}
}

// Retry warnings emitted while handling a sampled alert record carry trace_id.
func TestAlertWriterRetryLogCarriesTraceID(t *testing.T) {
	tr, exporter := alertTracing(t)
	logs := &strings.Builder{}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	poison := &kgo.Record{Topic: "alerts", Offset: 0, Value: []byte("{"), Headers: []kgo.RecordHeader{
		{Key: "traceparent", Value: []byte("00-" + alertTraceID + "-" + alertSpanID + "-01")}}}
	c := &poisonBatchConsumer{records: []*kgo.Record{poison}, cancel: cancel}
	order := []string{}
	settings := config.WriterConfig{BatchSize: 500, FlushInterval: time.Millisecond, MaxAttempts: 2,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}
	calls := 0
	w := alertwriter.New(c, &orderDetections{order: &order}, &orderWorkflows{tx: &orderTx{t: t, order: &order}},
		settings, zerolog.New(logs)).WithQuarantine(poisonDLQ{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
		calls++
		if calls == 1 {
			return errors.New("dlq recovering")
		}
		return nil
	}}).WithRetryWaiter((&recordingWaiter{}).wait).WithTracing(tr.Provider(), tr.Propagator())
	require.NoError(t, w.Run(ctx))
	require.Len(t, c.committed, 1)
	require.NoError(t, tr.ForceFlush(t.Context()))
	require.Len(t, exporter.GetSpans(), 1)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &line))
	require.Equal(t, "alert effect failed; retrying", line["message"])
	require.Equal(t, alertTraceID, line["trace_id"])
}
