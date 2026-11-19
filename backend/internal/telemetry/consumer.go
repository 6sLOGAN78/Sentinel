package telemetry

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Result label values for sentinel_clickhouse_write_duration_seconds.
const (
	WriteResultOK    = "ok"
	WriteResultError = "error"
)

const (
	labelGroup     = "group"
	labelResult    = "result"
	labelTopic     = "topic"
	labelPartition = "partition"
	labelRuleID    = "rule_id"
)

// ConsumerMetrics holds the spec 11.2 storage-writer metrics: position-based
// consumer lag with its topic partition count (the coverage denominator),
// ClickHouse write latency and DLQ counts. A nil
// *ConsumerMetrics is a valid no-op so callers need no guards.
type ConsumerMetrics struct {
	lag        *prometheus.GaugeVec
	partitions *prometheus.GaugeVec
	write      *prometheus.HistogramVec
	dlq        *prometheus.CounterVec
}

// NewConsumerMetrics registers the consumer metrics on reg.
func NewConsumerMetrics(reg prometheus.Registerer) *ConsumerMetrics {
	m := &ConsumerMetrics{
		lag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sentinel_kafka_consumer_lag",
			Help: "Broker end offset minus the next offset this process has durably processed.",
		}, []string{labelGroup, labelTopic, labelPartition}),
		// Labels group and topic only (ADR 0004): bounded and already used by lag.
		partitions: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sentinel_kafka_topic_partitions",
			Help: "Partitions of the consumed topic as listed by the broker; " +
				"coverage denominator for sentinel_kafka_consumer_lag.",
		}, []string{labelGroup, labelTopic}),
		write: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "sentinel_clickhouse_write_duration_seconds",
			Help:    "Duration of each ClickHouse insert attempt, by outcome.",
			Buckets: prometheus.ExponentialBuckets(durationBucketStart, durationBucketFactor, durationBucketCount),
		}, []string{labelResult}),
		dlq: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sentinel_dlq_events_total",
			Help: "Records acknowledged by the DLQ topic, by stable reason.",
		}, []string{"reason"}),
	}
	reg.MustRegister(m.lag, m.partitions, m.write, m.dlq)

	return m
}

// SetLag publishes lag for one owned partition.
func (m *ConsumerMetrics) SetLag(group, topic string, partition int32, lag int64) {
	if m == nil {
		return
	}

	m.lag.WithLabelValues(group, topic, partitionLabel(partition)).Set(float64(lag))
}

// DeleteLag removes the series of a partition this process no longer owns so
// sum() across replicas does not double count it.
func (m *ConsumerMetrics) DeleteLag(group, topic string, partition int32) {
	if m == nil {
		return
	}

	m.lag.DeleteLabelValues(group, topic, partitionLabel(partition))
}

// SetTopicPartitions publishes the partition count of the consumed topic,
// independent of how many partitions this process owns, so
// count(lag series) < topic partitions exposes an unreported partition.
func (m *ConsumerMetrics) SetTopicPartitions(group, topic string, n int) {
	if m == nil {
		return
	}

	m.partitions.WithLabelValues(group, topic).Set(float64(n))
}

// ObserveWrite records one ClickHouse insert attempt.
func (m *ConsumerMetrics) ObserveWrite(ok bool, elapsed time.Duration) {
	if m == nil {
		return
	}

	result := WriteResultError
	if ok {
		result = WriteResultOK
	}
	m.write.WithLabelValues(result).Observe(elapsed.Seconds())
}

// IncDLQ counts one acknowledged DLQ record. reason must be one of the fixed
// DLQ reason strings so cardinality stays bounded.
func (m *ConsumerMetrics) IncDLQ(reason string) {
	if m == nil {
		return
	}

	m.dlq.WithLabelValues(reason).Inc()
}

func partitionLabel(partition int32) string {
	return strconv.FormatInt(int64(partition), 10)
}
