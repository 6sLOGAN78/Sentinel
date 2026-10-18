package detection

import (
	"net/netip"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const (
	floodRuleID = "REQUEST_FLOOD"
	denyRuleID  = "DENY_SPIKE"
)

// volumeMatches admits only the exact event predicate with a valid source.
// Outcome, principal and destination never alter volume membership or identity.
func volumeMatches(event *contracts.Event, rule string) bool {
	if event.SourceIP == nil {
		return false
	}
	if _, err := netip.ParseAddr(*event.SourceIP); err != nil {
		return false
	}
	switch rule {
	case floodRuleID:
		return event.EventType == "net.request"
	case denyRuleID:
		return event.EventType == "policy.decision" && event.Action != nil && *event.Action == "deny"
	default:
		return false
	}
}
