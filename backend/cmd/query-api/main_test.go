package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

const clientTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"

func newTracing(t *testing.T, ratio float64) (*telemetry.Tracing, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tr, err := telemetry.NewTracing(t.Context(),
		config.TracingConfig{Enabled: true, SampleRatio: ratio, OTLPEndpoint: "http://127.0.0.1:4318"},
		config.ServiceQueryAPI, telemetry.WithExporter(exporter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })
	return tr, exporter
}

func apiLogTraceID(t *testing.T, sink *bytes.Buffer) string {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(sink.Bytes()))
	for scanner.Scan() {
		var line map[string]any
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line["message"] != "API" {
			continue
		}
		traceID, ok := line["trace_id"].(string)
		require.True(t, ok, "request log has no trace_id: %s", scanner.Text())
		return traceID
	}
	t.Fatalf("no API request log line in %q", sink.String())
	return ""
}

func TestQueryTracedHandlerNamesSpanAndLogsTraceID(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		ratio float64
		want  int
	}{
		"sampled":       {ratio: 1, want: 1},
		"client_forced": {ratio: 0, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tr, exporter := newTracing(t, tc.ratio)
			e := echo.New()
			var sink bytes.Buffer
			logger := zerolog.New(&sink)
			middleware.Apply(e, &logger, nil)
			e.GET("/api/v1/alerts/:id", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/alert-123", nil)
			req.Header.Set("Traceparent", "00-"+clientTraceID+"-00f067aa0ba902b7-01")
			tracedHandler(e, tr).ServeHTTP(httptest.NewRecorder(), req)
			require.NoError(t, tr.ForceFlush(t.Context()))

			logged := apiLogTraceID(t, &sink)
			require.NotEqual(t, clientTraceID, logged, "public endpoint never adopts the client trace")
			spans := exporter.GetSpans()
			require.Len(t, spans, tc.want, "client traceparent must not force sampling")
			if tc.want == 1 {
				require.Equal(t, "GET /api/v1/alerts/:id", spans[0].Name, "span named by route template")
				require.Equal(t, logged, spans[0].SpanContext.TraceID().String())
				require.Len(t, spans[0].Links, 1, "client context kept only as a link")
			}
		})
	}
}

// The SSE handler sets per-write deadlines and flushes through
// http.ResponseController; the tracing wrapper must keep both reachable.
func TestQueryTracedHandlerKeepsStreamWriterControls(t *testing.T) {
	t.Parallel()

	tr, exporter := newTracing(t, 1)
	e := echo.New()
	logger := zerolog.Nop()
	middleware.Apply(e, &logger, nil)
	e.GET("/api/v1/alerts/stream", func(c echo.Context) error {
		controller := http.NewResponseController(c.Response().Writer)
		if err := controller.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}
		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().WriteHeader(http.StatusOK)
		if _, err := c.Response().Writer.Write([]byte(": connected\n\n")); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		return controller.SetWriteDeadline(time.Time{})
	})
	server := httptest.NewServer(tracedHandler(e, tr))
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/alerts/stream", nil)
	require.NoError(t, err)
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "write deadline must reach the connection")
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, ": connected\n", line)

	require.Eventually(t, func() bool {
		require.NoError(t, tr.ForceFlush(t.Context()))
		return len(exporter.GetSpans()) == 1
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, "GET /api/v1/alerts/stream", exporter.GetSpans()[0].Name)
}
