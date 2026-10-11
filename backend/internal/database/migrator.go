package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	tern "github.com/jackc/tern/v2/migrate"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/migrations"
)

const versionTable = "schema_version"

// Migrate applies pending Postgres migrations with tern. It is idempotent.
func Migrate(ctx context.Context, c config.DatabaseConfig, log zerolog.Logger) error {
	conn, err := pgx.Connect(ctx, ConnString(c))
	if err != nil {
		return fmt.Errorf("connecting for migrations: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	m, err := tern.NewMigrator(ctx, conn, versionTable)
	if err != nil {
		return fmt.Errorf("constructing database migrator: %w", err)
	}

	if err = m.LoadMigrations(migrations.Postgres()); err != nil {
		return fmt.Errorf("loading database migrations: %w", err)
	}

	from, err := m.GetCurrentVersion(ctx)
	if err != nil {
		return fmt.Errorf("retrieving current database migration version: %w", err)
	}

	if err = m.Migrate(ctx); err != nil {
		return fmt.Errorf("applying database migrations: %w", err)
	}

	if int(from) == len(m.Migrations) {
		log.Info().Msgf("database schema up to date, version %d", len(m.Migrations))

		return nil
	}

	log.Info().Msgf("migrated database schema from version %d to %d", from, len(m.Migrations))

	return nil
}
