package alertstream

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"github.com/6sLOGAN78/sentinel/internal/retry"
)

// Listener owns one dedicated connection, independent of the query pool and
// number of clients. Notifications only wake durable reads; they carry no ID.
type Listener struct {
	config    *pgx.ConnConfig
	streams   *Handler
	connected atomic.Bool
	wait      retry.Waiter
}

// NewListener validates trusted startup configuration without opening a socket.
func NewListener(connectionString string, streams *Handler) (*Listener, error) {
	cfg, err := pgx.ParseConfig(connectionString)
	if err != nil {
		return nil, errors.New("invalid alert listener configuration")
	}
	cfg.RuntimeParams["application_name"] = "sentinel-alert-listener"
	cfg.ConnectTimeout = operationTimeout
	return &Listener{config: cfg, streams: streams}, nil
}

// WithRetryWaiter replaces the full-jitter reconnect wait before Run, chiefly
// so tests can observe ceilings without sleeping. Nil restores retry.Wait.
func (l *Listener) WithRetryWaiter(waiter retry.Waiter) *Listener {
	l.wait = waiter
	return l
}

// Ready reports actual LISTEN state, including the disconnected retry interval.
func (l *Listener) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !l.connected.Load() {
		return errors.New("alert listener disconnected")
	}
	return nil
}

// Run reconnects and re-LISTENs with cancellable full-jitter backoff whose
// ceiling doubles from 100ms to 5s, so replicas that lost PostgreSQL together
// do not reconnect together. Its owner joins this goroutine before the
// database pool closes.
func (l *Listener) Run(ctx context.Context) error {
	defer l.connected.Store(false)
	backoff := minBackoff
	for ctx.Err() == nil {
		connected := l.listen(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if connected {
			backoff = minBackoff
		}
		if waitRetry(ctx, l.wait, backoff) != nil {
			return nil
		}
		backoff = min(backoff*backoffFactor, maxBackoff)
	}
	return nil
}

func (l *Listener) listen(ctx context.Context) bool {
	connectCtx, stop := context.WithTimeout(ctx, operationTimeout)
	conn, err := pgx.ConnectConfig(connectCtx, l.config)
	if err != nil {
		stop()
		return false
	}
	defer func() {
		l.connected.Store(false)
		closeCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	_, err = conn.Exec(connectCtx, "LISTEN sentinel_alert_delivery")
	stop()
	if err != nil {
		return false
	}
	l.connected.Store(true)
	for {
		_, err = conn.WaitForNotification(ctx)
		if err != nil {
			return true
		}
		l.streams.notify()
	}
}
