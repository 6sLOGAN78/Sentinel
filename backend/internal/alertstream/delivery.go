// Package alertstream projects committed workflows into a gap-safe durable log.
package alertstream

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/query"
)

const (
	batchLimit       = 100
	defaultLimit     = 50
	maxPageLimit     = 200
	operationTimeout = 5 * time.Second
	rollbackTimeout  = time.Second
	minBackoff       = 100 * time.Millisecond
	maxBackoff       = 5 * time.Second
	backoffFactor    = 2
)

// ErrOverflow is terminal: continuing would make positive delivery IDs invalid.
var ErrOverflow = errors.New("alert delivery counter exhausted")

// Delivery uses a decimal string so JavaScript never rounds the durable ID.
type Delivery struct {
	Seq   string              `json:"seq"`
	Alert query.WorkflowAlert `json:"alert"`
}

// BackfillPage is bounded and advances only over processed tenant rows, or to
// the captured committed bound when that entire tenant interval was exhausted.
type BackfillPage struct {
	Items   []Delivery `json:"items"`
	NextSeq string     `json:"next_seq"`
	HasMore bool       `json:"has_more"`
}

// Store shares only PostgreSQL with other replicas; it does no foreign I/O.
type Store struct{ pool *pgxpool.Pool }

// NewStore binds the projector and backfill to the managed application pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Project locks the counter BEFORE discovering pending committed work, and
// holds it through COMMIT. Rollback (including an uncertain ACK) is retry safe.
func (s *Store) Project(parent context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(parent, operationTimeout)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() {
		rollbackCtx, stop := context.WithTimeout(context.Background(), rollbackTimeout)
		defer stop()
		_ = tx.Rollback(rollbackCtx)
	}()
	_, err = tx.Exec(ctx, `SELECT set_config('lock_timeout','1s',true),
	 set_config('statement_timeout','5s',true),set_config('transaction_timeout','5s',true),
	 set_config('idle_in_transaction_session_timeout','5s',true)`)
	if err != nil {
		return 0, err
	}
	var counter int64
	counterSQL := "SELECT next_seq FROM alert_delivery_counter WHERE singleton FOR UPDATE"
	if err = tx.QueryRow(ctx, counterSQL).Scan(&counter); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT p.alert_id,p.tenant_id,d.alert_id IS NOT NULL
	 FROM alert_delivery_pending p LEFT JOIN alert_deliveries d ON d.alert_id=p.alert_id
	 ORDER BY p.alert_id LIMIT $1`, batchLimit)
	if err != nil {
		return 0, err
	}
	ids, tenants, selected := make([]string, 0, batchLimit), make([]string, 0, batchLimit), make([]string, 0, batchLimit)
	for rows.Next() {
		var id, tenant string
		var delivered bool
		if err = rows.Scan(&id, &tenant, &delivered); err != nil {
			rows.Close()
			return 0, err
		}
		selected = append(selected, id)
		if !delivered {
			ids = append(ids, id)
			tenants = append(tenants, tenant)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if int64(len(ids)) > math.MaxInt64-counter {
		return 0, ErrOverflow
	}
	if len(ids) > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO alert_deliveries (delivery_seq,alert_id,tenant_id)
		 SELECT $1::bigint+ordinality,id,tenant FROM unnest($2::text[],$3::text[])
		 WITH ORDINALITY AS batch(id,tenant,ordinality)`, counter, ids, tenants)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, "UPDATE alert_delivery_counter SET next_seq=$1 WHERE singleton", counter+int64(len(ids)))
		if err != nil {
			return 0, err
		}
	}
	if len(selected) > 0 {
		_, err = tx.Exec(ctx, "DELETE FROM alert_delivery_pending WHERE alert_id=ANY($1)", selected)
		if err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(selected), nil
}

// Run drains finite batches, polls even without notifications, and bounds retry.
// Its owner must join it before closing the pool and fail lifecycle on overflow.
func (s *Store) Run(ctx context.Context) error {
	backoff := minBackoff
	for {
		n, err := s.Project(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrOverflow) {
			return err
		}
		delay := time.Second
		if err != nil {
			delay = backoff
			backoff = min(backoff*backoffFactor, maxBackoff)
		} else {
			backoff = minBackoff
			if n > 0 {
				continue
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// Read captures the committed global bound FIRST. The later READ COMMITTED
// page is restricted to that bound: a concurrent projector cannot cause a
// cursor jump over rows committed between these statements.
func (s *Store) Read(parent context.Context, tenant string, after int64, limit int) (BackfillPage, error) {
	page := BackfillPage{Items: make([]Delivery, 0), NextSeq: strconv.FormatInt(after, 10)}
	if after < 0 || limit < 1 || limit > maxPageLimit {
		return page, query.ErrInvalidQuery
	}
	ctx, cancel := context.WithTimeout(parent, operationTimeout)
	defer cancel()
	var bound int64
	counterSQL := "SELECT next_seq FROM alert_delivery_counter WHERE singleton"
	if err := s.pool.QueryRow(ctx, counterSQL).Scan(&bound); err != nil {
		return page, err
	}
	if after > bound {
		return page, query.ErrInvalidQuery
	}
	rows, err := s.pool.Query(ctx, `SELECT d.delivery_seq,w.alert_id,w.tenant_id,w.created_at,w.updated_at,
	 w.rule_id,w.rule_version,w.severity,w.entity_key,w.summary,w.event_count,
	 w.window_start,w.window_end,w.status,w.assignee,w.version
	 FROM alert_deliveries d JOIN alert_workflows w ON w.alert_id=d.alert_id AND w.tenant_id=d.tenant_id
	 WHERE d.tenant_id=$1 AND d.delivery_seq>$2 AND d.delivery_seq<=$3
	 ORDER BY d.delivery_seq LIMIT $4`, tenant, after, bound, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var item Delivery
		w := &item.Alert
		err = rows.Scan(&seq, &w.AlertID, &w.TenantID, &w.CreatedAt, &w.UpdatedAt, &w.RuleID, &w.RuleVersion,
			&w.Severity, &w.EntityKey, &w.Summary, &w.EventCount, &w.WindowStart, &w.WindowEnd,
			&w.Status, &w.Assignee, &w.Version)
		if err != nil {
			return page, err
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		item.Seq = strconv.FormatInt(seq, 10)
		page.Items = append(page.Items, item)
		page.NextSeq = item.Seq
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if !page.HasMore {
		page.NextSeq = strconv.FormatInt(bound, 10)
	}
	return page, nil
}

// Register installs the literal bearer-only route before the alert-ID route.
func (s *Store) Register(e *echo.Echo, verifier *auth.Verifier) {
	e.GET("/api/v1/alerts/backfill", s.backfill, verifier.Middleware)
}

func (s *Store) backfill(c echo.Context) error {
	p, err := auth.RequirePrincipal(c)
	if err != nil {
		return err
	}
	after, limit, err := deliveryOptions(c)
	if err != nil {
		return errs.NewBadRequestError("Invalid delivery query", false, nil, nil, nil)
	}
	page, err := s.Read(c.Request().Context(), p.TenantID, after, limit)
	if errors.Is(err, query.ErrInvalidQuery) {
		return errs.NewBadRequestError("Invalid delivery query", false, nil, nil, nil)
	}
	if err != nil {
		return errs.NewServiceUnavailableError(http.StatusText(http.StatusServiceUnavailable), 1)
	}
	return c.JSON(http.StatusOK, page)
}

func deliveryOptions(c echo.Context) (int64, int, error) {
	values, err := url.ParseQuery(c.Request().URL.RawQuery)
	if err != nil {
		return 0, 0, query.ErrInvalidQuery
	}
	after, limit := int64(0), defaultLimit
	for key, entries := range values {
		if len(entries) != 1 {
			return 0, 0, query.ErrInvalidQuery
		}
		raw := entries[0]
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != raw {
			return 0, 0, query.ErrInvalidQuery
		}
		switch key {
		case "after_seq":
			after = parsed
		case "limit":
			if parsed < 1 || parsed > maxPageLimit {
				return 0, 0, query.ErrInvalidQuery
			}
			limit = int(parsed)
		default:
			return 0, 0, query.ErrInvalidQuery
		}
	}
	return after, limit, nil
}
