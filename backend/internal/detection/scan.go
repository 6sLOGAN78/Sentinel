package detection

import (
	"net/netip"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

const scanRuleID = "PORT_SCAN"

// scanMatches requires complete IP grouping and an exact valid destination
// port. The shared window's ID admission bounds its port set to at most
// MaxWindowEvents entries; duplicate IDs cannot supply new ports.
func scanMatches(event *contracts.Event) bool {
	if event.EventType != "net.connection" || event.SourceIP == nil || event.DestinationIP == nil ||
		event.DestinationPort == nil || *event.DestinationPort < 0 || *event.DestinationPort > 65535 {
		return false
	}
	_, sourceErr := netip.ParseAddr(*event.SourceIP)
	_, destinationErr := netip.ParseAddr(*event.DestinationIP)
	return sourceErr == nil && destinationErr == nil
}
