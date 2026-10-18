package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/6sLOGAN78/sentinel/internal/config"
	"github.com/6sLOGAN78/sentinel/internal/detection"
)

// DetectionOwnership binds an assignment to its configured Kafka group.
type DetectionOwnership struct {
	Group, Incarnation, Member string
	Generation                 int32
	Token                      uint64
	Source                     detection.TopicPartition
	Config                     string
}

// DetectionDecision is the stored first disposition and canonical payload.
type DetectionDecision struct {
	Allowed   bool
	Canonical []byte
}

// DetectionDecisionStore persists operational decisions separately from workflows.
type DetectionDecisionStore struct {
	pool    *pgxpool.Pool
	budgets config.DetectorPGConfig
	rules   *detection.Rules
	begin   func(context.Context) (pgx.Tx, func(), error)
}

// NewDetectionDecisionStore uses the explicit bounded detector pool.
func NewDetectionDecisionStore(pool *pgxpool.Pool, budgets config.DetectorPGConfig,
	rules *detection.Rules) *DetectionDecisionStore {
	return &DetectionDecisionStore{pool: pool, budgets: budgets, rules: rules}
}

// Register fences an assignment and verifies its restored durable frontier.
func (*DetectionDecisionStore) Register(context.Context, DetectionOwnership, detection.DecisionFrontier) error {
	return errors.New("durable assignment registration absent")
}

// Decide returns the immutable first allow/suppress and stored canonical bytes.
func (*DetectionDecisionStore) Decide(context.Context, DetectionOwnership, detection.Candidate) (DetectionDecision, error) {
	return DetectionDecision{}, errors.New("durable decision transaction absent")
}
