package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeClickHouse struct {
	mu      sync.Mutex
	stmts   []string
	applied []string
	failTTL bool
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (f *fakeClickHouse) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	stmt := string(b)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.stmts = append(f.stmts, stmt)

	if f.failTTL && strings.HasPrefix(stmt, "ALTER TABLE") {
		http.Error(w, "secret response body", http.StatusInternalServerError)

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

func (f *fakeClickHouse) alterStatements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var got []string
	for _, stmt := range f.stmts {
		if strings.HasPrefix(stmt, "ALTER TABLE") {
			got = append(got, stmt)
		}
	}

	return got
}

func (f *fakeClickHouse) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		f.handler(recorder, req)

		return recorder.Result(), nil
	})}
}

func testEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestRunAppliesTTLAfterNoOpMigration(t *testing.T) {
	fake := &fakeClickHouse{}
	client := fake.client()

	args := []string{"--addr", "http://clickhouse.test", "--user", "usr", "--database", "configured_db"}
	env := testEnv(map[string]string{"SENTINEL_CLICKHOUSE_PASSWORD": "pw"})

	var first bytes.Buffer
	if err := runWithClient(args, env, &first, client); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "clickhouse migrations applied:") {
		t.Fatalf("first output = %q", first.String())
	}

	var second bytes.Buffer
	if err := runWithClient(append(args, "--events-ttl-days", "14"), env, &second, client); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.String(), "clickhouse schema up to date") {
		t.Fatalf("second output = %q", second.String())
	}

	got := fake.alterStatements()
	want := []string{
		"ALTER TABLE configured_db.events MODIFY TTL toDateTime(occurred_at) + INTERVAL 7 DAY",
		"ALTER TABLE configured_db.events MODIFY TTL toDateTime(occurred_at) + INTERVAL 14 DAY",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("TTL statements = %q, want %q", got, want)
	}
}

func TestEventsTTLFlagOverridesEnvironment(t *testing.T) {
	fake := &fakeClickHouse{}
	client := fake.client()

	args := []string{
		"--addr", "http://clickhouse.test",
		"--user", "usr",
		"--database", "configured_db",
		"--events-ttl-days", "21",
	}
	env := testEnv(map[string]string{
		"SENTINEL_CLICKHOUSE_PASSWORD":        "pw",
		"SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS": "14",
	})

	if err := runWithClient(args, env, io.Discard, client); err != nil {
		t.Fatal(err)
	}

	got := fake.alterStatements()
	if len(got) != 1 || !strings.Contains(got[0], "INTERVAL 21 DAY") {
		t.Fatalf("TTL statements = %q", got)
	}
}

func TestEventsTTLEnvironmentOverridesDefault(t *testing.T) {
	fake := &fakeClickHouse{}
	client := fake.client()
	args := []string{
		"--addr", "http://clickhouse.test",
		"--user", "usr",
		"--database", "configured_db",
	}
	env := testEnv(map[string]string{
		"SENTINEL_CLICKHOUSE_PASSWORD":        "pw",
		"SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS": "14",
	})

	if err := runWithClient(args, env, io.Discard, client); err != nil {
		t.Fatal(err)
	}

	got := fake.alterStatements()
	if len(got) != 1 || !strings.Contains(got[0], "INTERVAL 14 DAY") {
		t.Fatalf("TTL statements = %q", got)
	}
}

func TestEventsTTLInvalidEnvironmentMakesNoRequest(t *testing.T) {
	fake := &fakeClickHouse{}
	client := fake.client()
	args := []string{
		"--addr", "http://clickhouse.test",
		"--database", "configured_db",
	}
	env := testEnv(map[string]string{
		"SENTINEL_CLICKHOUSE_PASSWORD":        "pw",
		"SENTINEL_CLICKHOUSE_EVENTS_TTL_DAYS": "not-an-integer",
	})

	if err := runWithClient(args, env, io.Discard, client); err == nil {
		t.Fatal("expected invalid TTL environment error")
	}
	if got := fake.alterStatements(); len(got) != 0 {
		t.Fatalf("TTL statements sent for invalid environment: %q", got)
	}
}

func TestEventsTTLInvalidInputMakesNoRequest(t *testing.T) {
	for _, value := range []string{"0", "-1", "3651", "not-an-integer", "999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			fake := &fakeClickHouse{}
			client := fake.client()

			args := []string{
				"--addr", "http://clickhouse.test",
				"--database", "configured_db",
				"--events-ttl-days", value,
			}
			env := testEnv(map[string]string{"SENTINEL_CLICKHOUSE_PASSWORD": "pw"})

			if err := runWithClient(args, env, io.Discard, client); err == nil {
				t.Fatal("expected invalid TTL error")
			}
			if got := fake.alterStatements(); len(got) != 0 {
				t.Fatalf("TTL statements sent for invalid input: %q", got)
			}
		})
	}
}

func TestRunDoesNotLeakPasswordOrResponseBody(t *testing.T) {
	fake := &fakeClickHouse{failTTL: true}
	client := fake.client()

	const password = "top-secret-password"
	args := []string{"--addr", "http://clickhouse.test", "--database", "configured_db"}
	err := runWithClient(
		args,
		testEnv(map[string]string{"SENTINEL_CLICKHOUSE_PASSWORD": password}),
		io.Discard,
		client,
	)
	if err == nil {
		t.Fatal("expected TTL failure")
	}
	if strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "secret response body") {
		t.Fatalf("sensitive data leaked in error: %v", err)
	}
}
