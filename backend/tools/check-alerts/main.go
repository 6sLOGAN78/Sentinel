// Command check-alerts reconciles private current-run Kafka, CH and PG evidence.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	driverch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	storagech "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const (
	maxPrivateBytes  = 1 << 20
	verifyTimeout    = time.Minute
	connectTimeout   = 3 * time.Second
	queryTimeout     = 10 * time.Second
	alertPartitions  = 3
	sourcePartitions = 6
	readBatchSize    = 500
	brokerReadBytes  = 2 << 20
	fetchReadBytes   = 1 << 20
	fetchWait        = 100 * time.Millisecond
)

type manifest struct {
	SchemaVersion  int               `json:"schema_version"`
	Tenant         string            `json:"tenant"`
	SourceKeys     []string          `json:"source_keys"`
	EventIDs       []string          `json:"event_ids"`
	Expected       []string          `json:"expected_alert_bytes"`
	KafkaStart     map[int32]int64   `json:"kafka_start"`
	PreserveIDs    []string          `json:"preserve_event_ids"`
	PreserveAlerts []string          `json:"preserve_alert_bytes"`
	PriorEvents    map[string]string `json:"prior_events"`
	PriorCH        map[string]string `json:"prior_ch"`
	PriorPG        map[string]string `json:"prior_pg"`
}

// verificationFailure carries only a fixed stage name, never driver errors.
type verificationFailure struct{ stage string }

func (e verificationFailure) Error() string { return "alert verification failed: " + e.stage }

type eventQuerier interface {
	Query(context.Context, string, ...any) (driver.Rows, error)
}

func eventSnapshots(_ context.Context, _ eventQuerier, _ string, _ []string) (map[string]string, error) {
	return nil, errors.New("bounded event batch is unavailable")
}

func priorSnapshotsMatch(m manifest, events, ch, pg map[string]string) bool {
	return reflect.DeepEqual(events, m.PriorEvents) && reflect.DeepEqual(ch, m.PriorCH) && reflect.DeepEqual(pg, m.PriorPG)
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() { os.Exit(command()) }

func command() int {
	if len(os.Args) != 4 || os.Args[2] != "--manifest" || (os.Args[1] != "capture" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "alert verification failed: invalid arguments")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()
	m, err := loadManifest(os.Args[3])
	if err == nil {
		err = operate(ctx, os.Args[1], m)
	}
	if err != nil {
		var failure verificationFailure
		if errors.As(err, &failure) {
			fmt.Fprintln(os.Stderr, failure.Error())
		} else {
			fmt.Fprintln(os.Stderr, "alert verification failed: private_context")
		}
		return 1
	}
	return 0
}

func privateBytes(path string) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(abs, "/"), "/")
	for i, name := range parts {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= syscall.O_DIRECTORY
		}
		next, openErr := syscall.Openat(fd, name, flags, 0)
		_ = syscall.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "private-manifest")
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxPrivateBytes {
		return nil, errors.New("invalid private manifest")
	}
	return io.ReadAll(io.LimitReader(f, maxPrivateBytes+1))
}

func uniqueJSON(d *json.Decoder) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, keyErr := d.Token()
			name, isString := key.(string)
			if keyErr != nil || !isString || seen[name] {
				return errors.New("ambiguous JSON")
			}
			seen[name] = true
		}
		if err = uniqueJSON(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func loadManifest(path string) (manifest, error) {
	var m manifest
	raw, err := privateBytes(path)
	if err != nil {
		return m, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return m, errors.New("ambiguous JSON")
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return m, errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || m.SchemaVersion != 1 || m.Tenant != "demo" ||
		len(m.SourceKeys) == 0 || len(m.SourceKeys) > 128 || len(m.EventIDs) > 5000 ||
		len(m.PreserveIDs) > 5000 || len(m.Expected) > 64 || len(m.PreserveAlerts) > 64 {
		return m, errors.New("invalid manifest shape")
	}
	seen := map[string]bool{}
	for _, key := range m.SourceKeys {
		tenant, ip, ok := strings.Cut(key, "|")
		address, e := netip.ParseAddr(ip)
		if !ok || tenant != m.Tenant || e != nil || address.String() != ip || seen[key] {
			return m, errors.New("invalid source keys")
		}
		seen[key] = true
	}
	for _, ids := range [][]string{m.EventIDs, m.PreserveIDs} {
		seen = map[string]bool{}
		for _, id := range ids {
			parsed, e := uuid.Parse(id)
			if e != nil || parsed.String() != id || seen[id] {
				return m, errors.New("invalid acknowledged IDs")
			}
			seen[id] = true
		}
	}
	_, err = expectedSet(m)
	return m, err
}

func expectedSet(m manifest) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range m.Expected {
		a, err := contracts.DecodeAlert(strings.NewReader(raw))
		if err != nil || a.TenantID != m.Tenant || !runEntity(m, a) {
			return nil, errors.New("invalid expected alert")
		}
		canonical, e := json.Marshal(a)
		if e != nil || string(canonical) != raw || out[a.AlertID] != "" {
			return nil, errors.New("noncanonical expected alert")
		}
		out[a.AlertID] = raw
	}
	return out, nil
}

func runEntity(m manifest, a *contracts.Alert) bool {
	ip, _, _ := strings.Cut(a.EntityKey, "|")
	for _, key := range m.SourceKeys {
		_, source, _ := strings.Cut(key, "|")
		if ip == source {
			return true
		}
	}
	return false
}

func reconcile(m manifest, kafka, ch, pg [][]byte) error {
	expected, err := expectedSet(m)
	if err != nil {
		return err
	}
	for _, records := range [][][]byte{kafka, ch, pg} {
		actual := map[string]string{}
		for _, raw := range records {
			a, e := contracts.DecodeAlert(bytes.NewReader(raw))
			if e != nil || a.TenantID != m.Tenant || !runEntity(m, a) ||
				expected[a.AlertID] != string(raw) || actual[a.AlertID] != "" {
				return errors.New("alert set or payload differs")
			}
			actual[a.AlertID] = string(raw)
		}
		if !reflect.DeepEqual(expected, actual) {
			return errors.New("alert set incomplete")
		}
	}
	return nil
}

type stores struct {
	client                *kgo.Client
	admin                 *kadm.Client
	ch                    storagech.Conn
	pg                    *pgxpool.Pool
	brokers               []string
	alerts, source, group string
}

func openStores(ctx context.Context) (*stores, error) {
	s := &stores{brokers: strings.Split(env("SENTINEL_KAFKA.BROKERS", "127.0.0.1:29092"), ","),
		alerts: env("SENTINEL_KAFKA.ALERTS_TOPIC", "security.alerts.v1"),
		source: env("SENTINEL_KAFKA.EVENTS_TOPIC", "security.events.v1"),
		group:  env("SENTINEL_KAFKA.DETECTOR_GROUP", "sentinel-detector")}
	var err error
	s.client, err = kgo.NewClient(kgo.SeedBrokers(s.brokers...), kgo.BrokerMaxReadBytes(brokerReadBytes),
		kgo.FetchMaxBytes(fetchReadBytes),
		kgo.RequestTimeoutOverhead(connectTimeout))
	if err != nil {
		return nil, err
	}
	s.admin = kadm.NewClient(s.client)
	s.ch, err = storagech.Open(ctx, config.ClickHouseConfig{
		NativeAddress: env("SENTINEL_CLICKHOUSE.NATIVE_ADDRESS", "127.0.0.1:19000"),
		User:          env("SENTINEL_CLICKHOUSE.USER", "sentinel"), Password: os.Getenv("SENTINEL_CLICKHOUSE.PASSWORD"),
		Database: env("SENTINEL_CLICKHOUSE.DATABASE", "sentinel")})
	if err != nil {
		s.close()
		return nil, err
	}
	pc, err := pgxpool.ParseConfig("")
	if err != nil {
		s.close()
		return nil, err
	}
	pc.MaxConns = 2
	pc.ConnConfig.ConnectTimeout = connectTimeout
	pc.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	s.pg, err = pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *stores) close() {
	if s.client != nil {
		s.client.Close()
	}
	if s.ch != nil {
		_ = s.ch.Close()
	}
	if s.pg != nil {
		s.pg.Close()
	}
}

func (s *stores) ends(ctx context.Context) (map[int32]int64, error) {
	offsets, err := s.admin.ListEndOffsets(ctx, s.alerts)
	if err != nil {
		return nil, err
	}
	out := map[int32]int64{}
	for p, o := range offsets[s.alerts] {
		if o.Err != nil || o.Offset < 0 {
			return nil, errors.New("alert bounds unavailable")
		}
		out[p] = o.Offset
	}
	if len(out) != alertPartitions {
		return nil, errors.New("expected three alert partitions")
	}
	return out, nil
}

func rangeParts(starts, ends map[int32]int64) (map[int32]kgo.Offset, map[int32]int64, error) {
	parts := map[int32]kgo.Offset{}
	next := map[int32]int64{}
	for p, end := range ends {
		start, ok := starts[p]
		if !ok || start < 0 || end < start || end-start > 10000 {
			return nil, nil, errors.New("invalid current range")
		}
		if start < end {
			parts[p] = kgo.NewOffset().At(start)
			next[p] = start
		}
	}
	if len(starts) != len(ends) {
		return nil, nil, errors.New("incomplete current range")
	}
	return parts, next, nil
}

type rangeScan struct {
	manifest   manifest
	ends, next map[int32]int64
	seen       map[string]string
	records    [][]byte
	total      int
}

func (scan *rangeScan) record(record *kgo.Record) error {
	n, ok := scan.next[record.Partition]
	if !ok || record.Offset >= scan.ends[record.Partition] {
		return nil
	}
	if record.Offset != n {
		return errors.New("alert range gap")
	}
	scan.next[record.Partition]++
	if record.Offset+1 == scan.ends[record.Partition] {
		delete(scan.next, record.Partition)
	}
	scan.total += len(record.Value)
	if scan.total > 16<<20 {
		return errors.New("alert range too large")
	}
	a, err := contracts.DecodeAlert(bytes.NewReader(record.Value))
	if err != nil {
		return err
	}
	if !runEntity(scan.manifest, a) && !expectedID(scan.manifest, a.AlertID) {
		return nil
	}
	if string(record.Key) != a.TenantID+"|"+a.AlertID {
		return errors.New("wrong Kafka key")
	}
	if old := scan.seen[a.AlertID]; old != "" {
		if old != string(record.Value) {
			return errors.New("conflicting Kafka replay")
		}
		return nil
	}
	scan.seen[a.AlertID] = string(record.Value)
	scan.records = append(scan.records, record.Value)
	return nil
}

func (s *stores) kafkaRange(ctx context.Context, m manifest) ([][]byte, error) {
	ends, err := s.ends(ctx)
	if err != nil {
		return nil, err
	}
	parts, next, err := rangeParts(m.KafkaStart, ends)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, nil
	}
	reader, err := kgo.NewClient(kgo.SeedBrokers(s.brokers...),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{s.alerts: parts}),
		kgo.ConsumeResetOffset(kgo.NoResetOffset()), kgo.FetchMaxBytes(fetchReadBytes),
		kgo.FetchMaxPartitionBytes(fetchReadBytes), kgo.MaxConcurrentFetches(1),
		kgo.BrokerMaxReadBytes(brokerReadBytes), kgo.FetchMaxWait(fetchWait))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	scan := rangeScan{manifest: m, ends: ends, next: next, seen: map[string]string{}}
	for len(next) > 0 {
		fetch := reader.PollRecords(ctx, readBatchSize)
		if err = fetch.Err(); err != nil {
			return nil, err
		}
		for _, record := range fetch.Records() {
			if err = scan.record(record); err != nil {
				return nil, err
			}
		}
	}
	return scan.records, nil
}

func expectedID(m manifest, id string) bool {
	for _, raw := range m.Expected {
		var a contracts.Alert
		if json.Unmarshal([]byte(raw), &a) == nil && a.AlertID == id {
			return true
		}
	}
	return false
}

func sources(m manifest) []string {
	out := make([]string, 0, len(m.SourceKeys))
	for _, k := range m.SourceKeys {
		_, ip, _ := strings.Cut(k, "|")
		out = append(out, ip)
	}
	return out
}
func alertIDs(raws []string) []string {
	ids := make([]string, 0, len(raws))
	for _, raw := range raws {
		var a contracts.Alert
		if json.Unmarshal([]byte(raw), &a) == nil {
			ids = append(ids, a.AlertID)
		}
	}
	return ids
}

func (s *stores) chCurrent(ctx context.Context, m manifest) ([][]byte, error) {
	rows, err := s.ch.Query(ctx, `SELECT canonical_payload FROM alerts
		WHERE (tenant_id={tenant:String} AND splitByChar('|',entity_key)[1] IN {sources:Array(String)})
		OR alert_id IN {ids:Array(String)} GROUP BY alert_id,canonical_payload LIMIT 65`,
		driverch.Named("tenant", m.Tenant), driverch.Named("sources", sources(m)),
		driverch.Named("ids", alertIDs(m.Expected)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out [][]byte
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, []byte(raw))
	}
	return out, rows.Err()
}

func (s *stores) pgCurrent(ctx context.Context, m manifest) ([][]byte, error) {
	rows, err := s.pg.Query(ctx, `SELECT alert_id,tenant_id,rule_id,rule_version,severity,status,
		created_at,window_start,window_end,entity_key,summary,event_count,
		immutable_fingerprint,seq,version,assignee,updated_at
		FROM alert_workflows WHERE (tenant_id=$1 AND split_part(entity_key,'|',1)=ANY($2::text[]))
		OR alert_id=ANY($3::text[]) LIMIT 65`, m.Tenant, sources(m), alertIDs(m.Expected))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	expected, err := expectedSet(m)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for rows.Next() {
		var a contracts.Alert
		var fingerprint string
		var seq, version int64
		var assignee *string
		var updated time.Time
		if err = rows.Scan(&a.AlertID, &a.TenantID, &a.RuleID, &a.RuleVersion, &a.Severity, &a.Status,
			&a.CreatedAt.Time, &a.WindowStart.Time, &a.WindowEnd.Time, &a.EntityKey, &a.Summary,
			&a.EventCount, &fingerprint, &seq, &version, &assignee, &updated); err != nil {
			return nil, err
		}
		raw, ok := expected[a.AlertID]
		if !ok || seq < 1 || version != 1 || assignee != nil || !updated.Equal(a.CreatedAt.Time) {
			return nil, errors.New("unexpected workflow")
		}
		e, decodeErr := contracts.DecodeAlert(strings.NewReader(raw))
		if decodeErr != nil {
			return nil, decodeErr
		}
		fp, fpErr := storagech.ImmutableFingerprint(e)
		if fpErr != nil || fingerprint != fp || a.TenantID != e.TenantID || a.RuleID != e.RuleID ||
			a.RuleVersion != e.RuleVersion || a.Severity != e.Severity || a.Status != e.Status ||
			!a.CreatedAt.Equal(e.CreatedAt.Time) || !a.WindowStart.Equal(e.WindowStart.Time) ||
			!a.WindowEnd.Equal(e.WindowEnd.Time) || a.EntityKey != e.EntityKey ||
			a.Summary != e.Summary || a.EventCount != e.EventCount {
			return nil, errors.New("workflow payload differs")
		}
		out = append(out, []byte(raw))
	}
	return out, rows.Err()
}

func (s *stores) prior(ctx context.Context, m manifest) (
	map[string]string, map[string]string, map[string]string, error,
) {
	events := map[string]string{}
	ch := map[string]string{}
	pg := map[string]string{}
	store := storagech.NewEventStore(s.ch)
	for _, id := range m.PreserveIDs {
		event, err := store.Get(ctx, m.Tenant, id)
		if err != nil {
			return nil, nil, nil, err
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return nil, nil, nil, err
		}
		digest := sha256.Sum256(raw)
		events[id] = hex.EncodeToString(digest[:])
	}
	alerts := storagech.NewAlertStore(s.ch)
	for _, raw := range m.PreserveAlerts {
		a, err := contracts.DecodeAlert(strings.NewReader(raw))
		if err != nil || a.TenantID != m.Tenant {
			return nil, nil, nil, errors.New("invalid prior alert")
		}
		stored, getErr := alerts.Get(ctx, m.Tenant, a.AlertID)
		if getErr != nil {
			return nil, nil, nil, getErr
		}
		body, marshalErr := json.Marshal(stored)
		if marshalErr != nil || string(body) != raw {
			return nil, nil, nil, errors.New("prior CH differs")
		}
		ch[a.AlertID] = string(body)
		var row string
		if err = s.pg.QueryRow(ctx, `SELECT to_jsonb(w)::text FROM alert_workflows w
			WHERE tenant_id=$1 AND alert_id=$2`, m.Tenant, a.AlertID).Scan(&row); err != nil {
			return nil, nil, nil, err
		}
		pg[a.AlertID] = row
	}
	return events, ch, pg, nil
}

func (s *stores) drained(ctx context.Context) error {
	ends, err := s.admin.ListEndOffsets(ctx, s.source)
	if err != nil {
		return err
	}
	offsets, err := s.admin.FetchOffsetsForTopics(ctx, s.group, s.source)
	if err != nil {
		return err
	}
	if len(ends[s.source]) != sourcePartitions {
		return errors.New("source partition bounds missing")
	}
	for p, end := range ends[s.source] {
		if end.Err != nil {
			return end.Err
		}
		if end.Offset == 0 {
			continue
		}
		o, ok := offsets.Lookup(s.source, p)
		if !ok || o.Err != nil {
			return errors.New("missing checkpoint")
		}
		md, e := detection.CheckpointMetadata(detection.Checkpoint{Offset: o.At, Metadata: o.Metadata})
		if e != nil || !md.Drained || md.ProcessedNext != end.Offset || o.At != end.Offset {
			return errors.New("source checkpoint not drained")
		}
	}
	return nil
}

func operate(ctx context.Context, mode string, m manifest) error {
	s, err := openStores(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	if mode == "capture" {
		m.KafkaStart, err = s.ends(ctx)
		if err != nil {
			return err
		}
		m.PriorEvents, m.PriorCH, m.PriorPG, err = s.prior(ctx, m)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	if len(m.EventIDs) == 0 {
		return errors.New("unacknowledged run")
	}
	for {
		attempt, cancel := context.WithTimeout(ctx, queryTimeout)
		verifyErr := s.verify(attempt, m)
		cancel()
		if verifyErr == nil {
			return json.NewEncoder(os.Stdout).Encode(map[string]bool{
				"verified": true, "prior_rows_preserved": true, "drained": true})
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return verifyErr
		case <-timer.C:
		}
	}
}

func (s *stores) verify(ctx context.Context, m manifest) error {
	if err := s.drained(ctx); err != nil {
		return verificationFailure{"source_checkpoint"}
	}
	kafka, err := s.kafkaRange(ctx, m)
	if err != nil {
		return verificationFailure{"kafka_range"}
	}
	ch, err := s.chCurrent(ctx, m)
	if err != nil {
		return verificationFailure{"clickhouse_set"}
	}
	pg, err := s.pgCurrent(ctx, m)
	if err != nil {
		return verificationFailure{"workflow_payload"}
	}
	if err = reconcile(m, kafka, ch, pg); err != nil {
		return verificationFailure{"exact_payload_set"}
	}
	events, oldCH, oldPG, err := s.prior(ctx, m)
	if err != nil {
		return verificationFailure{"prior_read"}
	}
	if !reflect.DeepEqual(events, m.PriorEvents) ||
		!reflect.DeepEqual(oldCH, m.PriorCH) || !reflect.DeepEqual(oldPG, m.PriorPG) {
		return verificationFailure{"prior_content"}
	}
	store := storagech.NewEventStore(s.ch)
	for _, id := range m.EventIDs {
		if _, err = store.Get(ctx, m.Tenant, id); err != nil {
			return verificationFailure{"current_event_rows"}
		}
	}
	return nil
}
