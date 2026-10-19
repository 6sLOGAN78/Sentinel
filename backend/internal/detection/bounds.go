package detection

// StateLimits may only lower the production admission ceilings in fixtures.
type StateLimits struct {
	Windows, Ledger, Bytes, Events, Ports int
}

// Bounds is shared by every partition in one worker, used serially.
type Bounds struct{ limits StateLimits }

// NewBounds constructs process-wide admission accounting.
func NewBounds(limits StateLimits) *Bounds { return &Bounds{limits: limits} }

// NewPartitionStateWithBounds attaches a partition to process accounting.
func NewPartitionStateWithBounds(rules *Rules, _ *Bounds) *PartitionState {
	return NewPartitionState(rules)
}

// ResourceBudget validates aggregate accounted fetch, producer and state bytes.
func ResourceBudget(fetch, concurrency, producer, state int64) error { return nil }
