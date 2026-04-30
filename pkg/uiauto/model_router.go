package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/bandits"
)

// ModelTier represents a model capability tier.
type ModelTier int

// Model tiers for multi-tier routing (light -> smart -> VLM).
// WSL tiers map to local GPU models on a 2x RTX 3090 fleet.
const (
	TierLight ModelTier = iota
	TierSmart
	TierVLM
	ModelTierWSLFast
	ModelTierWSLSmart
	ModelTierWSLPowerful
)

func (t ModelTier) String() string {
	switch t {
	case TierLight:
		return "light"
	case TierSmart:
		return "smart"
	case TierVLM:
		return "vlm"
	case ModelTierWSLFast:
		return "wsl-fast"
	case ModelTierWSLSmart:
		return "wsl-smart"
	case ModelTierWSLPowerful:
		return "wsl-powerful"
	default:
		return "unknown"
	}
}

// IsWSLTier returns true if this tier maps to a local WSL GPU model.
func (t ModelTier) IsWSLTier() bool {
	return t == ModelTierWSLFast || t == ModelTierWSLSmart || t == ModelTierWSLPowerful
}

// ToWSLTier maps a standard tier to its WSL equivalent.
func ToWSLTier(t ModelTier) ModelTier {
	switch t {
	case TierLight:
		return ModelTierWSLFast
	case TierSmart:
		return ModelTierWSLSmart
	case TierVLM:
		return ModelTierWSLPowerful
	default:
		return t
	}
}

// RouterMetrics tracks model routing statistics.
type RouterMetrics struct {
	LightAttempts  int64
	LightSuccesses int64
	SmartAttempts  int64
	SmartSuccesses int64
	VLMAttempts    int64
	VLMSuccesses   int64
	Promotions     int64
	Demotions      int64
	TotalLatencyMs int64
	ActionCount    int64
}

// Snapshot returns a copy of current metrics.
func (m *RouterMetrics) Snapshot() RouterMetrics {
	return RouterMetrics{
		LightAttempts:  atomic.LoadInt64(&m.LightAttempts),
		LightSuccesses: atomic.LoadInt64(&m.LightSuccesses),
		SmartAttempts:  atomic.LoadInt64(&m.SmartAttempts),
		SmartSuccesses: atomic.LoadInt64(&m.SmartSuccesses),
		VLMAttempts:    atomic.LoadInt64(&m.VLMAttempts),
		VLMSuccesses:   atomic.LoadInt64(&m.VLMSuccesses),
		Promotions:     atomic.LoadInt64(&m.Promotions),
		Demotions:      atomic.LoadInt64(&m.Demotions),
		TotalLatencyMs: atomic.LoadInt64(&m.TotalLatencyMs),
		ActionCount:    atomic.LoadInt64(&m.ActionCount),
	}
}

// LightSuccessRate returns the light model success rate [0.0, 1.0].
func (m *RouterMetrics) LightSuccessRate() float64 {
	att := atomic.LoadInt64(&m.LightAttempts)
	if att == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&m.LightSuccesses)) / float64(att)
}

// ConvergenceState tracks whether the pattern set has converged (light succeeds consistently).
type ConvergenceState struct {
	mu              sync.RWMutex
	consecutiveHits int
	converged       bool
	threshold       int
}

func newConvergenceState(threshold int) *ConvergenceState {
	if threshold <= 0 {
		threshold = 5
	}
	return &ConvergenceState{threshold: threshold}
}

// RecordHit increments the consecutive hit counter and may mark convergence.
func (c *ConvergenceState) RecordHit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveHits++
	if c.consecutiveHits >= c.threshold {
		c.converged = true
	}
}

// RecordMiss resets the consecutive hit counter and clears convergence.
func (c *ConvergenceState) RecordMiss() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveHits = 0
	c.converged = false
}

// IsConverged returns true if consecutive hits have reached the threshold.
func (c *ConvergenceState) IsConverged() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.converged
}

// ConsecutiveHits returns the current consecutive hit count.
func (c *ConvergenceState) ConsecutiveHits() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.consecutiveHits
}

// PatternPhase represents the current phase of the pattern learning state machine.
type PatternPhase int

// Pattern learning phases for the state machine.
const (
	PhaseDiscovery PatternPhase = iota
	PhaseCruise
	PhaseEscalation
)

func (p PatternPhase) String() string {
	switch p {
	case PhaseDiscovery:
		return "discovery"
	case PhaseCruise:
		return "cruise"
	case PhaseEscalation:
		return "escalation"
	default:
		return "unknown"
	}
}

// PhaseTransition records a phase change for observability.
type PhaseTransition struct {
	From      PatternPhase
	To        PatternPhase
	Reason    string
	Timestamp time.Time
}

// PhaseStats holds phase duration and transition statistics.
type PhaseStats struct {
	PhaseDurations   map[PatternPhase]time.Duration
	TransitionCount  int
	DiscoveryEntries int64
	StableEntries    int64
	EscalationCount  int64
}

// PhaseTracker tracks pattern phase state and transitions.
type PhaseTracker struct {
	mu                   sync.RWMutex
	currentPhase         PatternPhase
	phaseHistory         []PhaseTransition
	phaseEnteredAt       map[PatternPhase]time.Time
	discoveryThreshold   int
	cruiseThreshold      int
	escalationTrigger    int
	consecutiveSmart     int
	consecutiveLightFail int
	consecutiveLightOk   int
	lastLightFailed      bool
	discoveryEntries     int64
	stableEntries        int64
	escalationCount      int64
}

// NewPhaseTracker creates a PhaseTracker with the given thresholds.
func NewPhaseTracker(discoveryThreshold, cruiseThreshold, escalationTrigger int) *PhaseTracker {
	if discoveryThreshold <= 0 {
		discoveryThreshold = 3
	}
	if cruiseThreshold <= 0 {
		cruiseThreshold = 5
	}
	if escalationTrigger <= 0 {
		escalationTrigger = 3
	}
	return &PhaseTracker{
		currentPhase:       PhaseDiscovery,
		phaseHistory:       nil,
		phaseEnteredAt:     make(map[PatternPhase]time.Time),
		discoveryThreshold: discoveryThreshold,
		cruiseThreshold:    cruiseThreshold,
		escalationTrigger:  escalationTrigger,
	}
}

// CurrentPhase returns the current pattern phase.
func (pt *PhaseTracker) CurrentPhase() PatternPhase {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.currentPhase
}

func (pt *PhaseTracker) transition(to PatternPhase, reason string) {
	now := time.Now()
	from := pt.currentPhase
	pt.phaseHistory = append(pt.phaseHistory, PhaseTransition{From: from, To: to, Reason: reason, Timestamp: now})
	pt.currentPhase = to
	pt.phaseEnteredAt[to] = now
	switch to {
	case PhaseDiscovery:
		pt.discoveryEntries++
	case PhaseCruise:
		pt.stableEntries++
	case PhaseEscalation:
		pt.escalationCount++
	}
}

// RecordSuccess records a successful execution and may transition phases.
func (pt *PhaseTracker) RecordSuccess(tier ModelTier) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	switch tier {
	case TierLight:
		pt.consecutiveLightFail = 0
		pt.consecutiveLightOk++
		pt.lastLightFailed = false
	case TierSmart, TierVLM:
		pt.consecutiveLightFail = 0
		pt.consecutiveLightOk = 0
		switch {
		case pt.currentPhase == PhaseEscalation:
			pt.transition(PhaseDiscovery, "smart_or_vlm_success_after_escalation")
			pt.consecutiveSmart = 0
		case pt.currentPhase == PhaseCruise && pt.lastLightFailed:
			pt.transition(PhaseDiscovery, "smart_success_after_light_failure_relearning")
			pt.consecutiveSmart = 0
		case tier == TierSmart:
			pt.consecutiveSmart++
			pt.lastLightFailed = false
			if pt.currentPhase == PhaseDiscovery && pt.consecutiveSmart >= pt.discoveryThreshold {
				pt.transition(PhaseCruise, "discovery_threshold_smart_successes")
			}
		}
	}
}

// RecordFailure records a failed execution and may escalate.
func (pt *PhaseTracker) RecordFailure(tier ModelTier) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	switch tier {
	case TierLight:
		pt.consecutiveLightFail++
		pt.consecutiveLightOk = 0
		pt.consecutiveSmart = 0
		pt.lastLightFailed = true
		if pt.currentPhase == PhaseCruise && pt.consecutiveLightFail >= pt.escalationTrigger {
			pt.transition(PhaseEscalation, "cruise_light_failures_escalation")
		}
	case TierSmart, TierVLM:
		pt.consecutiveSmart = 0
		pt.consecutiveLightFail++
		pt.lastLightFailed = true
	}
}

// History returns a copy of phase transition history.
func (pt *PhaseTracker) History() []PhaseTransition {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	if len(pt.phaseHistory) == 0 {
		return nil
	}
	out := make([]PhaseTransition, len(pt.phaseHistory))
	copy(out, pt.phaseHistory)
	return out
}

// Stats returns phase statistics including durations and transition counts.
func (pt *PhaseTracker) Stats() PhaseStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	durations := make(map[PatternPhase]time.Duration)
	now := time.Now()
	for phase, entered := range pt.phaseEnteredAt {
		durations[phase] = now.Sub(entered)
	}
	if len(pt.phaseHistory) > 0 {
		last := pt.phaseHistory[len(pt.phaseHistory)-1]
		durations[pt.currentPhase] = now.Sub(last.Timestamp)
	}

	return PhaseStats{
		PhaseDurations:   durations,
		TransitionCount:  len(pt.phaseHistory),
		DiscoveryEntries: pt.discoveryEntries,
		StableEntries:    pt.stableEntries,
		EscalationCount:  pt.escalationCount,
	}
}

// ModelRouter coordinates between light execution, smart discovery, and VLM verification.
// When a ContextualBandit is configured, tier selection uses Thompson Sampling
// instead of the fixed light-first cascade. The bandit learns which tier succeeds
// for each context (page complexity, selector stability, iframe depth) and
// balances exploration vs exploitation with cost-weighted scoring.
type ModelRouter struct {
	lightExecutor      *LightExecutor
	smartDiscoverer    *SmartDiscoverer
	vlmBridge          *VLMBridge
	tracker            *PatternTracker
	browser            *BrowserAgent
	breaker            *domheal.CircuitBreaker
	convergence        *ConvergenceState
	phaseTracker       *PhaseTracker
	bandit             *bandits.ContextualBandit
	Metrics            *RouterMetrics
	logger             *slog.Logger
	currentTier        ModelTier
	tierMu             sync.RWMutex
	promotionThreshold float64
	demotionThreshold  float64
}

// RouterOption configures ModelRouter behavior.
type RouterOption func(*ModelRouter)

// WithVLMBridge attaches a VLM fallback layer.
func WithVLMBridge(vlm *VLMBridge) RouterOption {
	return func(r *ModelRouter) { r.vlmBridge = vlm }
}

// WithConvergenceThreshold sets how many consecutive light successes trigger convergence.
func WithConvergenceThreshold(n int) RouterOption {
	return func(r *ModelRouter) { r.convergence = newConvergenceState(n) }
}

// WithPhaseThresholds configures the PhaseTracker with discovery, cruise, and escalation thresholds.
func WithPhaseThresholds(discoveryThreshold, cruiseThreshold, escalationTrigger int) RouterOption {
	return func(r *ModelRouter) {
		r.phaseTracker = NewPhaseTracker(discoveryThreshold, cruiseThreshold, escalationTrigger)
	}
}

// WithRouterLogger sets the router logger.
func WithRouterLogger(l *slog.Logger) RouterOption {
	return func(r *ModelRouter) { r.logger = l }
}

// WithBandit enables Thompson Sampling for tier selection.
// When set, ExecuteAction consults the bandit instead of always trying light first.
// The fixed cascade remains as a fallback during warmup or when the circuit breaker is open.
func WithBandit(b *bandits.ContextualBandit) RouterOption {
	return func(r *ModelRouter) { r.bandit = b }
}

// NewModelRouter creates a new ModelRouter.
func NewModelRouter(light *LightExecutor, smart *SmartDiscoverer, tracker *PatternTracker, browser *BrowserAgent, opts ...RouterOption) *ModelRouter {
	r := &ModelRouter{
		lightExecutor:      light,
		smartDiscoverer:    smart,
		tracker:            tracker,
		browser:            browser,
		breaker:            domheal.NewCircuitBreaker(3, 60),
		convergence:        newConvergenceState(5),
		Metrics:            &RouterMetrics{},
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		currentTier:        TierLight,
		promotionThreshold: 0.9,
		demotionThreshold:  0.5,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// CurrentTier returns the current model tier.
func (r *ModelRouter) CurrentTier() ModelTier {
	r.tierMu.RLock()
	defer r.tierMu.RUnlock()
	return r.currentTier
}

// IsConverged returns whether patterns have converged (light path consistently succeeds).
func (r *ModelRouter) IsConverged() bool {
	return r.convergence.IsConverged()
}

// PhaseTracker returns the phase tracker if configured, or nil.
func (r *ModelRouter) PhaseTracker() *PhaseTracker {
	return r.phaseTracker
}

// actionFeatures extracts bandit context features from the current action and page state.
func (r *ModelRouter) actionFeatures(action Action) bandits.Features {
	hasTestID := action.TargetID != ""
	failCount := 0
	if !r.breaker.Allow() {
		failCount = 3
	}
	return bandits.Features{
		SelectorCount:    1,
		HasDataTestID:    hasTestID,
		PreviousFailures: failCount,
	}
}

// armToTier maps a bandit arm to a ModelTier.
func armToTier(arm bandits.Arm) ModelTier {
	switch arm {
	case bandits.ArmSmart:
		return TierSmart
	case bandits.ArmVLM:
		return TierVLM
	default:
		return TierLight
	}
}

// tierToArm maps a ModelTier to a bandit arm.
func tierToArm(tier ModelTier) bandits.Arm {
	switch tier {
	case TierSmart:
		return bandits.ArmSmart
	case TierVLM:
		return bandits.ArmVLM
	default:
		return bandits.ArmLight
	}
}

// Bandit returns the configured bandit, or nil if not set.
func (r *ModelRouter) Bandit() *bandits.ContextualBandit { return r.bandit }

// ExecuteAction attempts to execute an action, escalating through tiers as needed.
// When a bandit is configured, it selects the starting tier via Thompson Sampling
// rather than always trying light first. The fixed cascade remains as a fallback.
func (r *ModelRouter) ExecuteAction(ctx context.Context, action Action) error {
	start := time.Now()
	defer func() {
		atomic.AddInt64(&r.Metrics.TotalLatencyMs, int64(time.Since(start)/time.Millisecond))
		atomic.AddInt64(&r.Metrics.ActionCount, 1)
	}()

	if r.bandit != nil {
		return r.banditGuidedExecution(ctx, action)
	}
	return r.cascadeExecution(ctx, action)
}

// banditGuidedExecution uses Thompson Sampling to select the starting tier.
func (r *ModelRouter) banditGuidedExecution(ctx context.Context, action Action) error {
	features := r.actionFeatures(action)
	arm := r.bandit.SelectArm(features)
	tier := armToTier(arm)

	r.logger.Debug("bandit selected tier",
		"target", action.TargetID,
		"arm", arm.String(),
		"tier", tier.String())

	var err error
	switch tier {
	case TierVLM:
		if r.vlmBridge != nil {
			err = r.vlmPath(ctx, action)
		} else {
			err = r.smartPath(ctx, action)
		}
	case TierSmart:
		err = r.smartPath(ctx, action)
	default:
		err = r.cascadeExecution(ctx, action)
	}

	reward := 1.0
	if err != nil {
		reward = 0.0
	}
	r.bandit.Update(features, arm, reward)

	if r.phaseTracker != nil {
		if err == nil {
			r.phaseTracker.RecordSuccess(tier)
		} else {
			r.phaseTracker.RecordFailure(tier)
		}
	}

	return err
}

// cascadeExecution is the original fixed light-first cascade.
func (r *ModelRouter) cascadeExecution(ctx context.Context, action Action) error {
	if r.breaker.Allow() {
		atomic.AddInt64(&r.Metrics.LightAttempts, 1)
		err := r.lightExecutor.Execute(ctx, action)
		if err == nil {
			atomic.AddInt64(&r.Metrics.LightSuccesses, 1)
			r.breaker.RecordSuccess()
			r.convergence.RecordHit()
			if r.phaseTracker != nil {
				r.phaseTracker.RecordSuccess(TierLight)
			}
			r.checkDemotion()
			return nil
		}
		r.breaker.RecordFailure()
		r.convergence.RecordMiss()
		if r.phaseTracker != nil {
			r.phaseTracker.RecordFailure(TierLight)
		}
		r.logger.Debug("light execution failed, escalating to smart",
			"target", action.TargetID, "error", err)
	}

	return r.smartPath(ctx, action)
}

func (r *ModelRouter) smartPath(ctx context.Context, action Action) error {
	atomic.AddInt64(&r.Metrics.SmartAttempts, 1)

	html, err := r.browser.CaptureDOM()
	if err != nil {
		return fmt.Errorf("failed to capture DOM for smart discovery: %w", err)
	}

	var newSelector string
	if action.Type == "evaluate" {
		newSelector, err = r.smartDiscoverer.DiscoverScript(ctx, action.Description, html)
	} else {
		newSelector, err = r.smartDiscoverer.DiscoverSelector(ctx, action.Description, html)
	}
	if err != nil {
		if r.phaseTracker != nil {
			r.phaseTracker.RecordFailure(TierSmart)
		}
		// If smart fails and we have VLM, try VLM
		if r.vlmBridge != nil {
			return r.vlmPath(ctx, action)
		}
		return fmt.Errorf("smart discovery failed: %w", err)
	}

	err = r.tracker.RegisterPattern(ctx, action.TargetID, newSelector, action.Description, html)
	if err != nil {
		return fmt.Errorf("failed to register new pattern: %w", err)
	}

	err = r.lightExecutor.Execute(ctx, action)
	if err != nil {
		if r.phaseTracker != nil {
			r.phaseTracker.RecordFailure(TierLight)
		}
		if r.vlmBridge != nil {
			return r.vlmPath(ctx, action)
		}
		return fmt.Errorf("action failed even after smart discovery: %w", err)
	}

	atomic.AddInt64(&r.Metrics.SmartSuccesses, 1)
	if r.phaseTracker != nil {
		r.phaseTracker.RecordSuccess(TierSmart)
	}
	r.checkPromotion()
	return nil
}

func (r *ModelRouter) vlmPath(ctx context.Context, action Action) error {
	atomic.AddInt64(&r.Metrics.VLMAttempts, 1)

	screenshot, err := r.browser.CaptureScreenshot()
	if err != nil {
		return fmt.Errorf("failed to capture screenshot for VLM: %w", err)
	}

	result, err := r.vlmBridge.AnalyzeScreenshot(ctx, action.Description, screenshot)
	if err != nil {
		if r.phaseTracker != nil {
			r.phaseTracker.RecordFailure(TierVLM)
		}
		return fmt.Errorf("VLM analysis failed: %w", err)
	}

	atomic.AddInt64(&r.Metrics.VLMSuccesses, 1)
	if r.phaseTracker != nil {
		r.phaseTracker.RecordSuccess(TierVLM)
	}
	r.logger.Info("VLM provided guidance", "target", action.TargetID, "result", result)
	return nil
}

func (r *ModelRouter) checkPromotion() {
	rate := r.Metrics.LightSuccessRate()
	if rate >= r.promotionThreshold {
		r.tierMu.Lock()
		if r.currentTier > TierLight {
			r.currentTier--
			atomic.AddInt64(&r.Metrics.Demotions, 1)
		}
		r.tierMu.Unlock()
	}
}

func (r *ModelRouter) checkDemotion() {
	rate := r.Metrics.LightSuccessRate()
	if rate < r.demotionThreshold && atomic.LoadInt64(&r.Metrics.LightAttempts) > 5 {
		r.tierMu.Lock()
		if r.currentTier < TierVLM {
			r.currentTier++
			atomic.AddInt64(&r.Metrics.Promotions, 1)
		}
		r.tierMu.Unlock()
	}
}

// ExecuteBatch routes a batch of actions through the appropriate tiers.
func (r *ModelRouter) ExecuteBatch(ctx context.Context, actions []Action) []error {
	errs := make([]error, len(actions))
	for i, action := range actions {
		errs[i] = r.ExecuteAction(ctx, action)
	}
	return errs
}
