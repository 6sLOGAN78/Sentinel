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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
	maxKeyFileBytes   = 128
	privateDirMode    = 0o700
	privateFileMode   = 0o600
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
	if command != "create" && command != "list" && command != "revoke" && command != "ensure" {
		return errors.New("unknown command; use --help")
	}
	flags := flag.NewFlagSet("keyctl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	tenant := flags.String("tenant", "", "tenant ID")
	scopes := flags.String("scopes", "", "comma-separated scopes")
	name := flags.String("name", "", "credential name")
	id := flags.String("key-id", "", "public key ID")
	outPath := flags.String("out", "", "private credential file")
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
	case "ensure":
		return ensure(ctx, store, *tenant, strings.Split(*scopes, ","), *name, *outPath)
	default:
		return apikeys.ErrInvalidMetadata
	}
}

func ensure(ctx context.Context, store *apikeys.Store, tenant string, scopes []string, name, path string) error {
	if err := apikeys.ValidateMetadata(tenant, scopes, name); err != nil {
		return err
	}
	if path == "" {
		return apikeys.ErrInvalidMetadata
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return errors.New("cannot prepare private directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe credential directory")
	}
	// Never chmod an existing parent such as the workspace or /tmp. The
	// operator must choose a dedicated private directory for credential files.
	if info.Mode().Perm() != privateDirMode {
		return errors.New("credential directory must have mode 0700")
	}
	matched, err := reusable(ctx, store, tenant, scopes, name, path)
	if err != nil || matched {
		return err
	}
	// Only revoke active rows belonging to this tenant and requested name.
	rows, err := store.List(ctx, tenant)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Name == name && row.RevokedAt == nil {
			if err = store.Revoke(ctx, row.KeyID); err != nil {
				return err
			}
		}
	}
	raw, err := store.Create(ctx, tenant, scopes, name)
	if err != nil {
		return err
	}
	if err = writePrivate(path, raw); err != nil {
		// A key that could not be delivered must not remain active.
		id, _, parseErr := apikeys.Parse(raw)
		if parseErr == nil {
			_ = store.Revoke(ctx, id)
		}
		return err
	}
	return nil
}

func reusable(
	ctx context.Context,
	store *apikeys.Store,
	tenant string,
	scopes []string,
	name string,
	path string,
) (bool, error) {
	// #nosec G304 -- no-follow and fstat guard the operator-selected path.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("unsafe credential file")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxKeyFileBytes {
		return false, errors.New("unsafe credential file")
	}
	if err = file.Chmod(privateFileMode); err != nil {
		return false, errors.New("cannot secure credential file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxKeyFileBytes+1))
	if err != nil {
		return false, errors.New("cannot read credential file")
	}
	id, secret, valid := parseCredential(data)
	if !valid {
		return false, nil
	}
	row, err := store.Lookup(ctx, id)
	if errors.Is(err, apikeys.ErrUnauthorized) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	requested := slices.Clone(scopes)
	slices.Sort(requested)
	requested = slices.Compact(requested)
	actual := slices.Clone(row.Scopes)
	slices.Sort(actual)
	return row.TenantID == tenant && row.Name == name && slices.Equal(actual, requested) &&
		apikeys.VerifyHash(secret, row.SecretHash), nil
}

func parseCredential(data []byte) (string, string, bool) {
	id, secret, err := apikeys.Parse(strings.TrimSpace(string(data)))
	return id, secret, err == nil
}

func writePrivate(path, raw string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".producer-key-*")
	if err != nil {
		return errors.New("cannot create private credential file")
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	defer func() { _ = file.Close() }()
	if err = file.Chmod(privateFileMode); err != nil {
		return errors.New("cannot secure credential file")
	}
	if _, err = file.WriteString(raw + "\n"); err != nil {
		return errors.New("cannot write credential file")
	}
	if err = file.Sync(); err != nil {
		return errors.New("cannot sync credential file")
	}
	if err = file.Close(); err != nil {
		return errors.New("cannot close credential file")
	}
	if err = os.Rename(temp, path); err != nil {
		return errors.New("cannot replace credential file")
	}
	return nil
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
