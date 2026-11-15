// Command detector-recover is the offline, audited recovery for a detector
// group whose committed checkpoints fell below the retained log start. It
// re-pins the SAME group through the Kafka admin API and never joins the
// group, writes detection_partition_fences, deletes groups or deletes records.
// Dry-run is the default; --apply requires the dry-run plan_digest, an
// operator and a reason, and writes a PostgreSQL audit row per partition.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/database"
	"github.com/6sLOGAN78/sentinel/internal/detection"
	"github.com/6sLOGAN78/sentinel/internal/storage/postgres"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2

	maxOperatorBytes = 128
	maxReasonBytes   = 1024

	kafkaCallTimeout = 10 * time.Second
	auditTimeout     = 10 * time.Second
	connectTimeout   = 15 * time.Second
	groupStateDead   = "Dead"
	digestPattern    = `^[0-9a-f]{64}$`
)

// Fixed, secret-free refusal and failure messages.
var (
	errDigestMismatch = errors.New("plan digest mismatch; rerun the dry-run and acknowledge its plan_digest")
	errActiveMembers  = errors.New("detector group has active members; stop every detector replica first")
	errMissingGroup   = errors.New("group metadata missing while PG fences exist")
	errKafka          = errors.New("kafka group state unavailable")
	errPlan           = errors.New("recovery plan invalid; the detector restore error still applies")
	errAudit          = errors.New("recovery audit failed; nothing was committed to Kafka")
	errStillPending   = errors.New("offset commit not verified; audit rows remain pending, rerun the same apply")
	errApplied        = errors.New("offsets committed and verified but audit rows remain pending; rerun the same apply")
	errOutput         = errors.New("plan output failed")
	errGroupChanged   = errors.New("detector group metadata changed during apply")
)

type usageError struct{ msg string }

func (e usageError) Error() string {
	return "usage: detector-recover [--apply --acknowledge-loss=<plan_digest> --operator=<name> " +
		"--reason=<text>]: " + e.msg
}

type options struct {
	apply                    bool
	digest, operator, reason string
}

type groupState struct {
	exists  bool
	members int
}

type kafkaPort interface {
	Group(ctx context.Context) (groupState, error)
	Bounds(ctx context.Context) ([]detection.PartitionBounds, error)
	Commit(ctx context.Context, entries []detection.RecoveryEntry) error
	Committed(ctx context.Context) (map[int32]detection.Checkpoint, error)
}

type auditPort interface {
	CountPartitionFences(ctx context.Context, group, topic string) (int64, error)
	RecordPending(ctx context.Context, plan detection.RecoveryPlan, operator, reason string) error
	MarkApplied(ctx context.Context, group, digest string) error
}

type planOutput struct {
	Group             string                    `json:"group"`
	Topic             string                    `json:"topic"`
	ConfigFingerprint string                    `json:"config_fingerprint"`
	Entries           []detection.RecoveryEntry `json:"entries"`
	PlanDigest        string                    `json:"plan_digest,omitempty"`
	NothingToRecover  bool                      `json:"nothing_to_recover"`
	Applied           bool                      `json:"applied"`
}

func main() {
	os.Exit(command(os.Args[1:]))
}

func command(args []string) int {
	opts, err := parseFlags(args)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r, closeAll, err := open(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	defer closeAll()
	if err = r.run(ctx, opts); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
	}
	return exitCodeFor(err)
}

func exitCodeFor(err error) int {
	var usage usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &usage):
		return exitUsage
	default:
		return exitFailure
	}
}

func parseFlags(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("detector-recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.apply, "apply", false, "apply the acknowledged plan")
	fs.StringVar(&opts.digest, "acknowledge-loss", "", "plan_digest printed by the dry-run")
	fs.StringVar(&opts.operator, "operator", "", "operator identity recorded in the audit")
	fs.StringVar(&opts.reason, "reason", "", "reason recorded in the audit")
	if fs.Parse(args) != nil {
		return options{}, usageError{"unknown or malformed flag"}
	}
	if fs.NArg() != 0 {
		return options{}, usageError{"unexpected positional arguments"}
	}
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !opts.apply {
		if set["acknowledge-loss"] || set["operator"] || set["reason"] {
			return options{}, usageError{"acknowledgment flags require --apply"}
		}
		return opts, nil
	}
	if !regexp.MustCompile(digestPattern).MatchString(opts.digest) {
		return options{}, usageError{"--apply requires --acknowledge-loss=<64 lowercase hex plan_digest>"}
	}
	if !validText(opts.operator, maxOperatorBytes, false) {
		return options{}, usageError{"--apply requires --operator (1..128 bytes, no control characters)"}
	}
	if !validText(opts.reason, maxReasonBytes, true) {
		return options{}, usageError{"--apply requires --reason (1..1024 bytes, not blank)"}
	}
	return opts, nil
}

func validText(value string, limit int, allowLayout bool) bool {
	if strings.TrimSpace(value) == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && (!allowLayout || (r != '\n' && r != '\t')) {
			return false
		}
	}
	return true
}

type recovery struct {
	kafka        kafkaPort
	audit        auditPort
	rules        *detection.Rules
	group, topic string
	out          io.Writer
	log          zerolog.Logger
}

func (r *recovery) run(ctx context.Context, opts options) error {
	group, err := r.kafka.Group(ctx)
	if err != nil {
		return errKafka
	}
	if !group.exists {
		return r.missingMetadata(ctx)
	}
	bounds, err := r.kafka.Bounds(ctx)
	if err != nil {
		return errKafka
	}
	if !anyCommitted(bounds) {
		return r.missingMetadata(ctx)
	}
	plan, err := detection.PlanHistoryRecovery(r.rules, r.group, bounds)
	if err != nil {
		return errPlan
	}
	if len(plan.Entries) == 0 {
		return r.nothingToRecover()
	}
	digest := plan.Digest()
	if !opts.apply {
		return r.print(plan, digest, false)
	}
	if digest != opts.digest {
		return errDigestMismatch
	}
	if err = r.requireStopped(ctx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return errAudit
	}
	return r.apply(ctx, plan, digest, opts)
}

// apply runs after the acknowledgment checks. Once rows are pending, signals
// no longer interrupt the bounded steps; any failure is completed by rerunning
// the same acknowledged apply, which the unique audit key makes idempotent.
func (r *recovery) apply(ctx context.Context, plan detection.RecoveryPlan, digest string, opts options) error {
	if r.audit.RecordPending(ctx, plan, opts.operator, opts.reason) != nil {
		return errAudit
	}
	ctx = context.WithoutCancel(ctx)
	if err := r.requireStopped(ctx); err != nil {
		return fmt.Errorf("%w; audit rows remain pending", err)
	}
	if r.kafka.Commit(ctx, plan.Entries) != nil {
		return errStillPending
	}
	committed, err := r.kafka.Committed(ctx)
	if err != nil {
		return errStillPending
	}
	for _, e := range plan.Entries {
		if got, ok := committed[e.Partition]; !ok || got != e.Repinned {
			return errStillPending
		}
	}
	if r.audit.MarkApplied(ctx, plan.Group, digest) != nil {
		return errApplied
	}
	var neverEvaluated int64
	discarded := 0
	for _, e := range plan.Entries {
		neverEvaluated += e.NeverEvaluatedTo - e.NeverEvaluatedFrom
		if e.OpenWindowsDiscarded {
			discarded++
		}
	}
	r.log.Info().Str("group", plan.Group).Int("partitions", len(plan.Entries)).
		Int64("never_evaluated_total", neverEvaluated).Int("open_windows_discarded_count", discarded).
		Str("plan_digest", digest).Msg("detector history recovery applied")
	return r.print(plan, digest, true)
}

func (r *recovery) requireStopped(ctx context.Context) error {
	group, err := r.kafka.Group(ctx)
	if err != nil {
		return errKafka
	}
	if !group.exists {
		return errGroupChanged
	}
	if group.members != 0 {
		return errActiveMembers
	}
	return nil
}

func anyCommitted(bounds []detection.PartitionBounds) bool {
	for _, b := range bounds {
		if b.HasCommitted {
			return true
		}
	}
	return false
}

// missingMetadata separates a never-started group from a group whose Kafka
// metadata was lost (for example after offsets retention). Fences are only read.
func (r *recovery) missingMetadata(ctx context.Context) error {
	fences, err := r.audit.CountPartitionFences(ctx, r.group, r.topic)
	if err != nil {
		return errAudit
	}
	if fences > 0 {
		return errMissingGroup
	}
	return r.nothingToRecover()
}

func (r *recovery) nothingToRecover() error {
	_, _ = fmt.Fprintln(os.Stderr, "nothing to recover")
	return r.print(detection.RecoveryPlan{Group: r.group, ConfigFingerprint: r.rules.Fingerprint()}, "", false)
}

func (r *recovery) print(plan detection.RecoveryPlan, digest string, applied bool) error {
	entries := plan.Entries
	if entries == nil {
		entries = []detection.RecoveryEntry{}
	}
	out := planOutput{Group: plan.Group, Topic: r.topic, ConfigFingerprint: plan.ConfigFingerprint, Entries: entries,
		PlanDigest: digest, NothingToRecover: len(entries) == 0, Applied: applied}
	body, err := json.Marshal(out)
	if err != nil {
		return errOutput
	}
	if _, err = r.out.Write(append(body, '\n')); err != nil {
		return errOutput
	}
	return nil
}

func open(ctx context.Context) (*recovery, func(), error) {
	cfg, err := config.Load(config.ServiceDetector)
	if err != nil {
		return nil, nil, errors.New("detector configuration invalid")
	}
	rules, err := loadRules(cfg.Detector.RulesPath)
	if err != nil {
		return nil, nil, errors.New("detector rules invalid")
	}
	brokers, err := config.ParseBrokers(cfg.Kafka.Brokers)
	if err != nil {
		return nil, nil, errors.New("kafka configuration invalid")
	}
	// Admin-only client: no consumer group option, so it never becomes a member.
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, nil, errKafka
	}
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	db, err := database.New(connectCtx, cfg.Database, zerolog.Nop())
	if err != nil {
		client.Close()
		return nil, nil, errors.New("postgres unavailable")
	}
	r := &recovery{
		kafka: &kafkaAdmin{admin: kadm.NewClient(client), group: cfg.Kafka.DetectorGroup, topic: cfg.Kafka.EventsTopic},
		audit: postgres.NewDetectionRecoveryStore(db.Pool, auditTimeout), rules: rules,
		group: cfg.Kafka.DetectorGroup, topic: cfg.Kafka.EventsTopic, out: os.Stdout,
		log: zerolog.New(os.Stderr).With().Timestamp().Str("tool", "detector-recover").Logger(),
	}
	return r, func() { db.Pool.Close(); client.Close() }, nil
}

func loadRules(path string) (*detection.Rules, error) {
	//nolint:gosec // Operator-owned configured path; LoadRules caps and strictly validates its contents.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	rules, err := detection.LoadRules(file)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	return rules, closeErr
}

type kafkaAdmin struct {
	admin        *kadm.Client
	group, topic string
}

func (k *kafkaAdmin) Group(ctx context.Context) (groupState, error) {
	ctx, cancel := context.WithTimeout(ctx, kafkaCallTimeout)
	defer cancel()
	groups, err := k.admin.DescribeGroups(ctx, k.group)
	described, present := groups[k.group]
	if present && errors.Is(described.Err, kerr.GroupIDNotFound) {
		return groupState{}, nil
	}
	if err != nil || !present || described.Err != nil {
		return groupState{}, errKafka
	}
	if described.State == groupStateDead {
		return groupState{}, nil
	}
	return groupState{exists: true, members: len(described.Members)}, nil
}

func (k *kafkaAdmin) Bounds(ctx context.Context) ([]detection.PartitionBounds, error) {
	ctx, cancel := context.WithTimeout(ctx, kafkaCallTimeout)
	defer cancel()
	starts, err := k.admin.ListStartOffsets(ctx, k.topic)
	if err != nil {
		return nil, errKafka
	}
	ends, err := k.admin.ListEndOffsets(ctx, k.topic)
	if err != nil {
		return nil, errKafka
	}
	offsets, err := k.admin.FetchOffsetsForTopics(ctx, k.group, k.topic)
	if err != nil || len(starts[k.topic]) == 0 || len(starts[k.topic]) != len(ends[k.topic]) {
		return nil, errKafka
	}
	bounds := make([]detection.PartitionBounds, 0, len(starts[k.topic]))
	for partition, start := range starts[k.topic] {
		end, okEnd := ends.Lookup(k.topic, partition)
		o, okOffset := offsets.Lookup(k.topic, partition)
		if !okEnd || !okOffset || start.Err != nil || end.Err != nil || o.Err != nil || o.At < -1 {
			return nil, errKafka
		}
		bounds = append(bounds, detection.PartitionBounds{Topic: k.topic, Partition: partition,
			Committed: o.At, HasCommitted: o.At >= 0, Metadata: o.Metadata, Start: start.Offset, End: end.Offset})
	}
	return bounds, nil
}

func (k *kafkaAdmin) Commit(ctx context.Context, entries []detection.RecoveryEntry) error {
	ctx, cancel := context.WithTimeout(ctx, kafkaCallTimeout)
	defer cancel()
	offsets := make(kadm.Offsets)
	for _, e := range entries {
		if e.Topic != k.topic {
			return errKafka
		}
		if offsets[e.Topic] == nil {
			offsets[e.Topic] = make(map[int32]kadm.Offset)
		}
		offsets[e.Topic][e.Partition] = kadm.Offset{Topic: e.Topic, Partition: e.Partition,
			At: e.Repinned.Offset, LeaderEpoch: -1, Metadata: e.Repinned.Metadata}
	}
	response, err := k.admin.CommitOffsets(ctx, k.group, offsets)
	if err != nil || response.Error() != nil {
		return errKafka
	}
	for _, e := range entries {
		if o, ok := response.Lookup(e.Topic, e.Partition); !ok || o.Err != nil {
			return errKafka
		}
	}
	return nil
}

func (k *kafkaAdmin) Committed(ctx context.Context) (map[int32]detection.Checkpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, kafkaCallTimeout)
	defer cancel()
	offsets, err := k.admin.FetchOffsetsForTopics(ctx, k.group, k.topic)
	if err != nil {
		return nil, errKafka
	}
	out := make(map[int32]detection.Checkpoint, len(offsets[k.topic]))
	for partition, o := range offsets[k.topic] {
		if o.Err != nil {
			return nil, errKafka
		}
		out[partition] = detection.Checkpoint{Offset: o.At, Metadata: o.Metadata}
	}
	return out, nil
}
