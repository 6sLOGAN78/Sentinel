package test_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const testdataRoot = "../internal/contracts/testdata"

// driftCase ties an OpenAPI component to its canonical JSON Schema file.
type driftCase struct {
	component string // components.schemas name
	schema    string // embedded schema file name
	fixtures  string // directory under testdata
}

func driftCases() []driftCase {
	return []driftCase{
		{component: "Event", schema: "event.v1.json", fixtures: "event"},
		{component: "Alert", schema: "alert.v1.json", fixtures: "alert"},
	}
}

func fixtureFiles(t *testing.T, dir string) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(testdataRoot, dir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no fixtures in %s", dir)

	return files
}

func readJSON(t *testing.T, path string) any {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var v any
	require.NoError(t, json.Unmarshal(raw, &v))

	return v
}

// TestOpenAPIDriftFixtures validates every contract fixture against the OpenAPI
// component. Go-only invalid fixtures (skew, window bounds, size) are excluded
// because JSON Schema cannot express them. Keywords that kin-openapi cannot
// express are covered by TestOpenAPIDriftStructure, which stays authoritative.
func TestOpenAPIDriftFixtures(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	for _, tc := range driftCases() {
		schema := standaloneSchema(t, doc, tc.component)

		for _, f := range fixtureFiles(t, tc.fixtures+"/valid") {
			err := schema.VisitJSON(readJSON(t, f), visitOptions()...)
			assert.NoError(t, err, "%s should be accepted: %s", tc.component, f)
		}

		for _, f := range fixtureFiles(t, tc.fixtures+"/invalid_schema") {
			err := schema.VisitJSON(readJSON(t, f), visitOptions()...)
			assert.Error(t, err, "%s should be rejected: %s", tc.component, f)
		}
	}
}

// TestOpenAPIDriftStructure compares the structure of the OpenAPI components with
// the canonical embedded JSON Schemas.
func TestOpenAPIDriftStructure(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	for _, tc := range driftCases() {
		raw, err := contracts.Schema(tc.schema)
		require.NoError(t, err)

		var canon map[string]any
		require.NoError(t, json.Unmarshal(raw, &canon))

		all := marshaledComponents(t, doc)
		oa := asMap(resolveRefs(all, all[tc.component], "#/components/schemas/"))

		assertRootEqual(t, tc.component, canon, oa)

		cProps := asMap(canon["properties"])
		oProps := asMap(oa["properties"])
		assert.Equal(t, sortedKeys(cProps), sortedKeys(oProps), "%s property names", tc.component)

		for name, cp := range cProps {
			op, ok := oProps[name]
			if !ok {
				continue
			}

			want := signature(resolveRefs(asMap(canon["$defs"]), cp, "#/$defs/"))
			got := signature(resolveRefs(all, op, "#/components/schemas/"))
			assert.Equal(t, want, got, "%s.%s differs: canonical (left) vs openapi (right)", tc.component, name)
		}
	}
}

// visitOptions enables JSON Schema 2020-12 validation plus strict IP format checks,
// which kin-openapi does not register by default.
func visitOptions() []openapi3.SchemaValidationOption {
	return []openapi3.SchemaValidationOption{
		openapi3.EnableJSONSchema2020(),
		openapi3.WithStringFormatValidator("ipv4", openapi3.NewIPValidator(true)),
		openapi3.WithStringFormatValidator("ipv6", openapi3.NewIPValidator(false)),
	}
}

func marshaledComponents(t *testing.T, doc *openapi3.T) map[string]any {
	t.Helper()

	raw, err := json.Marshal(doc.Components.Schemas)
	require.NoError(t, err)

	var all map[string]any
	require.NoError(t, json.Unmarshal(raw, &all))

	return all
}

// standaloneSchema returns the named component with every local $ref inlined. kin-openapi's
// JSON Schema 2020-12 validator silently falls back to the legacy validator when a schema
// still contains unresolved references, so validation must run on a self-contained schema.
func standaloneSchema(t *testing.T, doc *openapi3.T, name string) *openapi3.Schema {
	t.Helper()

	all := marshaledComponents(t, doc)
	raw, err := json.Marshal(resolveRefs(all, all[name], "#/components/schemas/"))
	require.NoError(t, err)

	var s openapi3.Schema
	require.NoError(t, json.Unmarshal(raw, &s))

	return &s
}

func assertRootEqual(t *testing.T, name string, canon, oa map[string]any) {
	t.Helper()

	assert.Equal(t, sortedStrings(canon["required"]), sortedStrings(oa["required"]), "%s required", name)
	assert.Equal(t, canon["additionalProperties"], oa["additionalProperties"], "%s additionalProperties", name)
	assert.Equal(t, "object", oa["type"], "%s type", name)
}

// resolveRefs inlines local references with the given prefix using the named definitions.
func resolveRefs(defs map[string]any, node any, prefix string) any {
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok {
			return resolveRefs(defs, defs[strings.TrimPrefix(ref, prefix)], prefix)
		}

		out := make(map[string]any, len(n))
		for k, v := range n {
			out[k] = resolveRefs(defs, v, prefix)
		}

		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = resolveRefs(defs, v, prefix)
		}

		return out
	default:
		return node
	}
}

// constraintKeys are the validation keywords compared between both sides. Annotations
// (title, description) and the format keyword spelling are intentionally ignored.
//
//nolint:gochecknoglobals // read-only keyword list
var constraintKeys = []string{
	"const", "enum", "pattern", "minLength", "maxLength", "minimum", "maximum",
	"minItems", "maxItems", "maxProperties",
}

// signature builds a deterministic, comparable description of the constraints of a schema node.
func signature(node any) string {
	n := asMap(node)
	parts := []string{}

	if t := typeSet(n["type"]); t != "" {
		parts = append(parts, "type="+t)
	}

	for _, k := range constraintKeys {
		if v, ok := n[k]; ok {
			parts = append(parts, k+"="+canonical(v))
		}
	}

	if f, ok := n["format"]; ok {
		parts = append(parts, "format="+fmt.Sprint(f))
	}

	if ap, ok := n["additionalProperties"]; ok {
		parts = append(parts, "additionalProperties="+signature(ap))
	}

	if items, ok := n["items"]; ok {
		parts = append(parts, "items="+signature(items))
	}

	if not, ok := n["not"]; ok {
		parts = append(parts, "not="+signature(not))
	}

	if one, ok := n["oneOf"].([]any); ok {
		subs := make([]string, 0, len(one))
		for _, s := range one {
			subs = append(subs, signature(s))
		}

		sort.Strings(subs)
		parts = append(parts, "oneOf=["+strings.Join(subs, "|")+"]")
	}

	sort.Strings(parts)

	return strings.Join(parts, ";")
}

func canonical(v any) string {
	if list, ok := v.([]any); ok {
		strs := make([]string, 0, len(list))
		for _, e := range list {
			strs = append(strs, fmt.Sprint(e))
		}

		sort.Strings(strs)

		return "[" + strings.Join(strs, ",") + "]"
	}

	return fmt.Sprint(v)
}

func typeSet(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		return strings.Join(sortedStrings(t), ",")
	default:
		return ""
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)

	return m
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func sortedStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))

	for _, e := range list {
		out = append(out, fmt.Sprint(e))
	}

	sort.Strings(out)

	return out
}
