// Package apikeys provisions and verifies tenant-scoped producer credentials.
package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	keyIDLength     = 12
	keyIDBytes      = 8
	keyParts        = 3
	secretBytes     = 32
	secretLength    = 43
	maxTenantLength = 63
	maxNameLength   = 128
	// ScopeRead permits tenant-scoped event reads.
	ScopeRead = "events:read"
	// ScopeWrite permits tenant-scoped event ingestion.
	ScopeWrite = "events:write"
)

// Fixed classifications deliberately exclude credentials and database errors.
var (
	ErrUnauthorized    = errors.New("unauthorized")
	ErrUnavailable     = errors.New("credential dependency unavailable")
	ErrInvalidMetadata = errors.New("invalid credential metadata")
)

// Generate creates a cryptographically random public ID and 256-bit secret.
func Generate() (string, error) {
	id := make([]byte, keyIDBytes)
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(id); err != nil {
		return "", ErrUnavailable
	}
	if _, err := rand.Read(secret); err != nil {
		return "", ErrUnavailable
	}
	encodedID := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id))[:keyIDLength]
	return "snt_" + encodedID + "_" + base64.RawURLEncoding.EncodeToString(secret), nil
}

// Parse validates canonical alphabets, lengths and base64 padding bits.
func Parse(raw string) (string, string, error) {
	parts := strings.SplitN(raw, "_", keyParts)
	if len(parts) != keyParts || parts[0] != "snt" || !validKeyID(parts[1]) || len(parts[2]) != secretLength {
		return "", "", ErrUnauthorized
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(decoded) != secretBytes {
		return "", "", ErrUnauthorized
	}
	return parts[1], parts[2], nil
}

func validKeyID(id string) bool {
	if len(id) != keyIDLength {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// Hash hashes the encoded secret, never the public ID or complete credential.
func Hash(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// VerifyHash compares a presented encoded secret with a stored SHA-256 hash.
func VerifyHash(secret string, expected []byte) bool {
	return subtle.ConstantTimeCompare(Hash(secret), expected) == 1
}

// ValidateMetadata applies the canonical tenant rules and supported scopes.
func ValidateMetadata(tenant string, scopes []string, name string) error {
	if tenant == "" || len(tenant) > maxTenantLength || name == "" || len(name) > maxNameLength || len(scopes) == 0 {
		return ErrInvalidMetadata
	}
	for i, c := range tenant {
		alnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if !alnum && (i == 0 || (c != '_' && c != '-')) {
			return ErrInvalidMetadata
		}
	}
	for _, scope := range scopes {
		if scope != ScopeRead && scope != ScopeWrite {
			return ErrInvalidMetadata
		}
	}
	return nil
}
