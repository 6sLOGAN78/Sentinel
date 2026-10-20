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

	"github.com/6sLOGAN78/sentinel/internal/alertwriter"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	storage "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

type protocolReservation struct {
	tenant, fingerprint string
	payload             []byte
}

// This models transaction visibility and ambiguous outcomes, not native PG.
// Native independent-connection visibility is covered in the integration family.
type protocolWorkflows struct {
	reservation *protocolReservation
	workflow    *contracts.Alert
	txs         []*protocolTx
	failCommit  int
	commitLands bool
}

type protocolTx struct {
	pgx.Tx
	store       *protocolWorkflows
	reservation *protocolReservation
	workflow    *contracts.Alert
	closed      bool
}

func (s *protocolWorkflows) BeginLocked(context.Context, string) (pgx.Tx, error) {
	for _, tx := range s.txs {
		if !tx.closed {
			return nil, errors.New("previous transaction still owns its connection")
		}
	}
	tx := &protocolTx{store: s}
	s.txs = append(s.txs, tx)
	return tx, nil
}

func (s *protocolWorkflows) Find(context.Context, pgx.Tx, string) (string, string, error) {
	if s.workflow == nil {
		return "", "", pgx.ErrNoRows
	}
	fp, err := alertwriter.ImmutableFingerprint(s.workflow)
	return s.workflow.TenantID, fp, err
}

func (*protocolWorkflows) Ensure(_ context.Context, tx pgx.Tx, a *contracts.Alert, _ string) error {
	tx.(*protocolTx).workflow = a
	return nil
}

func (s *protocolWorkflows) FindReservation(_ context.Context, tx pgx.Tx, _ string) (string, string, []byte, error) {
	r := tx.(*protocolTx).reservation
	if r == nil {
		r = s.reservation
	}
	if r == nil {
		return "", "", nil, pgx.ErrNoRows
	}
	return r.tenant, r.fingerprint, append([]byte(nil), r.payload...), nil
}

func (s *protocolWorkflows) InsertReservation(_ context.Context, tx pgx.Tx,
	_, tenant, fp string, payload []byte,
) error {
	if s.reservation == nil {
		tx.(*protocolTx).reservation = &protocolReservation{tenant, fp, append([]byte(nil), payload...)}
	}
	return nil
}

func (tx *protocolTx) Commit(context.Context) error {
	fail := len(tx.store.txs) == tx.store.failCommit
	if !fail || tx.store.commitLands {
		if tx.reservation != nil {
			tx.store.reservation = tx.reservation
		}
		if tx.workflow != nil {
			tx.store.workflow = tx.workflow
		}
	}
	tx.closed = true
	if fail {
		return errors.New("commit outcome unknown")
	}
	return nil
}

func (tx *protocolTx) Rollback(context.Context) error {
	tx.closed = true
	return nil
}

type protocolDetections struct {
	store     *protocolWorkflows
	retained  *contracts.Alert
	inserted  []*contracts.Alert
	insertErr error
	preflight int
}

func (s *protocolDetections) Get(context.Context, string, string) (*contracts.Alert, error) {
	s.preflight++
	if s.retained != nil {
		return s.retained, nil
	}
	return nil, storage.ErrNotFound
}

func (s *protocolDetections) InsertBatch(_ context.Context, alerts []*contracts.Alert) error {
	s.inserted = append(s.inserted, alerts...)
	if s.store.reservation == nil {
		return errors.New("CH insertion preceded committed reservation")
	}
	return s.insertErr
}

func runReservationProtocol(t *testing.T, a *contracts.Alert, store *protocolWorkflows,
	detections *protocolDetections,
) (*orderConsumer, int, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	order := []string{}
	c := &orderConsumer{order: &order, cancel: cancel, record: &kgo.Record{
		Topic: "alerts", Key: []byte(a.TenantID + "|" + a.AlertID), Value: raw, Offset: 4}}
	settings := config.WriterConfig{BatchSize: 1, FlushInterval: time.Millisecond, MaxAttempts: 1,
		RetryMinBackoff: time.Millisecond, RetryMaxBackoff: time.Millisecond,
		OperationTimeout: 50 * time.Millisecond, ShutdownTimeout: time.Second}
	quarantined := 0
	w := alertwriter.New(c, detections, store, settings, zerolog.Nop()).WithQuarantine(
		poisonDLQ{func(context.Context, *kgo.Record, kafkax.DLQReason) error {
			quarantined++
			return nil
		}})
	err = w.Run(ctx)
	return c, quarantined, err
}

func savedProtocolReservation(t *testing.T, a *contracts.Alert) *protocolReservation {
	t.Helper()
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	fp, err := alertwriter.ImmutableFingerprint(a)
	require.NoError(t, err)
	return &protocolReservation{a.TenantID, fp, raw}
}

// Skipping the committed reservation, using changed replay bytes, or treating
// corrupt operational state as poison breaks these public Writer outcomes.
func TestAlertWriterReservationProtocol(t *testing.T) {
	t.Run("commit reservation before CH", func(t *testing.T) {
		s := &protocolWorkflows{}
		d := &protocolDetections{store: s}
		c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
		require.NoError(t, err, "CH insertion must observe an already committed reservation")
		require.Len(t, c.committed, 1)
		require.Zero(t, dlq)
		require.NotNil(t, s.reservation)
		require.NotNil(t, s.workflow)
		require.Len(t, s.txs, 2)
		require.NotSame(t, s.txs[0], s.txs[1])
		require.Equal(t, 1, d.preflight)
	})
	for _, lands := range []bool{false, true} {
		t.Run(map[bool]string{false: "reservation commit rejected", true: "reservation commit answer lost"}[lands],
			func(t *testing.T) {
				s := &protocolWorkflows{failCommit: 1, commitLands: lands}
				d := &protocolDetections{store: s}
				a := identityAlert(t)
				c, dlq, err := runReservationProtocol(t, a, s, d)
				require.Error(t, err)
				require.Empty(t, d.inserted, "reservation commit uncertainty forbids this attempt's CH effect")
				require.Empty(t, c.committed)
				require.Zero(t, dlq)
				require.Nil(t, s.workflow)
				require.Equal(t, lands, s.reservation != nil)
				s.failCommit = 0
				c, dlq, err = runReservationProtocol(t, a, s, d)
				require.NoError(t, err)
				require.Len(t, c.committed, 1)
				require.Zero(t, dlq)
				require.NotNil(t, s.workflow)
			})
	}
	t.Run("CH failure retains only reservation", func(t *testing.T) {
		s := &protocolWorkflows{}
		d := &protocolDetections{store: s, insertErr: errors.New("CH unavailable")}
		c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
		require.Error(t, err)
		require.NotNil(t, s.reservation)
		require.Nil(t, s.workflow)
		require.Empty(t, c.committed)
		require.Zero(t, dlq)
	})
	for _, lands := range []bool{false, true} {
		t.Run(map[bool]string{false: "workflow commit rejected", true: "workflow commit answer lost"}[lands],
			func(t *testing.T) {
				s := &protocolWorkflows{failCommit: 2, commitLands: lands}
				d := &protocolDetections{store: s}
				c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
				require.Error(t, err)
				require.NotNil(t, s.reservation)
				require.Len(t, d.inserted, 1)
				require.Equal(t, lands, s.workflow != nil)
				require.Empty(t, c.committed)
				require.Zero(t, dlq)
				s.failCommit = 0
				d.retained = d.inserted[0]
				prior := s.workflow
				c, _, err = runReservationProtocol(t, identityAlert(t), s, d)
				require.NoError(t, err)
				require.Len(t, c.committed, 1)
				require.Len(t, d.inserted, 1)
				if lands {
					require.Same(t, prior, s.workflow, "replay must preserve existing workflow")
				}
			})
	}
	t.Run("mutable replay uses saved payload", func(t *testing.T) {
		a := identityAlert(t)
		s := &protocolWorkflows{reservation: savedProtocolReservation(t, a)}
		d := &protocolDetections{store: s}
		a.Status = "resolved"
		detected := contracts.NewTimestamp(a.CreatedAt.Add(time.Hour))
		a.DetectedAt = &detected
		c, dlq, err := runReservationProtocol(t, a, s, d)
		require.NoError(t, err)
		require.Len(t, c.committed, 1)
		require.Zero(t, dlq)
		require.Len(t, d.inserted, 1)
		require.Equal(t, "open", d.inserted[0].Status)
		require.Nil(t, d.inserted[0].DetectedAt)
		require.Equal(t, 1, d.preflight)
	})
	t.Run("immutable conflict quarantined before CH", func(t *testing.T) {
		a := identityAlert(t)
		s := &protocolWorkflows{reservation: savedProtocolReservation(t, a)}
		d := &protocolDetections{store: s}
		a.Summary = "different immutable content"
		c, dlq, err := runReservationProtocol(t, a, s, d)
		require.NoError(t, err)
		require.Equal(t, 1, dlq)
		require.Len(t, c.committed, 1)
		require.Empty(t, d.inserted)
		require.Nil(t, s.workflow)
	})
	t.Run("matching orphan supplies canonical payload", func(t *testing.T) {
		orphan := identityAlert(t)
		orphan.Status = "resolved"
		s := &protocolWorkflows{}
		d := &protocolDetections{store: s, retained: orphan}
		c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
		require.NoError(t, err)
		require.Len(t, c.committed, 1)
		require.Zero(t, dlq)
		require.Empty(t, d.inserted)
		require.NotNil(t, s.reservation)
		require.JSONEq(t, string(savedProtocolReservation(t, orphan).payload), string(s.reservation.payload))
		require.Equal(t, 1, d.preflight)
	})
	t.Run("differing orphan creates no claim", func(t *testing.T) {
		orphan := identityAlert(t)
		orphan.Summary = "original"
		s := &protocolWorkflows{}
		d := &protocolDetections{store: s, retained: orphan}
		c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
		require.NoError(t, err)
		require.Len(t, c.committed, 1)
		require.Equal(t, 1, dlq)
		require.Empty(t, d.inserted)
		require.Nil(t, s.reservation)
		require.Nil(t, s.workflow)
	})
	t.Run("corrupt reservation is operational", testCorruptReservations)
}

func testCorruptReservations(t *testing.T) {
	for _, kind := range []string{
		"missing payload", "invalid JSON", "oversized", "unknown field", "schema", "identity", "ID", "tenant", "fingerprint",
	} {
		t.Run(kind, func(t *testing.T) {
			a := identityAlert(t)
			r := savedProtocolReservation(t, a)
			switch kind {
			case "missing payload":
				r.payload = nil
			case "invalid JSON":
				r.payload = []byte(`{"secret":"do not leak"`)
			case "oversized":
				r.payload = []byte(strings.Repeat(" ", 1048577))
			case "unknown field":
				r.payload = append(r.payload[:len(r.payload)-1], []byte(`,"secret":"do not leak"}`)...)
			case "schema":
				a.SchemaVersion = 99
				r.payload, _ = json.Marshal(a)
			case "identity":
				a.EntityKey = "192.0.2.1|other"
				r.payload, _ = json.Marshal(a)
			case "ID":
				a.AlertID = strings.Repeat("a", 64)
				r.payload, _ = json.Marshal(a)
			case "tenant":
				r.tenant = "other"
			case "fingerprint":
				r.fingerprint = strings.Repeat("a", 64)
			}
			s := &protocolWorkflows{reservation: r}
			d := &protocolDetections{store: s}
			c, dlq, err := runReservationProtocol(t, identityAlert(t), s, d)
			require.Error(t, err)
			require.NotErrorIs(t, err, alertwriter.ErrConflict)
			require.NotContains(t, err.Error(), "do not leak")
			require.Zero(t, dlq)
			require.Empty(t, c.committed)
			require.Empty(t, d.inserted)
			require.Nil(t, s.workflow)
		})
	}
}
