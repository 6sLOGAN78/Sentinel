package query

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/6sLOGAN78/sentinel/internal/auth"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

type ruleParameters struct {
	Threshold  int   `json:"threshold"`
	WindowMS   int64 `json:"window_ms"`
	CooldownMS int64 `json:"cooldown_ms"`
}

type ruleDefinition struct {
	ID          string         `json:"rule_id"`
	Version     int            `json:"version"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Severity    string         `json:"severity"`
	Enabled     bool           `json:"enabled"`
	Parameters  ruleParameters `json:"parameters"`
}

// RulesHandler owns a finite copy of the validated process-start definitions.
// Historical alert explanations remain bound to their retained decisions.
type RulesHandler struct {
	auth  *auth.Verifier
	rules []ruleDefinition
}

// NewRulesHandler copies definitions so later caller changes cannot mutate reads.
func NewRulesHandler(definitions *detection.Rules, verifier *auth.Verifier) *RulesHandler {
	h := &RulesHandler{auth: verifier, rules: make([]ruleDefinition, 0, len(definitions.Rules))}
	for _, rule := range definitions.Rules {
		name, description := ruleDescription(rule.ID)
		h.rules = append(h.rules, ruleDefinition{ID: rule.ID, Version: rule.Version, Name: name, Description: description,
			Severity: rule.Severity, Enabled: rule.Enabled, Parameters: ruleParameters{
				Threshold: rule.Threshold, WindowMS: rule.Window.Milliseconds(), CooldownMS: rule.Cooldown.Milliseconds()}})
	}
	return h
}

// Register permits every verified role to read the same startup snapshot.
func (h *RulesHandler) Register(e *echo.Echo) {
	e.GET("/api/v1/rules", h.ListRules, h.auth.Middleware)
}

// ListRules never reads files or accepts executable or mutable configuration.
func (h *RulesHandler) ListRules(c echo.Context) error {
	if _, err := auth.RequirePrincipal(c); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, struct {
		Rules []ruleDefinition `json:"rules"`
	}{Rules: h.rules})
}

func ruleDescription(id string) (string, string) {
	switch id {
	case "AUTH_BRUTE_FORCE":
		return "Authentication brute force",
			"Counts failed authentication events for a source IP and principal in an event-time window."
	case "PORT_SCAN":
		return "Port scan", "Counts distinct destination ports for a source IP and destination IP in an event-time window."
	case "REQUEST_FLOOD":
		return "Request flood", "Counts network requests for a source IP in an event-time window."
	case "DENY_SPIKE":
		return "Deny spike", "Counts denied policy decisions for a source IP in an event-time window."
	default:
		return id, "Validated startup rule definition."
	}
}
