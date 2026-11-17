package kafkax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// PublishCanonical publishes the exact stored bytes, preserving replay evidence.
func (p *AlertProducer) PublishCanonical(ctx context.Context, canonical []byte) error {
	if len(canonical) == 0 || len(canonical) > 64<<10 {
		return errors.New("canonical alert exceeds size bound")
	}
	alert, err := contracts.DecodeAlert(bytes.NewReader(canonical))
	if err != nil {
		return errors.New("invalid canonical alert")
	}
	if err = alert.Validate(); err != nil {
		return errors.New("invalid canonical alert")
	}
	publishCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	record := &kgo.Record{Topic: p.topic, Key: []byte(alert.TenantID + "|" + alert.AlertID), Value: bytes.Clone(canonical)}
	p.inject(ctx, record)
	if err = p.client.ProduceSync(publishCtx, record).FirstErr(); err != nil {
		return errors.New("publish detector alerts failed")
	}
	return nil
}

const maxAlertsPerRecord = 4

type alertClient interface {
	ProduceSync(context.Context, ...*kgo.Record) kgo.ProduceResults
	Flush(context.Context) error
	Close()
}

// AlertProducer owns a separate bounded all-ISR client. Its readiness check
// reuses the established topic-leader check, including after broker outages.
type AlertProducer struct {
	client     alertClient
	readiness  *Producer
	topic      string
	timeout    time.Duration
	propagator propagation.TextMapPropagator
}

// WithPropagator sets how the caller's span context (the detector's detect
// span) is written into alert record headers. The producer starts no span of
// its own. Without it, W3C trace context is used.
func (p *AlertProducer) WithPropagator(prop propagation.TextMapPropagator) *AlertProducer {
	p.propagator = prop
	return p
}

// inject writes ctx's span context into record only when one is valid, so an
// untraced publish leaves the record headers unchanged.
func (p *AlertProducer) inject(ctx context.Context, record *kgo.Record) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return
	}
	prop := p.propagator
	if prop == nil {
		prop = propagation.TraceContext{}
	}
	prop.Inject(ctx, HeaderCarrier{Record: record})
}

// NewAlertProducer creates the bounded canonical alert publisher.
func NewAlertProducer(ctx context.Context, cfg config.KafkaConfig) (*AlertProducer, error) {
	brokers, err := config.ParseBrokers(cfg.Brokers)
	if err != nil {
		return nil, err
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...), kgo.DefaultProduceTopic(cfg.AlertsTopic), kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.DisableIdempotentWrite(), kgo.RecordDeliveryTimeout(cfg.ProduceTimeout),
		kgo.RecordRetries(cfg.RecordRetries), kgo.MaxBufferedRecords(cfg.MaxBufferedRecords),
		kgo.MaxBufferedBytes(cfg.MaxBufferedBytes),
	)
	if err != nil {
		return nil, err
	}
	probeCfg := cfg
	probeCfg.EventsTopic = cfg.AlertsTopic
	probe, err := NewProducer(ctx, probeCfg)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &AlertProducer{client: client, readiness: probe, topic: cfg.AlertsTopic, timeout: cfg.ProduceTimeout}, nil
}

// Publish waits for all-ISR acknowledgement of canonical tenant-keyed alerts.
func (p *AlertProducer) Publish(ctx context.Context, alerts []*contracts.Alert) error {
	if len(alerts) > maxAlertsPerRecord {
		return errors.New("alert batch exceeds bounds")
	}
	records := make([]*kgo.Record, 0, len(alerts))
	for _, alert := range alerts {
		if err := alert.Validate(); err != nil {
			return errors.New("invalid outgoing alert")
		}
		b, err := json.Marshal(alert)
		if err != nil {
			return err
		}
		if len(b) > 64<<10 {
			return errors.New("alert exceeds size bound")
		}
		record := &kgo.Record{Topic: p.topic, Key: []byte(alert.TenantID + "|" + alert.AlertID), Value: b}
		p.inject(ctx, record)
		records = append(records, record)
	}
	publishCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := p.client.ProduceSync(publishCtx, records...).FirstErr(); err != nil {
		return errors.New("publish detector alerts failed")
	}
	return nil
}

// Ping verifies broker and alert-topic leaders under the caller's deadline.
func (p *AlertProducer) Ping(ctx context.Context) error { return p.readiness.Ping(ctx) }

// Close flushes pending effects and closes both clients.
func (p *AlertProducer) Close(ctx context.Context) error {
	err := p.client.Flush(ctx)
	p.client.Close()
	return errors.Join(err, p.readiness.Close(ctx))
}
