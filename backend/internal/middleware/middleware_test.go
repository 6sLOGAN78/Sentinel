package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/errs"
	"github.com/6sLOGAN78/sentinel/internal/logger"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

type envelope struct {
	Error map[string]string `json:"error"`
}

func newEcho(t *testing.T) *echo.Echo {
	t.Helper()

	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, []string{"https://example.test"})

	e.GET("/panic", func(echo.Context) error { panic("boom secret-internal") })
	e.GET("/slow", func(echo.Context) error { return errs.NewTooManyRequestsError("slow", 7) })
	e.GET("/down", func(echo.Context) error { return errs.NewServiceUnavailableError("down", 3) })

	return e
}

func do(t *testing.T, e *echo.Echo, path string, hdr map[string]string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)

	return rec, env
}

func TestRecoverPanicYieldsEnvelope(t *testing.T) {
	t.Parallel()

	rec, env := do(t, newEcho(t), "/panic", nil)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", env.Error["code"])
	assert.NotEmpty(t, env.Error["request_id"])
	assert.Equal(t, env.Error["request_id"], rec.Header().Get("X-Request-ID"))
	assert.NotContains(t, rec.Body.String(), "secret-internal")
	assert.Len(t, env.Error, 3)
}

func TestRecoverRegisteredFirst(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	chain := middleware.Chain(&logger, nil)
	require.NotEmpty(t, chain)
	assert.Equal(t, "recover", chain[0].Name)
}

func TestRetryAfterEnvelope(t *testing.T) {
	t.Parallel()

	e := newEcho(t)

	rec, env := do(t, e, "/slow", nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "7", rec.Header().Get("Retry-After"))
	assert.Equal(t, "slow", env.Error["message"])

	rec, env = do(t, e, "/down", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "3", rec.Header().Get("Retry-After"))
	assert.Equal(t, "SERVICE_UNAVAILABLE", env.Error["code"])
}

func TestEchoHTTPErrorRenderedAsEnvelope(t *testing.T) {
	t.Parallel()

	rec, env := do(t, newEcho(t), "/nope", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "NOT_FOUND", env.Error["code"])
	assert.NotEmpty(t, env.Error["request_id"])
	assert.Len(t, env.Error, 3)
}

func TestRequestIDSanitized(t *testing.T) {
	t.Parallel()

	e := newEcho(t)

	rec, _ := do(t, e, "/slow", map[string]string{"X-Request-ID": "good.id_1-2"})
	assert.Equal(t, "good.id_1-2", rec.Header().Get("X-Request-ID"))

	for _, bad := range []string{strings.Repeat("a", 65), "bad id", "a\x01b", "x;y"} {
		rec, _ = do(t, e, "/slow", map[string]string{"X-Request-ID": bad})
		got := rec.Header().Get("X-Request-ID")
		assert.NotEqual(t, bad, got)
		assert.Len(t, got, 36)
	}
}

// syncBuffer is a goroutine-safe log sink for request-logger assertions.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// apiLogLine returns the request-logger ("API") line written to sink.
func apiLogLine(t *testing.T, sink *syncBuffer) map[string]any {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSpace(sink.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))

		if entry["message"] == "API" {
			return entry
		}
	}

	require.FailNow(t, "no API log line", sink.String())

	return nil
}

func newLoggedEcho(t *testing.T, sink *syncBuffer) *echo.Echo {
	t.Helper()

	e := echo.New()
	logger := zerolog.New(sink)
	middleware.Apply(e, &logger, []string{"https://example.test"})

	return e
}

func TestRequestLoggerStatusMatchesResponseForPlainError(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/plain", func(echo.Context) error { return errors.New("boom") })

	rec, env := do(t, e, "/plain", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", env.Error["code"])

	entry := apiLogLine(t, sink)
	assert.InDelta(t, float64(http.StatusInternalServerError), entry["status"], 0)
	assert.Equal(t, "error", entry["level"])
}

func TestRequestLoggerStatusForHTTPError(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/slow", func(echo.Context) error { return errs.NewTooManyRequestsError("slow", 1) })

	rec, _ := do(t, e, "/slow", nil)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	entry := apiLogLine(t, sink)
	assert.InDelta(t, float64(http.StatusTooManyRequests), entry["status"], 0)
	assert.Equal(t, "warn", entry["level"])
}

func TestRealIPIgnoresClientForwardingHeaders(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Real-IP", "198.51.100.8")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	entry := apiLogLine(t, sink)
	assert.Equal(t, "192.0.2.10", entry["ip"])
	assert.NotContains(t, sink.String(), "198.51.100.")
}

func TestApplyKeepsConfiguredIPExtractor(t *testing.T) {
	t.Parallel()

	_, proxies, err := net.ParseCIDR("192.0.2.0/24")
	require.NoError(t, err)

	e := echo.New()
	e.IPExtractor = echo.ExtractIPFromXFFHeader(echo.TrustIPRange(proxies))
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)

	var got string
	e.GET("/ip", func(c echo.Context) error {
		got = c.RealIP()

		return c.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.0.2.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	e.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "198.51.100.7", got)
}

func TestCORSEmptyDenies(t *testing.T) {
	t.Parallel()

	for _, origins := range [][]string{nil, {}} {
		e := echo.New()
		logger := zerolog.Nop()
		middleware.Apply(e, &logger, origins)
		e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			req := httptest.NewRequest(method, "/ok", nil)
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}

func TestCORSConfiguredOrigin(t *testing.T) {
	t.Parallel()

	e := newEcho(t)
	rec, _ := do(t, e, "/slow", map[string]string{"Origin": "https://example.test"})
	assert.Equal(t, "https://example.test", rec.Header().Get("Access-Control-Allow-Origin"))
	rec, _ = do(t, e, "/slow", map[string]string{"Origin": "https://evil.example"})
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestRequestLoggerNoQuery(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	e := newLoggedEcho(t, sink)
	e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	do(t, e, "/ok?secret=query-sentinel", nil)
	assert.NotContains(t, sink.String(), "query-sentinel")
	assert.Equal(t, "/ok", apiLogLine(t, sink)["uri"])
}

func TestNoKeyInLogs(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "plain", "typed", "echo", "client", "panic"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			sink := &syncBuffer{}
			e := newLoggedEcho(t, sink)
			e.Logger.SetOutput(sink)
			e.POST("/sensitive", func(c echo.Context) error {
				message := "key-sentinel query-sentinel body-sentinel"
				switch kind {
				case "plain":
					return errors.New(message)
				case "typed":
					return errs.NewInternalServerError().WithMessage(message)
				case "echo":
					return echo.NewHTTPError(http.StatusInternalServerError, message)
				case "client":
					return errs.NewBadRequestError(message, false, nil, nil, nil)
				case "panic":
					panic(message)
				default:
					return c.NoContent(http.StatusNoContent)
				}
			})

			req := httptest.NewRequest(http.MethodPost, "/sensitive?secret=query-sentinel", strings.NewReader("body-sentinel"))
			req.Header.Set("X-Api-Key", "key-sentinel")
			req.Header.Set("Authorization", "Bearer authorization-sentinel")
			req.Header.Set("User-Agent", "agent-sentinel")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			secrets := []string{
				"key-sentinel", "query-sentinel", "body-sentinel",
				"authorization-sentinel", "agent-sentinel",
			}
			for _, secret := range secrets {
				assert.NotContains(t, sink.String(), secret)
			}
			if kind == "client" {
				for line := range strings.SplitSeq(strings.TrimSpace(sink.String()), "\n") {
					var entry map[string]any
					require.NoError(t, json.Unmarshal([]byte(line), &entry))
					assert.Equal(t, "warn", entry["level"])
					assert.NotContains(t, entry, "stack")
				}
			}
		})
	}
}

// newContractEcho builds an Echo with the production JSON logger writing to
// sink. pre runs before the global chain (nil for none).
func newContractEcho(t *testing.T, sink *syncBuffer, pre echo.MiddlewareFunc) *echo.Echo {
	t.Helper()

	cfg := config.DefaultObservabilityConfig()
	cfg.Environment = "local"
	cfg.Logging.Level = "debug"
	log := logger.NewWithWriter(cfg, "query-api", sink)

	e := echo.New()
	if pre != nil {
		e.Use(pre)
	}

	middleware.Apply(e, &log, nil)

	return e
}

// logLines parses every line in sink as one JSON object.
func logLines(t *testing.T, sink *syncBuffer) ([]string, []map[string]any) {
	t.Helper()

	raw := strings.TrimSpace(sink.String())
	require.NotEmpty(t, raw)

	var lines []string

	var entries []map[string]any

	for line := range strings.SplitSeq(raw, "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry), line)

		lines = append(lines, line)
		entries = append(entries, entry)
	}

	return lines, entries
}

// duplicateKeys returns the top-level keys that appear more than once in the
// JSON object line. encoding/json silently keeps the last duplicate, so the
// raw token stream is inspected.
func duplicateKeys(t *testing.T, line string) []string {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)

	seen := map[string]bool{}

	var dups []string

	for dec.More() {
		keyTok, kerr := dec.Token()
		require.NoError(t, kerr)

		key, ok := keyTok.(string)
		require.True(t, ok)

		if seen[key] {
			dups = append(dups, key)
		}

		seen[key] = true

		var skip json.RawMessage
		require.NoError(t, dec.Decode(&skip))
	}

	return dups
}

func TestRequestLogJSONContract(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"success", "client", "server"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			sink := &syncBuffer{}
			e := newContractEcho(t, sink, nil)
			e.POST("/events", func(c echo.Context) error {
				var body map[string]any
				if err := c.Bind(&body); err != nil {
					return err
				}

				switch kind {
				case "client":
					return errs.NewBadRequestError("invalid event", false, nil, nil, nil)
				case "server":
					return errors.New("store failed")
				default:
					return c.NoContent(http.StatusAccepted)
				}
			})

			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(`{"password":"hunter2"}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req.Header.Set("X-Api-Key", "sk_test_secret_value")
			req.Header.Set("Authorization", "Bearer eyJhbGciOi.secret.sig")
			e.ServeHTTP(httptest.NewRecorder(), req)

			lines, entries := logLines(t, sink)
			for i, line := range lines {
				assert.Equal(t, 1, strings.Count(line, `"request_id":`), line)
				assert.LessOrEqual(t, strings.Count(line, `"ip":`), 1, line)
				assert.Empty(t, duplicateKeys(t, line), line)

				for _, secret := range []string{"sk_test_secret_value", "eyJhbGciOi", "hunter2"} {
					assert.NotContains(t, line, secret)
				}

				assert.Equal(t, "query-api", entries[i]["service"])
				assert.NotEmpty(t, entries[i]["level"])
				assert.NotEmpty(t, entries[i]["timestamp"])
				assert.NotContains(t, entries[i], "time")
			}

			assert.Equal(t, "/events", apiLogLine(t, sink)["uri"])
		})
	}
}

func TestRequestLogUserIDOnce(t *testing.T) {
	t.Parallel()

	setUser := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(middleware.UserIDKey, "user-1")

			return next(c)
		}
	}

	cases := map[string]struct {
		pre   echo.MiddlewareFunc
		route []echo.MiddlewareFunc
	}{
		// identity known before EnhanceContext: the context logger carries it
		"before_context": {pre: setUser},
		// identity set by route-level auth after EnhanceContext
		"route_auth": {route: []echo.MiddlewareFunc{setUser}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &syncBuffer{}
			e := newContractEcho(t, sink, tc.pre)
			e.GET("/me", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) }, tc.route...)
			do(t, e, "/me", nil)

			lines, _ := logLines(t, sink)
			for _, line := range lines {
				assert.LessOrEqual(t, strings.Count(line, `"user_id":`), 1, line)
			}

			api := apiLogLine(t, sink)
			assert.Equal(t, "user-1", api["user_id"])
		})
	}
}

func TestRequestLogTraceID(t *testing.T) {
	t.Parallel()

	traceID := trace.TraceID{
		0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6,
		0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36,
	}
	spanID := trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
	require.True(t, sc.IsValid())

	withSpan := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := trace.ContextWithSpanContext(c.Request().Context(), sc)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}

	t.Run("with_span", func(t *testing.T) {
		t.Parallel()

		sink := &syncBuffer{}
		e := newContractEcho(t, sink, withSpan)
		e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
		do(t, e, "/ok", nil)

		api := apiLogLine(t, sink)
		assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", api["trace_id"])
		assert.Equal(t, "00f067aa0ba902b7", api["span_id"])

		lines, _ := logLines(t, sink)
		for _, line := range lines {
			assert.Equal(t, 1, strings.Count(line, `"trace_id":`), line)
		}
	})

	t.Run("without_span", func(t *testing.T) {
		t.Parallel()

		sink := &syncBuffer{}
		e := newContractEcho(t, sink, nil)
		e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
		do(t, e, "/ok", nil)

		api := apiLogLine(t, sink)
		assert.NotContains(t, api, "trace_id")
		assert.NotContains(t, api, "span_id")
	})
}

func TestChainOrderIncludesTrace(t *testing.T) {
	t.Parallel()

	logger := zerolog.Nop()
	names := make([]string, 0, 8)
	for _, m := range middleware.Chain(&logger, nil) {
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"recover", "request_id", "trace", "context", "cors", "secure", "request_logger"}, names)
}

// tracedEdge wraps a logged Echo with the same otelhttp options as cmd/ingest.
func tracedEdge(
	t *testing.T, ratio float64, sink *syncBuffer,
) (http.Handler, *tracetest.InMemoryExporter, *telemetry.Tracing) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: ratio, OTLPEndpoint: "http://127.0.0.1:4318"},
		config.ServiceIngest, telemetry.WithExporter(exporter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

	e := newContractEcho(t, sink, nil)
	e.POST("/api/v1/events\\:batch", func(c echo.Context) error { return c.NoContent(http.StatusAccepted) })
	e.GET("/api/v1/alerts/:id", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	handler := otelhttp.NewHandler(e, "ingest",
		otelhttp.WithTracerProvider(tr.Provider()),
		otelhttp.WithPropagators(tr.Propagator()),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
	)

	return handler, exporter, tr
}

func serveEdge(t *testing.T, h http.Handler, method, target string, hdr map[string]string) {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(`{}`))
	req.RemoteAddr = "198.51.100.7:51000"
	req.Header.Set("User-Agent", "edge-test/1")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestEdgeServerSpanNamedByRouteTemplate(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	h, exporter, tr := tracedEdge(t, 1, sink)
	serveEdge(t, h, http.MethodPost, "/api/v1/events:batch", nil)
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "POST /api/v1/events:batch", span.Name)
	assert.Equal(t, trace.SpanKindServer, span.SpanKind)
	assert.Contains(t, span.Attributes, attribute.String("http.route", "/api/v1/events:batch"))
	for _, kv := range span.Attributes {
		for _, forbidden := range []string{
			"client.address", "network.peer.address", "user_agent.original", "url.path", "server.address",
		} {
			assert.NotEqual(t, forbidden, string(kv.Key))
		}
		assert.NotContains(t, kv.Value.String(), "198.51.100.7")
	}

	assert.Equal(t, span.SpanContext.TraceID().String(), apiLogLine(t, sink)["trace_id"])
}

func TestEdgeServerSpanUsesTemplateNotRawID(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	h, exporter, tr := tracedEdge(t, 1, sink)
	serveEdge(t, h, http.MethodGet, "/api/v1/alerts/secret-alert-id", nil)
	serveEdge(t, h, http.MethodGet, "/no/such/secret-path", nil)
	require.NoError(t, tr.ForceFlush(t.Context()))

	names := []string{}
	for _, span := range exporter.GetSpans() {
		names = append(names, span.Name)
	}
	assert.ElementsMatch(t, []string{"GET /api/v1/alerts/:id", "HTTP GET"}, names)
}

func TestEdgeClientTraceparentCannotForceSampling(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	h, exporter, tr := tracedEdge(t, 0, sink)
	serveEdge(t, h, http.MethodPost, "/api/v1/events:batch", map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	require.NoError(t, tr.ForceFlush(t.Context()))

	assert.Empty(t, exporter.GetSpans(), "public endpoint: the client sampled flag is not trusted")

	traceID, ok := apiLogLine(t, sink)["trace_id"].(string)
	require.True(t, ok, "request log still carries a trace_id when unsampled")
	assert.Regexp(t, `^[0-9a-f]{32}$`, traceID)
	assert.NotEqual(t, "4bf92f3577b34da6a3ce929d0e0e4736", traceID, "public endpoint starts a new root")
}

func TestEdgeServerSpanBoundsUnknownMethod(t *testing.T) {
	t.Parallel()

	sink := &syncBuffer{}
	h, exporter, tr := tracedEdge(t, 1, sink)
	serveEdge(t, h, "X-SECRET-METHOD", "/api/v1/alerts/1", nil)
	require.NoError(t, tr.ForceFlush(t.Context()))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "_OTHER /api/v1/alerts/:id", spans[0].Name)
}
