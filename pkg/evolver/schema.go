package evolver

import (
	"encoding/json"
	"fmt"
	"time"
)

// Gene represents a reusable capability enhancement that can be applied to
// agent behaviour. Genes carry their own validation commands and blast-radius
// estimates so the evolution engine can safely evaluate them.
type Gene struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    GeneCategory      `json:"category"`
	Tags        []string          `json:"tags,omitempty"`
	Payload     json.RawMessage   `json:"payload"`
	Validation  []ValidationStep  `json:"validation"`
	BlastRadius BlastRadius       `json:"blast_radius"`
	Origin      string            `json:"origin"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Version     int               `json:"version"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// GeneCategory classifies what kind of enhancement a Gene represents.
type GeneCategory string

// GeneCategory values.
const (
	GeneCategoryPrompt     GeneCategory = "prompt"
	GeneCategoryTool       GeneCategory = "tool"
	GeneCategoryWorkflow   GeneCategory = "workflow"
	GeneCategoryConfig     GeneCategory = "config"
	GeneCategorySelector   GeneCategory = "selector"
	GeneCategoryPattern    GeneCategory = "pattern"
	GeneCategoryResilience GeneCategory = "resilience"
)

// ValidationStep is a single check that must pass for a Gene to be considered
// safe. Commands are executed in a sandboxed context.
type ValidationStep struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Timeout string `json:"timeout,omitempty"`
}

// BlastRadius estimates the scope of impact when a Gene is applied.
type BlastRadius struct {
	Level           RiskLevel `json:"level"`
	AffectedModules []string  `json:"affected_modules,omitempty"`
	Reversible      bool      `json:"reversible"`
}

// RiskLevel categorises the risk of a mutation or gene application.
type RiskLevel string

// RiskLevel values.
const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// Capsule bundles a successful execution pattern with its environment
// fingerprint and outcome metrics. Capsules are the unit of reuse.
type Capsule struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	GeneIDs         []string          `json:"gene_ids"`
	Environment     EnvFingerprint    `json:"environment"`
	Metrics         CapsuleMetrics    `json:"metrics"`
	Status          CapsuleStatus     `json:"status"`
	AppliedCount    int               `json:"applied_count"`
	LastApplied     *time.Time        `json:"last_applied,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	ParentCapsuleID string            `json:"parent_capsule_id,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// EnvFingerprint captures the environment state when a capsule was created.
type EnvFingerprint struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	GoVer    string `json:"go_version,omitempty"`
	CommitID string `json:"commit_id,omitempty"`
	Branch   string `json:"branch,omitempty"`
}

// CapsuleMetrics records the measurable outcomes of applying a capsule.
type CapsuleMetrics struct {
	SuccessRate      float64        `json:"success_rate"`
	AvgLatencyMs     float64        `json:"avg_latency_ms"`
	EstimatedCostUSD float64        `json:"estimated_cost_usd"`
	SampleCount      int            `json:"sample_count"`
	CustomMetrics    map[string]any `json:"custom_metrics,omitempty"`
}

// CapsuleStatus tracks the lifecycle state of a capsule.
type CapsuleStatus string

// CapsuleStatus values.
const (
	CapsuleStatusDraft    CapsuleStatus = "draft"
	CapsuleStatusTesting  CapsuleStatus = "testing"
	CapsuleStatusActive   CapsuleStatus = "active"
	CapsuleStatusRetired  CapsuleStatus = "retired"
	CapsuleStatusRejected CapsuleStatus = "rejected"
)

// EvolutionEvent is an append-only audit record that tracks every action
// the evolution engine takes. Events reference their parent for lineage.
type EvolutionEvent struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      EventType       `json:"type"`
	ActorID   string          `json:"actor_id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	Outcome   EventOutcome    `json:"outcome"`
}

// EventType categorises evolution events.
type EventType string

// EventType values.
const (
	EventSignalDetected   EventType = "signal_detected"
	EventMutationProposed EventType = "mutation_proposed"
	EventValidationRun    EventType = "validation_run"
	EventGeneApplied      EventType = "gene_applied"
	EventCapsuleCreated   EventType = "capsule_created"
	EventCapsulePromoted  EventType = "capsule_promoted"
	EventCapsuleRetired   EventType = "capsule_retired"
	EventRollback         EventType = "rollback"
	EventHITLApproved     EventType = "hitl_approved"
	EventHITLRejected     EventType = "hitl_rejected"
)

// EventOutcome records the result of an evolution event.
type EventOutcome struct {
	Success bool   `json:"success"`
	Reason  string `json:"reason,omitempty"`
}

// Mutation represents a proposed change to agent behaviour. It captures the
// reasoning, risk estimate, and before/after state for review.
type Mutation struct {
	ID            string          `json:"id"`
	SignalID      string          `json:"signal_id"`
	GeneID        string          `json:"gene_id,omitempty"`
	Reasoning     string          `json:"reasoning"`
	RiskEstimate  RiskLevel       `json:"risk_estimate"`
	Strategy      EvolutionMode   `json:"strategy"`
	BeforeState   json.RawMessage `json:"before_state,omitempty"`
	AfterState    json.RawMessage `json:"after_state,omitempty"`
	Status        MutationStatus  `json:"status"`
	ValidationLog []string        `json:"validation_log,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
}

// MutationStatus tracks the lifecycle of a proposed mutation.
type MutationStatus string

// MutationStatus values.
const (
	MutationStatusPending    MutationStatus = "pending"
	MutationStatusApproved   MutationStatus = "approved"
	MutationStatusApplied    MutationStatus = "applied"
	MutationStatusRejected   MutationStatus = "rejected"
	MutationStatusRolledBack MutationStatus = "rolled_back"
)

// EvolutionMode controls how aggressive the evolution engine is.
type EvolutionMode string

// EvolutionMode values.
const (
	ModeRepairOnly EvolutionMode = "repair-only"
	ModeHarden     EvolutionMode = "harden"
	ModeBalanced   EvolutionMode = "balanced"
	ModeInnovate   EvolutionMode = "innovate"
)

// Validate checks required fields on a Gene.
func (g *Gene) Validate() error {
	if g.ID == "" {
		return fmt.Errorf("gene: id is required")
	}
	if g.Name == "" {
		return fmt.Errorf("gene: name is required")
	}
	if g.Category == "" {
		return fmt.Errorf("gene: category is required")
	}
	return nil
}

// Validate checks required fields on a Capsule.
func (c *Capsule) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("capsule: id is required")
	}
	if c.Name == "" {
		return fmt.Errorf("capsule: name is required")
	}
	if len(c.GeneIDs) == 0 {
		return fmt.Errorf("capsule: at least one gene_id is required")
	}
	return nil
}

// Validate checks required fields on a Mutation.
func (m *Mutation) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("mutation: id is required")
	}
	if m.SignalID == "" {
		return fmt.Errorf("mutation: signal_id is required")
	}
	if m.Reasoning == "" {
		return fmt.Errorf("mutation: reasoning is required")
	}
	return nil
}

// Validate checks required fields on an EvolutionEvent.
func (e *EvolutionEvent) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("event: id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("event: type is required")
	}
	if e.ActorID == "" {
		return fmt.Errorf("event: actor_id is required")
	}
	return nil
}
