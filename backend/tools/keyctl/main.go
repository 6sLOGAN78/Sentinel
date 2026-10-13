// Command keyctl provisions producer credentials. It is tooling, not a service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/apikeys"
	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
)

const (
	commandTimeout    = 15 * time.Second
	maxPort           = 65535
	poolSize          = 5
	connectionSeconds = 300
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "keyctl:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		_, err := fmt.Fprintln(output,
			"keyctl create|list|revoke|ensure: --tenant --scopes --name; revoke: --key-id; ensure: --out\n"+
				"Database: PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE PGSSLMODE. "+
				"Cached revocation takes up to 30 seconds.")
		return err
	}
	if len(args) == 0 {
		return errors.New("command required; use --help")
	}
	command := args[0]
	if command != "create" && command != "list" && command != "revoke" {
		return errors.New("unknown command; use --help")
	}
	flags := flag.NewFlagSet("keyctl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	tenant := flags.String("tenant", "", "tenant ID")
	scopes := flags.String("scopes", "", "comma-separated scopes")
	name := flags.String("name", "", "credential name")
	id := flags.String("key-id", "", "public key ID")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return apikeys.ErrInvalidMetadata
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cfg, err := databaseConfig()
	if err != nil {
		return err
	}
	db, err := database.New(ctx, cfg, zerolog.Nop())
	if err != nil {
		return apikeys.ErrUnavailable
	}
	defer db.Close()
	store := apikeys.NewStore(db.Pool)
	switch command {
	case "create":
		raw, createErr := store.Create(ctx, *tenant, strings.Split(*scopes, ","), *name)
		if createErr != nil {
			return createErr
		}
		_, err = fmt.Fprintln(output, raw)
		return err
	case "revoke":
		return store.Revoke(ctx, *id)
	case "list":
		rows, listErr := store.List(ctx, *tenant)
		if listErr != nil {
			return listErr
		}
		return json.NewEncoder(output).Encode(rows)
	default:
		return apikeys.ErrInvalidMetadata
	}
}

func databaseConfig() (config.DatabaseConfig, error) {
	port, err := strconv.Atoi(envOr("PGPORT", "5432"))
	if err != nil || port < 1 || port > maxPort {
		return config.DatabaseConfig{}, apikeys.ErrInvalidMetadata
	}
	return config.DatabaseConfig{
		Host: envOr("PGHOST", "127.0.0.1"), Port: port,
		User: envOr("PGUSER", "sentinel"), Password: os.Getenv("PGPASSWORD"),
		Name: envOr("PGDATABASE", "sentinel"), SSLMode: envOr("PGSSLMODE", "verify-full"),
		MaxOpenConns: poolSize, MaxIdleConns: poolSize,
		ConnMaxLifetime: connectionSeconds, ConnMaxIdleTime: connectionSeconds,
	}, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
