package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/aiwright"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/playwright"
)

// TaskStatus represents the lifecycle state of a UI automation task.
type TaskStatus int

// Task lifecycle states.
const (
	TaskPending TaskStatus = iota
	TaskRunning
	TaskCompleted
	TaskFailed
	TaskHealing
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskRunning:
		return "running"
	case TaskCompleted:
		return "completed"
	case TaskFailed:
		return "failed"
	case TaskHealing:
		return "healing"
	default:
		return "unknown"
	}
}

// TaskResult captures the full outcome of a MemberAgent task execution.
type TaskResult struct {
	TaskID       string
	Status       TaskStatus
	Actions      []ActionResult
	HealResults  []HealResult
	Duration     time.Duration
	Error        error
	PatternCount int
	Converged    bool
}

// ExecutorMetricsSnapshot holds exported executor metric values without the internal mutex.
type ExecutorMetricsSnapshot struct {
	TotalActions      int64
	SuccessActions    int64
	FailedActions     int64
	CacheHits         int64
	CacheMisses       int64
	StructuralMatches int64
	AvgLatencyMs      int64
}

// AggregatedMetrics provides a unified view of all sub-component metrics.
type AggregatedMetrics struct {
	Executor  ExecutorMetricsSnapshot
	Router    RouterMetrics
	Healer    HealerMetrics
	VLM       *VLMMetrics
	Degraded  bool                           `json:"degraded"`
	TargetCBs map[string]CircuitBreakerStats `json:"target_circuit_breakers,omitempty"`
	SmartCB   *CircuitBreakerStats           `json:"smart_circuit_breaker,omitempty"`
	VLMCB     *CircuitBreakerStats           `json:"vlm_circuit_breaker,omitempty"`
}

// MemberAgentConfig holds initialization parameters for a MemberAgent.
type MemberAgentConfig struct {
	Headless            bool
	RemoteDebugURL      string
	PatternFile         string
	LLMProvider         llm.Provider
	SmartModels         []string
	VLMModels           []string
	OmniParser          *OmniParserConfig
	AiWrightURL         string // ai-wright server URL (optional, enables SOM visual analysis)
	EnableAxeAudit      bool   // enable axe-core WCAG audit before self-healing
	EnablePlaywrightAgt bool   // enable Playwright Agents CLI for planning/generation
	Logger              *slog.Logger
}

// MemberAgent is the top-level orchestrator for UI automation.
// It composes BrowserAgent, PatternTracker, LightExecutor, SmartDiscoverer,
// ModelRouter, VLMBridge, and SelfHealer into a cohesive lifecycle.
// V2: per-target failure tracking and graceful degradation.
type MemberAgent struct {
	mu sync.RWMutex

	browser   *BrowserAgent
	tracker   *PatternTracker
	executor  *LightExecutor
	smart     *SmartDiscoverer
	vlm       *VLMBridge
	router    *ModelRouter
	healer    *SelfHealer
	logger    *slog.Logger
	taskCount int

	playwrightAgt *playwright.AgentsClient // optional Playwright Agents CLI

	// V2: per-target circuit breakers for graceful degradation
	targetCBs    map[string]*CircuitBreaker
	targetCBConf CircuitBreakerConfig
	degraded     bool // true when operating in degraded mode
}

// MemberAgentOption configures MemberAgent post-construction.
type MemberAgentOption func(*MemberAgent)

// WithMemberLogger overrides the logger.
func WithMemberLogger(l *slog.Logger) MemberAgentOption {
	return func(m *MemberAgent) { m.logger = l }
}

// NewMemberAgent constructs a fully wired MemberAgent from config.
func NewMemberAgent(cfg MemberAgentConfig, opts ...MemberAgentOption) (*MemberAgent, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	var browser *BrowserAgent
	var err error
	if cfg.RemoteDebugURL != "" {
		browser, err = NewBrowserAgentWithRemote(cfg.RemoteDebugURL)
	} else {
		browser, err = NewBrowserAgent(cfg.Headless)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create browser agent: %w", err)
	}

	patternFile := cfg.PatternFile
	if patternFile == "" {
		patternFile = "/tmp/uiauto_patterns.json"
	}
	driftDir := filepath.Dir(patternFile)

	tracker, err := NewPatternTracker(patternFile, driftDir)
	if err != nil {
		browser.Close()
		return nil, fmt.Errorf("failed to create pattern tracker: %w", err)
	}

	executor := NewLightExecutor(tracker, NewChromeDPBrowserAdapter(browser), WithLogger(cfg.Logger))

	var smart *SmartDiscoverer
	if cfg.LLMProvider != nil {
		smart = NewSmartDiscoverer(cfg.LLMProvider, cfg.SmartModels...)
	}

	var vlm *VLMBridge
	if cfg.LLMProvider != nil && len(cfg.VLMModels) > 0 {
		vlmOpts := []VLMOption{WithVLMLogger(cfg.Logger)}
		if cfg.OmniParser != nil {
			vlmOpts = append(vlmOpts, WithOmniParser(*cfg.OmniParser))
		}
		if cfg.AiWrightURL != "" {
			awClient := aiwright.NewClient(cfg.AiWrightURL)
			awBridge := aiwright.NewBridge(awClient, browser, aiwright.WithBridgeLogger(cfg.Logger))
			vlmOpts = append(vlmOpts, WithAiWright(awBridge))
		}
		vlm = NewVLMBridge(cfg.LLMProvider, cfg.VLMModels, vlmOpts...)
	}

	routerOpts := []RouterOption{WithRouterLogger(cfg.Logger)}
	if vlm != nil {
		routerOpts = append(routerOpts, WithVLMBridge(vlm))
	}
	router := NewModelRouter(executor, smart, tracker, browser, routerOpts...)

	healerOpts := []SelfHealerOption{WithHealerLogger(cfg.Logger)}
	if cfg.EnableAxeAudit {
		healerOpts = append(healerOpts, WithAccessibilityAuditor(NewBrowserAxeAdapter(browser)))
	}
	healer := NewSelfHealer(tracker, smart, vlm, browser, healerOpts...)

	var pwAgent *playwright.AgentsClient
	if cfg.EnablePlaywrightAgt {
		pwAgent = playwright.NewAgentsClient(playwright.WithLogger(cfg.Logger))
	}

	agent := &MemberAgent{
		browser:       browser,
		tracker:       tracker,
		executor:      executor,
		smart:         smart,
		vlm:           vlm,
		router:        router,
		healer:        healer,
		playwrightAgt: pwAgent,
		logger:        cfg.Logger,
		targetCBs:     make(map[string]*CircuitBreaker),
		targetCBConf:  DefaultCircuitBreakerConfig(),
	}
	for _, opt := range opts {
		opt(agent)
	}
	return agent, nil
}

// NewMemberAgentFromComponents builds a MemberAgent from pre-existing sub-components.
// Useful for testing or when components are already configured.
func NewMemberAgentFromComponents(
	browser *BrowserAgent,
	tracker *PatternTracker,
	executor *LightExecutor,
	smart *SmartDiscoverer,
	vlm *VLMBridge,
	router *ModelRouter,
	healer *SelfHealer,
	logger *slog.Logger,
) *MemberAgent {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &MemberAgent{
		browser:      browser,
		tracker:      tracker,
		executor:     executor,
		smart:        smart,
		vlm:          vlm,
		router:       router,
		healer:       healer,
		logger:       logger,
		targetCBs:    make(map[string]*CircuitBreaker),
		targetCBConf: DefaultCircuitBreakerConfig(),
	}
}

// Close releases all resources held by the MemberAgent.
func (m *MemberAgent) Close() {
	if m.browser != nil {
		m.browser.Close()
	}
}

// Navigate loads a URL and waits for the page to be ready.
func (m *MemberAgent) Navigate(url string) error {
	if m.browser == nil {
		return fmt.Errorf("browser not initialized")
	}
	m.logger.Info("navigating", "url", url)
	return m.browser.Navigate(url)
}

// RegisterPattern registers a new UI element pattern for future use.
func (m *MemberAgent) RegisterPattern(ctx context.Context, id, selector, description string) error {
	if m.browser == nil {
		return fmt.Errorf("browser not initialized")
	}
	html, err := m.browser.CaptureDOM()
	if err != nil {
		return fmt.Errorf("failed to capture DOM for pattern registration: %w", err)
	}
	return m.tracker.RegisterPattern(ctx, id, selector, description, html)
}

// DiscoverAndRegister uses the smart model to find a selector and register it.
func (m *MemberAgent) DiscoverAndRegister(ctx context.Context, id, description string) (string, error) {
	if m.smart == nil {
		return "", fmt.Errorf("smart discoverer not configured (no LLM provider)")
	}
	if m.browser == nil {
		return "", fmt.Errorf("browser not initialized")
	}

	html, err := m.browser.CaptureDOM()
	if err != nil {
		return "", fmt.Errorf("failed to capture DOM: %w", err)
	}

	selector, err := m.smart.DiscoverSelector(ctx, description, html)
	if err != nil {
		return "", fmt.Errorf("smart discovery failed: %w", err)
	}

	if err := m.tracker.RegisterPattern(ctx, id, selector, description, html); err != nil {
		return selector, fmt.Errorf("pattern registration failed (selector=%s): %w", selector, err)
	}
	return selector, nil
}

// ExecuteAction routes a single action through the ModelRouter (light -> smart -> VLM).
func (m *MemberAgent) ExecuteAction(ctx context.Context, action Action) error {
	return m.router.ExecuteAction(ctx, action)
}

// targetCircuitBreaker returns or creates a per-target circuit breaker.
func (m *MemberAgent) targetCircuitBreaker(targetID string) *CircuitBreaker {
	m.mu.Lock()
	defer m.mu.Unlock()

	cb, ok := m.targetCBs[targetID]
	if !ok {
		cb = NewCircuitBreaker("target:"+targetID, m.targetCBConf)
		m.targetCBs[targetID] = cb
	}
	return cb
}

// RunTask executes a named sequence of actions with automatic healing on failure.
// V2: per-target circuit breakers enable graceful degradation — if a target
// repeatedly fails, it is skipped rather than blocking the entire task.
func (m *MemberAgent) RunTask(ctx context.Context, taskID string, actions []Action) TaskResult {
	m.mu.Lock()
	m.taskCount++
	m.mu.Unlock()

	start := time.Now()
	result := TaskResult{
		TaskID: taskID,
		Status: TaskRunning,
	}

	m.logger.Info("starting task", "task_id", taskID, "actions", len(actions))

	for i, action := range actions {
		cb := m.targetCircuitBreaker(action.TargetID)

		// Graceful degradation: skip targets whose circuit breaker is open
		if !cb.Allow() {
			m.logger.Warn("target circuit breaker open, skipping action",
				"task_id", taskID,
				"step", i,
				"target", action.TargetID,
			)
			m.degraded = true
			ar := ActionResult{
				Action:   action,
				Error:    ErrCircuitOpen,
				Duration: time.Since(start),
				Method:   "circuit_breaker_skip",
			}
			result.Actions = append(result.Actions, ar)
			continue
		}

		err := m.router.ExecuteAction(ctx, action)
		ar := ActionResult{
			Action:   action,
			Error:    err,
			Duration: time.Since(start),
		}
		result.Actions = append(result.Actions, ar)

		if err != nil {
			m.logger.Warn("action failed, attempting self-heal",
				"task_id", taskID,
				"step", i,
				"target", action.TargetID,
				"error", err,
			)

			result.Status = TaskHealing
			healResult := m.healer.Heal(ctx, action.TargetID)
			result.HealResults = append(result.HealResults, healResult)

			if healResult.Success {
				cb.RecordSuccess()
				m.logger.Info("self-heal succeeded, retrying action",
					"task_id", taskID,
					"target", action.TargetID,
					"method", healResult.Method,
				)
				retryErr := m.router.ExecuteAction(ctx, action)
				retryResult := ActionResult{
					Action:   action,
					Error:    retryErr,
					Duration: time.Since(start),
					Method:   "post_heal_retry",
				}
				result.Actions = append(result.Actions, retryResult)

				if retryErr != nil {
					cb.RecordFailure()
					result.Status = TaskFailed
					result.Error = fmt.Errorf("action %d (%s) failed even after heal: %w", i, action.TargetID, retryErr)
					result.Duration = time.Since(start)
					return result
				}
				result.Status = TaskRunning
				continue
			}

			cb.RecordFailure()
			result.Status = TaskFailed
			result.Error = fmt.Errorf("action %d (%s) failed and heal exhausted: %w", i, action.TargetID, err)
			result.Duration = time.Since(start)
			return result
		}

		cb.RecordSuccess()
	}

	allPatterns, _ := m.tracker.store.Load(ctx)
	result.PatternCount = len(allPatterns)
	result.Converged = m.router.IsConverged()
	result.Status = TaskCompleted
	result.Duration = time.Since(start)

	m.logger.Info("task completed",
		"task_id", taskID,
		"duration", result.Duration,
		"patterns", result.PatternCount,
		"converged", result.Converged,
		"degraded", m.degraded,
	)
	return result
}

// IsDegraded returns true when the agent is operating in degraded mode
// (one or more targets have their circuit breaker open).
func (m *MemberAgent) IsDegraded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.degraded
}

// TargetCBStats returns circuit breaker stats for all tracked targets.
func (m *MemberAgent) TargetCBStats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]CircuitBreakerStats, len(m.targetCBs))
	for id, cb := range m.targetCBs {
		out[id] = cb.Stats()
	}
	return out
}

// DetectDriftAndHeal scans all known patterns and heals any that have drifted.
func (m *MemberAgent) DetectDriftAndHeal(ctx context.Context) []HealResult {
	return m.healer.DetectAndHeal(ctx)
}

// Metrics returns an aggregated snapshot of all sub-component metrics.
func (m *MemberAgent) Metrics() AggregatedMetrics {
	es := m.executor.Metrics.Snapshot()

	agg := AggregatedMetrics{
		Executor: ExecutorMetricsSnapshot{
			TotalActions:      es.TotalActions,
			SuccessActions:    es.SuccessActions,
			FailedActions:     es.FailedActions,
			CacheHits:         es.CacheHits,
			CacheMisses:       es.CacheMisses,
			StructuralMatches: es.StructuralMatches,
			AvgLatencyMs:      es.AvgLatencyMs,
		},
		Router:    m.router.Metrics.Snapshot(),
		Healer:    m.healer.Metrics.Snapshot(),
		Degraded:  m.IsDegraded(),
		TargetCBs: m.TargetCBStats(),
	}
	if m.vlm != nil {
		snap := m.vlm.Metrics.Snapshot()
		agg.VLM = &snap
	}
	if smartCB := m.healer.SmartCB(); smartCB != nil {
		s := smartCB.Stats()
		agg.SmartCB = &s
	}
	if vlmCB := m.healer.VLMCB(); vlmCB != nil {
		s := vlmCB.Stats()
		agg.VLMCB = &s
	}
	return agg
}

// Browser returns the underlying BrowserAgent for direct access when needed.
func (m *MemberAgent) Browser() *BrowserAgent { return m.browser }

// Tracker returns the underlying PatternTracker.
func (m *MemberAgent) Tracker() *PatternTracker { return m.tracker }

// Router returns the underlying ModelRouter.
func (m *MemberAgent) Router() *ModelRouter { return m.router }

// Healer returns the underlying SelfHealer.
func (m *MemberAgent) Healer() *SelfHealer { return m.healer }

// IsConverged returns whether the pattern set has converged.
func (m *MemberAgent) IsConverged() bool { return m.router.IsConverged() }

// CurrentTier returns the current model tier being used by the router.
func (m *MemberAgent) CurrentTier() ModelTier { return m.router.CurrentTier() }

// TaskCount returns how many tasks have been executed.
func (m *MemberAgent) TaskCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.taskCount
}

// --- V3: Concurrent Goroutine Page Exploration ---

// ExploreTarget defines a page to explore concurrently.
type ExploreTarget struct {
	URL         string
	ElementIDs  []string // pattern IDs to discover on this page
	Description string
}

// ExploreResult captures the outcome of exploring a single page.
type ExploreResult struct {
	URL          string
	Discovered   int
	Failed       int
	HealAttempts int
	Duration     time.Duration
	Error        error
}

// ConcurrentExploreConfig controls V3 concurrent exploration behavior.
type ConcurrentExploreConfig struct {
	MaxConcurrency int           // max goroutines (defaults to 4)
	PageTimeout    time.Duration // per-page context timeout (defaults to 30s)
}

// ConcurrentExplore fans out page discovery across goroutines with context
// timeout. Each target gets its own goroutine (up to MaxConcurrency). If the
// parent ctx is cancelled all in-flight explorations abort.
func (m *MemberAgent) ConcurrentExplore(ctx context.Context, targets []ExploreTarget, cfg ConcurrentExploreConfig) []ExploreResult {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}
	if cfg.PageTimeout <= 0 {
		cfg.PageTimeout = 30 * time.Second
	}

	results := make([]ExploreResult, len(targets))
	sem := make(chan struct{}, cfg.MaxConcurrency)
	var wg sync.WaitGroup

	for i, target := range targets {
		wg.Add(1)
		go func(idx int, tgt ExploreTarget) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			pageCtx, cancel := context.WithTimeout(ctx, cfg.PageTimeout)
			defer cancel()

			results[idx] = m.exploreSinglePage(pageCtx, tgt)
		}(i, target)
	}

	wg.Wait()
	return results
}

// exploreSinglePage navigates to a URL, discovers elements, and applies healing.
// MemberAgent's browser is shared, so callers serialise via the semaphore.
func (m *MemberAgent) exploreSinglePage(ctx context.Context, tgt ExploreTarget) ExploreResult {
	start := time.Now()
	res := ExploreResult{URL: tgt.URL}

	if err := ctx.Err(); err != nil {
		res.Error = fmt.Errorf("context cancelled before navigate: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	if err := m.Navigate(tgt.URL); err != nil {
		res.Error = fmt.Errorf("navigate failed: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	for _, elemID := range tgt.ElementIDs {
		if ctx.Err() != nil {
			res.Error = ctx.Err()
			break
		}

		_, err := m.DiscoverAndRegister(ctx, elemID, tgt.Description+" - "+elemID)
		if err != nil {
			res.Failed++
			m.logger.Warn("concurrent explore: discovery failed, attempting heal",
				"url", tgt.URL,
				"element", elemID,
				"error", err,
			)

			healResult := m.healer.Heal(ctx, elemID)
			res.HealAttempts++
			if healResult.Success {
				_, retryErr := m.DiscoverAndRegister(ctx, elemID, tgt.Description+" - "+elemID)
				if retryErr == nil {
					res.Discovered++
					res.Failed--
				}
			}
		} else {
			res.Discovered++
		}
	}

	res.Duration = time.Since(start)
	return res
}

// ExploreReport summarises a batch of concurrent exploration results.
type ExploreReport struct {
	TotalTargets      int
	TotalDiscovered   int
	TotalFailed       int
	TotalHealAttempts int
	TotalDuration     time.Duration
	Errors            []string
}

// SummariseExplore aggregates a set of ExploreResults into a report.
func SummariseExplore(results []ExploreResult) ExploreReport {
	var rpt ExploreReport
	rpt.TotalTargets = len(results)
	for _, r := range results {
		rpt.TotalDiscovered += r.Discovered
		rpt.TotalFailed += r.Failed
		rpt.TotalHealAttempts += r.HealAttempts
		rpt.TotalDuration += r.Duration
		if r.Error != nil {
			rpt.Errors = append(rpt.Errors, fmt.Sprintf("%s: %v", r.URL, r.Error))
		}
	}
	return rpt
}
