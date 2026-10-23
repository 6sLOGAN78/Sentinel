package query

import (
	"context"
	"errors"
	"time"

	"github.com/6sLOGAN78/sentinel/internal/contracts"
)

// ErrNotFound never distinguishes an unknown ID from another tenant's ID.
var ErrNotFound = errors.New("alert not found")

// StatusChange describes the forward-only lifecycle (at most two transitions).
type StatusChange struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Actor      string    `json:"actor"`
	ChangedAt  time.Time `json:"changed_at"`
}

// AlertDetail keeps authoritative workflow and immutable detection separate.
type AlertDetail struct {
	Workflow           WorkflowAlert     `json:"workflow"`
	Detection          contracts.Alert   `json:"detection"`
	Evidence           []contracts.Event `json:"evidence"`
	MissingEvidenceIDs []string          `json:"missing_evidence_ids"`
	RuleExplanation    string            `json:"rule_explanation"`
	History            []StatusChange    `json:"history"`
}

// WorkflowReader must establish tenant ownership before immutable reads.
type WorkflowReader interface {
	GetWorkflow(context.Context, string, string) (*WorkflowAlert, error)
	RuleExplanation(context.Context, string, string, string, int) (string, error)
}

// AlertDetailReader reads only retained tenant-scoped canonical data.
type AlertDetailReader interface {
	Detection(context.Context, string, string) (*contracts.Alert, error)
	Evidence(context.Context, string, []string) ([]contracts.Event, []string, error)
}
