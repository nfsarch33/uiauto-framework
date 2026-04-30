package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// MutationApplicator applies a mutation to the system and returns the outcome.
// Implementations must be idempotent and support rollback via the returned
// RollbackFunc.
type MutationApplicator interface {
	Apply(ctx context.Context, mut Mutation) (ApplyResult, error)
	Supports(mut Mutation) bool
}

// RollbackFunc undoes a previously applied mutation.
type RollbackFunc func(ctx context.Context) error

// ApplyResult captures the outcome of applying a mutation.
type ApplyResult struct {
	Applied    bool         `json:"applied"`
	Message    string       `json:"message"`
	RollbackFn RollbackFunc `json:"-"`
	AppliedAt  time.Time    `json:"applied_at"`
}

// ConfigMutator modifies JSON/YAML configuration files.
type ConfigMutator struct {
	logger *slog.Logger
}

// NewConfigMutator creates a ConfigMutator.
func NewConfigMutator(logger *slog.Logger) *ConfigMutator {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigMutator{logger: logger}
}

// Supports returns true for config-category mutations.
func (c *ConfigMutator) Supports(mut Mutation) bool {
	return mut.Strategy == ModeRepairOnly || mut.Strategy == ModeHarden
}

// Apply writes the AfterState to the config target, preserving BeforeState for rollback.
func (c *ConfigMutator) Apply(ctx context.Context, mut Mutation) (ApplyResult, error) {
	if mut.AfterState == nil {
		return ApplyResult{}, fmt.Errorf("config mutator: after_state is required")
	}

	var target struct {
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(mut.AfterState, &target); err != nil {
		return ApplyResult{}, fmt.Errorf("config mutator: parse after_state: %w", err)
	}

	if target.Path == "" {
		return ApplyResult{}, fmt.Errorf("config mutator: path is required in after_state")
	}

	beforeBytes := mut.BeforeState
	result := ApplyResult{
		Applied:   true,
		Message:   fmt.Sprintf("applied config change to %s", target.Path),
		AppliedAt: time.Now(),
		RollbackFn: func(_ context.Context) error {
			if beforeBytes == nil {
				return os.Remove(target.Path)
			}
			return os.WriteFile(target.Path, beforeBytes, 0644)
		},
	}

	if err := os.WriteFile(target.Path, target.Value, 0644); err != nil {
		return ApplyResult{}, fmt.Errorf("config mutator: write file: %w", err)
	}

	c.logger.Info("config mutation applied",
		slog.String("mutation_id", mut.ID),
		slog.String("path", target.Path),
	)
	return result, nil
}

// ThresholdMutator adjusts numeric thresholds in a registry-style store.
type ThresholdMutator struct {
	thresholds map[string]float64
	logger     *slog.Logger
}

// NewThresholdMutator creates a ThresholdMutator with an initial threshold map.
func NewThresholdMutator(initial map[string]float64, logger *slog.Logger) *ThresholdMutator {
	if logger == nil {
		logger = slog.Default()
	}
	m := make(map[string]float64, len(initial))
	for k, v := range initial {
		m[k] = v
	}
	return &ThresholdMutator{thresholds: m, logger: logger}
}

// Supports returns true for balanced/innovate strategy mutations.
func (t *ThresholdMutator) Supports(mut Mutation) bool {
	return mut.Strategy == ModeBalanced || mut.Strategy == ModeInnovate
}

// Apply adjusts a named threshold value from the mutation's AfterState.
func (t *ThresholdMutator) Apply(_ context.Context, mut Mutation) (ApplyResult, error) {
	var change struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(mut.AfterState, &change); err != nil {
		return ApplyResult{}, fmt.Errorf("threshold mutator: parse: %w", err)
	}

	oldVal := t.thresholds[change.Key]
	t.thresholds[change.Key] = change.Value

	result := ApplyResult{
		Applied:   true,
		Message:   fmt.Sprintf("threshold %q: %.4f -> %.4f", change.Key, oldVal, change.Value),
		AppliedAt: time.Now(),
		RollbackFn: func(_ context.Context) error {
			t.thresholds[change.Key] = oldVal
			return nil
		},
	}

	t.logger.Info("threshold mutation applied",
		slog.String("mutation_id", mut.ID),
		slog.String("key", change.Key),
		slog.Float64("old", oldVal),
		slog.Float64("new", change.Value),
	)
	return result, nil
}

// Get returns the current value for a threshold key.
func (t *ThresholdMutator) Get(key string) float64 { return t.thresholds[key] }

// PromptMutator modifies skill or system prompts stored as files.
type PromptMutator struct {
	logger *slog.Logger
}

// NewPromptMutator creates a PromptMutator.
func NewPromptMutator(logger *slog.Logger) *PromptMutator {
	if logger == nil {
		logger = slog.Default()
	}
	return &PromptMutator{logger: logger}
}

// Supports returns true for innovate-mode mutations.
func (p *PromptMutator) Supports(mut Mutation) bool {
	return mut.Strategy == ModeInnovate
}

// Apply writes the new prompt content from AfterState to the target file.
func (p *PromptMutator) Apply(_ context.Context, mut Mutation) (ApplyResult, error) {
	var target struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(mut.AfterState, &target); err != nil {
		return ApplyResult{}, fmt.Errorf("prompt mutator: parse: %w", err)
	}

	beforeBytes := mut.BeforeState
	result := ApplyResult{
		Applied:   true,
		Message:   fmt.Sprintf("prompt updated at %s", target.Path),
		AppliedAt: time.Now(),
		RollbackFn: func(_ context.Context) error {
			if beforeBytes == nil {
				return os.Remove(target.Path)
			}
			return os.WriteFile(target.Path, beforeBytes, 0644)
		},
	}

	if err := os.WriteFile(target.Path, []byte(target.Content), 0644); err != nil {
		return ApplyResult{}, fmt.Errorf("prompt mutator: write: %w", err)
	}

	p.logger.Info("prompt mutation applied",
		slog.String("mutation_id", mut.ID),
		slog.String("path", target.Path),
	)
	return result, nil
}
