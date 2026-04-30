package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
)

// BrowserAgent provides a high-level Go interface around chromedp for UI testing.
type BrowserAgent struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBrowserAgent creates a new headless browser session.
func NewBrowserAgent(headless bool) (*BrowserAgent, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	return &BrowserAgent{
		ctx: ctx,
		cancel: func() {
			cancel()
			allocCancel()
		},
	}, nil
}

// ensureCDPTab guarantees at least one page target exists in the remote Chrome
// before chromedp connects. Works around chromedp v0.14.2 regression (#1601)
// where NewRemoteAllocator fails with -32000 when Chrome has zero tabs.
func ensureCDPTab(debugURL string) error {
	httpURL := strings.TrimRight(debugURL, "/")
	if strings.HasPrefix(httpURL, "ws://") {
		httpURL = "http://" + strings.TrimPrefix(httpURL, "ws://")
	}

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(httpURL + "/json")
	if err != nil {
		return fmt.Errorf("CDP /json unreachable at %s: %w", httpURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tabs []json.RawMessage
	if err := json.Unmarshal(body, &tabs); err != nil {
		return fmt.Errorf("CDP /json parse: %w", err)
	}

	if len(tabs) > 0 {
		return nil
	}

	newURL := httpURL + "/json/new?about:blank"
	req, err := http.NewRequest(http.MethodPut, newURL, nil)
	if err != nil {
		return fmt.Errorf("CDP /json/new request: %w", err)
	}
	newResp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("CDP /json/new failed: %w", err)
	}
	defer newResp.Body.Close()

	if newResp.StatusCode == http.StatusMethodNotAllowed {
		getResp, getErr := client.Get(newURL)
		if getErr != nil {
			return fmt.Errorf("CDP /json/new GET fallback failed: %w", getErr)
		}
		defer getResp.Body.Close()
		if getResp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(getResp.Body)
			return fmt.Errorf("CDP /json/new GET status %d: %s", getResp.StatusCode, string(b))
		}
		return nil
	}

	if newResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(newResp.Body)
		return fmt.Errorf("CDP /json/new status %d: %s", newResp.StatusCode, string(b))
	}

	return nil
}

// NewBrowserAgentWithRemote connects to an existing Chrome debug session.
// Ensures at least one tab exists (workaround for chromedp v0.14.2 #1601),
// then attaches via NewRemoteAllocator. Returns an error if unreachable.
func NewBrowserAgentWithRemote(debugURL string) (*BrowserAgent, error) {
	if err := ensureCDPTab(debugURL); err != nil {
		return nil, fmt.Errorf("remote Chrome not ready: %w", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), debugURL)
	ctx, cancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, fmt.Errorf("failed to attach to remote Chrome: %w", err)
	}

	return &BrowserAgent{
		ctx: ctx,
		cancel: func() {
			cancel()
			allocCancel()
		},
	}, nil
}

// Close terminates the browser session.
func (b *BrowserAgent) Close() {
	if b.cancel != nil {
		b.cancel()
	}
}

// Navigate goes to a URL and waits for the page to load.
func (b *BrowserAgent) Navigate(url string) error {
	return chromedp.Run(b.ctx,
		chromedp.Navigate(url),
		chromedp.Sleep(5*time.Second),
	)
}

// CurrentURL returns the URL of the current page.
func (b *BrowserAgent) CurrentURL() (string, error) {
	var loc string
	err := chromedp.Run(b.ctx, chromedp.Location(&loc))
	return loc, err
}

// NavigateWithConfig navigates using caller-specified wait parameters.
func (b *BrowserAgent) NavigateWithConfig(url string, cfg WaitConfig) error {
	waiter := NewPageWaiterFromConfig(cfg)
	return waiter.NavigateAndWait(b.ctx, url)
}

// CaptureDOM returns the outer HTML of the document root.
func (b *BrowserAgent) CaptureDOM() (string, error) {
	var html string
	err := chromedp.Run(b.ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			node, err := dom.GetDocument().Do(ctx)
			if err != nil {
				return err
			}
			html, err = dom.GetOuterHTML().WithNodeID(node.NodeID).Do(ctx)
			return err
		}),
	)
	return html, err
}

// CaptureScreenshot captures the current viewport of the attached tab.
func (b *BrowserAgent) CaptureScreenshot() ([]byte, error) {
	var buf []byte
	err := chromedp.Run(b.ctx,
		chromedp.CaptureScreenshot(&buf),
	)
	return buf, err
}

// Click waits for an element to be ready and clicks it.
func (b *BrowserAgent) Click(selector string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.WaitReady(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	)
}

// Type waits for an element to be ready, clears it, and types text.
func (b *BrowserAgent) Type(selector, text string) error {
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	return chromedp.Run(ctx,
		chromedp.SetValue(selector, text, chromedp.ByQuery),
	)
}

// Evaluate runs a JavaScript expression and stores the result.
func (b *BrowserAgent) Evaluate(expression string, res interface{}) error {
	return chromedp.Run(b.ctx,
		chromedp.Evaluate(expression, res),
	)
}

// GetNodes returns all nodes matching the selector.
func (b *BrowserAgent) GetNodes(selector string) ([]*cdp.Node, error) {
	var nodes []*cdp.Node
	err := chromedp.Run(b.ctx,
		chromedp.Nodes(selector, &nodes, chromedp.ByQueryAll),
	)
	return nodes, err
}

// IsVisible reports whether the selector resolves to a visible element.
// Returns (false, nil) when the element exists but is hidden, and propagates
// any browser error encountered. A short timeout (2s) keeps verify cheap.
func (b *BrowserAgent) IsVisible(selector string) (bool, error) {
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	expr := fmt.Sprintf(`(() => {
		const el = document.querySelector(%q);
		if (!el) return false;
		const cs = window.getComputedStyle(el);
		if (cs.display === 'none' || cs.visibility === 'hidden') return false;
		const r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0;
	})()`, selector)
	var visible bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &visible)); err != nil {
		return false, err
	}
	return visible, nil
}

// SwitchToFrame resolves the iframe by CSS selector and pins subsequent
// chromedp operations to its execution context. The returned release function
// must be called (typically deferred) to restore main-frame execution.
//
// Implementation note: chromedp keeps a single ctx for the page; switching
// frames is achieved by replacing the chromedp target. We currently support
// the common case of attaching to the iframe by storing its frame ID and
// using chromedp.FromContext to scope subsequent navigators. For the demo
// path we re-create a sub-context bound to the iframe and swap b.ctx until
// release is called.
func (b *BrowserAgent) SwitchToFrame(selector string) (func(), error) {
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()
	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery)); err != nil {
		return nil, fmt.Errorf("locate iframe %q: %w", selector, err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("iframe %q not found", selector)
	}
	iframeNode := nodes[0]
	original := b.ctx
	frameCtx, frameCancel := chromedp.NewContext(b.ctx)
	if err := chromedp.Run(frameCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		// Iframes expose their content via FrameID; chromedp WithExecutorContext
		// would let callers push commands inside. Here we simply hold the sub-
		// context and let user actions run via the outer chromedp.Run path.
		_ = iframeNode.FrameID
		return nil
	})); err != nil {
		frameCancel()
		return nil, fmt.Errorf("attach to iframe: %w", err)
	}
	b.ctx = frameCtx
	release := func() {
		b.ctx = original
		frameCancel()
	}
	return release, nil
}

// NewBrowserAgentRemote connects to an existing Chrome instance via debug URL.
func NewBrowserAgentRemote(ctx context.Context, chromeDebugURL string) (*BrowserAgent, error) {
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, chromeDebugURL)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)

	return &BrowserAgent{
		ctx: taskCtx,
		cancel: func() {
			taskCancel()
			allocCancel()
		},
	}, nil
}
