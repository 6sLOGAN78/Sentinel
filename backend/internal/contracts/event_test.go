package contracts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

const eventSchemaName = "event.v1.json"

// fixtureFiles returns the sorted fixture paths in testdata/event/<dir>.
func fixtureFiles(t *testing.T, dir string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "event", dir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no fixtures in %s", dir)

	return paths
}

// schemaAccepts reports whether the compiled schema accepts the document.
func schemaAccepts(t *testing.T, sch *jsonschema.Schema, raw []byte) (bool, error) {
	t.Helper()

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	require.NoError(t, err)

	err = sch.Validate(doc)

	return err == nil, err
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	return raw
}

func TestSchemaAcceptsValidFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	for _, p := range fixtureFiles(t, "valid") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.True(t, ok, "schema must accept %s: %v", p, verr)
		})
	}
}

func TestSchemaRejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	files := fixtureFiles(t, "invalid_schema")
	require.GreaterOrEqual(t, len(files), 25)

	for _, p := range files {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.False(t, ok, "schema must reject %s", p)
			require.Error(t, verr)
		})
	}
}

func TestSchemaAcceptsGoOnlyFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	for _, p := range fixtureFiles(t, "invalid_go_only") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.True(t, ok, "schema cannot express this rule, must accept %s: %v", p, verr)
		})
	}
}

func TestSchemaAssertFormatRejectsBadIP(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	base := filepath.Join("testdata", "event", "invalid_schema")

	for _, name := range []string{"destination_ip_999.json", "destination_ip_short.json"} {
		ok, verr := schemaAccepts(t, sch, mustRead(t, filepath.Join(base, name)))
		require.False(t, ok, name)
		require.ErrorContains(t, verr, "destination_ip")
	}
}

// fixedNow is the validation clock the fixture corpus is written against.
var fixedNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // immutable test clock

// goAccepts reports whether DecodeEvent plus Validate accept the document.
func goAccepts(raw []byte) (bool, error) {
	ev, err := contracts.DecodeEvent(bytes.NewReader(raw))
	if err != nil {
		return false, err
	}

	if err = ev.Validate(fixedNow, contracts.DefaultLimits()); err != nil {
		return false, err
	}

	return true, nil
}

func TestFixtureAgreementValid(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	for _, p := range fixtureFiles(t, "valid") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			raw := mustRead(t, p)
			sOK, sErr := schemaAccepts(t, sch, raw)
			gOK, gErr := goAccepts(raw)
			require.True(t, sOK, "schema: %v", sErr)
			require.True(t, gOK, "go: %v", gErr)
		})
	}
}

func TestFixtureAgreementInvalid(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(eventSchemaName)
	require.NoError(t, err)

	for _, p := range fixtureFiles(t, "invalid_schema") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			raw := mustRead(t, p)
			sOK, _ := schemaAccepts(t, sch, raw)
			gOK, gErr := goAccepts(raw)
			require.False(t, sOK, "schema must reject")
			require.False(t, gOK, "go must reject")
			require.Error(t, gErr)
		})
	}
}

func TestFixtureGoOnlyRejected(t *testing.T) {
	t.Parallel()

	for _, p := range fixtureFiles(t, "invalid_go_only") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, err := goAccepts(mustRead(t, p))
			require.False(t, ok)
			require.Error(t, err)

			var fe *contracts.FieldError
			require.ErrorAs(t, err, &fe)
		})
	}
}

// validEvent returns a minimal valid event at fixedNow.
func validEvent() *contracts.Event {
	return &contracts.Event{
		SchemaVersion: 1,
		EventID:       "018ff000-0000-7000-8000-000000000001",
		TenantID:      "demo",
		OccurredAt:    contracts.NewTimestamp(fixedNow),
		Source:        "generator",
		EventType:     "auth.failure",
	}
}

func TestSkewBoundaries(t *testing.T) {
	t.Parallel()

	l := contracts.DefaultLimits()
	cases := []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"future exactly 5m", fixedNow.Add(5 * time.Minute), true},
		{"future 5m+1ms", fixedNow.Add(5*time.Minute + time.Millisecond), false},
		{"past exactly 168h", fixedNow.Add(-168 * time.Hour), true},
		{"past 168h+1ms", fixedNow.Add(-168*time.Hour - time.Millisecond), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ev := validEvent()
			ev.OccurredAt = contracts.NewTimestamp(tc.at)

			err := ev.Validate(fixedNow, l)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "occurred_at")
			}
		})
	}
}

func TestSkewLimitsFromConfig(t *testing.T) {
	t.Parallel()

	l := contracts.LimitsFromConfig(config.ContractsConfig{
		MaxAttrKeys: 2, MaxAttrBytes: 10, FutureSkew: time.Second, PastSkew: time.Second,
	})

	ev := validEvent()
	ev.OccurredAt = contracts.NewTimestamp(fixedNow.Add(2 * time.Second))
	require.Error(t, ev.Validate(fixedNow, l))
}

func TestFixtureAttributeBounds(t *testing.T) {
	t.Parallel()

	l := contracts.DefaultLimits()

	keys := func(n int) map[string]any {
		m := make(map[string]any, n)
		for i := range n {
			m[fmt.Sprintf("k%d", i)] = 1.0
		}

		return m
	}

	// {"k":"<n>"} encodes to n+8 bytes.
	sized := func(total int) map[string]any {
		return map[string]any{"k": strings.Repeat("v", total-8)}
	}

	cases := []struct {
		name  string
		attrs map[string]any
		lim   contracts.Limits
		ok    bool
	}{
		{"32 keys", keys(32), l, true},
		{"33 keys", keys(33), l, false},
		{"4096 bytes", sized(4096), l, true},
		{"4097 bytes", sized(4097), l, false},
		{"nested object", map[string]any{"a": map[string]any{"b": 1.0}}, l, false},
		{"nested array", map[string]any{"a": []any{1.0}}, l, false},
		{"null value", map[string]any{"a": nil}, l, false},
		{"smaller limit rejects earlier", keys(3), contracts.Limits{
			MaxAttrKeys: 2, MaxAttrBytes: l.MaxAttrBytes, FutureSkew: l.FutureSkew, PastSkew: l.PastSkew,
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ev := validEvent()
			ev.Attributes = tc.attrs

			err := ev.Validate(fixedNow, tc.lim)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "attributes")
			}
		})
	}
}

func TestFixtureDecodeStrictness(t *testing.T) {
	t.Parallel()

	good := string(mustRead(t, filepath.Join("testdata", "event", "valid", "minimal.json")))

	_, err := contracts.DecodeEvent(strings.NewReader(good + "\n{}"))
	require.Error(t, err, "trailing JSON must be rejected")

	_, err = contracts.DecodeEvent(strings.NewReader(good + "garbage"))
	require.Error(t, err)

	// Go's decoder matches keys case-insensitively; the contract must not.
	_, err = contracts.DecodeEvent(strings.NewReader(strings.Replace(good, `"tenant_id"`, `"Tenant_ID"`, 1)))
	require.Error(t, err)

	_, err = contracts.DecodeEvent(strings.NewReader(`{"schema_version":1,"unknown":true}`))
	require.Error(t, err)

	_, err = contracts.DecodeEvent(strings.NewReader(`[]`))
	require.Error(t, err)

	big := `{"source":"` + strings.Repeat("a", 2<<20) + `"}`
	_, err = contracts.DecodeEvent(strings.NewReader(big))
	require.ErrorIs(t, err, contracts.ErrEventTooLarge)
}

func TestFixtureMarshalMillisecondUTC(t *testing.T) {
	t.Parallel()

	full := mustRead(t, filepath.Join("testdata", "event", "valid", "full.json"))
	ev, err := contracts.DecodeEvent(bytes.NewReader(full))
	require.NoError(t, err)

	out, err := json.Marshal(ev)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, m["occurred_at"])
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, m["received_at"])

	// Round trip preserves every field.
	back, err := contracts.DecodeEvent(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, ev, back)
	require.Equal(t, "demo", back.TenantID)
	require.Equal(t, 443, *back.DestinationPort)

	// Sub-millisecond input is truncated, not rounded.
	sub := strings.Replace(string(full), `"2026-10-10T11:59:00Z"`, `"2026-10-10T11:59:00.123956789Z"`, 1)
	ev2, err := contracts.DecodeEvent(strings.NewReader(sub))
	require.NoError(t, err)

	out2, err := json.Marshal(ev2)
	require.NoError(t, err)
	require.Contains(t, string(out2), `"occurred_at":"2026-10-10T11:59:00.123Z"`)
}

func TestFixtureIPAndPortHandling(t *testing.T) {
	t.Parallel()

	port := func(p int) *int { return &p }
	str := func(s string) *string { return &s }

	cases := []struct {
		name string
		mut  func(*contracts.Event)
		ok   bool
	}{
		{"ipv4", func(e *contracts.Event) { e.SourceIP = str("192.0.2.10") }, true},
		{"ipv6", func(e *contracts.Event) { e.SourceIP = str("2001:db8::1") }, true},
		{"v4-mapped", func(e *contracts.Event) { e.SourceIP = str("::ffff:192.0.2.10") }, false},
		{"v4-mapped hex", func(e *contracts.Event) { e.SourceIP = str("::ffff:c000:20a") }, false},
		{"zone", func(e *contracts.Event) { e.SourceIP = str("fe80::1%eth0") }, false},
		{"garbage", func(e *contracts.Event) { e.DestinationIP = str("999.1.1.1") }, false},
		{"port 0", func(e *contracts.Event) { e.DestinationPort = port(0) }, true},
		{"port 65535", func(e *contracts.Event) { e.DestinationPort = port(65535) }, true},
		{"port 65536", func(e *contracts.Event) { e.DestinationPort = port(65536) }, false},
		{"port -1", func(e *contracts.Event) { e.DestinationPort = port(-1) }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ev := validEvent()
			tc.mut(ev)

			err := ev.Validate(fixedNow, contracts.DefaultLimits())
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
