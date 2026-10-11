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

	if _, err := fs.ReadFile(migrations.ClickHouse(), "001_create_database.sql"); err != nil {
		t.Fatalf("read: %v", err)
	}
}

// TestClickHouseMigrationsTargetConfiguredDatabase guards WR-09: the runner
// creates the configured database and sets it as the session default, so
// migrations must neither create databases nor qualify names with one.
func TestClickHouseMigrationsTargetConfiguredDatabase(t *testing.T) {
	t.Parallel()

	names, err := fs.Glob(migrations.ClickHouse(), "*.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("glob: %v %v", names, err)
	}

	for _, name := range names {
		b, rerr := fs.ReadFile(migrations.ClickHouse(), name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}

		code := strings.ToLower(stripLineComments(string(b)))
		if strings.Contains(code, "create database") || strings.Contains(code, "sentinel.") {
			t.Errorf("%s hardcodes a database", name)
		}
	}
}

// stripLineComments drops "--" comments so prose does not trip the checks.
func stripLineComments(sql string) string {
	var out []string

	for line := range strings.SplitSeq(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
