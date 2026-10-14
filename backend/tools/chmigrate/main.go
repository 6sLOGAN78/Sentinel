// Command chmigrate applies the embedded ClickHouse migrations. It is
// tooling invoked by `make migrate`, not a service binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/migrations"
)

const (
	eventsTTLEnv     = "SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS"
	defaultTTLDays   = "7"
	operationTimeout = 2 * time.Minute
)

var (
	errNoPassword       = errors.New("SENTINEL_CLICKHOUSE_PASSWORD is not set")
	errInvalidEventsTTL = errors.New("events TTL days must be an integer from 1 through 3650")
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "chmigrate:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout io.Writer) error {
	return runWithClient(args, getenv, stdout, nil)
}

func runWithClient(args []string, getenv func(string) string, stdout io.Writer, client *http.Client) error {
	ttlDefault := defaultTTLDays
	if configured := getenv(eventsTTLEnv); configured != "" {
		ttlDefault = configured
	}

	flags := flag.NewFlagSet("chmigrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	addr := flags.String("addr", "http://127.0.0.1:18123", "ClickHouse HTTP address")
	user := flags.String("user", "sentinel", "ClickHouse user")
	database := flags.String("database", "sentinel", "ClickHouse database")
	ttlRaw := flags.String("events-ttl-days", ttlDefault, "events retention in days (1..3650)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing arguments: %w", err)
	}

	ttlDays, err := strconv.Atoi(*ttlRaw)
	if err != nil || ttlDays < 1 || ttlDays > 3650 {
		return errInvalidEventsTTL
	}

	password := getenv("SENTINEL_CLICKHOUSE_PASSWORD")
	if password == "" {
		return errNoPassword
	}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	runner := chmigrate.New(*addr, *user, password, *database, client)
	applied, err := runner.Run(ctx, migrations.ClickHouse())
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	if len(applied) == 0 {
		_, _ = fmt.Fprintln(stdout, "clickhouse schema up to date")
	} else {
		_, _ = fmt.Fprintf(stdout, "clickhouse migrations applied: %d\n", len(applied))
	}

	if err = runner.SetEventsTTL(ctx, ttlDays); err != nil {
		return fmt.Errorf("setting events retention: %w", err)
	}

	return nil
}
