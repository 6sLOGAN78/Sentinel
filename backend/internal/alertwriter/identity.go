package alertwriter

import (
	"errors"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
	storageclickhouse "github.com/6sLOGAN78/sentinel/internal/storage/clickhouse"
)

const maxPrincipalRunes = 128

// ValidateIdentity preserves the entire first-pipe AUTH principal suffix; display
// delimiters never replace the length-prefixed contracts.DedupeKey primitive.
func ValidateIdentity(alert *contracts.Alert, key []byte) error {
	if alert == nil {
		return errors.New("nil alert")
	}
	if err := alert.Validate(); err != nil {
		return errors.New("invalid alert schema")
	}
	first, second, hasPipe := strings.Cut(alert.EntityKey, "|")
	entity := []string{first}
	switch alert.RuleID {
	case "AUTH_BRUTE_FORCE":
		if !hasPipe || second == "" || utf8.RuneCountInString(second) > maxPrincipalRunes {
			return errors.New("invalid AUTH entity")
		}
		entity = append(entity, second)
	case "PORT_SCAN":
		if !hasPipe || !validIP(second) {
			return errors.New("invalid SCAN entity")
		}
		entity = append(entity, second)
	case "REQUEST_FLOOD", "DENY_SPIKE":
		if hasPipe {
			return errors.New("invalid volume entity")
		}
	default:
		return errors.New("unsupported alert rule")
	}
	id := contracts.DedupeKey(alert.TenantID, alert.RuleID, entity, alert.WindowStart.Time)
	if !validIP(first) || id != alert.AlertID {
		return errors.New("invalid canonical alert identity")
	}
	if string(key) != alert.TenantID+"|"+alert.AlertID {
		return errors.New("invalid canonical alert source key")
	}
	return nil
}

func validIP(value string) bool {
	addr, err := netip.ParseAddr(value)
	return err == nil && addr.Zone() == "" && !addr.Is4In6() && (!addr.Is6() || !strings.Contains(value, "."))
}

// ImmutableFingerprint includes all immutable fields and evidence order. The
// storage adapter owns the shared policy to avoid a storage-to-worker import.
func ImmutableFingerprint(alert *contracts.Alert) (string, error) {
	return storageclickhouse.ImmutableFingerprint(alert)
}
