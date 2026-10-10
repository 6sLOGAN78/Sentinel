// Package database provides the Postgres connection pool and migrations.
package database

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"time"

	pgxzero "github.com/jackc/pgx-zerolog"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
	loggerConfig "github.com/6sLOGAN78/sentinel/internal/logger"
)

const pingTimeout = 10 * time.Second

// Database wraps the pgx connection pool.
type Database struct {
	Pool *pgxpool.Pool
	log  zerolog.Logger
}

// ConnString builds a Postgres URL from the config. userinfo is escaped by
// url.UserPassword so passwords with reserved characters round-trip.
func ConnString(c config.DatabaseConfig) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.Name,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}

	return u.String()
}

// PoolConfig returns the pgxpool configuration for the given settings.
func PoolConfig(c config.DatabaseConfig, log zerolog.Logger) (*pgxpool.Config, error) {
	pc, err := pgxpool.ParseConfig(ConnString(c))
	if err != nil {
		return nil, fmt.Errorf("parsing pgx pool config: %w", err)
	}

	pc.MaxConns = int32(min(max(c.MaxOpenConns, 1), math.MaxInt32)) //nolint:gosec // clamped to the int32 range
	pc.MaxConnLifetime = time.Duration(c.ConnMaxLifetime) * time.Second
	pc.MaxConnIdleTime = time.Duration(c.ConnMaxIdleTime) * time.Second

	// SQL tracing is for local debugging only.
	if level := log.GetLevel(); level <= zerolog.DebugLevel {
		pc.ConnConfig.Tracer = &tracelog.TraceLog{
			Logger:   pgxzero.NewLogger(loggerConfig.NewPgxLogger(level)),
			LogLevel: tracelog.LogLevel(loggerConfig.GetPgxTraceLogLevel(level)),
		}
	}

	return pc, nil
}

// New creates the pool and verifies connectivity. The pool is closed when the
// ping fails.
func New(ctx context.Context, c config.DatabaseConfig, log zerolog.Logger) (*Database, error) {
	pc, err := PoolConfig(c, log)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("creating pgx pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("pinging database: %w", err)
	}

	log.Info().Msg("connected to the database")

	return &Database{Pool: pool, log: log}, nil
}

// Close releases the connection pool.
func (db *Database) Close() {
	db.log.Info().Msg("closing database connection pool")
	db.Pool.Close()
}
