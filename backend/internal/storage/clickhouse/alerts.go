package clickhouse

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// ErrConflict marks inconsistent immutable content for an alert identity.
var ErrConflict = errors.New("immutable alert conflict")

// ImmutableFingerprint excludes workflow status and wall-clock detection time.
func ImmutableFingerprint(alert *contracts.Alert) (string, error) {
	if alert == nil {
		return "", errors.New("nil immutable alert")
	}
	immutable := *alert
	immutable.Status = ""
	immutable.DetectedAt = nil
	raw, err := json.Marshal(&immutable)
	if err != nil {
		return "", fmt.Errorf("encode immutable alert: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// AlertStore uses fresh native blocks and tenant-scoped reads across all parts.
type AlertStore struct{ conn Conn }

// NewAlertStore binds the native immutable store.
func NewAlertStore(conn Conn) *AlertStore { return &AlertStore{conn: conn} }

// InsertBatch prepares a fresh block on every attempt and aborts unsent batches.
func (s *AlertStore) InsertBatch(ctx context.Context, alerts []*contracts.Alert) error {
	if len(alerts) == 0 {
		return nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	batch, err := s.conn.PrepareBatch(queryCtx, `INSERT INTO alerts
		(tenant_id,alert_id,created_at,rule_id,rule_version,severity,window_start,window_end,
		entity_key,summary,event_count,sample_event_ids,immutable_fingerprint,canonical_payload)`)
	if err != nil {
		return err
	}
	defer func() {
		if !batch.IsSent() {
			_ = batch.Abort()
		}
	}()
	for _, alert := range alerts {
		if alert == nil {
			return errors.New("nil immutable alert")
		}
		fingerprint, fpErr := ImmutableFingerprint(alert)
		if fpErr != nil {
			return fpErr
		}
		raw, marshalErr := json.Marshal(alert)
		if marshalErr != nil {
			return marshalErr
		}
		if err = batch.Append(alert.TenantID, alert.AlertID, alert.CreatedAt.Time, alert.RuleID,
			int64(alert.RuleVersion), alert.Severity, alert.WindowStart.Time, alert.WindowEnd.Time,
			alert.EntityKey, alert.Summary, int64(alert.EventCount), alert.SampleEventIDs,
			fingerprint, string(raw)); err != nil {
			return err
		}
	}
	return batch.Send()
}

// Get detects conflicting retained orphans and returns one visible identity
// immediately, without FINAL, background merges or partition-local assumptions.
func (s *AlertStore) Get(ctx context.Context, tenant, id string) (*contracts.Alert, error) {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var raw, fingerprint string
	var variants uint64
	err := s.conn.QueryRow(queryCtx, `SELECT any(canonical_payload),any(immutable_fingerprint),
		uniqExact(immutable_fingerprint) FROM alerts
		WHERE tenant_id={tenant:String} AND alert_id={id:String} HAVING count()>0`,
		driverclickhouse.Named("tenant", tenant), driverclickhouse.Named("id", id)).Scan(&raw, &fingerprint, &variants)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if variants != 1 {
		return nil, ErrConflict
	}
	alert, err := contracts.DecodeAlert(strings.NewReader(raw))
	if err != nil {
		return nil, ErrConflict
	}
	actual, err := ImmutableFingerprint(alert)
	if err != nil || actual != fingerprint || alert.TenantID != tenant || alert.AlertID != id {
		return nil, ErrConflict
	}
	return alert, nil
}

// CountUnique deduplicates identity across partitions before asynchronous merges.
func (s *AlertStore) CountUnique(ctx context.Context, tenant string, since time.Time) (uint64, error) {
	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	query := `SELECT uniqExact(alert_id) FROM alerts WHERE tenant_id={tenant:String}`
	args := []any{driverclickhouse.Named("tenant", tenant)}
	if !since.IsZero() {
		query += ` AND created_at >= {since:DateTime64(3)}`
		args = append(args, driverclickhouse.Named("since", since))
	}
	var count uint64
	err := s.conn.QueryRow(queryCtx, query, args...).Scan(&count)
	return count, err
}
