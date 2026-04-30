package evolver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// PropagationConfig controls cross-machine capsule propagation.
type PropagationConfig struct {
	SourceNode  string   `json:"source_node"`
	TargetNodes []string `json:"target_nodes"`
	MaxCapsules int      `json:"max_capsules"`
	MinScore    float64  `json:"min_score"`
}

// PropagationResult summarises a capsule propagation run.
type PropagationResult struct {
	SourceNode     string        `json:"source_node"`
	CapsulesSent   int           `json:"capsules_sent"`
	TargetsReached int           `json:"targets_reached"`
	Errors         []string      `json:"errors,omitempty"`
	Duration       time.Duration `json:"duration"`
}

// CapsulePropagator handles cross-machine capsule distribution.
type CapsulePropagator struct {
	store  Store
	fleet  *PersistentFleetCoordinator
	logger *slog.Logger
}

// NewCapsulePropagator creates a propagator backed by the given store and fleet coordinator.
func NewCapsulePropagator(store Store, fleet *PersistentFleetCoordinator, logger *slog.Logger) *CapsulePropagator {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &CapsulePropagator{
		store:  store,
		fleet:  fleet,
		logger: logger,
	}
}

// Propagate distributes promoted capsules from the source to target nodes.
func (cp *CapsulePropagator) Propagate(ctx context.Context, cfg PropagationConfig) (*PropagationResult, error) {
	start := time.Now()
	result := &PropagationResult{SourceNode: cfg.SourceNode}

	capsules, err := cp.store.ListCapsules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list capsules: %w", err)
	}

	var eligible []Capsule
	for _, c := range capsules {
		if c.Status == CapsuleStatusActive {
			eligible = append(eligible, c)
		}
	}

	if cfg.MaxCapsules > 0 && len(eligible) > cfg.MaxCapsules {
		eligible = eligible[:cfg.MaxCapsules]
	}

	for _, targetNode := range cfg.TargetNodes {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, "context cancelled")
			break
		}

		for _, c := range eligible {
			share := PatternShare{
				SourceNode:  cfg.SourceNode,
				TargetNode:  targetNode,
				PatternID:   c.ID,
				PatternData: c.Name,
				Timestamp:   time.Now(),
			}
			if err := cp.fleet.SharePattern(ctx, share); err != nil {
				cp.logger.Warn("pattern share failed",
					"target", targetNode,
					"capsule", c.ID,
					"error", err,
				)
				result.Errors = append(result.Errors, fmt.Sprintf("%s/%s: %v", targetNode, c.ID, err))
				continue
			}
			result.CapsulesSent++
		}
		result.TargetsReached++
	}

	result.Duration = time.Since(start)
	cp.logger.Info("capsule propagation complete",
		"sent", result.CapsulesSent,
		"targets", result.TargetsReached,
	)
	return result, nil
}
