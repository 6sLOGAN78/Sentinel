package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

const (
	testGroup    = "sentinel-detector"
	testTopic    = "security.events.v1"
	testOperator = "ops-person"
	testReason   = "idle stack retention expired"
)

var errInjected = errors.New("injected")

type fakeKafka struct {
	calls     *[]string
	groups    []groupState // successive Group results; the last one repeats
	groupErr  error
	bounds    []detection.PartitionBounds
	boundsErr error
	commitErr error
	committed map[int32]detection.Checkpoint
	verifyErr error
	commits   int
}

func (f *fakeKafka) Group(context.Context) (groupState, error) {
	*f.calls = append(*f.calls, "group")
	if f.groupErr != nil {
		return groupState{}, f.groupErr
	}
	g := f.groups[0]
	if len(f.groups) > 1 {
		f.groups = f.groups[1:]
	}
	return g, nil
}

func (f *fakeKafka) Bounds(context.Context) ([]detection.PartitionBounds, error) {
	*f.calls = append(*f.calls, "bounds")
	return f.bounds, f.boundsErr
}

func (f *fakeKafka) Commit(_ context.Context, entries []detection.RecoveryEntry) error {
	*f.calls = append(*f.calls, "commit")
	if f.commitErr != nil {
		return f.commitErr
	}
	f.commits++
	f.committed = make(map[int32]detection.Checkpoint)
	for _, e := range entries {
		f.committed[e.Partition] = e.Repinned
	}
	return nil
}

func (f *fakeKafka) Committed(context.Context) (map[int32]detection.Checkpoint, error) {
	*f.calls = append(*f.calls, "committed")
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return f.committed, nil
}

type fakeAudit struct {
	calls      *[]string
	fences     int64
	pendingErr error
	appliedErr error
	pending    map[string]int
	applied    map[string]bool
}

func (f *fakeAudit) CountPartitionFences(_ context.Context, group, topic string) (int64, error) {
	*f.calls = append(*f.calls, "fences")
	if group != testGroup || topic != testTopic {
		return 0, errInjected
	}
	return f.fences, nil
}

func (f *fakeAudit) RecordPending(_ context.Context, plan detection.RecoveryPlan, operator, reason string) error {
	*f.calls = append(*f.calls, "pending")
	if f.pendingErr != nil {
		return f.pendingErr
	}
	if operator != testOperator || reason != testReason {
		return errInjected
	}
	if f.pending == nil {
		f.pending = make(map[string]int)
	}
	f.pending[plan.Digest()] = len(plan.Entries) // idempotent for the same digest
	return nil
}

func (f *fakeAudit) MarkApplied(_ context.Context, group, digest string) error {
	*f.calls = append(*f.calls, "applied")
	if f.appliedErr != nil {
		return f.appliedErr
	}
	if group != testGroup || f.pending[digest] == 0 {
		return errInjected
	}
	if f.applied == nil {
		f.applied = make(map[string]bool)
	}
	f.applied[digest] = true
	return nil
}

func testRules(t *testing.T) *detection.Rules {
	t.Helper()
	file, err := os.Open("../../../config/rules.yaml")
	require.NoError(t, err)
	defer file.Close()
	rules, err := detection.LoadRules(file)
	require.NoError(t, err)
	return rules
}

// expiredBounds returns a real acknowledged undrained checkpoint pinned at 0
// with processed_next=1, below a retained log [5, 9).
func expiredBounds(t *testing.T, rules *detection.Rules) []detection.PartitionBounds {
	t.Helper()
	state := detection.NewPartitionState(rules)
	ip, principal := "192.0.2.9", "recover-user"
	e := &contracts.Event{SchemaVersion: contracts.EventSchemaVersion, EventID: "00000000-0000-4000-8000-000000000001",
		TenantID: "demo", OccurredAt: contracts.NewTimestamp(time.Date(2026, 11, 1, 0, 0, 1, 0, time.UTC)),
		Source: "unit", EventType: "auth.failure", SourceIP: &ip, Principal: &principal, Attributes: map[string]any{}}
	_, err := state.Apply(0, e)
	require.NoError(t, err)
	require.NoError(t, state.Acknowledge(0))
	cp, err := state.Checkpoint()
	require.NoError(t, err)
	return []detection.PartitionBounds{
		{Topic: testTopic, Partition: 0, Committed: cp.Offset, HasCommitted: true, Metadata: cp.Metadata, Start: 5, End: 9},
		{Topic: testTopic, Partition: 1, Committed: -1, Start: 0, End: 0},
	}
}

type harness struct {
	calls  []string
	kafka  *fakeKafka
	audit  *fakeAudit
	stdout bytes.Buffer
	logs   bytes.Buffer
	rules  *detection.Rules
	digest string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{rules: testRules(t)}
	h.kafka = &fakeKafka{calls: &h.calls, groups: []groupState{{exists: true}}, bounds: expiredBounds(t, h.rules)}
	h.audit = &fakeAudit{calls: &h.calls}
	plan, err := detection.PlanHistoryRecovery(h.rules, testGroup, h.kafka.bounds)
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	h.digest = plan.Digest()
	return h
}

func (h *harness) run(t *testing.T, opts options) error {
	t.Helper()
	h.calls, h.stdout, h.logs = nil, bytes.Buffer{}, bytes.Buffer{}
	r := &recovery{kafka: h.kafka, audit: h.audit, rules: h.rules, group: testGroup, topic: testTopic,
		out: &h.stdout, log: zerolog.New(&h.logs)}
	return r.run(t.Context(), opts)
}

func (h *harness) applyOptions() options {
	return options{apply: true, digest: h.digest, operator: testOperator, reason: testReason}
}

func (h *harness) output(t *testing.T) planOutput {
	t.Helper()
	var out planOutput
	decoder := json.NewDecoder(&h.stdout)
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&out))
	return out
}

func TestParseFlags(t *testing.T) {
	digest := strings.Repeat("a", 64)
	ack := []string{"--apply", "--acknowledge-loss=" + digest, "--operator=" + testOperator, "--reason=" + testReason}
	opts, err := parseFlags(nil)
	require.NoError(t, err)
	require.False(t, opts.apply, "dry-run is the default")
	opts, err = parseFlags(ack)
	require.NoError(t, err)
	require.Equal(t, options{apply: true, digest: digest, operator: testOperator, reason: testReason}, opts)
	for name, tc := range map[string]struct {
		args   []string
		phrase string
	}{
		"apply without acknowledgment": {[]string{"--apply", "--operator=a", "--reason=b"}, "--acknowledge-loss"},
		"apply without operator":       {[]string{"--apply", "--acknowledge-loss=" + digest, "--reason=b"}, "--operator"},
		"apply without reason":         {[]string{"--apply", "--acknowledge-loss=" + digest, "--operator=a"}, "--reason"},
		"empty reason":                 {append(ack[:3:3], "--reason="), "--reason"},
		"blank reason":                 {append(ack[:3:3], "--reason=   "), "--reason"},
		"empty operator": {[]string{"--apply", "--acknowledge-loss=" + digest,
			"--operator=", "--reason=b"}, "--operator"},
		"long operator": {[]string{"--apply", "--acknowledge-loss=" + digest,
			"--operator=" + strings.Repeat("o", 129), "--reason=b"}, "--operator"},
		"control operator": {[]string{"--apply", "--acknowledge-loss=" + digest,
			"--operator=a\nb", "--reason=b"}, "--operator"},
		"long reason": {append(ack[:3:3], "--reason="+strings.Repeat("r", 1025)), "--reason"},
		"short digest": {[]string{"--apply", "--acknowledge-loss=abc",
			"--operator=a", "--reason=b"}, "--acknowledge-loss"},
		"uppercase digest": {[]string{"--apply", "--acknowledge-loss=" + strings.Repeat("A", 64),
			"--operator=a", "--reason=b"}, "--acknowledge-loss"},
		"unknown flag":         {[]string{"--force"}, "unknown or malformed flag"},
		"positional":           {[]string{"extra"}, "unexpected positional arguments"},
		"ack without apply":    {[]string{"--acknowledge-loss=" + digest}, "require --apply"},
		"reason without apply": {[]string{"--reason=x"}, "require --apply"},
	} {
		t.Run(name, func(t *testing.T) {
			_, parseErr := parseFlags(tc.args)
			require.Error(t, parseErr)
			require.Equal(t, exitUsage, exitCodeFor(parseErr))
			require.Contains(t, parseErr.Error(), tc.phrase)
		})
	}
	require.LessOrEqual(t, len(strings.Repeat("o", 128)), maxOperatorBytes)
	_, err = parseFlags(append(ack[:3:3], "--reason="+strings.Repeat("r", 1024)))
	require.NoError(t, err, "1024-byte reason is accepted")
}

func TestDryRun(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.run(t, options{}))
	require.Equal(t, []string{"group", "bounds"}, h.calls, "dry-run writes nothing")
	out := h.output(t)
	require.Equal(t, testGroup, out.Group)
	require.Equal(t, testTopic, out.Topic)
	require.Equal(t, h.digest, out.PlanDigest)
	require.Equal(t, h.rules.Fingerprint(), out.ConfigFingerprint)
	require.Len(t, out.Entries, 1)
	require.Equal(t, int64(1), out.Entries[0].NeverEvaluatedFrom)
	require.Equal(t, int64(5), out.Entries[0].NeverEvaluatedTo)
	require.True(t, out.Entries[0].OpenWindowsDiscarded)
	require.False(t, out.Applied)
	require.False(t, out.NothingToRecover)
	require.Zero(t, h.kafka.commits)
	require.Empty(t, h.audit.pending)
}

func TestApplyOrdering(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.run(t, h.applyOptions()))
	require.Equal(t, []string{"group", "bounds", "group", "pending", "group", "commit", "committed", "applied"}, h.calls)
	out := h.output(t)
	require.True(t, out.Applied)
	require.Equal(t, h.digest, out.PlanDigest)
	require.True(t, h.audit.applied[h.digest])
	logs := h.logs.String()
	require.Contains(t, logs, "detector history recovery applied")
	require.Contains(t, logs, h.digest)
	require.NotContains(t, logs, testOperator)
	require.NotContains(t, logs, testReason)
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	members := []groupState{{exists: true, members: 1}}
	for name, tc := range map[string]struct {
		setup  func(*harness)
		calls  []string
		phrase string
	}{
		"members before pending": {func(h *harness) { h.kafka.groups = members },
			[]string{"group", "bounds", "group"}, "detector group has active members"},
		"members before commit": {func(h *harness) {
			h.kafka.groups = []groupState{{exists: true}, {exists: true}, {exists: true, members: 1}}
		}, []string{"group", "bounds", "group", "pending", "group"}, "detector group has active members"},
		"pending fails": {func(h *harness) { h.audit.pendingErr = errInjected },
			[]string{"group", "bounds", "group", "pending"}, "audit"},
		"commit fails": {func(h *harness) { h.kafka.commitErr = errInjected },
			[]string{"group", "bounds", "group", "pending", "group", "commit"}, "remain pending"},
		"verify fails": {func(h *harness) { h.kafka.verifyErr = errInjected },
			[]string{"group", "bounds", "group", "pending", "group", "commit", "committed"}, "remain pending"},
		"mark applied fails": {func(h *harness) { h.audit.appliedErr = errInjected },
			[]string{"group", "bounds", "group", "pending", "group", "commit", "committed", "applied"}, "audit"},
		"group unavailable": {func(h *harness) { h.kafka.groupErr = errInjected }, []string{"group"}, "kafka"},
		"bounds unavailable": {func(h *harness) { h.kafka.boundsErr = errInjected },
			[]string{"group", "bounds"}, "kafka"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			tc.setup(h)
			err := h.run(t, h.applyOptions())
			require.Error(t, err)
			require.Equal(t, exitFailure, exitCodeFor(err))
			require.Contains(t, err.Error(), tc.phrase)
			require.Equal(t, tc.calls, h.calls)
			require.Empty(t, h.stdout.String(), "failures print no plan")
			require.False(t, h.audit.applied[h.digest])
		})
	}
	t.Run("verify mismatch keeps rows pending", func(t *testing.T) {
		h := newHarness(t)
		r := &recovery{kafka: &mismatchKafka{fakeKafka: h.kafka}, audit: h.audit, rules: h.rules, group: testGroup,
			topic: testTopic, out: &h.stdout, log: zerolog.New(&h.logs)}
		err := r.run(t.Context(), h.applyOptions())
		require.ErrorContains(t, err, "remain pending")
		require.Equal(t, 1, h.audit.pending[h.digest])
		require.False(t, h.audit.applied[h.digest])
	})
}

// mismatchKafka reports committed offsets that differ from the commit.
type mismatchKafka struct{ *fakeKafka }

func (m *mismatchKafka) Committed(context.Context) (map[int32]detection.Checkpoint, error) {
	*m.calls = append(*m.calls, "committed")
	return map[int32]detection.Checkpoint{0: {Offset: 4, Metadata: "{}"}}, nil
}

func TestCommitFailureRerunCompletes(t *testing.T) {
	h := newHarness(t)
	h.kafka.commitErr = errInjected
	require.ErrorContains(t, h.run(t, h.applyOptions()), "remain pending")
	require.Equal(t, 1, h.audit.pending[h.digest])
	h.kafka.commitErr = nil
	require.NoError(t, h.run(t, h.applyOptions()), "same digest reruns idempotently")
	require.True(t, h.audit.applied[h.digest])
	require.Equal(t, 1, h.kafka.commits)
}

func TestDigestMismatchRefusesBeforeWrites(t *testing.T) {
	h := newHarness(t)
	opts := h.applyOptions()
	opts.digest = strings.Repeat("0", 64)
	err := h.run(t, opts)
	require.ErrorContains(t, err, "plan digest mismatch")
	require.Equal(t, exitFailure, exitCodeFor(err))
	require.Equal(t, []string{"group", "bounds"}, h.calls)
	require.Empty(t, h.audit.pending)
	require.Zero(t, h.kafka.commits)
}

func TestNothingToRecover(t *testing.T) {
	for _, apply := range []bool{false, true} {
		h := newHarness(t)
		h.kafka.bounds[0].Start = 0 // committed checkpoint is retained
		opts := options{}
		if apply {
			opts = h.applyOptions()
		}
		require.NoError(t, h.run(t, opts))
		require.Equal(t, []string{"group", "bounds"}, h.calls)
		require.JSONEq(t, `{"group":"`+testGroup+`","topic":"`+testTopic+`","config_fingerprint":"`+
			h.rules.Fingerprint()+`","entries":[],"nothing_to_recover":true,"applied":false}`, h.stdout.String())
		require.Empty(t, h.audit.pending)
		require.Zero(t, h.kafka.commits)
	}
}

func TestMissingGroupMetadata(t *testing.T) {
	for name, tc := range map[string]func(*harness){
		"group absent": func(h *harness) { h.kafka.groups = []groupState{{exists: false}} },
		"no committed offsets": func(h *harness) {
			h.kafka.bounds = []detection.PartitionBounds{{Topic: testTopic, Partition: 0, Committed: -1, Start: 5, End: 9}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, apply := range []bool{false, true} {
				h := newHarness(t)
				tc(h)
				h.audit.fences = 6
				opts := options{}
				if apply {
					opts = h.applyOptions()
				}
				err := h.run(t, opts)
				require.ErrorContains(t, err, "group metadata missing while PG fences exist")
				require.Equal(t, exitFailure, exitCodeFor(err))
				require.NotContains(t, h.calls, "pending")
				require.NotContains(t, h.calls, "commit")
				require.Contains(t, h.calls, "fences")
				require.Empty(t, h.stdout.String())

				h.audit.fences = 0
				require.NoError(t, h.run(t, opts), "a never-started group has nothing to recover")
				require.True(t, h.output(t).NothingToRecover)
			}
		})
	}
}

func TestExitCodes(t *testing.T) {
	require.Equal(t, exitOK, exitCodeFor(nil))
	require.Equal(t, exitFailure, exitCodeFor(errInjected))
	_, err := parseFlags([]string{"--nope"})
	require.Equal(t, exitUsage, exitCodeFor(err))
}
