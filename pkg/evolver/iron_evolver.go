package evolver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// IronEvolverConfig configures the meta-agent orchestrator.
type IronEvolverConfig struct {
	AutoEvolve     bool
	EvolveInterval time.Duration
	MaxMutations   int
	DryRun         bool
	SafetyChecks   bool
	Logger         *slog.Logger
}

// DefaultIronEvolverConfig returns production defaults with guardrails enabled.
// DryRun is false so mutations actually apply, but SafetyChecks blocks
// high/critical-risk mutations and MaxMutations caps applied count to 1.
func DefaultIronEvolverConfig() IronEvolverConfig {
	return IronEvolverConfig{
		AutoEvolve:     false,
		EvolveInterval: 10 * time.Minute,
		MaxMutations:   1,
		DryRun:         false,
		SafetyChecks:   true,
		Logger:         slog.Default(),
	}
}

// IronEvolverStats tracks meta-agent execution statistics.
type IronEvolverStats struct {
	CyclesRun         int           `json:"cycles_run"`
	SignalsFound      int           `json:"signals_found"`
	MutationsCreated  int           `json:"mutations_created"`
	MutationsApplied  int           `json:"mutations_applied"`
	LastCycleAt       time.Time     `json:"last_cycle_at"`
	LastCycleDuration time.Duration `json:"last_cycle_duration_ms"`
	Errors            int           `json:"errors"`
}

// IronEvolver is the meta-agent that orchestrates the full
// trace → signal → mutation → evaluate → promote pipeline.
type IronEvolver struct {
	cfg      IronEvolverConfig
	bridge   *TraceBridge
	engine   *EvolutionEngine
	pipeline *PromotionPipeline
	sandbox  *EvolutionSandbox
	mu       sync.Mutex
	stats    IronEvolverStats
	stopCh   chan struct{}
	running  bool
}

// NewIronEvolver creates the meta-agent orchestrator.
func NewIronEvolver(
	cfg IronEvolverConfig,
	bridge *TraceBridge,
	engine *EvolutionEngine,
	pipeline *PromotionPipeline,
	sandbox *EvolutionSandbox,
) *IronEvolver {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &IronEvolver{
		cfg:      cfg,
		bridge:   bridge,
		engine:   engine,
		pipeline: pipeline,
		sandbox:  sandbox,
		stopCh:   make(chan struct{}),
	}
}

// RunCycle executes one full evolution cycle: mine → evolve → evaluate → promote.
func (ie *IronEvolver) RunCycle(ctx context.Context) error {
	start := time.Now()
	ie.mu.Lock()
	ie.stats.CyclesRun++
	ie.mu.Unlock()

	ie.cfg.Logger.Info("evolution cycle started", "cycle", ie.stats.CyclesRun)

	signals, err := ie.bridge.MineSignals()
	if err != nil {
		ie.recordError()
		return fmt.Errorf("mine signals: %w", err)
	}

	ie.mu.Lock()
	ie.stats.SignalsFound += len(signals)
	ie.mu.Unlock()

	if len(signals) == 0 {
		ie.cfg.Logger.Info("no signals found, cycle complete")
		ie.recordCycleDuration(start)
		return nil
	}

	mutations, err := ie.engine.Evolve(ctx, signals)
	if err != nil {
		ie.recordError()
		return fmt.Errorf("evolve: %w", err)
	}

	ie.mu.Lock()
	ie.stats.MutationsCreated += len(mutations)
	ie.mu.Unlock()

	if ie.cfg.DryRun {
		ie.cfg.Logger.Info("dry run: skipping mutation application",
			"signals", len(signals), "mutations", len(mutations))
		ie.recordCycleDuration(start)
		return nil
	}

	applied := 0
	limit := ie.cfg.MaxMutations
	if limit <= 0 {
		limit = len(mutations)
	}

	for i, mut := range mutations {
		if applied >= limit {
			ie.cfg.Logger.Info("max apply per cycle reached", "limit", limit, "applied", applied)
			break
		}
		if i >= len(mutations) {
			break
		}

		if ie.cfg.SafetyChecks && isHighRisk(mut.RiskEstimate) {
			ie.cfg.Logger.Warn("safety check: skipping high-risk mutation",
				"mutation", mut.ID,
				"risk", mut.RiskEstimate,
				"signal", mut.SignalID,
			)
			continue
		}

		sig := ie.findSignal(signals, mut.SignalID)
		if sig == nil {
			continue
		}

		ie.cfg.Logger.Info("applying mutation",
			"mutation", mut.ID,
			"risk", mut.RiskEstimate,
			"signal", mut.SignalID,
			"reasoning", truncateReason(mut.Reasoning, 120),
		)

		rec, err := ie.pipeline.Submit(ctx, mut, *sig)
		if err != nil {
			ie.cfg.Logger.Warn("pipeline submit failed", "mutation", mut.ID, "err", err)
			continue
		}

		if rec.Status == PromotionEvaluated && rec.Evaluation.Pass {
			if err := ie.pipeline.Approve(mut.ID, "iron-evolver", "auto-approved by meta-agent"); err != nil {
				ie.cfg.Logger.Warn("auto-approve failed", "mutation", mut.ID, "err", err)
				continue
			}
			applied++
			ie.cfg.Logger.Info("mutation applied successfully",
				"mutation", mut.ID,
				"applied_count", applied,
				"limit", limit,
			)
		}
	}

	ie.mu.Lock()
	ie.stats.MutationsApplied += applied
	ie.mu.Unlock()

	ie.cfg.Logger.Info("evolution cycle complete",
		"signals", len(signals),
		"mutations", len(mutations),
		"applied", applied,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	ie.recordCycleDuration(start)
	return nil
}

func (ie *IronEvolver) findSignal(signals []Signal, id string) *Signal {
	for i := range signals {
		if signals[i].ID == id {
			return &signals[i]
		}
	}
	return nil
}

func (ie *IronEvolver) recordError() {
	ie.mu.Lock()
	ie.stats.Errors++
	ie.mu.Unlock()
}

func (ie *IronEvolver) recordCycleDuration(start time.Time) {
	ie.mu.Lock()
	ie.stats.LastCycleAt = time.Now()
	ie.stats.LastCycleDuration = time.Since(start)
	ie.mu.Unlock()
}

// Start begins the auto-evolve background loop.
func (ie *IronEvolver) Start(ctx context.Context) {
	if !ie.cfg.AutoEvolve || ie.cfg.EvolveInterval <= 0 {
		return
	}

	ie.mu.Lock()
	if ie.running {
		ie.mu.Unlock()
		return
	}
	ie.running = true
	ie.mu.Unlock()

	go func() {
		ticker := time.NewTicker(ie.cfg.EvolveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ie.RunCycle(ctx); err != nil {
					ie.cfg.Logger.Warn("auto-evolve cycle failed", "err", err)
				}
			case <-ie.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the auto-evolve loop.
func (ie *IronEvolver) Stop() {
	ie.mu.Lock()
	defer ie.mu.Unlock()
	if ie.running {
		close(ie.stopCh)
		ie.running = false
	}
}

func isHighRisk(risk RiskLevel) bool {
	return risk == RiskHigh || risk == RiskCritical
}

func truncateReason(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Stats returns current execution statistics.
func (ie *IronEvolver) Stats() IronEvolverStats {
	ie.mu.Lock()
	defer ie.mu.Unlock()
	return ie.stats
}
