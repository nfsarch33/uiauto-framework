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
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/accessibility"
)

// HealStrategy defines which recovery methods to attempt.
type HealStrategy int

// Healing strategies as bitflags for composable repair approaches.
const (
	HealFingerprint HealStrategy = 1 << iota
	HealStructural
	HealSmartLLM
	HealVLM
	HealVLMJudge // Final fallback: VLMAsJudge + GenerateSelectorFromVLM
	HealAll      = HealFingerprint | HealStructural | HealSmartLLM | HealVLM | HealVLMJudge
)

// HealResult captures the outcome of a self-healing attempt.
type HealResult struct {
	TargetID    string
	OldSelector string
	NewSelector string
	Method      string // "fingerprint", "structural", "smart_llm", "vlm"
	Confidence  float64
	Duration    time.Duration
	Success     bool
	Error       error
}

// HealerMetrics tracks self-healing statistics.
type HealerMetrics struct {
	TotalAttempts    int64
	SuccessfulHeals  int64
	FailedHeals      int64
	FingerprintHeals int64
	StructuralHeals  int64
	SmartLLMHeals    int64
	VLMHeals         int64
	VLMJudgeHeals    int64
	AvgHealTimeMs    int64
}

// Snapshot returns an atomic copy of the current metrics.
func (m *HealerMetrics) Snapshot() HealerMetrics {
	return HealerMetrics{
		TotalAttempts:    atomic.LoadInt64(&m.TotalAttempts),
		SuccessfulHeals:  atomic.LoadInt64(&m.SuccessfulHeals),
		FailedHeals:      atomic.LoadInt64(&m.FailedHeals),
		FingerprintHeals: atomic.LoadInt64(&m.FingerprintHeals),
		StructuralHeals:  atomic.LoadInt64(&m.StructuralHeals),
		SmartLLMHeals:    atomic.LoadInt64(&m.SmartLLMHeals),
		VLMHeals:         atomic.LoadInt64(&m.VLMHeals),
		VLMJudgeHeals:    atomic.LoadInt64(&m.VLMJudgeHeals),
		AvgHealTimeMs:    atomic.LoadInt64(&m.AvgHealTimeMs),
	}
}

// SuccessRate returns the healing success rate.
func (m *HealerMetrics) SuccessRate() float64 {
	total := atomic.LoadInt64(&m.TotalAttempts)
	if total == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&m.SuccessfulHeals)) / float64(total)
}

// SelfHealer orchestrates the end-to-end self-healing loop:
// detect drift -> fingerprint match -> structural match -> LLM repair -> VLM verify.
// V2: circuit breakers on Smart LLM and VLM tiers prevent cascading failures.
type SelfHealer struct {
	tracker         *PatternTracker
	smartDiscoverer *SmartDiscoverer
	vlmBridge       *VLMBridge
	browser         *BrowserAgent
	structMatcher   *domheal.StructuralMatcher
	strategy        HealStrategy
	Metrics         *HealerMetrics
	logger          *slog.Logger

	// V2 circuit breakers for expensive tiers
	smartCB    *CircuitBreaker
	vlmCB      *CircuitBreaker
	vlmJudgeCB *CircuitBreaker

	axeAuditor accessibility.JSEvaluator // optional: runs axe-core audit before healing
}

// SelfHealerOption configures SelfHealer behavior.
type SelfHealerOption func(*SelfHealer)

// WithHealStrategy sets the healing strategy.
func WithHealStrategy(s HealStrategy) SelfHealerOption {
	return func(h *SelfHealer) { h.strategy = s }
}

// WithHealerLogger sets the logger.
func WithHealerLogger(l *slog.Logger) SelfHealerOption {
	return func(h *SelfHealer) { h.logger = l }
}

// WithSmartCircuitBreaker overrides the Smart LLM circuit breaker.
func WithSmartCircuitBreaker(cb *CircuitBreaker) SelfHealerOption {
	return func(h *SelfHealer) { h.smartCB = cb }
}

// WithVLMCircuitBreaker overrides the VLM circuit breaker.
func WithVLMCircuitBreaker(cb *CircuitBreaker) SelfHealerOption {
	return func(h *SelfHealer) { h.vlmCB = cb }
}

// WithVLMJudgeCircuitBreaker overrides the VLM-as-judge circuit breaker.
func WithVLMJudgeCircuitBreaker(cb *CircuitBreaker) SelfHealerOption {
	return func(h *SelfHealer) { h.vlmJudgeCB = cb }
}

// WithAccessibilityAuditor enables axe-core WCAG audit before healing.
func WithAccessibilityAuditor(eval accessibility.JSEvaluator) SelfHealerOption {
	return func(h *SelfHealer) { h.axeAuditor = eval }
}

// browserAxeAdapter wraps BrowserAgent to satisfy accessibility.JSEvaluator.
type browserAxeAdapter struct {
	browser *BrowserAgent
}

func (a *browserAxeAdapter) Evaluate(_ context.Context, expression string, result interface{}) error {
	return a.browser.Evaluate(expression, result)
}

// NewBrowserAxeAdapter creates an accessibility.JSEvaluator from a BrowserAgent.
func NewBrowserAxeAdapter(b *BrowserAgent) accessibility.JSEvaluator {
	return &browserAxeAdapter{browser: b}
}

// NewSelfHealer creates a new SelfHealer.
func NewSelfHealer(
	tracker *PatternTracker,
	smart *SmartDiscoverer,
	vlm *VLMBridge,
	browser *BrowserAgent,
	opts ...SelfHealerOption,
) *SelfHealer {
	h := &SelfHealer{
		tracker:         tracker,
		smartDiscoverer: smart,
		vlmBridge:       vlm,
		browser:         browser,
		structMatcher:   domheal.NewStructuralMatcher(0.6, slog.New(slog.NewTextHandler(io.Discard, nil))),
		strategy:        HealAll,
		Metrics:         &HealerMetrics{},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		smartCB:         NewCircuitBreaker("smart_llm", DefaultCircuitBreakerConfig()),
		vlmCB:           NewCircuitBreaker("vlm", DefaultCircuitBreakerConfig()),
		vlmJudgeCB:      NewCircuitBreaker("vlm_judge", DefaultCircuitBreakerConfig()),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Heal attempts to recover a broken selector using progressive escalation.
func (h *SelfHealer) Heal(ctx context.Context, targetID string) HealResult {
	start := time.Now()
	atomic.AddInt64(&h.Metrics.TotalAttempts, 1)

	result := HealResult{TargetID: targetID}

	pattern, ok := h.tracker.store.Get(ctx, targetID)
	if !ok {
		result.Error = fmt.Errorf("pattern not found: %s", targetID)
		result.Duration = time.Since(start)
		atomic.AddInt64(&h.Metrics.FailedHeals, 1)
		return result
	}
	result.OldSelector = pattern.Selector

	html, err := h.browser.CaptureDOM()
	if err != nil {
		result.Error = fmt.Errorf("failed to capture DOM: %w", err)
		result.Duration = time.Since(start)
		atomic.AddInt64(&h.Metrics.FailedHeals, 1)
		return result
	}

	// L0: Accessibility audit (optional, enriches ARIA data for repair strategies)
	if h.axeAuditor != nil {
		audit, axeErr := accessibility.InjectAndRun(ctx, h.axeAuditor)
		if axeErr != nil {
			h.logger.Warn("axe-core audit failed, continuing without", "error", axeErr)
		} else {
			h.logger.Info("axe-core audit complete",
				"target", targetID,
				"violations", len(audit.Violations),
				"passes", len(audit.Passes))
		}
	}

	// L1: Fingerprint matching
	if h.strategy&HealFingerprint != 0 {
		if r := h.tryFingerprint(ctx, pattern, html); r.Success {
			r.Duration = time.Since(start)
			h.recordSuccess(&r)
			return r
		}
	}

	// L2: Structural matching
	if h.strategy&HealStructural != 0 {
		if r := h.tryStructural(ctx, pattern, html); r.Success {
			r.Duration = time.Since(start)
			h.recordSuccess(&r)
			return r
		}
	}

	// L3: Smart LLM discovery (circuit-breaker protected)
	if h.strategy&HealSmartLLM != 0 {
		if h.smartCB.Allow() {
			r := h.trySmartLLM(ctx, pattern, html)
			if r.Success {
				h.smartCB.RecordSuccess()
				r.Duration = time.Since(start)
				h.recordSuccess(&r)
				return r
			}
			if r.Error != nil {
				h.smartCB.RecordFailure()
			}
		} else {
			h.logger.Warn("smart LLM circuit breaker open, skipping",
				"target", targetID)
		}
	}

	// L4: VLM visual verification (circuit-breaker protected)
	if h.strategy&HealVLM != 0 && h.vlmBridge != nil {
		if h.vlmCB.Allow() {
			r := h.tryVLM(ctx, pattern)
			if r.Success {
				h.vlmCB.RecordSuccess()
				r.Duration = time.Since(start)
				h.recordSuccess(&r)
				return r
			}
			if r.Error != nil {
				h.vlmCB.RecordFailure()
			}
		} else {
			h.logger.Warn("VLM circuit breaker open, skipping",
				"target", targetID)
		}
	}

	// L5: VLM-as-judge final fallback (circuit-breaker protected)
	if h.strategy&HealVLMJudge != 0 && h.vlmBridge != nil {
		if h.vlmJudgeCB.Allow() {
			r := h.tryVLMAsJudge(ctx, pattern)
			if r.Success {
				h.vlmJudgeCB.RecordSuccess()
				r.Duration = time.Since(start)
				h.recordSuccess(&r)
				return r
			}
			if r.Error != nil {
				h.vlmJudgeCB.RecordFailure()
			}
		} else {
			h.logger.Warn("VLM judge circuit breaker open, skipping",
				"target", targetID)
		}
	}

	result.Error = fmt.Errorf("all healing strategies exhausted for %s", targetID)
	result.Duration = time.Since(start)
	atomic.AddInt64(&h.Metrics.FailedHeals, 1)
	return result
}

func (h *SelfHealer) tryFingerprint(ctx context.Context, pattern UIPattern, html string) HealResult {
	result := HealResult{TargetID: pattern.ID, OldSelector: pattern.Selector, Method: "fingerprint"}

	currentFP := domheal.ParseDOMFingerprint(html)
	similarity := domheal.DOMFingerprintSimilarity(pattern.Fingerprint, currentFP)

	if similarity >= 0.7 {
		err := h.browser.Click(pattern.Selector)
		if err == nil {
			result.Success = true
			result.Confidence = similarity
			result.NewSelector = pattern.Selector
			atomic.AddInt64(&h.Metrics.FingerprintHeals, 1)
			return result
		}
	}
	return result
}

func (h *SelfHealer) tryStructural(ctx context.Context, pattern UIPattern, html string) HealResult {
	result := HealResult{TargetID: pattern.ID, OldSelector: pattern.Selector, Method: "structural"}

	_ = domheal.ParseStructuralSignature(html)
	h.structMatcher.CheckAndUpdate(pattern.ID, html)

	match, similarity, found := h.tracker.FindBestMatch(ctx, pattern.ID, html)
	if found && similarity >= 0.6 {
		result.Confidence = similarity
		result.NewSelector = match.Selector
		result.Success = true
		atomic.AddInt64(&h.Metrics.StructuralHeals, 1)
	}
	return result
}

func (h *SelfHealer) trySmartLLM(ctx context.Context, pattern UIPattern, html string) HealResult {
	result := HealResult{TargetID: pattern.ID, OldSelector: pattern.Selector, Method: "smart_llm"}

	newSel, err := h.smartDiscoverer.DiscoverSelector(ctx, pattern.Description, html)
	if err != nil {
		result.Error = err
		return result
	}

	// Verify the discovered selector works
	verifyErr := h.browser.Click(newSel)
	if verifyErr != nil {
		var text string
		verifyErr = h.browser.Evaluate(fmt.Sprintf(`document.querySelector("%s")?.tagName || ""`, newSel), &text)
		if verifyErr != nil || text == "" {
			return result
		}
	}

	err = h.tracker.RegisterPattern(ctx, pattern.ID, newSel, pattern.Description, html)
	if err != nil {
		result.Error = err
		return result
	}

	result.NewSelector = newSel
	result.Confidence = 0.8
	result.Success = true
	atomic.AddInt64(&h.Metrics.SmartLLMHeals, 1)
	return result
}

func (h *SelfHealer) tryVLM(ctx context.Context, pattern UIPattern) HealResult {
	result := HealResult{TargetID: pattern.ID, OldSelector: pattern.Selector, Method: "vlm"}

	screenshot, err := h.browser.CaptureScreenshot()
	if err != nil {
		result.Error = fmt.Errorf("screenshot failed: %w", err)
		return result
	}

	guidance, err := h.vlmBridge.AnalyzeScreenshot(ctx, pattern.Description, screenshot)
	if err != nil {
		result.Error = err
		return result
	}

	result.NewSelector = guidance
	result.Confidence = 0.6
	result.Success = true
	atomic.AddInt64(&h.Metrics.VLMHeals, 1)
	return result
}

func (h *SelfHealer) tryVLMAsJudge(ctx context.Context, pattern UIPattern) HealResult {
	result := HealResult{TargetID: pattern.ID, OldSelector: pattern.Selector, Method: "vlm_judge"}

	screenshot, err := h.browser.CaptureScreenshot()
	if err != nil {
		result.Error = fmt.Errorf("screenshot failed: %w", err)
		return result
	}

	judgment, err := h.vlmBridge.VLMAsJudge(ctx, pattern.Description, screenshot)
	if err != nil {
		result.Error = err
		return result
	}

	var newSelector string
	if judgment.Present && judgment.SuggestedSelector != "" {
		newSelector = judgment.SuggestedSelector
		result.Confidence = judgment.Confidence
	}
	if newSelector == "" {
		selResult, err := h.vlmBridge.GenerateSelectorFromVLM(ctx, pattern.Description, screenshot)
		if err != nil || selResult == nil || selResult.Selector == "" {
			result.Error = fmt.Errorf("VLM judge: element not present or selector generation failed")
			return result
		}
		newSelector = selResult.Selector
		result.Confidence = selResult.Confidence
	}

	// Verify the discovered selector works
	if verifyErr := h.browser.Click(newSelector); verifyErr != nil {
		var text string
		_ = h.browser.Evaluate(fmt.Sprintf(`document.querySelector("%s")?.tagName || ""`, newSelector), &text)
		if text == "" {
			result.Error = fmt.Errorf("VLM judge selector %q did not match any element", newSelector)
			return result
		}
	}

	html, _ := h.browser.CaptureDOM()
	if err := h.tracker.RegisterPattern(ctx, pattern.ID, newSelector, pattern.Description, html); err != nil {
		result.Error = err
		return result
	}

	result.NewSelector = newSelector
	result.Success = true
	atomic.AddInt64(&h.Metrics.VLMJudgeHeals, 1)
	return result
}

func (h *SelfHealer) recordSuccess(r *HealResult) {
	atomic.AddInt64(&h.Metrics.SuccessfulHeals, 1)

	// Boost the pattern's stored confidence after a successful heal
	_ = h.tracker.store.BoostConfidence(context.Background(), r.TargetID, 0.1)

	h.logger.Info("self-heal succeeded",
		"target", r.TargetID,
		"method", r.Method,
		"confidence", r.Confidence,
		"old_selector", r.OldSelector,
		"new_selector", r.NewSelector,
	)
}

// SmartCB returns the Smart LLM circuit breaker for observability.
func (h *SelfHealer) SmartCB() *CircuitBreaker { return h.smartCB }

// VLMCB returns the VLM circuit breaker for observability.
func (h *SelfHealer) VLMCB() *CircuitBreaker { return h.vlmCB }

// VLMJudgeCB returns the VLM-as-judge circuit breaker for observability.
func (h *SelfHealer) VLMJudgeCB() *CircuitBreaker { return h.vlmJudgeCB }

// HealBatch attempts to heal multiple broken patterns.
func (h *SelfHealer) HealBatch(ctx context.Context, targetIDs []string) []HealResult {
	results := make([]HealResult, len(targetIDs))
	for i, id := range targetIDs {
		results[i] = h.Heal(ctx, id)
	}
	return results
}

// DetectAndHeal scans all known patterns, detects drift, and heals broken ones.
func (h *SelfHealer) DetectAndHeal(ctx context.Context) []HealResult {
	patterns, err := h.tracker.store.Load(ctx)
	if err != nil {
		h.logger.Error("failed to load patterns for drift scan", "error", err)
		return nil
	}

	html, err := h.browser.CaptureDOM()
	if err != nil {
		h.logger.Error("failed to capture DOM for drift scan", "error", err)
		return nil
	}

	var results []HealResult
	for id := range patterns {
		drifted, _ := h.tracker.CheckDrift(id, html)
		if drifted {
			r := h.Heal(ctx, id)
			results = append(results, r)
		}
	}
	return results
}

// DetectAndHealConcurrent performs drift detection with goroutines for each
// pattern, enabling parallel drift checks. Healing is still sequential
// because browser actions are single-threaded.
// Each drift-check goroutine respects context cancellation.
func (h *SelfHealer) DetectAndHealConcurrent(ctx context.Context) []HealResult {
	patterns, err := h.tracker.store.Load(ctx)
	if err != nil {
		h.logger.Error("failed to load patterns for drift scan", "error", err)
		return nil
	}

	html, err := h.browser.CaptureDOM()
	if err != nil {
		h.logger.Error("failed to capture DOM for drift scan", "error", err)
		return nil
	}

	type driftResult struct {
		id      string
		drifted bool
	}

	ch := make(chan driftResult, len(patterns))
	var wg sync.WaitGroup

	for id := range patterns {
		wg.Add(1)
		go func(patternID string) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			drifted, _ := h.tracker.CheckDrift(patternID, html)
			select {
			case ch <- driftResult{id: patternID, drifted: drifted}:
			case <-ctx.Done():
			}
		}(id)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var driftedIDs []string
	for dr := range ch {
		if dr.drifted {
			driftedIDs = append(driftedIDs, dr.id)
		}
	}

	var results []HealResult
	for _, id := range driftedIDs {
		if ctx.Err() != nil {
			break
		}
		r := h.Heal(ctx, id)
		results = append(results, r)
	}
	return results
}
