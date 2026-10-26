package alertstream

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// Listener owns one dedicated connection, independent of the query pool and
// number of clients. Notifications only wake durable reads; they carry no ID.
type Listener struct {
	config    *pgx.ConnConfig
	streams   *Handler
	connected atomic.Bool
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

// Run reconnects and re-LISTENs with cancellable 100ms..5s backoff. Its owner
// joins this goroutine before the database pool closes.
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
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
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
