package detection

import (
	"errors"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// Candidate is the immutable first crossing consumed by durable decisions.
// Its accessors return values or copies, never reducer-owned mutable buffers.
type Candidate struct {
	source                 TopicPartition
	offset                 int64
	ordinal                int
	trigger                contracts.Timestamp
	canonical, fingerprint string
}

// Source identifies the partition containing the crossing record.
func (c Candidate) Source() TopicPartition { return c.source }

// Offset is the crossing record's Kafka offset.
func (c Candidate) Offset() int64 { return c.offset }

// RuleOrdinal is stable across YAML order: AUTH=0, SCAN=1, FLOOD=2, DENY=3.
// Decisions and checkpoint frontiers use this same source ordering.
func (c Candidate) RuleOrdinal() int { return c.ordinal }

// TriggerOccurredAt is the crossing event time, independent of window end.
func (c Candidate) TriggerOccurredAt() contracts.Timestamp { return c.trigger }

// CanonicalBytes returns a copy of the complete frozen open alert payload.
func (c Candidate) CanonicalBytes() []byte { return []byte(c.canonical) }

// Fingerprint identifies immutable content using the persistence policy.
func (c Candidate) Fingerprint() string { return c.fingerprint }

// ApplySource supplies the actual Kafka identity alongside the ordered offset.
func (s *PartitionState) ApplySource(source TopicPartition, offset int64, event *contracts.Event) (Outcome, error) {
	if source.Topic == "" || source.Partition < 0 {
		return Outcome{}, errors.New("invalid candidate source")
	}
	return s.apply(&source, offset, event)
}
