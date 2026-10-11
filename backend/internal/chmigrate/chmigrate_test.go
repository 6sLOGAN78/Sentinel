package chmigrate_test

import (
	"context"
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
	applied []string
	failOn  string
	badAuth bool
	badURL  bool
}

func (f *fakeCH) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	stmt := string(b)

	f.mu.Lock()
	defer f.mu.Unlock()

	u, p, ok := r.BasicAuth()
	if !ok || u != "usr" || p != "pw" {
		f.badAuth = true
	}

	if r.URL.User != nil || r.URL.RawQuery != "" {
		f.badURL = true
	}

	f.stmts = append(f.stmts, stmt)

	if f.failOn != "" && strings.Contains(stmt, f.failOn) {
		http.Error(w, "boom pw", http.StatusInternalServerError)

		return
	}

	switch {
	case strings.HasPrefix(stmt, "SELECT version"):
		_, _ = io.WriteString(w, strings.Join(f.applied, "\n"))
	case strings.HasPrefix(stmt, "INSERT INTO"):
		name := stmt[strings.Index(stmt, "('")+2 : strings.LastIndex(stmt, "')")]
		f.applied = append(f.applied, name)
	}
}

func newFake(t *testing.T) (*fakeCH, *httptest.Server) {
	t.Helper()

	f := &fakeCH{}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)

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
			"applied_at DateTime DEFAULT now()) ENGINE = MergeTree ORDER BY version",
		"SELECT version FROM db1.schema_migrations FORMAT TabSeparated",
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
