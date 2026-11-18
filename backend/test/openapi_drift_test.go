package test_test

import (
	"encoding/json"
	"fmt"
	"net/http"
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
			require.NoError(t, err, "%s should be accepted: %s", tc.component, f)
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

func TestOpenAPIErrorDetailsAreBoundedAndStrict(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	errorBody := doc.Components.Schemas["Error"].Value.Properties["error"].Value
	detailsRef := errorBody.Properties["details"]
	require.NotNil(t, detailsRef, "Error.error.details must be documented")
	details := detailsRef.Value
	require.NotNil(t, details.MaxItems)
	assert.Equal(t, uint64(50), *details.MaxItems)
	require.NotNil(t, details.Items)
	require.NotNil(t, details.Items.Value.AdditionalProperties.Has)
	assert.False(t, *details.Items.Value.AdditionalProperties.Has)
	assert.ElementsMatch(t, []string{"error", "field"}, details.Items.Value.Required)
	assert.Contains(t, details.Items.Value.Properties, "field")
	assert.Contains(t, details.Items.Value.Properties, "error")
	assert.Len(t, details.Items.Value.Properties, 2)
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

// TestOpenAPIConsumerLagContract pins the lag-only response shape: one row per
// pipeline consumer group and nothing else (no tenant, volume or partition).
func TestOpenAPIConsumerLagContract(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	op := operations(doc)["GET "+pathConsumerLag]
	require.NotNil(t, op, "GET %s must be documented", pathConsumerLag)
	assert.Empty(t, op.Parameters, "the PromQL is a server constant; no parameters are accepted")

	for _, code := range []string{"200", "400", "401", "403", "503"} {
		require.NotNil(t, op.Responses.Value(code), "missing response %s", code)
	}

	ok := op.Responses.Value("200").Value.Content.Get("application/json").Schema
	assert.Equal(t, "#/components/schemas/ConsumerLag", ok.Ref)

	body := doc.Components.Schemas["ConsumerLag"].Value
	require.NotNil(t, body)
	assert.ElementsMatch(t, []string{"observed_at", "consumer_lag"}, body.Required)
	assert.Len(t, body.Properties, 2)
	require.NotNil(t, body.AdditionalProperties.Has)
	assert.False(t, *body.AdditionalProperties.Has)

	rows := body.Properties["consumer_lag"].Value
	require.NotNil(t, rows.MaxItems)
	assert.Equal(t, uint64(3), rows.MinItems)
	assert.Equal(t, uint64(3), *rows.MaxItems)
	row := rows.Items.Value
	assert.ElementsMatch(t, []string{"group", "lag"}, row.Required)
	assert.Len(t, row.Properties, 2)
	require.NotNil(t, row.AdditionalProperties.Has)
	assert.False(t, *row.AdditionalProperties.Has)
	assert.ElementsMatch(t, []any{"event-writer", "detector", "alert-writer"}, row.Properties["group"].Value.Enum)
	assert.Equal(t, "#/components/schemas/SystemMetric", row.Properties["lag"].Ref)
	metric, err := json.Marshal(doc.Components.Schemas["SystemMetric"].Value)
	require.NoError(t, err)
	assert.Contains(t, string(metric), `"operator_only"`, "non-operator tenants get the operator_only reason")
	assert.Contains(t, op.Description, "operator_only", "the operator rule must be documented")

	summary := operations(doc)["GET /api/v1/metrics/summary"]
	require.NotNil(t, summary)
	assert.NotContains(t, summary.Description, "unavailable until Phase 5")
	assert.Contains(t, summary.Description, "Consumer lag is served by GET /api/v1/metrics/lag.")
}

// TestOpenAPIConsumerLagDrift validates real handler bodies for every
// availability state against the documented schema, and proves the schema
// itself rejects a fabricated zero for an unavailable value.
func TestOpenAPIConsumerLagDrift(t *testing.T) {
	t.Parallel()

	schema := standaloneSchema(t, loadOpenAPI(t), "ConsumerLag")

	for name, result := range consumerLagResults() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newConsumerLagFixture(t, result)
			status, raw := f.get(t, "analyst")
			require.Equal(t, http.StatusOK, status, string(raw))

			var body any
			require.NoError(t, json.Unmarshal(raw, &body))
			require.NoError(t, schema.VisitJSON(body, visitOptions()...), string(raw))
		})
	}

	// A non-operator tenant's real body (every row operator_only) is valid.
	f := newConsumerLagFixture(t, consumerLagResults()["available"])
	status, raw := f.getAs(t, "analyst", contractOtherTenant)
	require.Equal(t, http.StatusOK, status, string(raw))
	require.Contains(t, string(raw), `"operator_only"`)
	require.NoError(t, schema.VisitJSON(decodeAny(t, string(raw)), visitOptions()...), string(raw))

	valid := func(lag string) string {
		return `{"observed_at":"2026-11-17T12:00:00Z","consumer_lag":[` +
			`{"group":"event-writer","lag":` + lag + `},` +
			`{"group":"detector","lag":{"state":"available","value":1}},` +
			`{"group":"alert-writer","lag":{"state":"available","value":2}}]}`
	}
	require.NoError(t, schema.VisitJSON(decodeAny(t, valid(`{"state":"available","value":0}`)), visitOptions()...),
		"a measured zero lag is a real value")

	for name, raw := range map[string]string{
		"unavailable_with_zero": valid(`{"state":"unavailable","reason":"no_data","value":0}`),
		"operator_only_value":   valid(`{"state":"unavailable","reason":"operator_only","value":5}`),
		"available_operator":    valid(`{"state":"available","reason":"operator_only","value":5}`),
		"available_no_value":    valid(`{"state":"available"}`),
		"unavailable_no_reason": valid(`{"state":"unavailable"}`),
		"unknown_reason":        valid(`{"state":"unavailable","reason":"boom"}`),
		"unknown_state":         valid(`{"state":"stale","value":1}`),
		"tenant_leak":           strings.Replace(valid(`{"state":"available","value":0}`), "{", `{"tenant_id":"a",`, 1),
		"extra_row_key":         valid(`{"state":"available","value":0},"partition":3`),
		"unknown_group":         strings.Replace(valid(`{"state":"available","value":0}`), "event-writer", "ingest", 1),
		"missing_row":           `{"observed_at":"2026-11-17T12:00:00Z","consumer_lag":[]}`,
		"non_utc_observed_at":   strings.Replace(valid(`{"state":"available","value":0}`), "12:00:00Z", "12:00:00+01:00", 1),
	} {
		assert.Error(t, schema.VisitJSON(decodeAny(t, raw), visitOptions()...), "%s should be rejected", name)
	}
}

func decodeAny(t *testing.T, raw string) any {
	t.Helper()

	var v any
	require.NoError(t, json.Unmarshal([]byte(raw), &v), raw)

	return v
}

// TestOpenAPISystemStatusContract pins the parameterless, admin-only System
// operation and its closed response shape.
func TestOpenAPISystemStatusContract(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	op := operations(doc)["GET "+pathSystem]
	require.NotNil(t, op, "GET %s must be documented", pathSystem)
	assert.Empty(t, op.Parameters, "the PromQL is a server constant; no parameters are accepted")

	for _, code := range []string{"200", "400", "401", "403", "503"} {
		require.NotNil(t, op.Responses.Value(code), "missing response %s", code)
	}

	ok := op.Responses.Value("200").Value.Content.Get("application/json").Schema
	assert.Equal(t, "#/components/schemas/SystemStatus", ok.Ref)
	assert.Contains(t, op.Description, "SENTINEL_AUTH.OPERATOR_TENANTS", "the operator rule must be documented")

	body := doc.Components.Schemas["SystemStatus"].Value
	require.NotNil(t, body)
	assert.ElementsMatch(t, []string{
		"observed_at", "metrics_source", "services", "ingest", "http_errors", "dlq_rate", "consumer_lag", "latency",
	}, body.Required)
	assert.Contains(t, body.Properties, "grafana_url", "grafana_url is documented and optional")
	require.NotNil(t, body.AdditionalProperties.Has)
	assert.False(t, *body.AdditionalProperties.Has)

	services := body.Properties["services"].Value
	require.NotNil(t, services.MaxItems)
	assert.Equal(t, uint64(5), services.MinItems)
	assert.Equal(t, uint64(5), *services.MaxItems)
	assert.Equal(t, "#/components/schemas/SystemService", services.Items.Ref)
	service := doc.Components.Schemas["SystemService"].Value
	assert.ElementsMatch(t, []any{"ingest", "event-writer", "detector", "alert-writer", "query-api"},
		service.Properties["name"].Value.Enum)
	assert.Equal(t, "#/components/schemas/SystemReadiness", service.Properties["readiness"].Ref)

	raw, err := json.Marshal(doc.Components.Schemas["SystemReadiness"].Value)
	require.NoError(t, err)
	for _, state := range []string{"ready", "degraded", "not_ready", "unavailable"} {
		assert.Contains(t, string(raw), `"`+state+`"`, "readiness state %s", state)
	}
}

// TestOpenAPISystemStatusDrift validates real handler bodies for every
// availability state against the documented schema, and proves the schema
// rejects fabricated zeros.
func TestOpenAPISystemStatusDrift(t *testing.T) {
	t.Parallel()

	schema := standaloneSchema(t, loadOpenAPI(t), "SystemStatus")

	var available map[string]any

	for name, tc := range systemCases() {
		f := newSystemFixture(t, tc)
		status, raw := f.get(t, "admin")
		require.Equal(t, http.StatusOK, status, string(raw))

		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		require.NoError(t, schema.VisitJSON(body, visitOptions()...), "%s: %s", name, raw)

		if name == "available" {
			available = body
		}
	}

	require.NotNil(t, available)

	mutate := func(edit func(m map[string]any)) any {
		raw, err := json.Marshal(available)
		require.NoError(t, err)

		var copied map[string]any
		require.NoError(t, json.Unmarshal(raw, &copied))
		edit(copied)

		return copied
	}
	firstService := func(m map[string]any) map[string]any {
		services, _ := m["services"].([]any)
		first, _ := services[0].(map[string]any)

		return first
	}

	for name, body := range map[string]any{
		"metric_unavailable_with_zero": mutate(func(m map[string]any) {
			m["dlq_rate"] = map[string]any{"state": "unavailable", "reason": "no_data", "value": 0}
		}),
		"readiness_unavailable_with_counts": mutate(func(m map[string]any) {
			firstService(m)["readiness"] = map[string]any{
				"state": "unavailable", "reason": "not_reachable", "ready_replicas": 0, "probed_replicas": 0,
			}
		}),
		"readiness_ready_without_counts": mutate(func(m map[string]any) {
			firstService(m)["readiness"] = map[string]any{"state": "ready"}
		}),
		"readiness_unknown_state": mutate(func(m map[string]any) {
			firstService(m)["readiness"] = map[string]any{"state": "stale", "ready_replicas": 1, "probed_replicas": 1}
		}),
		"tenant_leak": mutate(func(m map[string]any) { m["tenant_id"] = "a" }),
		"unknown_result": mutate(func(m map[string]any) {
			ingest, _ := m["ingest"].(map[string]any)
			byResult, _ := ingest["by_result"].(map[string]any)
			byResult["dropped"] = 1
		}),
		"missing_service": mutate(func(m map[string]any) {
			services, _ := m["services"].([]any)
			m["services"] = services[1:]
		}),
		"non_utc_observed_at": mutate(func(m map[string]any) { m["observed_at"] = "2026-11-18T12:00:00+01:00" }),
	} {
		assert.Error(t, schema.VisitJSON(body, visitOptions()...), "%s should be rejected", name)
	}
}
