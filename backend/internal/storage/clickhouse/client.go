// Package clickhouse provides the native ClickHouse event persistence adapter.
package clickhouse

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	driverclickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	dialTimeout        = 5 * time.Second
	readTimeout        = 30 * time.Second
	connectionMaxAge   = time.Hour
	maxOpenConnections = 4
	maxIdleConnections = 2
)

// Conn is the native driver interface used by event storage and integration tests.
type Conn = driver.Conn

// Open creates and verifies a bounded native ClickHouse connection pool.
func Open(ctx context.Context, cfg config.ClickHouseConfig) (Conn, error) {
	conn, err := driverclickhouse.Open(&driverclickhouse.Options{
		Addr: []string{cfg.NativeAddress},
		Auth: driverclickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		DialContext:     dialWithinDeadline,
		DialTimeout:     dialTimeout,
		ReadTimeout:     readTimeout,
		MaxOpenConns:    maxOpenConnections,
		MaxIdleConns:    maxIdleConnections,
		ConnMaxLifetime: connectionMaxAge,
		Compression:     &driverclickhouse.Compression{Method: driverclickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err = conn.Ping(ctx); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	return conn, nil
}

// dialWithinDeadline connects within ctx and keeps the driver's handshake
// inside ctx's deadline. clickhouse-go v2.49 otherwise dials with
// net.DialTimeout and bounds the handshake by DialTimeout alone, so an
// operation timeout shorter than DialTimeout was overrun against a server that
// accepts but never answers (a paused container). ctx is the driver's acquire
// context, whose deadline is min(caller deadline, now+DialTimeout).
func dialWithinDeadline(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial clickhouse: %w", err)
	}
	limit, ok := ctx.Deadline()
	if !ok {
		return conn, nil
	}

	return &handshakeConn{Conn: conn, limit: limit}, nil
}

// handshakeConn clamps deadlines to limit until the handshake ends. The driver
// clears the deadline (zero time) when the handshake returns; from then on, or
// once limit has passed, deadlines pass through unchanged, so pooled reuse
// follows each later operation's own context.
type handshakeConn struct {
	net.Conn

	limit    time.Time
	released atomic.Bool
}

func (c *handshakeConn) clamp(deadline time.Time) time.Time {
	if c.released.Load() {
		return deadline
	}
	if deadline.IsZero() || !time.Now().Before(c.limit) {
		c.released.Store(true)

		return deadline
	}
	if deadline.After(c.limit) {
		return c.limit
	}

	return deadline
}

func (c *handshakeConn) SetDeadline(deadline time.Time) error {
	return c.Conn.SetDeadline(c.clamp(deadline))
}

func (c *handshakeConn) SetReadDeadline(deadline time.Time) error {
	return c.Conn.SetReadDeadline(c.clamp(deadline))
}

func (c *handshakeConn) SetWriteDeadline(deadline time.Time) error {
	return c.Conn.SetWriteDeadline(c.clamp(deadline))
}
