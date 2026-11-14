package detection

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// recoveryDigestDomain separates plan digests from every other Sentinel hash.
const recoveryDigestDomain = "sentinel.detector.history-recovery.v1\x00"

// PartitionBounds is one partition's committed checkpoint and retained log range.
// HasCommitted is false when the group holds no committed offset for it.
type PartitionBounds struct {
	Topic        string
	Partition    int32
	Committed    int64
	HasCommitted bool
	Metadata     string
	Start, End   int64
}

// RecoveryEntry describes the acknowledged loss and re-pinned checkpoint for
// one partition whose committed checkpoint fell below the retained log start.
// The never-evaluated range is half-open: [NeverEvaluatedFrom, NeverEvaluatedTo).
type RecoveryEntry struct {
	Topic                string     `json:"topic"`
	Partition            int32      `json:"partition"`
	Committed            int64      `json:"committed"`
	Start                int64      `json:"log_start"`
	End                  int64      `json:"log_end"`
	NeverEvaluatedFrom   int64      `json:"never_evaluated_from"`
	NeverEvaluatedTo     int64      `json:"never_evaluated_to"`
	OpenWindowsDiscarded bool       `json:"open_windows_discarded"`
	Repinned             Checkpoint `json:"repinned"`
}

// RecoveryPlan is the deterministic, operator-acknowledged recovery for a group.
type RecoveryPlan struct {
	Group             string          `json:"group"`
	ConfigFingerprint string          `json:"config_fingerprint"`
	Entries           []RecoveryEntry `json:"entries"`
}

// PlanHistoryRecovery computes the recovery plan for every partition whose
// committed checkpoint is below its retained log start. Partitions without a
// committed offset or with a retained checkpoint produce no entry. Any
// inconsistent input yields an error and never a partial plan. It is pure: no
// clock, no I/O, and it authorizes nothing by itself.
func PlanHistoryRecovery(rules *Rules, group string, parts []PartitionBounds) (RecoveryPlan, error) {
	if rules == nil || group == "" {
		return RecoveryPlan{}, errors.New("history recovery requires rules and a consumer group")
	}
	seen := make(map[TopicPartition]bool, len(parts))
	entries := make([]RecoveryEntry, 0, len(parts))
	for _, p := range parts {
		key := TopicPartition{Topic: p.Topic, Partition: p.Partition}
		if p.Topic == "" || p.Partition < 0 || seen[key] {
			return RecoveryPlan{}, errors.New("invalid or duplicate recovery partition")
		}
		seen[key] = true
		expired, err := partitionExpired(p)
		if err != nil {
			return RecoveryPlan{}, fmt.Errorf("%s[%d]: %w", p.Topic, p.Partition, err)
		}
		if !expired {
			continue
		}
		entry, err := RepinExpiredCheckpoint(rules, Checkpoint{Offset: p.Committed, Metadata: p.Metadata}, p.Start, p.End)
		if err != nil {
			return RecoveryPlan{}, fmt.Errorf("%s[%d]: %w", p.Topic, p.Partition, err)
		}
		entry.Topic, entry.Partition = p.Topic, p.Partition
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b RecoveryEntry) int {
		return cmp.Or(cmp.Compare(a.Topic, b.Topic), cmp.Compare(a.Partition, b.Partition))
	})
	return RecoveryPlan{Group: group, ConfigFingerprint: rules.Fingerprint(), Entries: entries}, nil
}

func partitionExpired(p PartitionBounds) (bool, error) {
	if p.Start < 0 || p.End < p.Start {
		return false, errors.New("invalid retained log range")
	}
	if !p.HasCommitted {
		return false, nil
	}
	if p.Committed > p.End {
		return false, errors.New("committed checkpoint beyond log end")
	}
	return p.Committed < p.Start, nil
}

// RepinExpiredCheckpoint re-pins one expired checkpoint to the first offset the
// detector can still evaluate: max(log start, processed_next). Records already
// evaluated in [start, processed_next) are never replayed with empty state,
// which would contradict immutable decisions. Event-time clocks, effective
// gates, the decision frontier and the rule manifest are preserved; only the
// pin, ProcessedNext and Drained change. Open window state is discarded.
func RepinExpiredCheckpoint(rules *Rules, old Checkpoint, start, end int64) (RecoveryEntry, error) {
	if rules == nil || start < 0 || end < start {
		return RecoveryEntry{}, errors.New("invalid history recovery bounds")
	}
	if old.Offset >= start {
		return RecoveryEntry{}, errors.New("checkpoint history not expired")
	}
	m, err := CheckpointMetadata(old)
	if err != nil {
		return RecoveryEntry{}, fmt.Errorf("expired checkpoint metadata: %w", err)
	}
	if m.ConfigFingerprint != rules.Fingerprint() {
		return RecoveryEntry{}, errors.New("checkpoint configuration differs; use the drained transition runbook")
	}
	if m.ProcessedNext > end {
		return RecoveryEntry{}, errors.New("checkpoint processed beyond log end")
	}
	pin := max(start, m.ProcessedNext)
	entry := RecoveryEntry{
		Committed: old.Offset, Start: start, End: end,
		NeverEvaluatedFrom: m.ProcessedNext, NeverEvaluatedTo: pin, OpenWindowsDiscarded: !m.Drained,
	}
	m.ProcessedNext, m.Drained = pin, true
	raw, err := json.Marshal(m)
	if err != nil {
		return RecoveryEntry{}, fmt.Errorf("encode re-pinned checkpoint: %w", err)
	}
	if len(raw) > MaxMetadataBytes {
		return RecoveryEntry{}, errors.New("re-pinned checkpoint exceeds metadata bounds")
	}
	entry.Repinned = Checkpoint{Offset: pin, Metadata: string(raw)}
	if err = NewPartitionState(rules).Restore(entry.Repinned); err != nil {
		return RecoveryEntry{}, fmt.Errorf("re-pinned checkpoint self-check: %w", err)
	}
	return entry, nil
}

// Digest is the lowercase hex sha256 of a length-prefixed canonical encoding
// of the group, fingerprint and every entry field, including the re-pinned
// checkpoint. Operators acknowledge loss by echoing it back.
func (p RecoveryPlan) Digest() string {
	buf := []byte(recoveryDigestDomain)
	buf = appendDigestString(buf, p.Group)
	buf = appendDigestString(buf, p.ConfigFingerprint)
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(p.Entries)))
	for _, e := range p.Entries {
		buf = appendDigestString(buf, e.Topic)
		buf = binary.BigEndian.AppendUint32(buf, uint32(e.Partition)) //nolint:gosec // bit pattern only, not arithmetic.
		for _, v := range []int64{e.Committed, e.Start, e.End, e.NeverEvaluatedFrom, e.NeverEvaluatedTo, e.Repinned.Offset} {
			buf = binary.BigEndian.AppendUint64(buf, uint64(v)) //nolint:gosec // bit pattern only, not arithmetic.
		}
		buf = binary.BigEndian.AppendUint64(buf, boolDigest(e.OpenWindowsDiscarded))
		buf = appendDigestString(buf, e.Repinned.Metadata)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

func appendDigestString(buf []byte, s string) []byte {
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(s)))
	return append(buf, s...)
}

func boolDigest(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
