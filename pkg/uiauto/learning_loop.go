package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/evolver"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/bandits"
)

// LearningLoop connects SelfEvaluator → SignalMiner → EvolutionEngine to
// create a closed-loop self-improvement cycle. Each tick:
//  1. Evaluates current agent effectiveness
//  2. Converts low scores to evolution signals
//  3. Runs the engine to produce mutations
//  4. Feeds results back to PatternTracker via confidence adjustments
//  5. Persists bandit arm statistics for cross-run continuity
type LearningLoop struct {
	mu             sync.Mutex
	agent          *MemberAgent
	evaluator      *SelfEvaluator
	miner          *evolver.SignalMiner
	engine         *evolver.EvolutionEngine
	traceBridge    *evolver.TraceBridge
	bandit         *bandits.ContextualBandit
	banditStateDir string
	logger         *slog.Logger
	history        []LoopIteration
	maxHist        int
}

// LoopIteration records one cycle of the learning loop.
type LoopIteration struct {
	Timestamp time.Time          `json:"timestamp"`
	Score     EffectivenessScore `json:"score"`
	Signals   []evolver.Signal   `json:"signals"`
	Mutations []evolver.Mutation `json:"mutations"`
	Duration  time.Duration      `json:"duration_ns"`
}

// LearningLoopConfig controls loop behavior.
type LearningLoopConfig struct {
	Agent          *MemberAgent
	Evaluator      *SelfEvaluator
	Miner          *evolver.SignalMiner
	Engine         *evolver.EvolutionEngine
	TraceBridge    *evolver.TraceBridge // optional: feeds traces to the unified selfimprove engine
	Bandit         *bandits.ContextualBandit
	BanditStateDir string // directory for bandit state persistence; empty disables
	Logger         *slog.Logger
	MaxHist        int
}

// NewLearningLoop creates a connected learning loop.
func NewLearningLoop(cfg LearningLoopConfig) *LearningLoop {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.MaxHist <= 0 {
		cfg.MaxHist = 100
	}
	return &LearningLoop{
		agent:          cfg.Agent,
		evaluator:      cfg.Evaluator,
		miner:          cfg.Miner,
		engine:         cfg.Engine,
		traceBridge:    cfg.TraceBridge,
		bandit:         cfg.Bandit,
		banditStateDir: cfg.BanditStateDir,
		logger:         cfg.Logger,
		maxHist:        cfg.MaxHist,
	}
}

// Tick runs one iteration of the learning loop.
func (l *LearningLoop) Tick(ctx context.Context) (LoopIteration, error) {
	start := time.Now()
	iter := LoopIteration{Timestamp: start}

	// Step 1: Evaluate current effectiveness
	score := l.evaluator.Evaluate()
	iter.Score = score

	// Step 2: Convert metrics to traces for signal mining
	traces := l.metricsToTraces(score)

	// Step 2.5: Forward traces to the unified selfimprove engine if bridge is configured
	if l.traceBridge != nil {
		for _, trace := range traces {
			meta := map[string]string{"source": "learning_loop"}
			if err := l.traceBridge.RecordUIAutoTrace(
				trace.TaskName, trace.Success, trace.LatencyMs, trace.ToolsCalled, meta,
			); err != nil {
				l.logger.Warn("trace bridge record failed", "error", err)
			}
		}
	}

	// Step 3: Mine signals from traces
	signals := l.miner.Mine(traces)
	iter.Signals = signals

	// Step 4: Feed signals to evolution engine
	if len(signals) > 0 {
		mutations, err := l.engine.Evolve(ctx, signals)
		if err != nil {
			l.logger.Warn("evolution engine error", "error", err)
		} else {
			iter.Mutations = mutations
		}
	}

	// Step 5: Feedback to pattern tracker via evaluator
	l.evaluator.FeedbackToTracker(ctx)

	// Step 6: Persist bandit arm statistics for cross-run continuity
	if l.bandit != nil && l.banditStateDir != "" {
		if err := l.persistBanditState(); err != nil {
			l.logger.Warn("bandit state persistence failed", "error", err)
		}
	}

	iter.Duration = time.Since(start)

	l.mu.Lock()
	l.history = append(l.history, iter)
	if len(l.history) > l.maxHist {
		l.history = l.history[len(l.history)-l.maxHist:]
	}
	l.mu.Unlock()

	l.logger.Info("learning loop tick",
		"overall_score", score.OverallScore,
		"signals", len(signals),
		"mutations", len(iter.Mutations),
		"duration", iter.Duration,
	)

	return iter, nil
}

// metricsToTraces converts effectiveness scores to execution traces for mining.
func (l *LearningLoop) metricsToTraces(score EffectivenessScore) []evolver.ExecutionTrace {
	var traces []evolver.ExecutionTrace
	now := time.Now()

	agg := l.agent.Metrics()

	for i := int64(0); i < agg.Executor.FailedActions; i++ {
		traces = append(traces, evolver.ExecutionTrace{
			ID:        fmt.Sprintf("loop-fail-%d", i),
			TaskName:  "ui_action",
			StartTime: now.Add(-time.Minute),
			EndTime:   now,
			Success:   false,
			ErrorMsg:  "action execution failed",
		})
	}

	for i := int64(0); i < agg.Executor.SuccessActions; i++ {
		latency := agg.Executor.AvgLatencyMs
		traces = append(traces, evolver.ExecutionTrace{
			ID:        fmt.Sprintf("loop-ok-%d", i),
			TaskName:  "ui_action",
			StartTime: now.Add(-time.Minute),
			EndTime:   now,
			LatencyMs: float64(latency),
			Success:   true,
		})
	}

	return traces
}

// History returns the recent iterations.
func (l *LearningLoop) History(n int) []LoopIteration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n > len(l.history) {
		n = len(l.history)
	}
	out := make([]LoopIteration, n)
	copy(out, l.history[len(l.history)-n:])
	return out
}

// --- Bandit State Persistence ---

// BanditSnapshot captures the serialisable state of all bandit posteriors.
type BanditSnapshot struct {
	Timestamp  time.Time                                         `json:"timestamp"`
	TotalPulls int64                                             `json:"total_pulls"`
	Contexts   map[string][bandits.NumArms]bandits.BetaPosterior `json:"contexts"`
}

func (l *LearningLoop) persistBanditState() error {
	if err := os.MkdirAll(l.banditStateDir, 0o755); err != nil {
		return fmt.Errorf("create bandit state dir: %w", err)
	}

	snapshot := BanditSnapshot{
		Timestamp:  time.Now().UTC(),
		TotalPulls: l.bandit.TotalPulls(),
		Contexts:   make(map[string][bandits.NumArms]bandits.BetaPosterior),
	}

	for _, key := range []string{"low:stable", "low:unstable", "mid:stable", "mid:unstable", "high:stable", "high:unstable"} {
		f := bandits.Features{}
		switch {
		case key == "low:stable":
			f = bandits.Features{PageComplexity: 0.1, HasDataTestID: true}
		case key == "low:unstable":
			f = bandits.Features{PageComplexity: 0.1, MutationIntensity: 0.5}
		case key == "mid:stable":
			f = bandits.Features{PageComplexity: 0.4, HasDataTestID: true}
		case key == "mid:unstable":
			f = bandits.Features{PageComplexity: 0.4, MutationIntensity: 0.5}
		case key == "high:stable":
			f = bandits.Features{PageComplexity: 0.7, HasDataTestID: true}
		case key == "high:unstable":
			f = bandits.Features{PageComplexity: 0.7, MutationIntensity: 0.5}
		}
		stats := l.bandit.Stats(f)
		if stats[0].Trials > 0 || stats[1].Trials > 0 || stats[2].Trials > 0 {
			snapshot.Contexts[key] = stats
		}
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bandit state: %w", err)
	}

	path := filepath.Join(l.banditStateDir, "bandit-state.json")
	return os.WriteFile(path, data, 0o644)
}

// --- Fleet Pattern Sharing ---

// PatternExport is a serializable snapshot of patterns for fleet sharing.
type PatternExport struct {
	AgentID    string            `json:"agent_id"`
	ExportedAt time.Time         `json:"exported_at"`
	Patterns   []UIPattern       `json:"patterns"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ExportPatterns serializes the agent's learned patterns for sharing.
func ExportPatterns(agentID string, store PatternStorage, ctx context.Context) (*PatternExport, error) {
	patterns, err := store.Load(ctx)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load patterns: %w", err)
	}
	if patterns == nil {
		patterns = make(map[string]UIPattern)
	}

	export := &PatternExport{
		AgentID:    agentID,
		ExportedAt: time.Now().UTC(),
		Metadata:   map[string]string{"version": "2.0"},
	}
	for _, p := range patterns {
		export.Patterns = append(export.Patterns, p)
	}
	return export, nil
}

// SavePatternExport writes a pattern export to a JSON file.
func SavePatternExport(export *PatternExport, path string) error {
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal export: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadPatternExport reads a pattern export from a JSON file.
func LoadPatternExport(path string) (*PatternExport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read export: %w", err)
	}
	var export PatternExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("unmarshal export: %w", err)
	}
	return &export, nil
}

// ImportPatterns merges imported patterns into the local store, preferring
// higher-confidence patterns when IDs collide.
func ImportPatterns(store PatternStorage, imported *PatternExport, ctx context.Context) (int, error) {
	existing, err := store.Load(ctx)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("load existing: %w", err)
	}
	if existing == nil {
		existing = make(map[string]UIPattern)
	}

	var merged int
	for _, p := range imported.Patterns {
		local, exists := existing[p.ID]
		if exists && local.Confidence >= p.Confidence {
			continue
		}
		if err := store.Set(ctx, p); err != nil {
			return merged, fmt.Errorf("set pattern %s: %w", p.ID, err)
		}
		merged++
	}
	return merged, nil
}

// --- KPI Framework ---

// AgentKPI defines a key performance indicator with target and alert thresholds.
type AgentKPI struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Current     float64 `json:"current"`
	Target      float64 `json:"target"`
	AlertBelow  float64 `json:"alert_below"`
	Unit        string  `json:"unit"`
}

// KPIFramework tracks and evaluates agent performance against targets.
type KPIFramework struct {
	mu   sync.Mutex
	kpis map[string]AgentKPI
}

// NewKPIFramework creates a framework with default agent KPIs.
func NewKPIFramework() *KPIFramework {
	kf := &KPIFramework{kpis: make(map[string]AgentKPI)}

	defaults := []AgentKPI{
		{Name: "action_success_rate", Description: "Percentage of UI actions that succeed", Target: 0.95, AlertBelow: 0.80, Unit: "ratio"},
		{Name: "cache_hit_rate", Description: "Pattern cache hit rate", Target: 0.70, AlertBelow: 0.40, Unit: "ratio"},
		{Name: "heal_success_rate", Description: "Self-healing success rate", Target: 0.80, AlertBelow: 0.50, Unit: "ratio"},
		{Name: "cost_per_action", Description: "Average LLM cost per action", Target: 0.001, AlertBelow: 0, Unit: "usd"},
		{Name: "overall_score", Description: "Composite effectiveness score", Target: 0.80, AlertBelow: 0.50, Unit: "ratio"},
		{Name: "heal_frequency", Description: "Healing events per hour (lower is better)", Target: 2.0, AlertBelow: 0, Unit: "per_hour"},
	}
	for _, kpi := range defaults {
		kf.kpis[kpi.Name] = kpi
	}
	return kf
}

// UpdateFromScore updates KPIs from an EffectivenessScore.
func (kf *KPIFramework) UpdateFromScore(score EffectivenessScore) {
	kf.mu.Lock()
	defer kf.mu.Unlock()

	kf.setKPI("action_success_rate", score.ActionSuccessRate)
	kf.setKPI("cache_hit_rate", score.CacheHitRate)
	kf.setKPI("heal_success_rate", score.HealSuccessRate)
	kf.setKPI("cost_per_action", score.EstimatedCostUSD)
	kf.setKPI("overall_score", score.OverallScore)
	kf.setKPI("heal_frequency", score.HealFrequency)
}

func (kf *KPIFramework) setKPI(name string, value float64) {
	if kpi, ok := kf.kpis[name]; ok {
		kpi.Current = value
		kf.kpis[name] = kpi
	}
}

// GetKPI returns a specific KPI.
func (kf *KPIFramework) GetKPI(name string) (AgentKPI, bool) {
	kf.mu.Lock()
	defer kf.mu.Unlock()
	kpi, ok := kf.kpis[name]
	return kpi, ok
}

// AllKPIs returns all tracked KPIs.
func (kf *KPIFramework) AllKPIs() []AgentKPI {
	kf.mu.Lock()
	defer kf.mu.Unlock()
	var result []AgentKPI
	for _, kpi := range kf.kpis {
		result = append(result, kpi)
	}
	return result
}

// Alerts returns KPIs that are below their alert threshold.
func (kf *KPIFramework) Alerts() []AgentKPI {
	kf.mu.Lock()
	defer kf.mu.Unlock()
	var alerts []AgentKPI
	for _, kpi := range kf.kpis {
		if kpi.AlertBelow > 0 && kpi.Current < kpi.AlertBelow {
			alerts = append(alerts, kpi)
		}
	}
	return alerts
}

// OnTarget returns true if all KPIs meet their targets.
func (kf *KPIFramework) OnTarget() bool {
	kf.mu.Lock()
	defer kf.mu.Unlock()
	for _, kpi := range kf.kpis {
		lowerIsBetter := kpi.Unit == "per_hour" || kpi.Unit == "usd"
		if lowerIsBetter {
			if kpi.Current > kpi.Target {
				return false
			}
		} else {
			if kpi.Current < kpi.Target {
				return false
			}
		}
	}
	return true
}
