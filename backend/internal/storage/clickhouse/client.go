// Package clickhouse provides the native ClickHouse event persistence adapter.
package clickhouse

import (
	"context"
	"fmt"
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
