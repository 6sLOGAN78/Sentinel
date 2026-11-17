// Package eventwriter implements bounded commit-after-insert event persistence.
package eventwriter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/kafkax"
	"github.com/6sLOGAN78/sentinel/internal/retry"
	"github.com/6sLOGAN78/sentinel/internal/telemetry"
)

// maxStoreLinks caps the sampled record contexts linked from one store span,
// bounding span size however large the ClickHouse batch is.
const maxStoreLinks = 32

// Consumer is the manual offset lifecycle required by the writer.
type Consumer interface {
	PollRecords(context.Context, int) ([]*kgo.Record, error)
	CommitRecords(context.Context, ...*kgo.Record) error
	AllowRebalance()
}

// EventStore durably inserts canonical events. Each call must prepare a fresh
// native batch so ambiguous failures can be retried safely.
type EventStore interface {
	InsertBatch(context.Context, []*contracts.Event) error
}

// QuarantinePublisher durably acknowledges one poison record before its source
// offset is eligible to advance.
type QuarantinePublisher interface {
	Publish(context.Context, *kgo.Record, kafkax.DLQReason) error
}

// Writer owns one bounded poll/decode/insert/commit loop.
type Writer struct {
	consumer Consumer
	store    EventStore
	dlq      QuarantinePublisher
	settings config.WriterConfig
	limits   contracts.Limits
	log      zerolog.Logger
	wait     retry.Waiter
	metrics  *telemetry.ConsumerMetrics
	tracer   trace.Tracer
	prop     propagation.TextMapPropagator
}

// New creates a writer from explicit dependencies and validated settings.
func New(
	consumer Consumer,
	store EventStore,
	dlq QuarantinePublisher,
	settings config.WriterConfig,
	limits contracts.Limits,
	log zerolog.Logger,
) *Writer {
	return &Writer{
		consumer: consumer, store: store, dlq: dlq, settings: settings, limits: limits, log: log,
		tracer: noop.NewTracerProvider().Tracer("sentinel/eventwriter"), prop: propagation.TraceContext{},
	}
}

// WithTracing makes each ClickHouse batch that holds at least one sampled
// record start one "store" span linked to at most maxStoreLinks sampled
// record contexts. Batches without a sampled record start no span.
func (w *Writer) WithTracing(tp trace.TracerProvider, prop propagation.TextMapPropagator) *Writer {
	w.tracer = tp.Tracer("sentinel/eventwriter")
	w.prop = prop
	return w
}

// WithRetryWaiter replaces the full-jitter retry wait before Run, chiefly so
// tests can observe ceilings without sleeping. Nil restores retry.Wait.
func (w *Writer) WithRetryWaiter(waiter retry.Waiter) *Writer {
	w.wait = waiter
	return w
}

// WithMetrics observes every ClickHouse insert attempt, retries included.
// Nil disables observation.
func (w *Writer) WithMetrics(metrics *telemetry.ConsumerMetrics) *Writer {
	w.metrics = metrics
	return w
}

func (w *Writer) waitRetry(ctx context.Context, ceiling time.Duration) error {
	if w.wait != nil {
		return w.wait(ctx, ceiling)
	}
	return retry.Wait(ctx, ceiling)
}

func (w *Writer) retryPolicy() retry.Policy {
	return retry.Policy{
		Min: w.settings.RetryMinBackoff, Max: w.settings.RetryMaxBackoff, Attempts: w.settings.MaxAttempts,
	}
}

// Run polls no more than one in-memory batch, stops polling during storage
// retries, and commits only partition-contiguous durable effect prefixes.
func (w *Writer) Run(ctx context.Context) error {
	if _, err := config.ComputeWriterBudgets(w.settings); err != nil {
		return err
	}
	return w.run(ctx)
}

func (w *Writer) run(ctx context.Context) error {
	records := make([]*kgo.Record, 0, w.settings.BatchSize)
	var flushAt time.Time

	for {
		if ctx.Err() != nil {
			return w.flushOnShutdown(records)
		}
		if len(records) >= w.settings.BatchSize {
			if err := w.processOwned(ctx, records); err != nil {
				return err
			}
			records = records[:0]
			flushAt = time.Time{}

			continue
		}

		result, err := w.poll(ctx, len(records), flushAt)
		if err != nil {
			w.consumer.AllowRebalance()
			return err
		}
		if result.shutdown {
			return w.flushOnShutdown(records)
		}
		if result.flush {
			if err = w.processOwned(ctx, records); err != nil {
				return err
			}
			records = records[:0]
			flushAt = time.Time{}
			continue
		}
		if len(result.records) > 0 && len(records) == 0 {
			flushAt = time.Now().Add(w.settings.FlushInterval)
		}
		records = append(records, result.records...)
	}
}

type pollResult struct {
	records  []*kgo.Record
	flush    bool
	shutdown bool
}

func (w *Writer) poll(ctx context.Context, pending int, flushAt time.Time) (pollResult, error) {
	pollCtx := ctx
	cancel := func() {}
	if pending > 0 {
		pollCtx, cancel = context.WithDeadline(ctx, flushAt)
	}
	defer cancel()

	records, err := w.consumer.PollRecords(pollCtx, w.settings.BatchSize-pending)
	if err != nil {
		return pollError(ctx, pending, err)
	}
	if len(records) > 0 {
		return pollResult{records: records}, nil
	}

	return pollResult{
		flush:    pending > 0 && !time.Now().Before(flushAt),
		shutdown: ctx.Err() != nil,
	}, nil
}

func pollError(ctx context.Context, pending int, err error) (pollResult, error) {
	switch {
	case ctx.Err() != nil:
		// Only genuine parent cancellation transfers work to shutdown.
		//nolint:nilerr // translated into a bounded shutdown flush by the caller
		return pollResult{shutdown: true}, nil
	case pending > 0 && errors.Is(err, context.DeadlineExceeded):
		// The poll deadline is the wall-clock batch timer, not a consumer failure.
		return pollResult{flush: true}, nil
	default:
		return pollResult{}, err
	}
}

func (w *Writer) processOwned(ctx context.Context, records []*kgo.Record) error {
	err := w.process(ctx, records)
	if err != nil && ctx.Err() != nil {
		return w.flushOnShutdown(records)
	}
	w.consumer.AllowRebalance()

	return err
}

func (w *Writer) flushOnShutdown(records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), w.settings.ShutdownTimeout)
	defer cancel()
	// Release the final owned batch even if the shutdown deadline expires.
	defer w.consumer.AllowRebalance()

	return w.process(shutdownCtx, records)
}

func (w *Writer) process(ctx context.Context, records []*kgo.Record) error {
	if len(records) == 0 {
		return nil
	}
	budgets, err := config.ComputeWriterBudgets(w.settings)
	if err != nil {
		return err
	}
	processingCtx, cancelProcessing := context.WithTimeout(ctx, budgets.Processing)
	defer cancelProcessing()

	partitions, err := groupContiguousRecords(records)
	if err != nil {
		return err
	}
	classified, events := w.classifyPartitions(partitions, len(records))
	if len(events) > 0 {
		insertCtx, cancelInsert := context.WithTimeout(processingCtx, budgets.Retry)
		err = w.insertTraced(insertCtx, classified, sortedPartitionKeys(partitions), events)
		cancelInsert()
		if err != nil {
			return err
		}
		if err = afterInsertBeforeCommit(processingCtx); err != nil {
			return err
		}
	}

	processingDeadline, _ := processingCtx.Deadline()
	effectsDeadline := time.Now().Add(budgets.Retry)
	if latest := processingDeadline.Add(-w.settings.OperationTimeout); latest.Before(effectsDeadline) {
		effectsDeadline = latest
	}
	effectsCtx, cancelEffects := context.WithDeadline(processingCtx, effectsDeadline)
	committable, effectErr := w.durablePrefixes(effectsCtx, classified, sortedPartitionKeys(partitions))
	cancelEffects()
	if len(committable) == 0 {
		return effectErr
	}
	// Effects expiry leaves this independent reservation live; original parent
	// cancellation and the overall processing deadline still bound the commit.
	commitCtx, cancel := context.WithTimeout(processingCtx, w.settings.OperationTimeout)
	defer cancel()
	if err = w.consumer.CommitRecords(commitCtx, committable...); err != nil {
		return fmt.Errorf("commit durable event effects: %w", errors.Join(err, effectErr))
	}

	return effectErr
}

// insertTraced wraps insertWithRetry in one "store" span when the batch holds
// a sampled record. The first sampled context parents the span, so the
// ParentBased sampler keeps it whatever the root ratio, and every collected
// context (the parent included) is a link. Retries become span events.
func (w *Writer) insertTraced(
	ctx context.Context, classified map[topicPartition][]recordEffect, keys []topicPartition,
	events []*contracts.Event,
) error {
	links := w.storeLinks(ctx, classified, keys)
	if len(links) == 0 {
		return w.insertWithRetry(ctx, events)
	}
	storeCtx, span := w.tracer.Start(trace.ContextWithRemoteSpanContext(ctx, links[0].SpanContext), "store",
		trace.WithSpanKind(trace.SpanKindConsumer), trace.WithLinks(links...),
		trace.WithAttributes(attribute.Int("batch.size", len(events)), attribute.Int("batch.partitions", len(keys))))
	defer span.End()
	err := w.insertWithRetry(storeCtx, events)
	if err != nil {
		span.SetStatus(codes.Error, "store failed")
	}
	return err
}

// storeLinks collects up to maxStoreLinks distinct sampled contexts from the
// records being inserted, in partition/offset order.
func (w *Writer) storeLinks(
	ctx context.Context, classified map[topicPartition][]recordEffect, keys []topicPartition,
) []trace.Link {
	links := make([]trace.Link, 0, maxStoreLinks)
	seen := make(map[linkKey]struct{}, maxStoreLinks)
	for _, key := range keys {
		for _, effect := range classified[key] {
			if len(links) == maxStoreLinks {
				break
			}
			if effect.event == nil {
				continue
			}
			sc, sampled := kafkax.SampledRecordContext(ctx, w.prop, effect.record)
			id := linkKey{traceID: sc.TraceID(), spanID: sc.SpanID()}
			if _, dup := seen[id]; !sampled || dup {
				continue
			}
			seen[id] = struct{}{}
			links = append(links, trace.Link{SpanContext: sc})
		}
	}
	return links
}

// linkKey identifies a linked span context; records of one ingest request
// share it and are linked once.
type linkKey struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

func (w *Writer) classifyPartitions(
	partitions map[topicPartition][]*kgo.Record,
	recordCount int,
) (map[topicPartition][]recordEffect, []*contracts.Event) {
	classified := make(map[topicPartition][]recordEffect, len(partitions))
	events := make([]*contracts.Event, 0, recordCount)
	for key, partitionRecords := range partitions {
		effects := make([]recordEffect, 0, len(partitionRecords))
		for _, record := range partitionRecords {
			effect := w.classify(record)
			effects = append(effects, effect)
			if effect.event != nil {
				events = append(events, effect.event)
			}
		}
		classified[key] = effects
	}

	return classified, events
}

func (w *Writer) durablePrefixes(
	ctx context.Context,
	classified map[topicPartition][]recordEffect,
	keys []topicPartition,
) ([]*kgo.Record, error) {
	committable := make([]*kgo.Record, 0, len(keys))
	var effectErr error
	for _, key := range keys {
		var lastDurable *kgo.Record
		for _, effect := range classified[key] {
			if effect.reason != "" {
				if err := ctx.Err(); err != nil {
					effectErr = errors.Join(effectErr, err)
					break
				}
				if err := w.publishWithRetry(ctx, effect.record, effect.reason); err != nil {
					effectErr = errors.Join(effectErr, err)
					break
				}
			}
			lastDurable = effect.record
		}
		if lastDurable != nil {
			committable = append(committable, lastDurable)
		}
	}

	return committable, effectErr
}

type recordEffect struct {
	record *kgo.Record
	event  *contracts.Event
	reason kafkax.DLQReason
}

func (w *Writer) classify(record *kgo.Record) recordEffect {
	effect := recordEffect{record: record}
	event, err := contracts.DecodeEvent(bytes.NewReader(record.Value))
	if err != nil {
		effect.reason = kafkax.DLQReasonDecode

		return effect
	}
	if event.ReceivedAt == nil || event.ReceivedAt.IsZero() {
		effect.reason = kafkax.DLQReasonMissingReceivedAt

		return effect
	}
	if event.OccurredAt.IsZero() {
		effect.reason = kafkax.DLQReasonValidation

		return effect
	}
	// Ingest already checked wall-clock skew. Replay validates the same
	// structure relative to the immutable event timestamp, not today's clock.
	if err = event.Validate(event.OccurredAt.Time, w.limits); err != nil {
		effect.reason = kafkax.DLQReasonValidation

		return effect
	}
	effect.event = event

	return effect
}

func (w *Writer) publishWithRetry(
	ctx context.Context,
	record *kgo.Record,
	reason kafkax.DLQReason,
) error {
	var lastErr error
	policy := w.retryPolicy()
	for attempt := 1; attempt <= policy.Attempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		lastErr = w.dlq.Publish(operationCtx, record, reason)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == policy.Attempts || ctx.Err() != nil {
			break
		}

		w.log.Warn().Int("attempt", attempt).Str("reason", string(reason)).
			Msg("DLQ publish failed; retrying")
		if err := w.waitRetry(ctx, policy.Ceiling(attempt)); err != nil {
			return fmt.Errorf("publish DLQ record at %s: %w", recordLocation(record), errors.Join(lastErr, err))
		}
	}

	return fmt.Errorf(
		"publish DLQ record at %s after %d attempts: %w",
		recordLocation(record), w.settings.MaxAttempts, lastErr,
	)
}

func groupContiguousRecords(records []*kgo.Record) (map[topicPartition][]*kgo.Record, error) {
	partitions := make(map[topicPartition][]*kgo.Record)
	for _, record := range records {
		key := topicPartition{topic: record.Topic, partition: record.Partition}
		partitionRecords := partitions[key]
		if len(partitionRecords) > 0 && record.Offset != partitionRecords[len(partitionRecords)-1].Offset+1 {
			return nil, fmt.Errorf(
				"non-contiguous Kafka batch at %s/%d: offset %d follows %d",
				record.Topic, record.Partition, record.Offset, partitionRecords[len(partitionRecords)-1].Offset,
			)
		}
		partitions[key] = append(partitionRecords, record)
	}

	return partitions, nil
}

func sortedPartitionKeys(partitions map[topicPartition][]*kgo.Record) []topicPartition {
	keys := make([]topicPartition, 0, len(partitions))
	for key := range partitions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].topic == keys[j].topic {
			return keys[i].partition < keys[j].partition
		}

		return keys[i].topic < keys[j].topic
	})

	return keys
}

func recordLocation(record *kgo.Record) string {
	return fmt.Sprintf("%s/%d/%d", record.Topic, record.Partition, record.Offset)
}

func (w *Writer) insertWithRetry(ctx context.Context, events []*contracts.Event) error {
	var lastErr error
	policy := w.retryPolicy()
	for attempt := 1; attempt <= policy.Attempts; attempt++ {
		operationCtx, cancel := context.WithTimeout(ctx, w.settings.OperationTimeout)
		started := time.Now()
		lastErr = w.store.InsertBatch(operationCtx, events)
		w.metrics.ObserveWrite(lastErr == nil, time.Since(started))
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == policy.Attempts || ctx.Err() != nil {
			break
		}

		trace.SpanFromContext(ctx).AddEvent("retry", trace.WithAttributes(attribute.Int("attempt", attempt)))
		log := telemetry.SampledTraceLogger(ctx, w.log)
		log.Warn().Int("attempt", attempt).Msg("event insert failed; retrying")
		if err := w.waitRetry(ctx, policy.Ceiling(attempt)); err != nil {
			return fmt.Errorf("insert event batch: %w", errors.Join(lastErr, err))
		}
	}

	return fmt.Errorf("insert event batch after %d attempts: %w", w.settings.MaxAttempts, lastErr)
}

type topicPartition struct {
	topic     string
	partition int32
}
