package query_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/query"
)

const promVectorBody = `{"status":"success","data":{"resultType":"vector","result":[` +
	`{"metric":{"group":"grp-ew"},"value":[1763382615,"12345"]},` +
	`{"metric":{"group":"grp-det"},"value":[1763382615,"0"]},` +
	`{"metric":{"group":"grp-nan"},"value":[1763382615,"NaN"]},` +
	`{"metric":{"group":"grp-inf"},"value":[1763382615,"+Inf"]},` +
	`{"metric":{"group":"grp-ninf"},"value":[1763382615,"-Inf"]}]}}`

// promServer answers /api/v1/query with body and records the received form.
func promServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Pointer[url.Values]) {
	t.Helper()
	got := &atomic.Pointer[url.Values]{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		form := r.Form
		got.Store(&form)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestPrometheusMetricsVectorByLabel(t *testing.T) {
	t.Parallel()
	srv, got := promServer(t, http.StatusOK, promVectorBody)
	metrics, err := query.NewPrometheusMetrics(srv.URL)
	require.NoError(t, err)
	at := time.Date(2025, time.November, 17, 12, 30, 15, 0, time.UTC)
	series, err := metrics.Query(t.Context(), wantLagQuery, at)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"grp-ew": 12345, "grp-det": 0}, series,
		"NaN and infinite samples are dropped and therefore count as no data")
	form := got.Load()
	require.NotNil(t, form)
	assert.Equal(t, wantLagQuery, form.Get("query"))
	assert.Equal(t, "1763382615", form.Get("time"))
}

func TestPrometheusMetricsUnlabeledSampleUsesEmptyKey(t *testing.T) {
	t.Parallel()
	srv, _ := promServer(t, http.StatusOK,
		`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"3"]}]}}`)
	metrics, err := query.NewPrometheusMetrics(srv.URL)
	require.NoError(t, err)
	series, err := metrics.Query(t.Context(), "sum(up)", time.Unix(1, 0))
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"": 3}, series)
}

func TestPrometheusMetricsEmptyVectorIsEmptyMap(t *testing.T) {
	t.Parallel()
	srv, _ := promServer(t, http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	metrics, err := query.NewPrometheusMetrics(srv.URL)
	require.NoError(t, err)
	series, err := metrics.Query(t.Context(), wantLagQuery, time.Unix(1, 0))
	require.NoError(t, err)
	assert.Empty(t, series)
}

func TestPrometheusMetricsFailures(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"server_error": {http.StatusInternalServerError, `{"status":"error","errorType":"internal","error":"boom"}`},
		"bad_query":    {http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"parse"}`},
		"not_vector":   {http.StatusOK, `{"status":"success","data":{"resultType":"scalar","result":[1,"2"]}}`},
		"garbage":      {http.StatusOK, `not json`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _ := promServer(t, tc.status, tc.body)
			metrics, err := query.NewPrometheusMetrics(srv.URL)
			require.NoError(t, err)
			series, err := metrics.Query(t.Context(), wantLagQuery, time.Unix(1, 0))
			require.Error(t, err)
			assert.Nil(t, series)
			assert.NotErrorIs(t, err, query.ErrPrometheusNotConfigured)
		})
	}
}

func TestPrometheusMetricsUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()
	metrics, err := query.NewPrometheusMetrics(addr)
	require.NoError(t, err)
	_, err = metrics.Query(t.Context(), wantLagQuery, time.Unix(1, 0))
	require.Error(t, err)
}

func TestPrometheusMetricsTimeoutIsBounded(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	metrics, err := query.NewPrometheusMetrics(srv.URL)
	require.NoError(t, err)
	start := time.Now()
	_, err = metrics.Query(t.Context(), wantLagQuery, time.Unix(1, 0))
	require.Error(t, err)
	assert.Less(t, time.Since(start), lagTimeoutBound+time.Second, "the adapter applies its own 2 s timeout")
}

func TestPrometheusMetricsNotConfigured(t *testing.T) {
	t.Parallel()
	metrics, err := query.NewPrometheusMetrics("")
	require.NoError(t, err, "an empty URL is a valid, explicitly unconfigured deployment")
	series, err := metrics.Query(t.Context(), wantLagQuery, time.Unix(1, 0))
	require.ErrorIs(t, err, query.ErrPrometheusNotConfigured)
	assert.Nil(t, series)
}

func TestPrometheusMetricsRejectsInvalidURL(t *testing.T) {
	t.Parallel()
	_, err := query.NewPrometheusMetrics("http://[::1")
	require.Error(t, err)
}
