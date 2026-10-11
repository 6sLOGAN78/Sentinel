package contracts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

const (
	// dedupeDomain is the domain separator and encoding version.
	dedupeDomain = "sentinel.alert.v1"

	minWindowYear = 2000
	maxWindowYear = 2100
)

// DedupeKey returns the alert_id for one detection: the lowercase SHA-256 hex
// (64 characters) of a canonical, injective encoding of the inputs.
//
// Encoding: the domain separator "sentinel.alert.v1", then tenant and ruleID
// each with a big-endian uint32 length prefix, a big-endian uint32 entity
// component count, each entity component with a uint32 length prefix, and the
// big-endian uint64 of windowStart in UTC Unix milliseconds. Length prefixes
// and the component count make the encoding injective, so values containing
// "|" cannot collide.
//
// This is the only function allowed to produce an alert_id. The human-readable
// dedupe_key and entity_key in spec section 6.2 are display only. Changing
// this encoding is a breaking change: stored alerts would be orphaned and the
// golden vectors in dedupe_test.go fail on any change. DedupeKey is total;
// use ValidWindow to reject out-of-range windows.
func DedupeKey(tenant, ruleID string, entity []string, windowStart time.Time) string {
	b := make([]byte, 0, len(dedupeDomain)+sha256.Size)
	b = append(b, dedupeDomain...)
	b = appendLP(b, tenant)
	b = appendLP(b, ruleID)
	b = binary.BigEndian.AppendUint32(b, uint32(len(entity))) //nolint:gosec // entity slice length fits uint32
	for _, e := range entity {
		b = appendLP(b, e)
	}

	ms := windowStart.UTC().UnixMilli()
	// Two's-complement of the millisecond value is intended and injective.
	b = binary.BigEndian.AppendUint64(b, uint64(ms))

	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

// appendLP appends s with a big-endian uint32 length prefix.
func appendLP(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s))) //nolint:gosec // string length fits uint32
	return append(b, s...)
}

// ValidWindow reports whether t lies within years 2000 through 2100 UTC.
func ValidWindow(t time.Time) bool {
	y := t.UTC().Year()

	return !t.IsZero() && y >= minWindowYear && y <= maxWindowYear
}
