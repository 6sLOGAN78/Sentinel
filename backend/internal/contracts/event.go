package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/google/uuid"
)

// Event contract constants (spec section 6.1, CONTEXT D-13).
const (
	// EventSchemaVersion is the only supported schema_version.
	EventSchemaVersion = 1

	maxTenantLen    = 63
	maxSourceLen    = 64
	maxPrincipalLen = 128
	maxPort         = 65535
	canonicalUUIDLn = 36
	maxEventBytes   = 1 << 20

	defaultAttrKeys  = 32
	defaultAttrBytes = 4096
	defaultFuture    = 5 * time.Minute
	defaultPast      = 168 * time.Hour

	timestampLayout = "2006-01-02T15:04:05.000Z"
)

// ErrEventTooLarge is returned by DecodeEvent when the payload exceeds the decode cap.
var ErrEventTooLarge = errors.New("event payload exceeds decode limit")

// Timestamp is an RFC 3339 UTC instant with a Z suffix, held at millisecond precision.
type Timestamp struct {
	time.Time
}

// NewTimestamp returns t converted to UTC and truncated to the millisecond.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{Time: t.UTC().Truncate(time.Millisecond)}
}

// UnmarshalJSON parses an RFC 3339 string that must end in Z.
func (ts *Timestamp) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("timestamp must be a string: %w", err)
	}

	if !strings.HasSuffix(s, "Z") {
		return fmt.Errorf("timestamp %q must be UTC with a Z suffix", s)
	}

	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("timestamp %q is not RFC 3339: %w", s, err)
	}

	ts.Time = t.UTC().Truncate(time.Millisecond)

	return nil
}

// MarshalJSON renders the instant as UTC with exactly three fractional digits.
func (ts Timestamp) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(ts.UTC().Format(timestampLayout))
	if err != nil {
		return nil, fmt.Errorf("marshal timestamp: %w", err)
	}

	return b, nil
}

// Event is the canonical security event published on security.events.v1.
type Event struct {
	SchemaVersion   int            `json:"schema_version"`
	EventID         string         `json:"event_id"`
	TenantID        string         `json:"tenant_id"`
	OccurredAt      Timestamp      `json:"occurred_at"`
	ReceivedAt      *Timestamp     `json:"received_at,omitempty"`
	Source          string         `json:"source"`
	EventType       string         `json:"event_type"`
	SourceIP        *string        `json:"source_ip,omitempty"`
	DestinationIP   *string        `json:"destination_ip,omitempty"`
	DestinationPort *int           `json:"destination_port,omitempty"`
	Protocol        *string        `json:"protocol,omitempty"`
	Principal       *string        `json:"principal,omitempty"`
	Action          *string        `json:"action,omitempty"`
	Outcome         *string        `json:"outcome,omitempty"`
	Attributes      map[string]any `json:"attributes,omitempty"`
}

// Limits bounds attributes and timestamp skew during validation.
type Limits struct {
	MaxAttrKeys  int
	MaxAttrBytes int
	FutureSkew   time.Duration
	PastSkew     time.Duration
}

// DefaultLimits returns the spec defaults: 32 keys, 4 KiB, +5m and -7d.
func DefaultLimits() Limits {
	return Limits{
		MaxAttrKeys:  defaultAttrKeys,
		MaxAttrBytes: defaultAttrBytes,
		FutureSkew:   defaultFuture,
		PastSkew:     defaultPast,
	}
}

// LimitsFromConfig converts the contracts configuration into Limits.
func LimitsFromConfig(c config.ContractsConfig) Limits {
	return Limits{
		MaxAttrKeys:  c.MaxAttrKeys,
		MaxAttrBytes: c.MaxAttrBytes,
		FutureSkew:   c.FutureSkew,
		PastSkew:     c.PastSkew,
	}
}

// FieldError reports the field that failed contract validation.
type FieldError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *FieldError) Error() string {
	return e.Field + ": " + e.Message
}

func fieldErr(field, format string, args ...any) error {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// eventFields lists the exact root keys accepted by the contract.
func eventFields() map[string]struct{} {
	keys := []string{
		"schema_version", "event_id", "tenant_id", "occurred_at", "received_at", "source",
		"event_type", "source_ip", "destination_ip", "destination_port", "protocol",
		"principal", "action", "outcome", "attributes",
	}
	m := make(map[string]struct{}, len(keys))

	for _, k := range keys {
		m[k] = struct{}{}
	}

	return m
}

// DecodeEvent strictly decodes one JSON event: unknown root fields (matched
// case-sensitively), type mismatches and trailing data are all rejected.
// Input beyond 1 MiB is refused with ErrEventTooLarge.
func DecodeEvent(r io.Reader) (*Event, error) {
	return strictDecode[Event](r, "event", eventFields())
}

// strictDecode reads at most maxEventBytes of JSON from r and decodes exactly
// one object into T, rejecting unknown root fields, type mismatches and
// trailing data. what names the contract in error messages.
func strictDecode[T any](r io.Reader, what string, allowed map[string]struct{}) (*T, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxEventBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}

	if len(raw) > maxEventBytes {
		return nil, ErrEventTooLarge
	}

	keys, err := rootKeys(raw)
	if err != nil {
		var fe *FieldError
		if errors.As(err, &fe) {
			return nil, err
		}

		return nil, fmt.Errorf("decode %s: %w", what, err)
	}

	for k := range keys {
		if _, ok := allowed[k]; !ok {
			return nil, fieldErr(k, "unknown field")
		}
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var v T
	if err = dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}

	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode %s: trailing data after JSON object", what)
	}

	return &v, nil
}

// Validate checks every contract rule against the clock now and limits l.
func (e *Event) Validate(now time.Time, l Limits) error {
	checks := []func() error{
		e.checkIdentity,
		e.checkEnums,
		e.checkIPsAndPort,
		e.checkStrings,
		func() error { return e.checkTimes(now, l) },
		func() error { return e.checkAttributes(l) },
	}

	for _, c := range checks {
		if err := c(); err != nil {
			return err
		}
	}

	return nil
}

func (e *Event) checkIdentity() error {
	if e.SchemaVersion != EventSchemaVersion {
		return fieldErr("schema_version", "must be %d", EventSchemaVersion)
	}

	if !canonicalUUID(e.EventID) {
		return fieldErr("event_id", "must be a canonical lowercase hyphenated UUID")
	}

	if !validTenant(e.TenantID) {
		return fieldErr("tenant_id", "must match ^[a-z0-9][a-z0-9_-]{0,62}$")
	}

	return nil
}

func canonicalUUID(s string) bool {
	if len(s) != canonicalUUIDLn || s != strings.ToLower(s) {
		return false
	}

	_, err := uuid.Parse(s)

	return err == nil
}

func validTenant(s string) bool {
	if s == "" || len(s) > maxTenantLen {
		return false
	}

	for i := range len(s) {
		c := s[i]
		isAlnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')

		if isAlnum || (i > 0 && (c == '_' || c == '-')) {
			continue
		}

		return false
	}

	return true
}

func (e *Event) checkEnums() error {
	switch e.EventType {
	case "auth.success", "auth.failure", "net.connection", "net.request", "policy.decision":
	default:
		return fieldErr("event_type", "unsupported value %q", e.EventType)
	}

	if err := optEnum("protocol", e.Protocol, "tcp", "udp", "icmp", "other"); err != nil {
		return err
	}

	if err := optEnum("action", e.Action, "allow", "deny"); err != nil {
		return err
	}

	return optEnum("outcome", e.Outcome, "success", "failure")
}

func optEnum(field string, v *string, allowed ...string) error {
	if v == nil {
		return nil
	}

	for _, a := range allowed {
		if *v == a {
			return nil
		}
	}

	return fieldErr(field, "unsupported value %q", *v)
}

func (e *Event) checkIPsAndPort() error {
	if err := optIP("source_ip", e.SourceIP); err != nil {
		return err
	}

	if err := optIP("destination_ip", e.DestinationIP); err != nil {
		return err
	}

	if p := e.DestinationPort; p != nil && (*p < 0 || *p > maxPort) {
		return fieldErr("destination_port", "must be within 0..%d", maxPort)
	}

	return nil
}

func optIP(field string, v *string) error {
	if v == nil {
		return nil
	}

	addr, err := netip.ParseAddr(*v)
	if err != nil {
		return fieldErr(field, "invalid IP address")
	}

	if addr.Zone() != "" || addr.Is4In6() || (addr.Is6() && strings.Contains(*v, ".")) {
		return fieldErr(field, "zoned, IPv4-mapped or mixed-notation addresses are not accepted")
	}

	return nil
}

func (e *Event) checkStrings() error {
	if n := utf8.RuneCountInString(e.Source); n < 1 || n > maxSourceLen {
		return fieldErr("source", "length must be within 1..%d", maxSourceLen)
	}

	if e.Principal != nil && utf8.RuneCountInString(*e.Principal) > maxPrincipalLen {
		return fieldErr("principal", "length must be at most %d", maxPrincipalLen)
	}

	return nil
}

func (e *Event) checkTimes(now time.Time, l Limits) error {
	if e.OccurredAt.IsZero() {
		return fieldErr("occurred_at", "is required")
	}

	if e.OccurredAt.After(now.Add(l.FutureSkew)) {
		return fieldErr("occurred_at", "is more than %s in the future", l.FutureSkew)
	}

	if e.OccurredAt.Before(now.Add(-l.PastSkew)) {
		return fieldErr("occurred_at", "is more than %s in the past", l.PastSkew)
	}

	return nil
}

func (e *Event) checkAttributes(l Limits) error {
	if len(e.Attributes) > l.MaxAttrKeys {
		return fieldErr("attributes", "more than %d keys", l.MaxAttrKeys)
	}

	for k, v := range e.Attributes {
		switch v.(type) {
		case string, float64, bool:
		default:
			return fieldErr("attributes."+k, "value must be a string, number or boolean")
		}
	}

	if len(e.Attributes) == 0 {
		return nil
	}

	b, err := json.Marshal(e.Attributes)
	if err != nil {
		return fieldErr("attributes", "cannot be encoded: %v", err)
	}

	if len(b) > l.MaxAttrBytes {
		return fieldErr("attributes", "encoded size exceeds %d bytes", l.MaxAttrBytes)
	}

	return nil
}
