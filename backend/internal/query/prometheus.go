package query

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// prometheusTimeout bounds every query so a slow Prometheus cannot stall a
// request; query-api readiness never depends on Prometheus.
const prometheusTimeout = 2 * time.Second

// ErrPrometheusNotConfigured reports that no Prometheus URL was configured.
var ErrPrometheusNotConfigured = errors.New("prometheus not configured")

// SystemMetrics runs one fixed, server-side PromQL instant query. The result
// maps the single grouping label value (or "" for an unlabeled sample) to its
// value. An empty map means Prometheus answered with no data.
type SystemMetrics interface {
	Query(ctx context.Context, promql string, at time.Time) (map[string]float64, error)
}

type prometheusMetrics struct {
	api v1.API
}

type unconfiguredMetrics struct{}

func (unconfiguredMetrics) Query(context.Context, string, time.Time) (map[string]float64, error) {
	return nil, ErrPrometheusNotConfigured
}

// NewPrometheusMetrics returns a SystemMetrics adapter over the Prometheus
// HTTP API. An empty url returns an adapter that always reports
// ErrPrometheusNotConfigured, so callers never invent values.
func NewPrometheusMetrics(url string) (SystemMetrics, error) {
	if url == "" {
		return unconfiguredMetrics{}, nil
	}
	client, err := api.NewClient(api.Config{Address: url})
	if err != nil {
		// Not wrapped: the parse error would echo the configured URL.
		return nil, errors.New("prometheus client: invalid address")
	}
	return &prometheusMetrics{api: v1.NewAPI(client)}, nil
}

// Query evaluates promql at the given instant with a 2 s timeout. Only vector
// results are accepted; NaN and infinite samples are dropped as no data.
func (m *prometheusMetrics) Query(ctx context.Context, promql string, at time.Time) (map[string]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, prometheusTimeout)
	defer cancel()
	value, _, _, err := m.api.Query(ctx, promql, at)
	if err != nil {
		return nil, fmt.Errorf("prometheus query: %w", err)
	}
	vector, ok := value.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("prometheus query: unexpected result type %T", value)
	}
	out := make(map[string]float64, len(vector))
	for _, sample := range vector {
		key, keyErr := sampleKey(sample.Metric)
		if keyErr != nil {
			return nil, keyErr
		}
		v := float64(sample.Value)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if _, dup := out[key]; dup {
			return nil, errors.New("prometheus query: duplicate series for one label value")
		}
		out[key] = v
	}
	return out, nil
}

// sampleKey returns the single label value of an aggregated sample.
func sampleKey(metric model.Metric) (string, error) {
	switch len(metric) {
	case 0:
		return "", nil
	case 1:
		for _, v := range metric {
			return string(v), nil
		}
	}
	return "", errors.New("prometheus query: expected at most one grouping label")
}
