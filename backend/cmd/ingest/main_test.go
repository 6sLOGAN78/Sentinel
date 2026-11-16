package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/middleware"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

func TestTracedHandlerNamesSpanAndIgnoresClientSampling(t *testing.T) {
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

			exporter := tracetest.NewInMemoryExporter()
			tr, err := telemetry.NewTracing(t.Context(),
				config.TracingConfig{Enabled: true, SampleRatio: tc.ratio, OTLPEndpoint: "http://127.0.0.1:4318"},
				config.ServiceIngest, telemetry.WithExporter(exporter))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tr.Shutdown(context.Background())) })

			e := echo.New()
			logger := zerolog.Nop()
			middleware.Apply(e, &logger, nil)
			e.POST("/api/v1/events\\:batch", func(c echo.Context) error { return c.NoContent(http.StatusAccepted) })

			req := httptest.NewRequest(http.MethodPost, "/api/v1/events:batch", nil)
			req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
			tracedHandler(e, tr).ServeHTTP(httptest.NewRecorder(), req)
			require.NoError(t, tr.ForceFlush(t.Context()))

			spans := exporter.GetSpans()
			require.Len(t, spans, tc.want)
			if tc.want == 1 {
				require.Equal(t, "POST /api/v1/events:batch", spans[0].Name)
				require.NotEqual(t, "4bf92f3577b34da6a3ce929d0e0e4736", spans[0].SpanContext.TraceID().String(),
					"public endpoint starts a new root")
				require.Len(t, spans[0].Links, 1, "client context kept only as a link")
			}
		})
	}
}
