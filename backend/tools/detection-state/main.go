// Command detection-state reads private operator context; it never joins a group.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

const maxProtocolBytes = 64 << 10
const maxSourceKeys = 128
const observerReadBytes = 2 << 20
const scanFetchBytes = 1 << 20
const scanBatchSize = 500
const observerRequestOverhead = 5 * time.Second
const scanFetchWait = 250 * time.Millisecond

// stdin/stdout are a private pipe, not terminal diagnostics. The caller owns
// mode-0600 snapshot/schedule files. No credentials, broker errors or payloads
// enter diagnostics; configuration uses the same environment names as services.
func main() {
	os.Exit(command())
}

func command() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source, err := newKafkaObserver()
	if err == nil {
		defer source.client.Close()
		if len(os.Args) != 1 {
			err = errors.New("unexpected helper arguments")
		} else {
			err = runProtocol(ctx, os.Stdin, os.Stdout, source)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "detection snapshot failed; check private context, history and quiescent producers")
		return 1
	}
	return 0
}

type snapshotRequest struct {
	SchemaVersion      int       `json:"schema_version"`
	ProducersQuiescent bool      `json:"producers_quiescent"`
	SourceKeys         []string  `json:"source_keys"`
	StartedAt          time.Time `json:"started_at"`
	Deadline           time.Time `json:"deadline"`
}

func runProtocol(ctx context.Context, input io.Reader, output io.Writer, source observer) error {
	raw, err := io.ReadAll(io.LimitReader(input, maxProtocolBytes+1))
	if err != nil || len(raw) > maxProtocolBytes {
		return errors.New("private context exceeds bounds")
	}
	if err = validateRequestFields(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request snapshotRequest
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || request.SchemaVersion != 1 ||
		!request.ProducersQuiescent || len(request.SourceKeys) == 0 || len(request.SourceKeys) > maxSourceKeys {
		return errors.New("invalid private snapshot context")
	}
	seen := make(map[string]bool)
	tenantPattern := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	for _, key := range request.SourceKeys {
		tenant, ip, present := strings.Cut(key, "|")
		address, parseErr := netip.ParseAddr(ip)
		if !present || !tenantPattern.MatchString(tenant) || parseErr != nil || address.String() != ip || seen[key] {
			return errors.New("invalid private source key")
		}
		seen[key] = true
	}
	result, err := caughtUp(ctx, source, request.SourceKeys, request.StartedAt, request.Deadline, time.Now, waitContext)
	if err != nil {
		return err
	}
	body, err := json.Marshal(result)
	if err != nil || len(body) > maxProtocolBytes {
		return errors.New("snapshot output exceeds bounds")
	}
	_, err = output.Write(append(body, '\n'))
	return err
}

func validateRequestFields(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("private context must be an object")
	}
	seen := make(map[string]bool)
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return errors.New("ambiguous private context")
		}
		switch name {
		case "schema_version", "producers_quiescent", "source_keys", "started_at", "deadline":
		default:
			return errors.New("unknown private context field")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return errors.New("null private context field")
		}
	}
	if _, err = d.Token(); err != nil || len(seen) != 5 {
		return errors.New("missing private context field")
	}
	return nil
}

func waitContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type kafkaObserver struct {
	client                    *kgo.Client
	admin                     *kadm.Client
	brokers                   []string
	topic, group, fingerprint string
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func newKafkaObserver() (*kafkaObserver, error) {
	brokers, err := config.ParseBrokers(environment("SENTINEL_KAFKA.BROKERS", "127.0.0.1:9092"))
	if err != nil {
		return nil, errors.New("invalid Kafka environment")
	}
	// Operator-owned path, read with the existing bounded strict rules loader.
	file, err := os.Open(environment("SENTINEL_DETECTOR.RULES_PATH", "../config/rules.yaml"))
	if err != nil {
		return nil, errors.New("rules unavailable")
	}
	rules, loadErr := detection.LoadRules(file)
	closeErr := file.Close()
	if loadErr != nil || closeErr != nil {
		return nil, errors.New("invalid snapshot rules")
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.BrokerMaxReadBytes(observerReadBytes),
		kgo.FetchMaxBytes(scanFetchBytes),
		kgo.RequestTimeoutOverhead(observerRequestOverhead))
	if err != nil {
		return nil, errors.New("kafka observer unavailable")
	}
	return &kafkaObserver{client: client, admin: kadm.NewClient(client), brokers: brokers,
		topic: environment("SENTINEL_KAFKA.EVENTS_TOPIC", "security.events.v1"),
		group: environment("SENTINEL_KAFKA.DETECTOR_GROUP", "sentinel-detector"), fingerprint: rules.Fingerprint()}, nil
}

func (k *kafkaObserver) Fingerprint() string { return k.fingerprint }

func (k *kafkaObserver) History(ctx context.Context) (map[int32]history, error) {
	starts, err := k.admin.ListStartOffsets(ctx, k.topic)
	if err != nil {
		return nil, err
	}
	ends, err := k.admin.ListEndOffsets(ctx, k.topic)
	if err != nil {
		return nil, err
	}
	if len(starts[k.topic]) != sourcePartitions || len(ends[k.topic]) != sourcePartitions {
		return nil, errors.New("source requires six fixed partitions")
	}
	out := make(map[int32]history, sourcePartitions)
	for p := range int32(sourcePartitions) {
		start, sok := starts.Lookup(k.topic, p)
		end, eok := ends.Lookup(k.topic, p)
		if !sok || !eok || start.Err != nil || end.Err != nil {
			return nil, errors.New("source history unavailable")
		}
		out[p] = history{start.Offset, end.Offset}
	}
	return out, nil
}

func (k *kafkaObserver) Checkpoints(ctx context.Context) (map[int32]*detection.Checkpoint, error) {
	offsets, err := k.admin.FetchOffsetsForTopics(ctx, k.group, k.topic)
	if err != nil {
		return nil, err
	}
	out := make(map[int32]*detection.Checkpoint, sourcePartitions)
	for p := range int32(sourcePartitions) {
		o, present := offsets.Lookup(k.topic, p)
		if !present || o.Err != nil || o.At < -1 {
			return nil, errors.New("checkpoint response incomplete")
		}
		if o.At >= 0 {
			out[p] = &detection.Checkpoint{Offset: o.At, Metadata: o.Metadata}
		}
	}
	return out, nil
}

func (k *kafkaObserver) Scan(ctx context.Context, p int32, start, end int64, visit func(*kgo.Record) error) error {
	if start == end {
		return nil
	}
	// Direct assignment never joins a group, commits, seeks an existing client
	// or resets a detector offset. No-reset makes concurrent retention terminal.
	reader, err := kgo.NewClient(kgo.SeedBrokers(k.brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{k.topic: {p: kgo.NewOffset().At(start)}}),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()), kgo.FetchMaxBytes(scanFetchBytes),
		kgo.FetchMaxPartitionBytes(scanFetchBytes), kgo.MaxConcurrentFetches(1),
		kgo.BrokerMaxReadBytes(observerReadBytes), kgo.FetchMaxWait(scanFetchWait))
	if err != nil {
		return err
	}
	defer reader.Close()
	for ctx.Err() == nil {
		fetch := reader.PollRecords(ctx, scanBatchSize)
		if err = fetch.Err(); err != nil {
			return err
		}
		for _, record := range fetch.Records() {
			if record.Offset >= end {
				return nil
			}
			if err = visit(record); err != nil {
				return err
			}
			if record.Offset == end-1 {
				return nil
			}
		}
	}
	return ctx.Err()
}
