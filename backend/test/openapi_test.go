package test_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const openAPIPath = "../../api/openapi.yaml"

const (
	errorSchemaRef = "#/components/schemas/Error"
	pathBatch      = "/api/v1/events:batch"
	pathAlertByID  = "/api/v1/alerts/{alert_id}"
	pathStream     = "/api/v1/alerts/stream"
	maxBatchEvents = 500
	maxPageLimit   = 200
)

// loadOpenAPI loads and resolves the OpenAPI document without allowing external refs.
func loadOpenAPI(t *testing.T) *openapi3.T {
	t.Helper()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	abs, err := filepath.Abs(openAPIPath)
	require.NoError(t, err)

	doc, err := loader.LoadFromFile(abs)
	require.NoError(t, err)

	return doc
}

func operations(doc *openapi3.T) map[string]*openapi3.Operation {
	out := map[string]*openapi3.Operation{}

	for p, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			out[method+" "+p] = op
		}
	}

	return out
}

func TestOpenAPIDocumentIsValid(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	require.True(t, strings.HasPrefix(doc.OpenAPI, "3.1"), "openapi version %q", doc.OpenAPI)
	require.NoError(t, doc.Validate(t.Context()))
}

func TestOpenAPIRoutes(t *testing.T) {
	t.Parallel()

	want := []string{
		"POST /api/v1/events:batch",
		"GET /api/v1/events",
		"GET /api/v1/events/{event_id}",
		"GET /api/v1/alerts",
		"GET /api/v1/alerts/{alert_id}",
		"PATCH /api/v1/alerts/{alert_id}",
		"GET /api/v1/alerts/stream",
		"GET /api/v1/rules",
		"GET /api/v1/metrics/summary",
		"GET /healthz",
		"GET /readyz",
		"GET /metrics",
	}
	sort.Strings(want)

	var got []string
	for k := range operations(loadOpenAPI(t)) {
		got = append(got, k)
	}

	sort.Strings(got)
	assert.Equal(t, want, got)
}

func TestOpenAPIBatchResponses(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	op := operations(doc)["POST "+pathBatch]
	require.NotNil(t, op)

	for _, code := range []string{"202", "400", "401", "403", "413", "429", "503"} {
		require.NotNil(t, op.Responses.Value(code), "missing response %s", code)
	}

	for _, code := range []string{"429", "503"} {
		require.Contains(t, op.Responses.Value(code).Value.Headers, "Retry-After", "status %s", code)
	}

	body := op.Responses.Value("202").Value.Content.Get("application/json").Schema.Value
	assert.ElementsMatch(t,
		[]string{"accepted_count", "rejected_count", "event_ids", "request_id"}, body.Required)

	req := op.RequestBody.Value.Content.Get("application/json").Schema.Value
	events := req.Properties["events"].Value
	require.NotNil(t, events.MaxItems)
	assert.Equal(t, uint64(maxBatchEvents), *events.MaxItems)
	assert.Equal(t, uint64(1), events.MinItems)
	assert.Contains(t, events.Items.Ref, "/Event")
}

func TestOpenAPISecurity(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	api := doc.Components.SecuritySchemes["ApiKeyAuth"].Value
	require.NotNil(t, api)
	assert.Equal(t, "apiKey", api.Type)
	assert.Equal(t, "header", api.In)

	bearer := doc.Components.SecuritySchemes["BearerAuth"].Value
	require.NotNil(t, bearer)
	assert.Equal(t, "http", bearer.Type)
	assert.Equal(t, "bearer", bearer.Scheme)
	assert.Equal(t, "JWT", bearer.BearerFormat)

	// No credential may travel in the URL (SSE tokens included).
	for name, s := range doc.Components.SecuritySchemes {
		assert.NotEqual(t, "query", s.Value.In, "scheme %s uses a query token", name)
	}

	for key, op := range operations(doc) {
		names := securityNames(doc, op)

		switch {
		case key == "POST "+pathBatch:
			assert.Equal(t, []string{"ApiKeyAuth"}, names, key)
		case key == "GET /healthz", key == "GET /readyz":
			assert.Empty(t, names, key)
		case key == "GET /metrics":
			// internal only, documented without public auth
		default:
			assert.Equal(t, []string{"BearerAuth"}, names, key)
		}
	}
}

func securityNames(doc *openapi3.T, op *openapi3.Operation) []string {
	reqs := doc.Security
	if op.Security != nil {
		reqs = *op.Security
	}

	var names []string

	for _, r := range reqs {
		for n := range r {
			names = append(names, n)
		}
	}

	sort.Strings(names)

	return names
}

func TestOpenAPIEnvelopeAndLists(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	errSchema := doc.Components.Schemas["Error"].Value
	assert.Equal(t, []string{"error"}, errSchema.Required)
	assert.ElementsMatch(t,
		[]string{"code", "message", "request_id"}, errSchema.Properties["error"].Value.Required)

	for _, name := range []string{"EventList", "AlertList"} {
		s := doc.Components.Schemas[name].Value
		assert.ElementsMatch(t, []string{"items", "next_cursor", "has_more"}, s.Required, name)
	}

	patch := operations(doc)["PATCH "+pathAlertByID]
	require.NotNil(t, patch)

	pb := patch.RequestBody.Value.Content.Get("application/json").Schema.Value
	assert.Contains(t, pb.Required, "status")
	assert.Contains(t, pb.Required, "version")
	assert.ElementsMatch(t, []any{"open", "acknowledged", "resolved"}, pb.Properties["status"].Value.Enum)
	assert.True(t, pb.Properties["version"].Value.Type.Includes("integer"))
}

func TestOpenAPIErrorResponsesUseEnvelope(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)
	checked := 0

	for key, op := range operations(doc) {
		for code, ref := range op.Responses.Map() {
			if strings.HasPrefix(code, "2") {
				continue
			}

			mt := ref.Value.Content.Get("application/json")
			require.NotNil(t, mt, "%s %s has no application/json body", key, code)
			assert.Equal(t, errorSchemaRef, mt.Schema.Ref, "%s %s", key, code)

			checked++
		}
	}

	assert.Positive(t, checked)
}

func TestOpenAPIBoundsAndStream(t *testing.T) {
	t.Parallel()

	doc := loadOpenAPI(t)

	limit := doc.Components.Parameters["Limit"].Value
	require.NotNil(t, limit)
	require.NotNil(t, limit.Schema.Value.Max)
	assert.InDelta(t, float64(maxPageLimit), *limit.Schema.Value.Max, 0)

	stream := operations(doc)["GET "+pathStream]
	require.NotNil(t, stream)
	assert.NotNil(t, stream.Responses.Value("200").Value.Content.Get("text/event-stream"))
}
