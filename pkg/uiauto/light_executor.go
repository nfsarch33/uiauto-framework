package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Action represents a UI action to perform.
type Action struct {
	// Type selects the executor backend: click | type | read | evaluate |
	// wait | verify | frame.
	//
	//   verify -- assert selector resolves to a visible element. Cheap (2s budget).
	//   frame  -- attach to an iframe; used for embedded apps (any third-party
	//             widget served via an iframe). Children action items run in
	//             iframe context.
	Type        string
	TargetID    string      // ID of the pattern to use
	Description string      // Human-readable description of the target
	Value       string      // Text to type (Type=="type") or selector (Type=="wait")
	Result      interface{} // Pointer to store result (Type=="evaluate")
	// Children carries a sub-batch executed inside an iframe context. Set when
	// Type=="frame" and the test wants to compose iframe-scoped actions.
	Children []Action
	Timeout  time.Duration
}

// ActionResult captures the outcome of a single action execution.
type ActionResult struct {
	Action   Action
	Error    error
	Duration time.Duration
	Retries  int
	Method   string // "cached", "structural_match", "smart_recovery"
}

// BatchResult captures the outcome of a batch execution.
type BatchResult struct {
	Results   []ActionResult
	TotalTime time.Duration
	Succeeded int
	Failed    int
	NeedSmart int // actions that need smart model recovery
}

// ExecutorMetrics tracks execution statistics.
type ExecutorMetrics struct {
	TotalActions      int64
	SuccessActions    int64
	FailedActions     int64
	CacheHits         int64
	CacheMisses       int64
	StructuralMatches int64
	AvgLatencyMs      int64
	mu                sync.Mutex
	latencies         []time.Duration
}

func (m *ExecutorMetrics) record(success bool, cacheHit bool, structMatch bool, latency time.Duration) {
	if success {
		atomic.AddInt64(&m.SuccessActions, 1)
	} else {
		atomic.AddInt64(&m.FailedActions, 1)
	}
	atomic.AddInt64(&m.TotalActions, 1)
	if cacheHit {
		atomic.AddInt64(&m.CacheHits, 1)
	} else {
		atomic.AddInt64(&m.CacheMisses, 1)
	}
	if structMatch {
		atomic.AddInt64(&m.StructuralMatches, 1)
	}

	m.mu.Lock()
	m.latencies = append(m.latencies, latency)
	var total time.Duration
	for _, l := range m.latencies {
		total += l
	}
	if len(m.latencies) > 0 {
		atomic.StoreInt64(&m.AvgLatencyMs, int64(total/time.Duration(len(m.latencies))/time.Millisecond))
	}
	m.mu.Unlock()
}

// Snapshot returns a copy of current metrics.
func (m *ExecutorMetrics) Snapshot() ExecutorMetrics {
	return ExecutorMetrics{
		TotalActions:      atomic.LoadInt64(&m.TotalActions),
		SuccessActions:    atomic.LoadInt64(&m.SuccessActions),
		FailedActions:     atomic.LoadInt64(&m.FailedActions),
		CacheHits:         atomic.LoadInt64(&m.CacheHits),
		CacheMisses:       atomic.LoadInt64(&m.CacheMisses),
		StructuralMatches: atomic.LoadInt64(&m.StructuralMatches),
		AvgLatencyMs:      atomic.LoadInt64(&m.AvgLatencyMs),
	}
}

// LightExecutor executes actions using known patterns with batch support and metrics.
type LightExecutor struct {
	tracker        *PatternTracker
	browser        Browser
	logger         *slog.Logger
	Metrics        *ExecutorMetrics
	defaultTimeout time.Duration
	maxRetries     int
}

// LightExecutorOption configures LightExecutor behavior.
type LightExecutorOption func(*LightExecutor)

// WithTimeout sets the default per-action timeout.
func WithTimeout(d time.Duration) LightExecutorOption {
	return func(e *LightExecutor) { e.defaultTimeout = d }
}

// WithMaxRetries sets the maximum retry count per action.
func WithMaxRetries(n int) LightExecutorOption {
	return func(e *LightExecutor) { e.maxRetries = n }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) LightExecutorOption {
	return func(e *LightExecutor) { e.logger = l }
}

// NewLightExecutor creates a new LightExecutor.
func NewLightExecutor(tracker *PatternTracker, browser Browser, opts ...LightExecutorOption) *LightExecutor {
	e := &LightExecutor{
		tracker:        tracker,
		browser:        browser,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:        &ExecutorMetrics{},
		defaultTimeout: 30 * time.Second,
		maxRetries:     2,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute attempts to perform a single action using cached patterns.
func (e *LightExecutor) Execute(ctx context.Context, action Action) error {
	result := e.executeWithMetrics(ctx, action)
	return result.Error
}

func (e *LightExecutor) executeWithMetrics(ctx context.Context, action Action) ActionResult {
	start := time.Now()
	timeout := e.defaultTimeout
	if action.Timeout > 0 {
		timeout = action.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := ActionResult{Action: action}

	pattern, ok := e.tracker.store.Get(ctx, action.TargetID)
	if !ok {
		result.Error = fmt.Errorf("pattern not found for target: %s", action.TargetID)
		result.Duration = time.Since(start)
		e.Metrics.record(false, false, false, result.Duration)
		return result
	}

	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		result.Retries = attempt
		err := e.performAction(ctx, action, pattern.Selector)
		if err == nil {
			pattern.LastSeen = time.Now()
			_ = e.tracker.store.Set(ctx, pattern)
			result.Method = "cached"
			result.Duration = time.Since(start)
			e.Metrics.record(true, true, false, result.Duration)
			return result
		}

		if attempt < e.maxRetries {
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				result.Duration = time.Since(start)
				e.Metrics.record(false, true, false, result.Duration)
				return result
			case <-time.After(100 * time.Millisecond * time.Duration(attempt+1)):
			}
		}
	}

	// Cache miss path: try structural matching
	html, captureErr := e.browser.CaptureDOM()
	if captureErr != nil {
		result.Error = fmt.Errorf("failed to capture DOM for drift recovery: %w", captureErr)
		result.Duration = time.Since(start)
		e.Metrics.record(false, false, false, result.Duration)
		return result
	}

	match, similarity, found := e.tracker.FindBestMatch(ctx, action.TargetID, html)
	if !found {
		result.Error = fmt.Errorf("action failed and no structural match found (similarity: %.3f)", similarity)
		result.Method = "cache_miss"
		result.Duration = time.Since(start)
		e.Metrics.record(false, false, false, result.Duration)
		return result
	}

	err := e.performAction(ctx, action, match.Selector)
	if err != nil {
		result.Error = fmt.Errorf("element structure matched (similarity: %.3f) but selector failed, needs smart recovery", similarity)
		result.Method = "structural_match_failed"
		result.Duration = time.Since(start)
		e.Metrics.record(false, false, true, result.Duration)
		return result
	}

	result.Method = "structural_match"
	result.Duration = time.Since(start)
	e.Metrics.record(true, false, true, result.Duration)
	return result
}

// ExecuteBatch executes multiple actions sequentially, collecting all results.
// Stops on first error if stopOnError is true.
func (e *LightExecutor) ExecuteBatch(ctx context.Context, actions []Action, stopOnError bool) BatchResult {
	start := time.Now()
	batch := BatchResult{Results: make([]ActionResult, 0, len(actions))}

	for _, action := range actions {
		select {
		case <-ctx.Done():
			batch.Results = append(batch.Results, ActionResult{
				Action: action,
				Error:  ctx.Err(),
			})
			batch.Failed++
			batch.TotalTime = time.Since(start)
			return batch
		default:
		}

		result := e.executeWithMetrics(ctx, action)
		batch.Results = append(batch.Results, result)
		if result.Error != nil {
			batch.Failed++
			if result.Method == "structural_match_failed" || result.Method == "cache_miss" {
				batch.NeedSmart++
			}
			if stopOnError {
				break
			}
		} else {
			batch.Succeeded++
		}
	}

	batch.TotalTime = time.Since(start)
	return batch
}

// DiscoverParallel discovers multiple elements concurrently, returning discovered selectors.
// Each goroutine respects context cancellation before starting work.
func (e *LightExecutor) DiscoverParallel(ctx context.Context, targets []Action) map[string]ActionResult {
	results := make(map[string]ActionResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, target := range targets {
		wg.Add(1)
		go func(t Action) {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				mu.Lock()
				results[t.TargetID] = ActionResult{Action: t, Error: err}
				mu.Unlock()
				return
			}
			r := e.executeWithMetrics(ctx, t)
			mu.Lock()
			results[t.TargetID] = r
			mu.Unlock()
		}(target)
	}

	wg.Wait()
	return results
}

func (e *LightExecutor) performAction(ctx context.Context, action Action, selector string) error {
	switch action.Type {
	case "click":
		return e.browser.Click(selector)
	case "type":
		return e.browser.Type(selector, action.Value)
	case "evaluate":
		return e.browser.Evaluate(selector, action.Result)
	case "read":
		var text string
		err := e.browser.Evaluate(fmt.Sprintf(`document.querySelector("%s")?.innerText || ""`, selector), &text)
		if err != nil {
			return err
		}
		if text == "" {
			return fmt.Errorf("element not found or empty: %s", selector)
		}
		return nil
	case "wait":
		waitSelector := selector
		if action.Value != "" {
			waitSelector = action.Value
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			visible, err := e.browser.IsVisible(waitSelector)
			if err == nil && visible {
				return nil
			}
			select {
			case <-ctx.Done():
				if err != nil {
					return fmt.Errorf("wait for %s: %w", waitSelector, err)
				}
				return fmt.Errorf("wait for %s: %w", waitSelector, ctx.Err())
			case <-ticker.C:
			}
		}
	case "verify":
		visible, err := e.browser.IsVisible(selector)
		if err != nil {
			return fmt.Errorf("verify %s: %w", selector, err)
		}
		if !visible {
			return fmt.Errorf("verify %s: element not visible", selector)
		}
		return nil
	case "frame":
		release, err := e.browser.SwitchToFrame(selector)
		if err != nil {
			return fmt.Errorf("frame %s: %w", selector, err)
		}
		defer release()
		// Run nested children sequentially inside the iframe context.
		for i, child := range action.Children {
			if err := ctx.Err(); err != nil {
				return err
			}
			childResult := e.executeWithMetrics(ctx, child)
			if childResult.Error != nil {
				return fmt.Errorf("frame child[%d] %q: %w", i, child.Description, childResult.Error)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}
