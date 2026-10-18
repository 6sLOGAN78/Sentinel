package detection

import "github.com/6sLOGAN78/sentinel/internal/contracts"

// Candidate is the immutable first crossing consumed by durable decisions.
// Its accessors return values or copies, never reducer-owned mutable buffers.
type Candidate struct{}

func (Candidate) Source() TopicPartition                 { return TopicPartition{} }
func (Candidate) Offset() int64                          { return 0 }
func (Candidate) RuleOrdinal() int                       { return 0 }
func (Candidate) TriggerOccurredAt() contracts.Timestamp { return contracts.Timestamp{} }
func (Candidate) CanonicalBytes() []byte                 { return nil }
func (Candidate) Fingerprint() string                    { return "" }

// ApplySource supplies the actual Kafka identity alongside the ordered offset.
func (s *PartitionState) ApplySource(_ TopicPartition, offset int64, event *contracts.Event) (Outcome, error) {
	return s.Apply(offset, event)
}
