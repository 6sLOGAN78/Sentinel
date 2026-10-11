// Package database provides the Postgres connection pool and migrations.
package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	pgxzero "github.com/jackc/pgx-zerolog"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
	loggerConfig "github.com/6sLOGAN78/sentinel/internal/logger"
)

const pingTimeout = 10 * time.Second

var errInvalidConfig = errors.New("invalid database configuration")

// redactedKeys are removed from pgx trace data before logging.
//
//nolint:gochecknoglobals // constant lookup list
var redactedKeys = []string{"args", "password", "connstring", "conn_string", "dsn"}

type redactingLogger struct{ inner tracelog.Logger }

// RedactingLogger wraps a tracelog.Logger and strips bound query arguments and
// credential-like keys from the data before delegating.
func RedactingLogger(inner tracelog.Logger) tracelog.Logger {
	return redactingLogger{inner: inner}
}

func (r redactingLogger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	clean := make(map[string]any, len(data))

	for k, v := range data {
		if !slices.Contains(redactedKeys, strings.ToLower(k)) {
			clean[k] = v
		}
	}

	r.inner.Log(ctx, level, msg, clean)
}

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
		// The cause is deliberately dropped: it may echo the DSN, which holds
		// the password.
		return nil, errInvalidConfig
	}

	pc.MaxConns = int32(min(max(c.MaxOpenConns, 1), math.MaxInt32))
	pc.MaxConnLifetime = time.Duration(c.ConnMaxLifetime) * time.Second
	pc.MaxConnIdleTime = time.Duration(c.ConnMaxIdleTime) * time.Second

	// SQL tracing is for local debugging only.
	if level := log.GetLevel(); level <= zerolog.DebugLevel {
		pc.ConnConfig.Tracer = &tracelog.TraceLog{
			Logger:   RedactingLogger(pgxzero.NewLogger(loggerConfig.NewPgxLogger(level))),
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

	if err = pool.Ping(pingCtx); err != nil {
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
