// Package chmigrate applies ordered .sql migrations to ClickHouse over HTTP.
package chmigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxErrBody    = 512
	clientTimeout = 30 * time.Second
	table         = "schema_migrations"
)

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ErrInvalidDatabase is returned when the database name is not a safe identifier.
var ErrInvalidDatabase = errors.New("invalid database identifier")

// Runner applies migrations to one ClickHouse database.
type Runner struct {
	baseURL  string
	user     string
	password string
	database string
	client   *http.Client
}

// New creates a Runner. addr is host:port or a full http(s) URL. A nil client
// gets a bounded default.
func New(addr, user, password, database string, client *http.Client) *Runner {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}

	if client == nil {
		client = &http.Client{Timeout: clientTimeout}
	}

	return &Runner{
		baseURL: strings.TrimRight(addr, "/"), user: user, password: password,
		database: database, client: client,
	}
}

// URL returns the HTTP endpoint the runner targets.
func (r *Runner) URL() string { return r.baseURL }

// Run applies every embedded migration not yet recorded and returns the names
// applied in order.
func (r *Runner) Run(ctx context.Context, fsys fs.FS) ([]string, error) {
	if !identRe.MatchString(r.database) {
		return nil, ErrInvalidDatabase
	}

	db := r.database
	bootstrap := []string{
		"CREATE DATABASE IF NOT EXISTS " + db,
		"CREATE TABLE IF NOT EXISTS " + db + "." + table +
			" (version String, applied_at DateTime DEFAULT now()) ENGINE = MergeTree ORDER BY version",
	}

	for _, stmt := range bootstrap {
		if _, err := r.exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
	}

	body, err := r.exec(ctx, "SELECT version FROM "+db+"."+table+" FORMAT TabSeparated")
	if err != nil {
		return nil, fmt.Errorf("listing applied migrations: %w", err)
	}

	done := map[string]bool{}

	for _, v := range strings.Fields(body) {
		done[v] = true
	}

	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}

	sort.Strings(names)

	applied := []string{}

	for _, name := range names {
		if done[name] {
			continue
		}

		if err = r.apply(ctx, fsys, name); err != nil {
			return applied, err
		}

		applied = append(applied, name)
	}

	return applied, nil
}

func (r *Runner) apply(ctx context.Context, fsys fs.FS, name string) error {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}

	for _, stmt := range splitStatements(string(raw)) {
		if _, err = r.exec(ctx, stmt); err != nil {
			return fmt.Errorf("applying %s: %w", name, err)
		}
	}

	rec := "INSERT INTO " + r.database + "." + table + " (version) VALUES ('" + name + "')"
	if _, err = r.exec(ctx, rec); err != nil {
		return fmt.Errorf("recording %s: %w", name, err)
	}

	return nil
}

// splitStatements splits on a semicolon at end of line and drops empty parts.
func splitStatements(sql string) []string {
	var out []string

	var cur []string

	flush := func() {
		if s := strings.TrimSpace(strings.Join(cur, "\n")); s != "" {
			out = append(out, s)
		}

		cur = nil
	}

	for _, line := range strings.Split(sql, "\n") {
		t := strings.TrimRight(line, " \t\r")
		if strings.HasSuffix(t, ";") {
			cur = append(cur, strings.TrimSuffix(t, ";"))
			flush()

			continue
		}

		cur = append(cur, line)
	}

	flush()

	return out
}

func (r *Runner) exec(ctx context.Context, stmt string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/", strings.NewReader(stmt))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	req.SetBasicAuth(r.user, r.password)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("clickhouse request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxErrBody*maxErrBody))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// Body is read bounded but intentionally not echoed (T-07-06).
		return "", fmt.Errorf("clickhouse returned status %d", resp.StatusCode)
	}

	return string(b), nil
}
