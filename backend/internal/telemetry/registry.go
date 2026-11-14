// Package telemetry builds per-process Prometheus registries and the Sentinel
// service metrics. It holds no package-level state: every process constructs
// its own registry and injects it, so tests and binaries never share series.
// Label names follow docs/decisions/0004-metrics-label-policy.md.
package telemetry

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry returns an isolated registry with the Go runtime and process
// collectors already registered.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return reg
}

// Handler serves reg in the Prometheus text exposition format.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// AllowedLabelNames returns the label dimensions permitted by ADR 0004.
func AllowedLabelNames() []string {
	return []string{
		labelResult, "status", "rule_id", "severity", "reason", "group", "topic", "partition", "service",
	}
}

// LabelNamesOutsideAllowlist gathers reg and returns the sorted, distinct
// label names that are not in AllowedLabelNames. Tests use it as the metrics
// hygiene guard. Families from the standard Go and process collectors (go_,
// process_ prefixes, e.g. go_info{version}) are not Sentinel metrics and are
// skipped.
func LabelNamesOutsideAllowlist(reg prometheus.Gatherer) ([]string, error) {
	families, err := reg.Gather()
	if err != nil {
		return nil, fmt.Errorf("gather metrics: %w", err)
	}

	allowed := AllowedLabelNames()
	var outside []string
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "go_") || strings.HasPrefix(family.GetName(), "process_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				name := pair.GetName()
				if !slices.Contains(allowed, name) && !slices.Contains(outside, name) {
					outside = append(outside, name)
				}
			}
		}
	}
	slices.Sort(outside)

	return outside, nil
}
