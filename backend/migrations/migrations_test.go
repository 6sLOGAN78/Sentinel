package migrations_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/6sLOGAN78/sentinel/migrations"
)

func TestMigrationsEmbedPostgres(t *testing.T) {
	t.Parallel()

	b, err := fs.ReadFile(migrations.Postgres(), "001_baseline.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	if !strings.Contains(string(b), "---- create above / drop below ----") {
		t.Fatal("missing tern separator")
	}
}

func TestMigrationsEmbedClickHouse(t *testing.T) {
	t.Parallel()

	b, err := fs.ReadFile(migrations.ClickHouse(), "001_create_database.sql")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(string(b), "CREATE DATABASE IF NOT EXISTS sentinel") {
		t.Fatal("unexpected content")
	}
}
