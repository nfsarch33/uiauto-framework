package uiauto

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// WaitStrategy is a bitmask for page-readiness checks.
type WaitStrategy int

// Wait strategies as bitflags for composable page-load detection.
const (
	WaitNetworkIdle WaitStrategy = 1 << iota
	WaitDOMStable
	WaitElementVisible
	WaitElementEnabled
	WaitSPARouteChange
)

// WaitConfig lets callers tune page-wait behaviour per request.
type WaitConfig struct {
	Timeout        time.Duration
	Strategy       WaitStrategy
	StableFor      time.Duration // how long DOM or network must be quiet
	PollInterval   time.Duration // DOM-stability poll cadence
	ContinueOnErr  bool          // if true, wait errors are non-fatal
	TargetSelector string        // CSS selector for WaitElementVisible/Enabled strategies
}

// DefaultWaitConfig returns production-safe defaults.
func DefaultWaitConfig() WaitConfig {
	return WaitConfig{
		Timeout:       15 * time.Second,
		Strategy:      WaitNetworkIdle | WaitDOMStable,
		StableFor:     500 * time.Millisecond,
		PollInterval:  100 * time.Millisecond,
		ContinueOnErr: false,
	}
}

// PageWaiter orchestrates multiple wait strategies for page readiness.
// V2: ContinueOnErr support, per-strategy error collection, and target selector.
type PageWaiter struct {
	timeout        time.Duration
	strategy       WaitStrategy
	stableFor      time.Duration
	pollInterval   time.Duration
	continueOnErr  bool
	targetSelector string
	metrics        *Metrics
}

// NewPageWaiter creates a page waiter with the given timeout and strategy.
func NewPageWaiter(timeout time.Duration, strategy WaitStrategy) *PageWaiter {
	return &PageWaiter{
		timeout:      timeout,
		strategy:     strategy,
		stableFor:    500 * time.Millisecond,
		pollInterval: 100 * time.Millisecond,
	}
}

// WithMetrics attaches Prometheus metrics to the PageWaiter.
func (w *PageWaiter) WithMetrics(m *Metrics) *PageWaiter {
	w.metrics = m
	return w
}

// WithTargetSelector sets the CSS selector for WaitElementVisible/Enabled strategies.
func (w *PageWaiter) WithTargetSelector(sel string) *PageWaiter {
	w.targetSelector = sel
	return w
}

// NewPageWaiterFromConfig creates a waiter from a WaitConfig.
func NewPageWaiterFromConfig(cfg WaitConfig) *PageWaiter {
	si := cfg.StableFor
	if si == 0 {
		si = 500 * time.Millisecond
	}
	pi := cfg.PollInterval
	if pi == 0 {
		pi = 100 * time.Millisecond
	}
	return &PageWaiter{
		timeout:        cfg.Timeout,
		strategy:       cfg.Strategy,
		stableFor:      si,
		pollInterval:   pi,
		continueOnErr:  cfg.ContinueOnErr,
		targetSelector: cfg.TargetSelector,
	}
}

// PrepareNetworkIdleWait sets up the Chrome lifecycle listener and returns a
// blocking function. Call this BEFORE navigation so the networkIdle event
// cannot race past an unregistered listener.
func (w *PageWaiter) PrepareNetworkIdleWait(ctx context.Context) (wait func() error, err error) {
	if err := chromedp.Run(ctx, page.Enable(), page.SetLifecycleEventsEnabled(true)); err != nil {
		return nil, fmt.Errorf("enable lifecycle events: %w", err)
	}

	ch := make(chan struct{})
	var once sync.Once

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if lce, ok := ev.(*page.EventLifecycleEvent); ok {
			if lce.Name == "networkIdle" {
				once.Do(func() { close(ch) })
			}
		}
	})

	return func() error {
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil
}

// WaitResult collects per-strategy outcomes from a composite wait operation.
type WaitResult struct {
	Strategy WaitStrategy
	Label    string
	Err      error
	Duration time.Duration
}

// NavigateAndWait performs an atomic navigate-then-wait where the lifecycle
// listener is registered BEFORE the navigation starts, preventing the race
// where networkIdle fires before the listener is attached.
//
// V2: When ContinueOnErr is true, individual strategy failures are collected
// in the returned WaitResult slice instead of aborting the whole operation.
func (w *PageWaiter) NavigateAndWait(ctx context.Context, url string) error {
	_, err := w.NavigateAndWaitV2(ctx, url)
	return err
}

// NavigateAndWaitV2 is the V2 entry point that returns per-strategy results.
func (w *PageWaiter) NavigateAndWaitV2(ctx context.Context, url string) ([]WaitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	start := time.Now()
	stratLabel := w.strategyLabel()
	var results []WaitResult

	var waitNetIdle func() error
	if w.strategy&WaitNetworkIdle != 0 {
		fn, err := w.PrepareNetworkIdleWait(ctx)
		if err != nil {
			w.recordMetrics(stratLabel, "error", time.Since(start))
			return nil, fmt.Errorf("prepare network idle: %w", err)
		}
		waitNetIdle = fn
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		w.recordMetrics(stratLabel, "error", time.Since(start))
		return nil, fmt.Errorf("navigate to %s: %w", url, err)
	}

	if waitNetIdle != nil {
		sStart := time.Now()
		err := waitNetIdle()
		results = append(results, WaitResult{
			Strategy: WaitNetworkIdle, Label: "network_idle",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			w.recordMetrics(stratLabel, "timeout", time.Since(start))
			return results, fmt.Errorf("network idle wait: %w", err)
		}
	}

	if w.strategy&WaitDOMStable != 0 {
		sStart := time.Now()
		err := w.WaitForDOMStable(ctx, w.stableFor)
		results = append(results, WaitResult{
			Strategy: WaitDOMStable, Label: "dom_stable",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			w.recordMetrics(stratLabel, "timeout", time.Since(start))
			return results, fmt.Errorf("DOM stability wait: %w", err)
		}
	}

	if w.strategy&WaitElementVisible != 0 && w.targetSelector != "" {
		sStart := time.Now()
		err := w.WaitForElement(ctx, w.targetSelector)
		results = append(results, WaitResult{
			Strategy: WaitElementVisible, Label: "element_visible",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			w.recordMetrics(stratLabel, "timeout", time.Since(start))
			return results, fmt.Errorf("element visible wait: %w", err)
		}
	}

	if w.strategy&WaitElementEnabled != 0 && w.targetSelector != "" {
		sStart := time.Now()
		err := w.WaitForElementEnabled(ctx, w.targetSelector)
		results = append(results, WaitResult{
			Strategy: WaitElementEnabled, Label: "element_enabled",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			w.recordMetrics(stratLabel, "timeout", time.Since(start))
			return results, fmt.Errorf("element enabled wait: %w", err)
		}
	}

	w.recordMetrics(stratLabel, "success", time.Since(start))
	return results, nil
}

func (w *PageWaiter) strategyLabel() string {
	switch w.strategy {
	case WaitNetworkIdle:
		return "network_idle"
	case WaitDOMStable:
		return "dom_stable"
	case WaitNetworkIdle | WaitDOMStable:
		return "network_and_dom"
	default:
		return "custom"
	}
}

func (w *PageWaiter) recordMetrics(strategy, result string, dur time.Duration) {
	if w.metrics == nil {
		return
	}
	w.metrics.PageWaitDuration.WithLabelValues(strategy).Observe(dur.Seconds())
	w.metrics.PageWaitTotal.WithLabelValues(strategy, result).Inc()
}

// WaitForPageReady composes wait strategies for pages that have already
// navigated. For new navigations, prefer NavigateAndWait to avoid races.
//
// V2: Respects ContinueOnErr -- when true, strategy failures are collected
// but do not abort the overall wait.
func (w *PageWaiter) WaitForPageReady(ctx context.Context) error {
	_, err := w.WaitForPageReadyV2(ctx)
	return err
}

// WaitForPageReadyV2 returns per-strategy results alongside the final error.
func (w *PageWaiter) WaitForPageReadyV2(ctx context.Context) ([]WaitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	var results []WaitResult

	if w.strategy&WaitNetworkIdle != 0 {
		sStart := time.Now()
		err := w.WaitForNetworkIdle(ctx)
		results = append(results, WaitResult{
			Strategy: WaitNetworkIdle, Label: "network_idle",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			return results, fmt.Errorf("network idle wait failed: %w", err)
		}
	}

	if w.strategy&WaitDOMStable != 0 {
		sStart := time.Now()
		err := w.WaitForDOMStable(ctx, w.stableFor)
		results = append(results, WaitResult{
			Strategy: WaitDOMStable, Label: "dom_stable",
			Err: err, Duration: time.Since(sStart),
		})
		if err != nil && !w.continueOnErr {
			return results, fmt.Errorf("DOM stability wait failed: %w", err)
		}
	}

	return results, nil
}

// WaitForElement waits for a CSS selector to become visible.
func (w *PageWaiter) WaitForElement(ctx context.Context, sel string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	return chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery))
}

// WaitForElementEnabled waits for a CSS selector to be visible and not disabled.
// It first waits for the element to appear in the DOM, then polls until the
// element's "disabled" property is false. This handles dynamically created
// elements that may not exist initially (where chromedp.WaitEnabled alone
// can fail with "could not find node").
func (w *PageWaiter) WaitForElementEnabled(ctx context.Context, sel string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("wait for element visible %q: %w", sel, err)
	}

	checkScript := fmt.Sprintf(`(function() {
		var el = document.querySelector(%q);
		if (!el) return false;
		return !el.disabled;
	})()`, sel)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("element %q not enabled before timeout: %w", sel, ctx.Err())
		case <-ticker.C:
			var enabled bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(checkScript, &enabled)); err != nil {
				continue
			}
			if enabled {
				return nil
			}
		}
	}
}

// WaitForNetworkIdle waits for Chrome's native networkIdle lifecycle event.
// Prefer PrepareNetworkIdleWait + NavigateAndWait for new navigations; this
// method is for post-navigation waits (SPA transitions, lazy loads).
//
// To avoid a timing race where networkIdle fires before the listener is
// registered, we first check if the page is already in a complete/idle
// state. If so, we return immediately rather than hanging until context
// timeout.
func (w *PageWaiter) WaitForNetworkIdle(ctx context.Context) error {
	if err := chromedp.Run(ctx, page.Enable(), page.SetLifecycleEventsEnabled(true)); err != nil {
		return err
	}

	// Pre-check: if document is already complete and no pending XHR/fetch,
	// networkIdle has already passed -- return immediately.
	var readyState string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.readyState`, &readyState)); err == nil {
		if readyState == "complete" {
			var pending int64
			checkPending := `(function(){try{return performance.getEntriesByType('resource').filter(e=>!e.responseEnd).length}catch(e){return 0}})()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(checkPending, &pending)); err == nil && pending == 0 {
				return nil
			}
		}
	}

	ch := make(chan struct{})
	var once sync.Once

	lctx, lcancel := context.WithCancel(ctx)
	defer lcancel()

	chromedp.ListenTarget(lctx, func(ev interface{}) {
		if lce, ok := ev.(*page.EventLifecycleEvent); ok {
			if lce.Name == "networkIdle" {
				once.Do(func() { close(ch) })
			}
		}
	})

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitForSPARouteChange waits for the URL to change and then waits for DOM stability.
func (w *PageWaiter) WaitForSPARouteChange(ctx context.Context, currentURL string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf("window.location.href !== '%s'", currentURL), nil))
	if err != nil {
		return fmt.Errorf("URL did not change: %w", err)
	}

	return w.WaitForDOMStable(ctx, w.stableFor)
}

// WaitForDOMStable waits until the DOM stops mutating for stableFor duration.
func (w *PageWaiter) WaitForDOMStable(ctx context.Context, stableFor time.Duration) error {
	// Reset mutation counter on each call so stale values from previous
	// navigations do not fool the stability check.
	initScript := `
		(function() {
			window.__dom_mutations = 0;
			if (window.__dom_observer) { window.__dom_observer.disconnect(); }
			window.__dom_observer = new MutationObserver(() => {
				window.__dom_mutations++;
			});
			window.__dom_observer.observe(document, { childList: true, subtree: true, attributes: true });
			return window.__dom_mutations;
		})()
	`

	if err := chromedp.Run(ctx, chromedp.Evaluate(initScript, nil)); err != nil {
		return fmt.Errorf("inject MutationObserver: %w", err)
	}

	readScript := `window.__dom_mutations`

	var lastMutations int
	stableStart := time.Now()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var currentMutations int
			err := chromedp.Run(ctx, chromedp.Evaluate(readScript, &currentMutations))
			if err != nil {
				stableStart = time.Now()
				continue
			}

			if currentMutations == lastMutations {
				if time.Since(stableStart) >= stableFor {
					return nil
				}
			} else {
				lastMutations = currentMutations
				stableStart = time.Now()
			}
		}
	}
}
