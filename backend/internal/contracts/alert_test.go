package contracts_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/stretchr/testify/require"
)

const alertSchemaName = "alert.v1.json"

// alertFixtures returns the sorted fixture paths in testdata/alert/<dir>.
func alertFixtures(t *testing.T, dir string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "alert", dir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no alert fixtures in %s", dir)

	return paths
}

// alertGoAccepts reports whether DecodeAlert plus Validate accept the document.
func alertGoAccepts(raw []byte) (bool, error) {
	a, err := contracts.DecodeAlert(bytes.NewReader(raw))
	if err != nil {
		return false, err
	}

	if err := a.Validate(); err != nil {
		return false, err
	}

	return true, nil
}

func TestAlertSchemaAcceptsValidFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(alertSchemaName)
	require.NoError(t, err)

	for _, p := range alertFixtures(t, "valid") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.True(t, ok, "schema must accept %s: %v", p, verr)
		})
	}
}

func TestAlertSchemaRejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(alertSchemaName)
	require.NoError(t, err)

	files := alertFixtures(t, "invalid_schema")
	require.GreaterOrEqual(t, len(files), 15)

	for _, p := range files {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.False(t, ok, "schema must reject %s", p)
			require.Error(t, verr)
		})
	}
}

func TestAlertSchemaAcceptsGoOnlyFixtures(t *testing.T) {
	t.Parallel()

	sch, err := CompileSchema(alertSchemaName)
	require.NoError(t, err)

	for _, p := range alertFixtures(t, "invalid_go_only") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, verr := schemaAccepts(t, sch, mustRead(t, p))
			require.True(t, ok, "schema cannot express this rule, must accept %s: %v", p, verr)
		})
	}
}

func TestAlertAgreementValid(t *testing.T) {
	t.Parallel()

	for _, p := range alertFixtures(t, "valid") {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()

			ok, err := alertGoAccepts(mustRead(t, p))
			require.True(t, ok, "go must accept %s: %v", p, err)
		})
	}
}

func TestAlertAgreementInvalid(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"invalid_schema", "invalid_go_only"} {
		for _, p := range alertFixtures(t, dir) {
			t.Run(dir+"/"+filepath.Base(p), func(t *testing.T) {
				t.Parallel()

				ok, err := alertGoAccepts(mustRead(t, p))
				require.False(t, ok, "go must reject %s", p)
				require.Error(t, err)
			})
		}
	}
}

func TestAlertGoOnlyReasons(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"window_end_equals_start": "window_end",
		"window_end_before_start": "window_end",
		"window_start_1999":       "window_start",
		"window_end_2101":         "window_end",
	}

	for name, field := range want {
		_, err := alertGoAccepts(mustRead(t, filepath.Join("testdata", "alert", "invalid_go_only", name+".json")))
		require.ErrorContains(t, err, field, name)
	}
}

func TestAlertValidAlertIDsAreHex(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`^[0-9a-f]{64}$`)

	for _, p := range alertFixtures(t, "valid") {
		var doc struct {
			AlertID string `json:"alert_id"`
		}

		require.NoError(t, json.Unmarshal(mustRead(t, p), &doc))
		require.Regexp(t, re, doc.AlertID, p)
	}
}

func TestAlertFixtureIDsMatchDedupeKey(t *testing.T) {
	t.Parallel()

	a, err := contracts.DecodeAlert(bytes.NewReader(mustRead(t, filepath.Join("testdata", "alert", "valid", "spec_example.json"))))
	require.NoError(t, err)
	require.Equal(t,
		contracts.DedupeKey("demo", "AUTH_BRUTE_FORCE", []string{"192.0.2.10", "example-user"}, a.WindowStart.Time),
		a.AlertID)
}

func TestAlertTimestampsMarshalMillisecondZ(t *testing.T) {
	t.Parallel()

	a, err := contracts.DecodeAlert(bytes.NewReader(mustRead(t, filepath.Join("testdata", "alert", "valid", "spec_example.json"))))
	require.NoError(t, err)

	out, err := json.Marshal(a)
	require.NoError(t, err)

	s := string(out)
	require.Contains(t, s, `"created_at":"2026-10-09T10:05:01.000Z"`)
	require.Contains(t, s, `"window_start":"2026-10-09T10:00:00.000Z"`)
	require.Contains(t, s, `"window_end":"2026-10-09T10:05:00.000Z"`)
	require.NotContains(t, s, "detected_at")

	full, err := contracts.DecodeAlert(bytes.NewReader(mustRead(t, filepath.Join("testdata", "alert", "valid", "full_detected_at.json"))))
	require.NoError(t, err)

	out, err = json.Marshal(full)
	require.NoError(t, err)
	require.Contains(t, string(out), `"detected_at":"2026-10-09T10:05:02.250Z"`)
}

func TestAlertRoundTripSpecExample(t *testing.T) {
	t.Parallel()

	a, err := contracts.DecodeAlert(bytes.NewReader(mustRead(t, filepath.Join("testdata", "alert", "valid", "spec_example.json"))))
	require.NoError(t, err)
	require.NoError(t, a.Validate())

	out, err := json.Marshal(a)
	require.NoError(t, err)

	b, err := contracts.DecodeAlert(bytes.NewReader(out))
	require.NoError(t, err)
	require.NoError(t, b.Validate())
	require.Equal(t, a, b)
	require.Equal(t, "high", b.Severity)
	require.Equal(t, 12, b.EventCount)
	require.Equal(t, "192.0.2.10|example-user", b.EntityKey)
}

func TestAlertDecodeRejectsCaseVariantKeyAndTrailingData(t *testing.T) {
	t.Parallel()

	raw := mustRead(t, filepath.Join("testdata", "alert", "valid", "spec_example.json"))

	variant := bytes.Replace(raw, []byte(`"alert_id"`), []byte(`"ALERT_ID"`), 1)
	_, err := contracts.DecodeAlert(bytes.NewReader(variant))
	require.Error(t, err)

	_, err = contracts.DecodeAlert(bytes.NewReader(append(append([]byte{}, raw...), []byte(`{}`)...)))
	require.Error(t, err)
}

func TestAlertDecodeRejectsOversizeInput(t *testing.T) {
	t.Parallel()

	_, err := contracts.DecodeAlert(strings.NewReader(`{"summary":"` + strings.Repeat("x", 1<<20) + `"}`))
	require.ErrorIs(t, err, contracts.ErrEventTooLarge)
}
