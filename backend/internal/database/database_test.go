package database_test

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	pgxzero "github.com/jackc/pgx-zerolog"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
)

func testCfg() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host: "127.0.0.1", Port: 5432, User: "us@er", Password: "p@ss:w/rd?#% +",
		Name: "sentinel", SSLMode: "disable",
		MaxOpenConns: 7, ConnMaxLifetime: 120, ConnMaxIdleTime: 30,
	}
}

func TestConnStringSpecialPassword(t *testing.T) {
	t.Parallel()

	cfg := testCfg()

	pc, err := pgxpool.ParseConfig(database.ConnString(cfg))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cc := pc.ConnConfig
	if cc.Password != cfg.Password || cc.User != cfg.User || cc.Host != cfg.Host ||
		int(cc.Port) != cfg.Port || cc.Database != cfg.Name {
		t.Fatalf("round trip mismatch: %+v", cc)
	}
}

func TestConnStringParseFailureHidesPassword(t *testing.T) {
	t.Parallel()

	cfg := testCfg()
	cfg.SSLMode = "bogus"

	_, err := database.PoolConfig(cfg, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error")
	}

	if strings.Contains(err.Error(), cfg.Password) || strings.Contains(err.Error(), "p@ss") {
		t.Fatalf("error leaks password: %v", err)
	}
}

func TestRedactPgxTraceArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := database.RedactingLogger(pgxzero.NewLogger(zerolog.New(&buf)))
	l.Log(context.Background(), tracelog.LogLevelInfo, "Query", map[string]any{
		"sql": "select 1", "args": []any{"secret"}, "password": "secret", "connString": "postgres://u:secret@h",
	})

	out := buf.String()
	if strings.Contains(out, "secret") || !strings.Contains(out, "select 1") {
		t.Fatalf("unexpected log line: %s", out)
	}
}

func TestConnStringPoolConfig(t *testing.T) {
	t.Parallel()

	pc, err := database.PoolConfig(testCfg(), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	if pc.MaxConns != 7 || pc.MaxConnLifetime != 120*time.Second || pc.MaxConnIdleTime != 30*time.Second {
		t.Fatalf("unexpected pool config: %d %v %v", pc.MaxConns, pc.MaxConnLifetime, pc.MaxConnIdleTime)
	}
}

func TestClosePoolOnPingFailure(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener is not TCP")
	}

	_ = l.Close()

	cfg := testCfg()
	cfg.Port = addr.Port

	start := time.Now()
	db, err := database.New(context.Background(), cfg, zerolog.Nop())

	if err == nil || db != nil {
		t.Fatalf("expected error and nil db, got %v %v", db, err)
	}

	if time.Since(start) > 11*time.Second {
		t.Fatal("ping did not respect timeout")
	}
}
