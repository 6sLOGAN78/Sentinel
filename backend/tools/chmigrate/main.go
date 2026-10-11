// Command chmigrate applies the embedded ClickHouse migrations. It is
// tooling invoked by `make migrate`, not a service binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
	"github.com/6sLOGAN78/sentinel/migrations"
)

var errNoPassword = errors.New("SENTINEL_CLICKHOUSE_PASSWORD is not set")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "chmigrate:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "http://127.0.0.1:18123", "ClickHouse HTTP address")
	user := flag.String("user", "sentinel", "ClickHouse user")
	database := flag.String("database", "sentinel", "ClickHouse database")
	flag.Parse()

	password := os.Getenv("SENTINEL_CLICKHOUSE_PASSWORD")
	if password == "" {
		return errNoPassword
	}

	applied, err := chmigrate.New(*addr, *user, password, *database, nil).
		Run(context.Background(), migrations.ClickHouse())
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	if len(applied) == 0 {
		fmt.Println("clickhouse schema up to date")

		return nil
	}

	fmt.Printf("clickhouse migrations applied: %d\n", len(applied))

	return nil
}
