package chmigrate_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/6sLOGAN78/sentinel/internal/chmigrate"
)

type fakeCH struct {
	mu      sync.Mutex
	stmts   []string
	dbs     []string
	applied []string
	failOn  string
	badAuth bool
	badURL  bool
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type fakeServer struct {
	URL    string
	client *http.Client
}

func (s *fakeServer) Client() *http.Client { return s.client }

func (f *fakeCH) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	stmt := string(b)

	f.mu.Lock()
	defer f.mu.Unlock()

	u, p, ok := r.BasicAuth()
	if !ok || u != "usr" || p != "pw" {
		f.badAuth = true
	}

	// Only the session database may travel in the URL; never credentials.
	q := r.URL.Query()
	q.Del("database")

	if r.URL.User != nil || len(q) != 0 {
		f.badURL = true
	}

	f.stmts = append(f.stmts, stmt)
	f.dbs = append(f.dbs, r.URL.Query().Get("database"))

	if f.failOn != "" && strings.Contains(stmt, f.failOn) {
		http.Error(w, "boom pw", http.StatusInternalServerError)

		return
	}

	switch {
	case strings.HasPrefix(stmt, "SELECT DISTINCT version"):
		_, _ = io.WriteString(w, strings.Join(f.applied, "\n"))
	case strings.HasPrefix(stmt, "INSERT INTO"):
		name := stmt[strings.Index(stmt, "('")+2 : strings.LastIndex(stmt, "')")]
		f.applied = append(f.applied, name)
	}
}

func newFake(t *testing.T) (*fakeCH, *fakeServer) {
	t.Helper()

	f := &fakeCH{}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}

		recorder := httptest.NewRecorder()
		f.handler(recorder, req)

		return recorder.Result(), nil
	})}
	srv := &fakeServer{URL: "http://clickhouse.test", client: client}
	t.Cleanup(client.CloseIdleConnections)

	return f, srv
}

var files = fstest.MapFS{ //nolint:gochecknoglobals // test fixture
	"002_b.sql": {Data: []byte("CREATE TABLE b (x Int8) ENGINE = Memory;\n\n")},
	"001_a.sql": {Data: []byte("CREATE TABLE a (x Int8) ENGINE = Memory;\nCREATE TABLE c (x Int8) ENGINE = Memory;\n")},
}

func TestChmigrateRunOrderAndIdempotent(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "db1", srv.Client())

	got, err := r.Run(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0] != "001_a.sql" || got[1] != "002_b.sql" {
		t.Fatalf("applied = %v", got)
	}

	want := []string{
		"CREATE DATABASE IF NOT EXISTS db1",
		"CREATE TABLE IF NOT EXISTS db1.schema_migrations (version String, " +
			"applied_at DateTime DEFAULT now()) ENGINE = ReplacingMergeTree ORDER BY version",
		"SELECT DISTINCT version FROM db1.schema_migrations FORMAT TabSeparated",
		"CREATE TABLE a (x Int8) ENGINE = Memory",
		"CREATE TABLE c (x Int8) ENGINE = Memory",
		"INSERT INTO db1.schema_migrations (version) VALUES ('001_a.sql')",
		"CREATE TABLE b (x Int8) ENGINE = Memory",
		"INSERT INTO db1.schema_migrations (version) VALUES ('002_b.sql')",
	}
	if strings.Join(f.stmts, "|") != strings.Join(want, "|") {
		t.Fatalf("statements:\n%q\nwant\n%q", f.stmts, want)
	}

	if f.badAuth || f.badURL {
		t.Fatalf("auth header missing or credentials in URL: auth=%v url=%v", f.badAuth, f.badURL)
	}

	// Bootstrap runs before the database exists; everything after it uses
	// the configured database as the session default.
	wantDBs := []string{"", "", "db1", "db1", "db1", "db1", "db1", "db1"}
	if strings.Join(f.dbs, ",") != strings.Join(wantDBs, ",") {
		t.Fatalf("session databases = %q, want %q", f.dbs, wantDBs)
	}

	n := len(f.stmts)

	again, err := r.Run(context.Background(), files)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run: %v %v", again, err)
	}

	for _, s := range f.stmts[n:] {
		if strings.HasPrefix(s, "CREATE TABLE a") || strings.HasPrefix(s, "INSERT") {
			t.Fatalf("second run sent migration statement %q", s)
		}
	}
}

func TestChmigrateAddrForms(t *testing.T) {
	t.Parallel()

	a := chmigrate.New("127.0.0.1:18123", "u", "p", "d", nil).URL()
	b := chmigrate.New("http://127.0.0.1:18123", "u", "p", "d", nil).URL()

	if a != b || a != "http://127.0.0.1:18123" {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestChmigrateErrorWrapped(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	f.failOn = "CREATE TABLE b"
	r := chmigrate.New(srv.URL, "usr", "pw", "db1", srv.Client())

	_, err := r.Run(context.Background(), files)
	if err == nil || !strings.Contains(err.Error(), "002_b.sql") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}

	if strings.Contains(err.Error(), "boom") {
		t.Fatal("response body leaked into error")
	}
}

func TestChmigrateContextCancel(t *testing.T) {
	t.Parallel()

	_, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "db1", srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.Run(ctx, files); err == nil {
		t.Fatal("expected error")
	}
}

func TestChmigrateInvalidDatabase(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "db; DROP", srv.Client())

	if _, err := r.Run(context.Background(), files); err == nil {
		t.Fatal("expected error")
	}

	if len(f.stmts) != 0 {
		t.Fatal("statements sent for invalid identifier")
	}
}

func TestSetEventsTTL(t *testing.T) {
	t.Parallel()

	for _, days := range []int{1, 14, 3650} {
		t.Run(fmt.Sprintf("%d_days", days), func(t *testing.T) {
			t.Parallel()

			f, srv := newFake(t)
			r := chmigrate.New(srv.URL, "usr", "pw", "retention_db", srv.Client())

			if err := r.SetEventsTTL(context.Background(), days); err != nil {
				t.Fatal(err)
			}

			want := fmt.Sprintf(
				"ALTER TABLE retention_db.events MODIFY TTL toDateTime(occurred_at) + INTERVAL %d DAY",
				days,
			)
			if len(f.stmts) != 1 || f.stmts[0] != want {
				t.Fatalf("statements = %q, want %q", f.stmts, want)
			}
			if len(f.dbs) != 1 || f.dbs[0] != "retention_db" {
				t.Fatalf("session databases = %q, want retention_db", f.dbs)
			}
		})
	}
}

func TestSetEventsTTLRejectsInvalidDaysWithoutRequest(t *testing.T) {
	t.Parallel()

	for _, days := range []int{-1, 0, 3651} {
		t.Run(fmt.Sprintf("%d_days", days), func(t *testing.T) {
			t.Parallel()

			f, srv := newFake(t)
			r := chmigrate.New(srv.URL, "usr", "pw", "retention_db", srv.Client())

			if err := r.SetEventsTTL(context.Background(), days); err == nil {
				t.Fatal("expected invalid retention error")
			}
			if len(f.stmts) != 0 {
				t.Fatalf("statements sent for invalid retention: %q", f.stmts)
			}
		})
	}
}

func TestSetEventsTTLRejectsInvalidDatabaseWithoutRequest(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "db; DROP TABLE events", srv.Client())

	if err := r.SetEventsTTL(context.Background(), 14); err == nil {
		t.Fatal("expected invalid database error")
	}
	if len(f.stmts) != 0 {
		t.Fatalf("statements sent for invalid database: %q", f.stmts)
	}
}

func TestSetEventsTTLErrorIsSanitized(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	f.failOn = "ALTER TABLE"
	r := chmigrate.New(srv.URL, "usr", "pw", "retention_db", srv.Client())

	err := r.SetEventsTTL(context.Background(), 14)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "pw") {
		t.Fatalf("ClickHouse response or password leaked in error: %v", err)
	}
}

func TestSetEventsTTLHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	_, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "retention_db", srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.SetEventsTTL(ctx, 14); err == nil {
		t.Fatal("expected canceled request error")
	}
}

func TestChmigrateSplitsOutsideCommentsAndStrings(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	r := chmigrate.New(srv.URL, "usr", "pw", "db1", srv.Client())

	fsys := fstest.MapFS{
		"001_x.sql": {Data: []byte("-- header; with semicolon;\n" +
			"CREATE TABLE a (s String DEFAULT 'x;\\';y', t String DEFAULT 'it''s;') ENGINE = Memory; -- trailing;\n" +
			"/* block;\n comment; */\n" +
			"CREATE TABLE `b;` (x Int8) ENGINE = Memory;\n" +
			"-- trailing note\n")},
		"002_note.sql": {Data: []byte("-- comment only;\n/* nothing; */\n\n")},
	}

	got, err := r.Run(context.Background(), fsys)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(got, ",") != "001_x.sql,002_note.sql" {
		t.Fatalf("applied = %v", got)
	}

	want := []string{
		"-- header; with semicolon;\n" +
			"CREATE TABLE a (s String DEFAULT 'x;\\';y', t String DEFAULT 'it''s;') ENGINE = Memory",
		"-- trailing;\n/* block;\n comment; */\nCREATE TABLE `b;` (x Int8) ENGINE = Memory",
		"INSERT INTO db1.schema_migrations (version) VALUES ('001_x.sql')",
		"INSERT INTO db1.schema_migrations (version) VALUES ('002_note.sql')",
	}

	sent := f.stmts[3:] // after bootstrap (2) and the version listing (1)
	if strings.Join(sent, "|") != strings.Join(want, "|") {
		t.Fatalf("statements:\n%q\nwant\n%q", sent, want)
	}
}

func TestChmigrateReplaysInterruptedMigration(t *testing.T) {
	t.Parallel()

	f, srv := newFake(t)
	f.failOn = "CREATE TABLE IF NOT EXISTS y"
	r := chmigrate.New(srv.URL, "usr", "pw", "db1", srv.Client())

	fsys := fstest.MapFS{"001_xy.sql": {Data: []byte(
		"CREATE TABLE IF NOT EXISTS x (a Int8) ENGINE = Memory;\n" +
			"CREATE TABLE IF NOT EXISTS y (a Int8) ENGINE = Memory;\n")}}

	if _, err := r.Run(context.Background(), fsys); err == nil {
		t.Fatal("expected failure on the second statement")
	}

	f.mu.Lock()
	recorded := len(f.applied)
	f.failOn = ""
	f.mu.Unlock()

	if recorded != 0 {
		t.Fatal("version recorded although the migration failed")
	}

	got, err := r.Run(context.Background(), fsys)
	if err != nil || strings.Join(got, ",") != "001_xy.sql" {
		t.Fatalf("rerun: %v %v", got, err)
	}

	creates := 0

	for _, s := range f.stmts {
		if strings.HasPrefix(s, "CREATE TABLE IF NOT EXISTS x") {
			creates++
		}
	}

	if creates != 2 {
		t.Fatalf("first statement sent %d times, want 2 (replayed idempotently)", creates)
	}
}
