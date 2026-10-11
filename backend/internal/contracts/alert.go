package contracts

import (
	"io"
	"unicode/utf8"
)

// Alert contract constants (spec section 6.2, CONTEXT D-16).
const (
	// AlertSchemaVersion is the only supported alert schema_version.
	AlertSchemaVersion = 1

	alertIDLen        = 64
	maxRuleIDLen      = 64
	maxSampleEventIDs = 20
	maxEntityKeyLen   = 512
	maxSummaryLen     = 512
	maxDedupeKeyLen   = 1024
)

// Alert is the canonical security alert published on security.alerts.v1.
type Alert struct {
	SchemaVersion  int        `json:"schema_version"`
	AlertID        string     `json:"alert_id"`
	TenantID       string     `json:"tenant_id"`
	RuleID         string     `json:"rule_id"`
	RuleVersion    int        `json:"rule_version"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	CreatedAt      Timestamp  `json:"created_at"`
	WindowStart    Timestamp  `json:"window_start"`
	WindowEnd      Timestamp  `json:"window_end"`
	DetectedAt     *Timestamp `json:"detected_at,omitempty"`
	EntityKey      string     `json:"entity_key"`
	Summary        string     `json:"summary"`
	EventCount     int        `json:"event_count"`
	SampleEventIDs []string   `json:"sample_event_ids"`
	DedupeKey      string     `json:"dedupe_key"`
}

// alertFields lists the exact root keys accepted by the alert contract.
func alertFields() map[string]struct{} {
	keys := []string{
		"schema_version", "alert_id", "tenant_id", "rule_id", "rule_version", "severity",
		"status", "created_at", "window_start", "window_end", "detected_at", "entity_key",
		"summary", "event_count", "sample_event_ids", "dedupe_key",
	}
	m := make(map[string]struct{}, len(keys))

	for _, k := range keys {
		m[k] = struct{}{}
	}

	return m
}

// DecodeAlert strictly decodes one JSON alert: unknown root fields (matched
// case-sensitively), type mismatches and trailing data are rejected. Input
// beyond 1 MiB is refused with ErrEventTooLarge.
func DecodeAlert(r io.Reader) (*Alert, error) {
	return strictDecode[Alert](r, "alert", alertFields())
}

// Validate checks every alert contract rule.
func (a *Alert) Validate() error {
	checks := []func() error{
		a.checkIdentity,
		a.checkEnums,
		a.checkTimes,
		a.checkStrings,
		a.checkEvidence,
	}

	for _, c := range checks {
		if err := c(); err != nil {
			return err
		}
	}

	return nil
}

func (a *Alert) checkIdentity() error {
	if a.SchemaVersion != AlertSchemaVersion {
		return fieldErr("schema_version", "must be %d", AlertSchemaVersion)
	}

	if !validAlertID(a.AlertID) {
		return fieldErr("alert_id", "must be %d lowercase hex characters", alertIDLen)
	}

	if !validTenant(a.TenantID) {
		return fieldErr("tenant_id", "must match ^[a-z0-9][a-z0-9_-]{0,62}$")
	}

	if !validRuleID(a.RuleID) {
		return fieldErr("rule_id", "must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	if a.RuleVersion < 1 {
		return fieldErr("rule_version", "must be at least 1")
	}

	return nil
}

func validAlertID(s string) bool {
	if len(s) != alertIDLen {
		return false
	}

	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

func validRuleID(s string) bool {
	if s == "" || len(s) > maxRuleIDLen {
		return false
	}

	for i := range len(s) {
		c := s[i]
		upper := c >= 'A' && c <= 'Z'

		if upper || (i > 0 && ((c >= '0' && c <= '9') || c == '_')) {
			continue
		}

		return false
	}

	return true
}

func (a *Alert) checkEnums() error {
	switch a.Severity {
	case "low", "medium", "high", "critical":
	default:
		return fieldErr("severity", "unsupported value %q", a.Severity)
	}

	switch a.Status {
	case "open", "acknowledged", "resolved":
	default:
		return fieldErr("status", "unsupported value %q", a.Status)
	}

	return nil
}

func (a *Alert) checkTimes() error {
	if a.CreatedAt.IsZero() {
		return fieldErr("created_at", "is required")
	}

	if !ValidWindow(a.WindowStart.Time) {
		return fieldErr("window_start", "must be within years 2000..2100")
	}

	if !ValidWindow(a.WindowEnd.Time) {
		return fieldErr("window_end", "must be within years 2000..2100")
	}

	if !a.WindowEnd.After(a.WindowStart.Time) {
		return fieldErr("window_end", "must be after window_start")
	}

	return nil
}

func (a *Alert) checkStrings() error {
	if n := utf8.RuneCountInString(a.EntityKey); n < 1 || n > maxEntityKeyLen {
		return fieldErr("entity_key", "length must be within 1..%d", maxEntityKeyLen)
	}

	if n := utf8.RuneCountInString(a.Summary); n < 1 || n > maxSummaryLen {
		return fieldErr("summary", "length must be within 1..%d", maxSummaryLen)
	}

	if n := utf8.RuneCountInString(a.DedupeKey); n < 1 || n > maxDedupeKeyLen {
		return fieldErr("dedupe_key", "length must be within 1..%d", maxDedupeKeyLen)
	}

	return nil
}

func (a *Alert) checkEvidence() error {
	if a.EventCount < 1 {
		return fieldErr("event_count", "must be at least 1")
	}

	if a.SampleEventIDs == nil {
		return fieldErr("sample_event_ids", "is required")
	}

	if len(a.SampleEventIDs) > maxSampleEventIDs {
		return fieldErr("sample_event_ids", "more than %d entries", maxSampleEventIDs)
	}

	for _, id := range a.SampleEventIDs {
		if !canonicalUUID(id) {
			return fieldErr("sample_event_ids", "entry %q is not a canonical UUID", id)
		}
	}

	return nil
}
