package clickhouse_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storage "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

type alertConn struct {
	driver.Conn
	prepare func(context.Context, string) (driver.Batch, error)
	query   func(context.Context, string, ...any) driver.Row
}

func (c *alertConn) PrepareBatch(ctx context.Context, query string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
	return c.prepare(ctx, query)
}

func (c *alertConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return c.query(ctx, query, args...)
}

type alertBatch struct {
	driver.Batch
	appendErr, sendErr error
	sent, aborted      bool
	rows               [][]any
}

func (b *alertBatch) Append(values ...any) error {
	b.rows = append(b.rows, values)
	return b.appendErr
}
func (b *alertBatch) Send() error  { b.sent = b.sendErr == nil; return b.sendErr }
func (b *alertBatch) IsSent() bool { return b.sent }
func (b *alertBatch) Abort() error { b.aborted = true; return nil }

type alertRow struct {
	driver.Row
	values []any
	err    error
}

func (r alertRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}

func fingerprintBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func alertFixture() *contracts.Alert {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := contracts.NewTimestamp(start.Add(time.Minute))
	id := contracts.DedupeKey("tenant-a", "AUTH_BRUTE_FORCE", []string{"192.0.2.1", "alice"}, start)
	return &contracts.Alert{SchemaVersion: 1, AlertID: id, TenantID: "tenant-a", RuleID: "AUTH_BRUTE_FORCE",
		RuleVersion: 1, Severity: "high", Status: "open", CreatedAt: end, WindowStart: contracts.NewTimestamp(start),
		WindowEnd: end, EntityKey: "192.0.2.1|alice", Summary: "Failed logins", EventCount: 5,
		SampleEventIDs: []string{"00000000-0000-4000-8000-000000000001"}, DedupeKey: id}
}

// Reusing a sent/failed native block or omitting Abort breaks these assertions.
func TestAlertStoreFreshBatch(t *testing.T) {
	for _, failure := range []string{"append", "send", "nil"} {
		t.Run(failure, func(t *testing.T) {
			first := &alertBatch{}
			if failure == "append" {
				first.appendErr = errors.New("append failed")
			}
			if failure == "send" {
				first.sendErr = errors.New("send failed")
			}
			second := &alertBatch{}
			batches := []*alertBatch{first, second}
			calls := 0
			conn := &alertConn{prepare: func(ctx context.Context, query string) (driver.Batch, error) {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				require.Contains(t, query, "INSERT INTO alerts")
				batch := batches[calls]
				calls++
				return batch, nil
			}}
			store := storage.NewAlertStore(conn)
			input := alertFixture()
			if failure == "nil" {
				input = nil
			}
			require.Error(t, store.InsertBatch(t.Context(), []*contracts.Alert{input}))
			require.Equal(t, 1, calls, "each failed attempt must prepare its own native batch")
			require.True(t, first.aborted, "unsent native batch must release its resources")
			require.NoError(t, store.InsertBatch(t.Context(), []*contracts.Alert{alertFixture()}))
			require.Equal(t, 2, calls)
			require.True(t, second.sent)
			require.False(t, second.aborted)
			require.Len(t, second.rows, 1)
			require.Equal(t, "tenant-a", second.rows[0][0])
			require.NoError(t, store.InsertBatch(t.Context(), nil))
			require.Equal(t, 2, calls, "empty input must not allocate a block")
		})
	}
}

// SQL must aggregate before returning and bind tenant/ID values independently.
func TestAlertStoreTenantQueries(t *testing.T) {
	alert := alertFixture()
	raw, err := json.Marshal(alert)
	require.NoError(t, err)
	// Compute independently from production fingerprint policy.
	immutable := *alert
	immutable.Status = ""
	immutable.DetectedAt = nil
	canonical, err := json.Marshal(immutable)
	require.NoError(t, err)
	fingerprint := fingerprintBytes(canonical)
	t.Run("detail", func(t *testing.T) {
		calls := 0
		conn := &alertConn{query: func(ctx context.Context, query string, args ...any) driver.Row {
			calls++
			_, ok := ctx.Deadline()
			require.True(t, ok)
			require.Contains(t, query, "tenant_id={tenant:String}")
			require.Contains(t, query, "alert_id={id:String}")
			require.Contains(t, query, "uniqExact(immutable_fingerprint)")
			require.NotContains(t, strings.ToUpper(query), "FINAL")
			require.NotContains(t, query, alert.AlertID)
			require.Equal(t, []any{driverclickhouse.Named("tenant", alert.TenantID), driverclickhouse.Named("id", alert.AlertID)}, args)
			return alertRow{values: []any{string(raw), fingerprint, uint64(1)}}
		}}
		got, getErr := storage.NewAlertStore(conn).Get(t.Context(), alert.TenantID, alert.AlertID)
		require.NoError(t, getErr)
		require.Equal(t, alert, got)
		require.Equal(t, 1, calls)
	})
	t.Run("count", func(t *testing.T) {
		for _, since := range []time.Time{{}, alert.CreatedAt.Time} {
			conn := &alertConn{query: func(ctx context.Context, query string, args ...any) driver.Row {
				_, ok := ctx.Deadline()
				require.True(t, ok)
				require.Contains(t, query, "SELECT uniqExact(alert_id)")
				require.Contains(t, query, "tenant_id={tenant:String}")
				require.NotContains(t, query, "tenant-a' OR 1=1 --")
				want := []any{driverclickhouse.Named("tenant", "tenant-a' OR 1=1 --")}
				if !since.IsZero() {
					require.Contains(t, query, "created_at >= {since:DateTime64(3)}")
					want = append(want, driverclickhouse.Named("since", since))
				}
				require.Equal(t, want, args)
				return alertRow{values: []any{uint64(2)}}
			}}
			count, countErr := storage.NewAlertStore(conn).CountUnique(t.Context(), "tenant-a' OR 1=1 --", since)
			require.NoError(t, countErr)
			require.Equal(t, uint64(2), count)
		}
	})
	t.Run("absence and inconsistent retained content", func(t *testing.T) {
		for _, row := range []alertRow{{err: sql.ErrNoRows}, {values: []any{string(raw), fingerprint, uint64(2)}}, {values: []any{string(raw), "wrong", uint64(1)}}} {
			conn := &alertConn{query: func(context.Context, string, ...any) driver.Row { return row }}
			got, getErr := storage.NewAlertStore(conn).Get(t.Context(), alert.TenantID, alert.AlertID)
			require.Error(t, getErr)
			require.Nil(t, got)
			if row.err != nil {
				require.ErrorIs(t, getErr, storage.ErrNotFound)
			}
		}
	})
}
