package uiauto

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestPageWaiter_WaitForElement(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<body>
				<div id="target" style="display:none;">Hello</div>
				<script>
					setTimeout(() => {
						document.getElementById("target").style.display = "block";
					}, 500);
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Close()

	if err := chromedp.Run(browser.ctx, chromedp.Navigate(ts.URL)); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	waiter := NewPageWaiter(2*time.Second, WaitElementVisible)
	ctx := browser.ctx

	start := time.Now()
	err = waiter.WaitForElement(ctx, "#target")
	if err != nil {
		t.Fatalf("WaitForElement failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 400*time.Millisecond {
		t.Errorf("WaitForElement returned too quickly: %v", elapsed)
	}
}

func TestPageWaiter_WaitForElementEnabled(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<body>
				<button id="submit" disabled>Submit</button>
				<script>
					setTimeout(() => {
						document.getElementById("submit").disabled = false;
					}, 500);
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Close()

	if err := chromedp.Run(browser.ctx, chromedp.Navigate(ts.URL)); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	waiter := NewPageWaiter(3*time.Second, WaitElementEnabled)

	start := time.Now()
	err = waiter.WaitForElementEnabled(browser.ctx, "#submit")
	if err != nil {
		t.Fatalf("WaitForElementEnabled failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 400*time.Millisecond {
		t.Errorf("WaitForElementEnabled returned too quickly: %v", elapsed)
	}
}

func TestPageWaiter_WaitForDOMStable(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<body>
				<div id="content"></div>
				<script>
					let count = 0;
					const interval = setInterval(() => {
						document.getElementById("content").innerHTML += "<p>Item " + count + "</p>";
						count++;
						if (count > 5) clearInterval(interval);
					}, 100);
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Close()

	if err := chromedp.Run(browser.ctx, chromedp.Navigate(ts.URL)); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	waiter := NewPageWaiter(3*time.Second, WaitDOMStable)
	ctx := browser.ctx

	start := time.Now()
	err = waiter.WaitForDOMStable(ctx, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForDOMStable failed: %v", err)
	}
	elapsed := time.Since(start)

	// 5 items * 100ms = 500ms + 300ms stability wait = ~800ms
	if elapsed < 700*time.Millisecond {
		t.Errorf("WaitForDOMStable returned too quickly: %v", elapsed)
	}
}

func TestPageWaiter_WaitForNetworkIdle(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/data" {
			time.Sleep(500 * time.Millisecond)
			w.Write([]byte(`{"status": "ok"}`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<body>
				<script>
					fetch('/data');
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Close()

	// Wait for network idle after navigating
	waiter := NewPageWaiter(3*time.Second, WaitNetworkIdle)

	if err := chromedp.Run(browser.ctx, chromedp.Navigate(ts.URL)); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	start := time.Now()
	err = waiter.WaitForNetworkIdle(browser.ctx)
	if err != nil {
		t.Fatalf("WaitForNetworkIdle failed: %v", err)
	}
	elapsed := time.Since(start)

	// Since networkIdle happens 500ms after the last request finishes,
	// and the request takes 500ms, it should take at least 500ms.
	if elapsed < 400*time.Millisecond {
		t.Errorf("WaitForNetworkIdle returned too quickly: %v", elapsed)
	}
}

func TestNewPageWaiterFromConfig(t *testing.T) {
	cfg := DefaultWaitConfig()
	cfg.Timeout = 30 * time.Second
	cfg.Strategy = WaitDOMStable
	cfg.StableFor = 1 * time.Second
	cfg.PollInterval = 200 * time.Millisecond

	w := NewPageWaiterFromConfig(cfg)
	if w.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", w.timeout)
	}
	if w.strategy != WaitDOMStable {
		t.Errorf("strategy = %v, want WaitDOMStable", w.strategy)
	}
	if w.stableFor != 1*time.Second {
		t.Errorf("stableFor = %v, want 1s", w.stableFor)
	}
	if w.pollInterval != 200*time.Millisecond {
		t.Errorf("pollInterval = %v, want 200ms", w.pollInterval)
	}
}

func TestNewPageWaiterFromConfig_Defaults(t *testing.T) {
	w := NewPageWaiterFromConfig(WaitConfig{
		Timeout:  5 * time.Second,
		Strategy: WaitNetworkIdle,
	})
	if w.stableFor != 500*time.Millisecond {
		t.Errorf("stableFor should default to 500ms, got %v", w.stableFor)
	}
	if w.pollInterval != 100*time.Millisecond {
		t.Errorf("pollInterval should default to 100ms, got %v", w.pollInterval)
	}
}

func TestDefaultWaitConfig(t *testing.T) {
	cfg := DefaultWaitConfig()
	if cfg.Timeout != 15*time.Second {
		t.Errorf("Timeout = %v, want 15s", cfg.Timeout)
	}
	if cfg.Strategy != WaitNetworkIdle|WaitDOMStable {
		t.Errorf("Strategy = %v, want NetworkIdle|DOMStable", cfg.Strategy)
	}
	if cfg.ContinueOnErr {
		t.Error("ContinueOnErr should be false by default")
	}
}

func TestPageWaiter_NavigateAndWait(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/data" {
			time.Sleep(300 * time.Millisecond)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
			<div id="content">Loading</div>
			<script>
				fetch('/data').then(r => r.json()).then(d => {
					document.getElementById('content').textContent = 'Loaded';
				});
			</script>
		</body></html>`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	defer browser.Close()

	waiter := NewPageWaiter(5*time.Second, WaitNetworkIdle|WaitDOMStable)
	if err := waiter.NavigateAndWait(browser.ctx, ts.URL); err != nil {
		t.Fatalf("NavigateAndWait: %v", err)
	}

	var text string
	if err := browser.Evaluate(`document.getElementById('content').textContent`, &text); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if text != "Loaded" {
		t.Errorf("content = %q, want 'Loaded'", text)
	}
}

func TestPageWaiter_WaitForSPARouteChange(t *testing.T) {
	skipWithoutBrowser(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`
			<html>
			<body>
				<script>
					setTimeout(() => {
						window.history.pushState({}, '', '/new-route');
					}, 500);
				</script>
			</body>
			</html>
		`))
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Close()

	if err := chromedp.Run(browser.ctx, chromedp.Navigate(ts.URL)); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	waiter := NewPageWaiter(3*time.Second, WaitSPARouteChange)

	start := time.Now()
	err = waiter.WaitForSPARouteChange(browser.ctx, ts.URL+"/")
	if err != nil {
		t.Fatalf("WaitForSPARouteChange failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 400*time.Millisecond {
		t.Errorf("WaitForSPARouteChange returned too quickly: %v", elapsed)
	}
}
