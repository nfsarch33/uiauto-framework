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

// Browser abstracts browser automation backends (chromedp, Playwright MCP, etc.).
// All uiauto components depend on this interface, not on BrowserAgent directly.
type Browser interface {
	Navigate(url string) error
	NavigateWithConfig(url string, cfg WaitConfig) error
	CaptureDOM() (string, error)
	CaptureScreenshot() ([]byte, error)
	Click(selector string) error
	Type(selector, text string) error
	Evaluate(expression string, res interface{}) error
	// IsVisible reports whether the selector currently resolves to an element
	// that is in the DOM and not hidden via display:none, visibility:hidden,
	// or zero-size box. Backs the "verify" action type.
	IsVisible(selector string) (bool, error)
	// SwitchToFrame attaches subsequent calls to the given iframe element.
	// The returned release function must be called to restore the main frame
	// context. Backs the "frame" action type for any embedded iframe app.
	SwitchToFrame(selector string) (release func(), err error)
	Close()
}

// BrowserFactory creates new Browser instances on demand.
type BrowserFactory func() (Browser, error)

// BrowserPool manages a bounded set of reusable Browser sessions.
type BrowserPool struct {
	mu          sync.Mutex
	factory     BrowserFactory
	maxSize     int
	idle        []Browser
	active      int32
	logger      *slog.Logger
	closeOnce   sync.Once
	closed      bool
	totalLeased int64
}

// BrowserPoolConfig controls pool sizing and behavior.
type BrowserPoolConfig struct {
	MaxSize int
	Factory BrowserFactory
	Logger  *slog.Logger
}

// NewBrowserPool creates a pool with the given factory and max concurrency.
func NewBrowserPool(cfg BrowserPoolConfig) *BrowserPool {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 4
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &BrowserPool{
		factory: cfg.Factory,
		maxSize: cfg.MaxSize,
		idle:    make([]Browser, 0, cfg.MaxSize),
		logger:  cfg.Logger,
	}
}

// Acquire returns an idle browser or creates a new one if under the limit.
// If the pool is full, it blocks until one is returned or ctx is cancelled.
func (p *BrowserPool) Acquire(ctx context.Context) (Browser, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, fmt.Errorf("browser pool is closed")
		}

		// Return an idle session if available
		if len(p.idle) > 0 {
			b := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]
			atomic.AddInt32(&p.active, 1)
			atomic.AddInt64(&p.totalLeased, 1)
			p.mu.Unlock()
			return b, nil
		}

		// Create a new session if under limit
		if int(atomic.LoadInt32(&p.active))+len(p.idle) < p.maxSize {
			atomic.AddInt32(&p.active, 1)
			atomic.AddInt64(&p.totalLeased, 1)
			p.mu.Unlock()
			b, err := p.factory()
			if err != nil {
				atomic.AddInt32(&p.active, -1)
				return nil, fmt.Errorf("browser factory failed: %w", err)
			}
			return b, nil
		}
		p.mu.Unlock()

		// Pool full — wait and retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Release returns a browser to the pool for reuse.
func (p *BrowserPool) Release(b Browser) {
	p.mu.Lock()
	defer p.mu.Unlock()

	atomic.AddInt32(&p.active, -1)
	if p.closed {
		b.Close()
		return
	}
	if len(p.idle) < p.maxSize {
		p.idle = append(p.idle, b)
	} else {
		b.Close()
	}
}

// CloseAll shuts down all idle browsers and marks the pool as closed.
func (p *BrowserPool) CloseAll() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.closed = true
		for _, b := range p.idle {
			b.Close()
		}
		p.idle = nil
	})
}

// Stats returns current pool utilization metrics.
func (p *BrowserPool) Stats() BrowserPoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return BrowserPoolStats{
		MaxSize:     p.maxSize,
		Idle:        len(p.idle),
		Active:      int(atomic.LoadInt32(&p.active)),
		TotalLeased: atomic.LoadInt64(&p.totalLeased),
		Closed:      p.closed,
	}
}

// BrowserPoolStats exposes pool metrics for observability.
type BrowserPoolStats struct {
	MaxSize     int   `json:"max_size"`
	Idle        int   `json:"idle"`
	Active      int   `json:"active"`
	TotalLeased int64 `json:"total_leased"`
	Closed      bool  `json:"closed"`
}

// ChromeDPBrowserAdapter wraps BrowserAgent to satisfy the Browser interface.
type ChromeDPBrowserAdapter struct {
	agent *BrowserAgent
}

// NewChromeDPBrowserAdapter creates an adapter from an existing BrowserAgent.
func NewChromeDPBrowserAdapter(agent *BrowserAgent) *ChromeDPBrowserAdapter {
	return &ChromeDPBrowserAdapter{agent: agent}
}

// Navigate opens the given URL in the browser.
func (a *ChromeDPBrowserAdapter) Navigate(url string) error {
	return a.agent.Navigate(url)
}

// NavigateWithConfig opens the given URL with a custom wait configuration.
func (a *ChromeDPBrowserAdapter) NavigateWithConfig(url string, cfg WaitConfig) error {
	return a.agent.NavigateWithConfig(url, cfg)
}

// CaptureDOM returns the current page's outer HTML.
func (a *ChromeDPBrowserAdapter) CaptureDOM() (string, error) {
	return a.agent.CaptureDOM()
}

// CaptureScreenshot takes a full-page screenshot and returns PNG bytes.
func (a *ChromeDPBrowserAdapter) CaptureScreenshot() ([]byte, error) {
	return a.agent.CaptureScreenshot()
}

// Click clicks the element matching the given selector.
func (a *ChromeDPBrowserAdapter) Click(selector string) error {
	return a.agent.Click(selector)
}

// Type types text into the element matching the given selector.
func (a *ChromeDPBrowserAdapter) Type(selector, text string) error {
	return a.agent.Type(selector, text)
}

// Evaluate runs a JavaScript expression in the page context.
func (a *ChromeDPBrowserAdapter) Evaluate(expression string, res interface{}) error {
	return a.agent.Evaluate(expression, res)
}

// IsVisible reports whether the selector resolves to a visible element.
func (a *ChromeDPBrowserAdapter) IsVisible(selector string) (bool, error) {
	return a.agent.IsVisible(selector)
}

// SwitchToFrame attaches subsequent calls to an iframe; release restores main.
func (a *ChromeDPBrowserAdapter) SwitchToFrame(selector string) (func(), error) {
	return a.agent.SwitchToFrame(selector)
}

// Close releases browser resources.
func (a *ChromeDPBrowserAdapter) Close() {
	a.agent.Close()
}

// Unwrap returns the underlying BrowserAgent (for chromedp-specific ops like GetNodes).
func (a *ChromeDPBrowserAdapter) Unwrap() *BrowserAgent {
	return a.agent
}

// ChromeDPFactory returns a BrowserFactory that creates headless chromedp sessions.
func ChromeDPFactory(headless bool) BrowserFactory {
	return func() (Browser, error) {
		agent, err := NewBrowserAgent(headless)
		if err != nil {
			return nil, err
		}
		return NewChromeDPBrowserAdapter(agent), nil
	}
}

// ChromeDPRemoteFactory returns a BrowserFactory connecting to an existing Chrome debug session.
func ChromeDPRemoteFactory(debugURL string) BrowserFactory {
	return func() (Browser, error) {
		agent, err := NewBrowserAgentWithRemote(debugURL)
		if err != nil {
			return nil, err
		}
		return NewChromeDPBrowserAdapter(agent), nil
	}
}
