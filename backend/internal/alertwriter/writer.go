// Package alertwriter persists immutable detections before insert-only workflows
// and commits only the durable consumed-next source prefix.
package alertwriter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const (
	queryTimeout      = 5 * time.Second
	rollbackTimeout   = time.Second
	maxPrincipalRunes = 128
	backoffMultiplier = 2
)

// ErrConflict marks differing immutable content for an existing alert identity.
var ErrConflict = errors.New("immutable alert conflict")

// Consumer is the owned, manually committed Kafka batch lifecycle.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
}

// DetectionStore is immutable evidence storage with immediately deduplicated reads.
type DetectionStore interface {
	InsertBatch(context.Context, []*contracts.Alert) error
	Get(context.Context, string, string) (*contracts.Alert, error)
}

// WorkflowStore holds the per-ID transaction lock until both sinks are durable.
type WorkflowStore interface {
	BeginLocked(context.Context, string) (pgx.Tx, error)
	Find(context.Context, pgx.Tx, string) (string, string, error)
	Ensure(context.Context, pgx.Tx, *contracts.Alert, string) error
}

// Writer owns one finite alert batch at a time.
type Writer struct {
	consumer   Consumer
	detections DetectionStore
	workflows  WorkflowStore
	settings   config.WriterConfig
	log        zerolog.Logger
}

// New binds explicit dependencies without creating alternate persistence paths.
func New(consumer Consumer, detections DetectionStore, workflows WorkflowStore,
	settings config.WriterConfig, log zerolog.Logger) *Writer {
	return &Writer{consumer: consumer, detections: detections, workflows: workflows, settings: settings, log: log}
}

// Run processes each poll under one aggregate deadline, releasing ownership on
// all exits. Parent cancellation leaves uncommitted input available for replay.
func (w *Writer) Run(ctx context.Context) error {
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		records, pollErr := w.consumer.PollRecords(ctx, w.settings.BatchSize)
		if pollErr != nil {
			w.consumer.AllowRebalance()
			if ctx.Err() != nil {
				//nolint:nilerr // Parent cancellation is normal lifecycle shutdown; source remains replayable.
				return nil
			}
			return errors.New("poll alert records failed")
		}
		if len(records) == 0 {
			w.consumer.AllowRebalance()
			continue
		}
		batchCtx, cancel := context.WithTimeout(ctx, budgets.Processing)
		err = w.process(batchCtx, records, budgets)
		cancel()
		w.consumer.AllowRebalance()
		if err != nil {
			if ctx.Err() != nil {
				//nolint:nilerr // Cancellation never acknowledges incomplete source work.
				return nil
			}
			return err
		}
	}
	return nil
}

type topicPartition struct {
	topic     string
	partition int32
}

func groupRecords(records []*kgo.Record) (map[topicPartition][]*kgo.Record, []topicPartition, error) {
	partitions := make(map[topicPartition][]*kgo.Record)
	for _, record := range records {
		if record == nil {
			return nil, nil, errors.New("nil alert source record")
		}
		key := topicPartition{record.Topic, record.Partition}
		prior := partitions[key]
		if len(prior) > 0 && record.Offset != prior[len(prior)-1].Offset+1 {
			return nil, nil, errors.New("noncontiguous alert source offsets")
		}
		partitions[key] = append(prior, record)
	}
	keys := make([]topicPartition, 0, len(partitions))
	for key := range partitions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].topic == keys[j].topic {
			return keys[i].partition < keys[j].partition
		}
		return keys[i].topic < keys[j].topic
	})
	return partitions, keys, nil
}

func (w *Writer) process(ctx context.Context, records []*kgo.Record, budgets config.WriterBudgets) error {
	partitions, keys, err := groupRecords(records)
	if err != nil {
		return err
	}
	effectsCtx, cancel := context.WithTimeout(ctx, budgets.Retry)
	defer cancel()
	committable := make([]*kgo.Record, 0, len(keys))
	var effectErr error
	for _, key := range keys {
		var last *kgo.Record
		for _, record := range partitions[key] {
			if err = w.persistRecord(effectsCtx, record); err != nil {
				effectErr = errors.Join(effectErr, err)
				break
			}
			last = record
		}
		if last != nil {
			committable = append(committable, last)
		}
	}
	if len(committable) > 0 {
		commitCtx, cancelCommit := context.WithTimeout(ctx, w.settings.OperationTimeout)
		defer cancelCommit()
		if err = w.consumer.CommitRecords(commitCtx, committable...); err != nil {
			return errors.New("commit durable alert source prefix failed")
		}
	}
	return effectErr
}

func (w *Writer) persistRecord(ctx context.Context, record *kgo.Record) error {
	alert, err := contracts.DecodeAlert(bytes.NewReader(record.Value))
	if err != nil {
		return errors.New("invalid alert encoding")
	}
	if err = ValidateIdentity(alert, record.Key); err != nil {
		return err
	}
	fingerprint, err := ImmutableFingerprint(alert)
	if err != nil {
		return errors.New("alert fingerprint failed")
	}
	backoff := w.settings.RetryMinBackoff
	for attempt := 1; attempt <= w.settings.MaxAttempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		err = w.persist(operationCtx, alert, fingerprint)
		cancel()
		if err == nil || errors.Is(err, ErrConflict) {
			return err
		}
		if attempt == w.settings.MaxAttempts || ctx.Err() != nil {
			break
		}
		w.log.Warn().Int("attempt", attempt).Msg("alert persistence failed; retrying")
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("alert persistence budget exhausted")
		case <-timer.C:
		}
		backoff = min(backoff*backoffMultiplier, w.settings.RetryMaxBackoff)
	}
	// Driver errors may contain payloads/SQL arguments; terminal logs are sanitized.
	return errors.New("alert persistence failed within bounded attempts")
}

func (w *Writer) persist(ctx context.Context, alert *contracts.Alert, fingerprint string) error {
	tx, err := w.workflows.BeginLocked(ctx, alert.AlertID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tenant, winner, err := w.workflows.Find(ctx, tx, alert.AlertID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && (tenant != alert.TenantID || winner != fingerprint) {
		return ErrConflict
	}
	retained, getErr := w.detections.Get(ctx, alert.TenantID, alert.AlertID)
	if getErr != nil && !errors.Is(getErr, storageclickhouse.ErrNotFound) {
		return getErr
	}
	if getErr == nil {
		retainedFingerprint, fpErr := ImmutableFingerprint(retained)
		if fpErr != nil {
			return fpErr
		}
		if retainedFingerprint != fingerprint {
			return ErrConflict
		}
	} else if err = w.detections.InsertBatch(ctx, []*contracts.Alert{alert}); err != nil {
		return err
	}
	if err = w.workflows.Ensure(ctx, tx, alert, fingerprint); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// ValidateIdentity preserves the entire first-pipe AUTH principal suffix; display
// delimiters never replace the length-prefixed contracts.DedupeKey primitive.
func ValidateIdentity(alert *contracts.Alert, key []byte) error {
	if alert == nil {
		return errors.New("nil alert")
	}
	if err := alert.Validate(); err != nil {
		return errors.New("invalid alert schema")
	}
	first, second, hasPipe := strings.Cut(alert.EntityKey, "|")
	entity := []string{first}
	switch alert.RuleID {
	case "AUTH_BRUTE_FORCE":
		if !hasPipe || second == "" || utf8.RuneCountInString(second) > maxPrincipalRunes {
			return errors.New("invalid AUTH entity")
		}
		entity = append(entity, second)
	case "PORT_SCAN":
		if !hasPipe || !validIP(second) {
			return errors.New("invalid SCAN entity")
		}
		entity = append(entity, second)
	case "REQUEST_FLOOD", "DENY_SPIKE":
		if hasPipe {
			return errors.New("invalid volume entity")
		}
	default:
		return errors.New("unsupported alert rule")
	}
	id := contracts.DedupeKey(alert.TenantID, alert.RuleID, entity, alert.WindowStart.Time)
	if !validIP(first) || id != alert.AlertID {
		return errors.New("invalid canonical alert identity")
	}
	if string(key) != alert.TenantID+"|"+alert.AlertID {
		return errors.New("invalid canonical alert source key")
	}
	return nil
}

func validIP(value string) bool {
	addr, err := netip.ParseAddr(value)
	return err == nil && addr.Zone() == "" && !addr.Is4In6() && (!addr.Is6() || !strings.Contains(value, "."))
}

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
// It remains cohesive here until the immediate 03-17 adapter extraction.
type AlertStore struct{ conn storageclickhouse.Conn }

// NewAlertStore binds the native immutable store.
func NewAlertStore(conn storageclickhouse.Conn) *AlertStore { return &AlertStore{conn: conn} }

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
		return nil, storageclickhouse.ErrNotFound
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

// PostgresStore creates workflows without mutating a winner on replay.
type PostgresStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewPostgresStore bounds native transactions independently of caller behavior.
func NewPostgresStore(pool *pgxpool.Pool, timeout time.Duration) *PostgresStore {
	return &PostgresStore{pool: pool, timeout: min(max(timeout, time.Millisecond), queryTimeout)}
}

// BeginLocked acquires a transaction-scoped per-ID lock before any CH effect.
func (s *PostgresStore) BeginLocked(ctx context.Context, id string) (pgx.Tx, error) {
	beginCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tx, err := s.pool.Begin(beginCtx)
	if err != nil {
		return nil, err
	}
	limit := fmt.Sprintf("%dms", max(int64(1), s.timeout.Milliseconds()))
	if _, err = tx.Exec(beginCtx, `SELECT set_config('lock_timeout',$1,true),
		set_config('statement_timeout',$1,true),set_config('idle_in_transaction_session_timeout',$1,true),
		set_config('transaction_timeout',$1,true)`, limit); err != nil {
		rollback(tx)
		return nil, err
	}
	digest := sha256.Sum256([]byte(id))
	// Hash collisions serialize unrelated identities; row uniqueness and full
	// fingerprint validation remain authoritative, so collisions cannot alias data.
	lockID := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	if _, err = tx.Exec(beginCtx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

// Find reads the authoritative winner inside its already locked transaction.
func (*PostgresStore) Find(ctx context.Context, tx pgx.Tx, id string) (string, string, error) {
	var tenant, fingerprint string
	err := tx.QueryRow(ctx, `SELECT tenant_id,immutable_fingerprint
		FROM alert_workflows WHERE alert_id=$1`, id).Scan(&tenant, &fingerprint)
	return tenant, fingerprint, err
}

// Ensure is deliberately insert-only: resolved workflow metadata and seq survive.
func (*PostgresStore) Ensure(ctx context.Context, tx pgx.Tx, alert *contracts.Alert, fingerprint string) error {
	_, err := tx.Exec(ctx, `INSERT INTO alert_workflows (alert_id,tenant_id,created_at,updated_at,
		rule_id,rule_version,severity,entity_key,summary,event_count,window_start,window_end,immutable_fingerprint)
		VALUES ($1,$2,$3,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (alert_id) DO NOTHING`,
		alert.AlertID, alert.TenantID, alert.CreatedAt.Time, alert.RuleID, alert.RuleVersion, alert.Severity,
		alert.EntityKey, alert.Summary, alert.EventCount, alert.WindowStart.Time, alert.WindowEnd.Time, fingerprint)
	return err
}
