package contracts_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
