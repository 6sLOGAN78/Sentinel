package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	delayBucketStart  = 0.05
	delayBucketFactor = 2
	delayBucketCount  = 14
)

// DetectorMetrics holds the spec 11.2 detector metrics plus the capacity and
// pin-retention signals from ADR 0007. A nil *DetectorMetrics is a valid no-op
// so callers need no guards.
type DetectorMetrics struct {
	alerts       *prometheus.CounterVec
	delay        prometheus.Histogram
	late         prometheus.Counter
	windows      prometheus.Gauge
	ledger       prometheus.Gauge
	bytes        prometheus.Gauge
	windowsLimit prometheus.Gauge
	ledgerLimit  prometheus.Gauge
	bytesLimit   prometheus.Gauge
	pinMargin    *prometheus.GaugeVec
}

func newGauge(name, help string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
}

// NewDetectorMetrics registers the detector metrics on reg.
func NewDetectorMetrics(reg prometheus.Registerer) *DetectorMetrics {
	m := &DetectorMetrics{
		alerts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sentinel_detection_alerts_total",
			Help: "Allowed alert publications (replays after a crash count again; unique alerts come from PG/CH).",
		}, []string{labelRuleID, "severity"}),
		delay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "sentinel_detection_delay_seconds",
			Help:    "Detector wall clock at publish ack minus trigger occurred_at.",
			Buckets: prometheus.ExponentialBuckets(delayBucketStart, delayBucketFactor, delayBucketCount),
		}),
		late: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "sentinel_detection_late_events_total",
			Help: "Valid inputs classified below the event-time watermark (replay may count an input again).",
		}),
		windows: newGauge("sentinel_detector_state_windows", "Open rule windows held by this process."),
		ledger: newGauge("sentinel_detector_state_ledger_entries",
			"Unacknowledged source prefix ledger entries held by this process."),
		bytes: newGauge("sentinel_detector_state_bytes", "Accounted retained detector state bytes."),
		windowsLimit: newGauge("sentinel_detector_state_windows_limit",
			"Effective admission ceiling for open rule windows."),
		ledgerLimit: newGauge("sentinel_detector_state_ledger_entries_limit",
			"Effective admission ceiling for prefix ledger entries."),
		bytesLimit: newGauge("sentinel_detector_state_bytes_limit",
			"Effective admission ceiling for accounted state bytes."),
		pinMargin: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sentinel_detector_pin_retention_margin_offsets",
			Help: "Committed detector pin offset minus the partition log start offset; 0 means history is about to be lost.",
		}, []string{labelTopic, labelPartition}),
	}
	reg.MustRegister(m.alerts, m.delay, m.late, m.windows, m.ledger, m.bytes,
		m.windowsLimit, m.ledgerLimit, m.bytesLimit, m.pinMargin)

	return m
}

// Published records one acknowledged alert publication and its detection delay.
// ruleID and severity come from the frozen alert, which only carries the fixed
// rule identifiers and severities, so cardinality stays bounded.
func (m *DetectorMetrics) Published(ruleID, severity string, delay time.Duration) {
	if m == nil {
		return
	}

	m.alerts.WithLabelValues(ruleID, severity).Inc()
	m.delay.Observe(delay.Seconds())
}

// IncLate counts one valid input classified strictly below the watermark.
func (m *DetectorMetrics) IncLate() {
	if m == nil {
		return
	}

	m.late.Inc()
}

// SetState publishes current process-wide state usage.
func (m *DetectorMetrics) SetState(windows, ledger, bytes int) {
	if m == nil {
		return
	}

	m.windows.Set(float64(windows))
	m.ledger.Set(float64(ledger))
	m.bytes.Set(float64(bytes))
}

// SetLimits publishes the effective admission ceilings.
func (m *DetectorMetrics) SetLimits(windows, ledger, bytes int) {
	if m == nil {
		return
	}

	m.windowsLimit.Set(float64(windows))
	m.ledgerLimit.Set(float64(ledger))
	m.bytesLimit.Set(float64(bytes))
}

// SetPinMargin publishes the retention margin of one owned partition.
func (m *DetectorMetrics) SetPinMargin(topic string, partition int32, margin int64) {
	if m == nil {
		return
	}

	m.pinMargin.WithLabelValues(topic, partitionLabel(partition)).Set(float64(margin))
}

// DeletePinMargin removes the series of a partition this process no longer owns.
func (m *DetectorMetrics) DeletePinMargin(topic string, partition int32) {
	if m == nil {
		return
	}

	m.pinMargin.DeleteLabelValues(topic, partitionLabel(partition))
}
