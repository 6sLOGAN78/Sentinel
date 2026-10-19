package detection

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const (
	metadataNull   = "null"
	metadataPrefix = "prefix_max_event_time"
	metadataSealed = "sealed_through"
)

// Checkpoint couples an exact resume pin with its recovery metadata.
type Checkpoint struct {
	Offset   int64
	Metadata string
}

// DecisionFrontier identifies the last fully effected durable candidate.
type DecisionFrontier struct {
	None        bool   `json:"none"`
	Offset      int64  `json:"offset,omitempty"`
	RuleOrdinal int    `json:"rule_ordinal,omitempty"`
	AlertID     string `json:"alert_id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Metadata is the bounded format-2 recovery envelope. Both nullable clocks
// must be present: null is the explicit empty-prefix sentinel, not omission.
type Metadata struct {
	Format             int              `json:"format"`
	ConfigFingerprint  string           `json:"config_fingerprint"`
	RuleVersions       map[string]int   `json:"rule_versions"`
	PrefixMaxEventTime *int64           `json:"prefix_max_event_time"`
	SealedThrough      *int64           `json:"sealed_through"`
	ProcessedNext      int64            `json:"processed_next"`
	RuleEffectiveFrom  map[string]int64 `json:"rule_effective_from"`
	DecisionFrontier   DecisionFrontier `json:"decision_frontier"`
	Drained            bool             `json:"drained"`
}

func decodeCheckpointMetadata(raw string) (Metadata, error) {
	// Walk the bounded document first: encoding/json otherwise accepts duplicate
	// members (including nested version/clock keys), case aliases and null bools.
	check := json.NewDecoder(strings.NewReader(raw))
	if err := uniqueMetadataObject(check); err != nil {
		return Metadata{}, err
	}
	var extra any
	if !errors.Is(check.Decode(&extra), io.EOF) {
		return Metadata{}, errors.New("trailing checkpoint data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return Metadata{}, err
	}
	keys := []string{"format", "config_fingerprint", "rule_versions", metadataPrefix,
		metadataSealed, "processed_next", "rule_effective_from", "decision_frontier", "drained"}
	if len(fields) != len(keys) {
		return Metadata{}, errors.New("checkpoint field set mismatch")
	}
	for _, key := range keys {
		value, present := fields[key]
		if !present || !validNullableMetadata(key, value) {
			return Metadata{}, errors.New("checkpoint missing explicit field")
		}
	}
	var marker map[string]json.RawMessage
	if err := json.Unmarshal(fields["decision_frontier"], &marker); err != nil {
		return Metadata{}, err
	}
	if value, present := marker["none"]; !present || string(value) == metadataNull {
		return Metadata{}, errors.New("checkpoint missing explicit decision frontier")
	}
	for key, value := range marker {
		switch key {
		case "none", "offset", "rule_ordinal", "alert_id", "fingerprint":
			if string(value) != metadataNull {
				continue
			}
		}
		return Metadata{}, errors.New("invalid checkpoint decision field")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	var m Metadata
	if err := d.Decode(&m); err != nil {
		return Metadata{}, errors.New("invalid checkpoint metadata")
	}
	return m, nil
}

func validateCheckpointContext(m Metadata, pin int64) error {
	if m.PrefixMaxEventTime != nil && !contracts.ValidWindow(time.UnixMilli(*m.PrefixMaxEventTime)) ||
		m.SealedThrough != nil && !contracts.ValidWindow(time.UnixMilli(*m.SealedThrough)) {
		return errors.New("invalid checkpoint time")
	}
	if m.PrefixMaxEventTime != nil && m.SealedThrough == nil {
		return errors.New("checkpoint missing sealed floor")
	}
	if m.Drained && m.ProcessedNext != pin {
		return errors.New("invalid drained checkpoint")
	}
	if !m.Drained && (m.ProcessedNext == pin || m.SealedThrough == nil) ||
		m.PrefixMaxEventTime != nil && *m.SealedThrough < *m.PrefixMaxEventTime-Lateness.Milliseconds() {
		return errors.New("inconsistent checkpoint prefix")
	}
	return nil
}

func uniqueMetadataObject(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("invalid checkpoint object")
	}
	seen := make(map[string]bool)
	for d.More() {
		key, keyErr := d.Token()
		name, ok := key.(string)
		if keyErr != nil || !ok || seen[name] {
			return errors.New("duplicate checkpoint field")
		}
		seen[name] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return err
		}
		if !validNullableMetadata(name, value) {
			return errors.New("null checkpoint scalar")
		}
		if len(value) > 0 && value[0] == '{' {
			if err = uniqueMetadataObject(json.NewDecoder(strings.NewReader(string(value)))); err != nil {
				return err
			}
		}
	}
	_, err = d.Token()
	return err
}

func validNullableMetadata(key string, value json.RawMessage) bool {
	return string(value) != metadataNull || key == metadataPrefix || key == metadataSealed
}
