// Package chmigrate applies ordered .sql migrations to ClickHouse over HTTP.
//
// ClickHouse has no transactional DDL and no advisory locks, so a migration
// is applied statement by statement and its version is recorded last. Every
// statement must therefore be idempotent (CREATE ... IF NOT EXISTS, DROP ...
// IF EXISTS, ALTER ... ADD COLUMN IF NOT EXISTS) so that a run interrupted
// after statement N can simply be repeated; migrations_test enforces this.
// Run one migrator at a time (make migrate locally, a single Job in a
// cluster): concurrent runners are not coordinated.
package chmigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	maxErrBody    = 512
	clientTimeout = 30 * time.Second
	table         = "schema_migrations"
)

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ErrInvalidDatabase is returned when the database name is not a safe identifier.
var ErrInvalidDatabase = errors.New("invalid database identifier")

// ErrInvalidEventsTTL is returned when the requested retention is outside the
// supported operational range.
var ErrInvalidEventsTTL = errors.New("events TTL days must be between 1 and 3650")

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

// Ping performs a real ClickHouse query over the configured HTTP protocol.
// The response body and connection details remain internal to the runner.
func (r *Runner) Ping(ctx context.Context) error {
	if !identRe.MatchString(r.database) {
		return ErrInvalidDatabase
	}
	if _, err := r.exec(ctx, "SELECT 1", r.database); err != nil {
		return fmt.Errorf("pinging clickhouse: %w", err)
	}

	return nil
}

// SetEventsTTL changes retention for the events table in this runner's
// configured database. Only the validated database identifier and integer
// number of days are inserted into the fixed DDL statement.
func (r *Runner) SetEventsTTL(ctx context.Context, days int) error {
	if !identRe.MatchString(r.database) {
		return ErrInvalidDatabase
	}
	if days < 1 || days > 3650 {
		return ErrInvalidEventsTTL
	}

	stmt := "ALTER TABLE " + r.database +
		".events MODIFY TTL toDateTime(occurred_at) + INTERVAL " + strconv.Itoa(days) + " DAY"
	if _, err := r.exec(ctx, stmt, r.database); err != nil {
		return fmt.Errorf("setting events TTL: %w", err)
	}

	return nil
}

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
			" (version String, applied_at DateTime DEFAULT now()) ENGINE = ReplacingMergeTree ORDER BY version",
	}

	// Bootstrap statements are fully qualified and run before the database
	// exists, so they are sent without a default database.
	for _, stmt := range bootstrap {
		if _, err := r.exec(ctx, stmt, ""); err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
	}

	body, err := r.exec(ctx, "SELECT DISTINCT version FROM "+db+"."+table+" FORMAT TabSeparated", db)
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

	// Migration statements run with the configured database as the session
	// default, so unqualified names land there rather than in "default".
	for _, stmt := range SplitStatements(string(raw)) {
		if _, err = r.exec(ctx, stmt, r.database); err != nil {
			return fmt.Errorf("applying %s: %w", name, err)
		}
	}

	rec := "INSERT INTO " + r.database + "." + table + " (version) VALUES ('" + name + "')"
	if _, err = r.exec(ctx, rec, r.database); err != nil {
		return fmt.Errorf("recording %s: %w", name, err)
	}

	return nil
}

// SplitStatements splits sql on semicolons that are outside string literals,
// quoted identifiers and comments. Chunks that hold only whitespace and
// comments (for example a trailing note after the last statement) are
// dropped, because ClickHouse rejects them as empty queries.
func SplitStatements(sql string) []string {
	var s splitter

	for i := 0; i < len(sql); {
		i = s.step(sql, i)
	}

	s.flush()

	return s.out
}

// splitter accumulates the statement being scanned.
type splitter struct {
	out     []string
	cur     strings.Builder
	hasCode bool
}

// step consumes one token starting at sql[i] and returns the next index.
func (s *splitter) step(sql string, i int) int {
	rest := sql[i:]

	switch {
	case rest[0] == ';':
		s.flush()

		return i + 1
	case strings.HasPrefix(rest, "--"):
		return i + s.comment(rest, "\n", 0)
	case strings.HasPrefix(rest, "/*"):
		return i + s.comment(rest, "*/", len("*/"))
	case rest[0] == '\'' || rest[0] == '"' || rest[0] == '`':
		end := quotedEnd(rest)
		s.cur.WriteString(rest[:end])
		s.hasCode = true

		return i + end
	default:
		s.cur.WriteByte(rest[0])

		if !unicode.IsSpace(rune(rest[0])) {
			s.hasCode = true
		}

		return i + 1
	}
}

// comment copies a comment that ends at term (keeping keep bytes of the
// terminator) or at end of input, and returns its length.
func (s *splitter) comment(rest, term string, keep int) int {
	const opener = len("--") // "--" and "/*" are both two bytes

	end := len(rest)
	if j := strings.Index(rest[opener:], term); j >= 0 {
		end = opener + j + keep
	}

	s.cur.WriteString(rest[:end])

	return end
}

// quotedEnd returns the index just past the literal that opens rest[0],
// honouring backslash escapes; doubled quotes scan as two adjacent literals.
func quotedEnd(rest string) int {
	quote := rest[0]

	for j := 1; j < len(rest); j++ {
		switch rest[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		}
	}

	return len(rest)
}

func (s *splitter) flush() {
	if s.hasCode {
		s.out = append(s.out, strings.TrimSpace(s.cur.String()))
	}

	s.cur.Reset()
	s.hasCode = false
}

// exec posts stmt to ClickHouse. A non-empty database is sent as the
// session default via the database query parameter; credentials are only
// ever sent in the Authorization header.
func (r *Runner) exec(ctx context.Context, stmt, database string) (string, error) {
	target := r.baseURL + "/"
	if database != "" {
		target += "?" + url.Values{"database": {database}}.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(stmt))
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
