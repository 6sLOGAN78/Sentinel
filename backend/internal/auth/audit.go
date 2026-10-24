package auth

import (
	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/middleware"
)

// Audit records a service security outcome, not a durable database receipt.
// Callers supply only verified identity (zero Principal before authentication).
// An allowlist keeps request bodies, credentials and arbitrary error strings
// out of the security event even if a caller accidentally supplies one.
func Audit(c echo.Context, action, outcome, reason string, p Principal) {
	if action != "producer_auth" && action != "alert_transition" {
		return
	}
	if outcome != "allowed" && outcome != "denied" && outcome != "unavailable" {
		return
	}
	switch reason {
	case "invalid_credential", "auth_unavailable", "scope_forbidden", "tenant_forbidden",
		"role_forbidden", "invalid_request", "conflict", "not_found", "store_unavailable", "committed":
	default:
		return
	}
	event := middleware.GetLogger(c).Info().
		Str("event", "security_audit").
		Str("action", action).
		Str("outcome", outcome).
		Str("reason", reason)
	if p.Subject != "" {
		event = event.Str("actor", p.Subject).Str("tenant_id", p.TenantID)
	}
	if p.Role != "" {
		event = event.Str("role", p.Role)
	}
	event.Msg("Security outcome")
}
