package detection

import "errors"

// TransitionCheckpoint validates a drained handover against the saved rules.
// The caller must prove that Kafka acknowledged this checkpoint and that all
// old owners stopped; this pure function cannot authorize a PG generation.
func TransitionCheckpoint(previous, next *Rules, cp Checkpoint, sealed int64) (Checkpoint, error) {
	return Checkpoint{}, errors.New("configuration handover unavailable")
}
