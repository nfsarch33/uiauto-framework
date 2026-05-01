package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PageWaiterBenchResult captures the result of a single page wait benchmark.
type PageWaiterBenchResult struct {
	PageType       string        `json:"page_type"`
	URL            string        `json:"url"`
	WaitDuration   time.Duration `json:"wait_duration_ns"`
	WaitDurationMs float64       `json:"wait_duration_ms"`
	DOMCaptured    bool          `json:"dom_captured"`
	DOMLength      int           `json:"dom_length"`
	Strategy       string        `json:"strategy"`
	Success        bool          `json:"success"`
	Error          string        `json:"error,omitempty"`
}

// PageWaiterBenchReport is the complete benchmark report.
type PageWaiterBenchReport struct {
	Timestamp time.Time               `json:"timestamp"`
	Platform  string                  `json:"platform"`
	Results   []PageWaiterBenchResult `json:"results"`
	Summary   BenchSummary            `json:"summary"`
}

// BenchSummary provides aggregate benchmark statistics.
type BenchSummary struct {
	TotalPages  int     `json:"total_pages"`
	Passed      int     `json:"passed"`
	Failed      int     `json:"failed"`
	AvgWaitMs   float64 `json:"avg_wait_ms"`
	MaxWaitMs   float64 `json:"max_wait_ms"`
	SuccessRate float64 `json:"success_rate"`
}

func requireBenchmark(t *testing.T) {
	t.Helper()
	if os.Getenv("UIAUTO_BENCHMARK") != "1" {
		t.Skip("set UIAUTO_BENCHMARK=1 to run benchmark tests")
	}
}

func benchmarkFixtureServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/static", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><h1 id="heading">Static Page</h1><p>Simple content.</p></body></html>`)
	})

	mux.HandleFunc("/ssr", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>SSR Page</title></head><body>
			<header><nav><a href="/">Home</a><a href="/about">About</a></nav></header>
			<main id="content"><h1>Server Rendered</h1><ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul></main>
			<footer>Footer</footer>
		</body></html>`)
	})

	mux.HandleFunc("/delayed", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="container"></div>
			<script>
				setTimeout(function() {
					document.getElementById('container').innerHTML = '<div id="loaded"><h1>Loaded</h1><p>Content appeared after delay.</p></div>';
				}, 800);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/spa", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="app"><p>Loading...</p></div>
			<script>
				var phases = ['Phase 1: Init', 'Phase 2: Data', 'Phase 3: Render'];
				var delay = 200;
				phases.forEach(function(phase, i) {
					setTimeout(function() {
						var el = document.createElement('div');
						el.className = 'phase';
						el.textContent = phase;
						document.getElementById('app').appendChild(el);
					}, delay * (i + 1));
				});
				setTimeout(function() {
					document.getElementById('app').querySelector('p').textContent = 'Ready';
				}, delay * (phases.length + 1));
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/lazy-images", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<h1>Lazy Content</h1>
			<div id="gallery"></div>
			<script>
				setTimeout(function() {
					var gallery = document.getElementById('gallery');
					for (var i = 0; i < 5; i++) {
						var img = document.createElement('div');
						img.className = 'lazy-item';
						img.setAttribute('data-loaded', 'true');
						img.textContent = 'Item ' + (i+1);
						gallery.appendChild(img);
					}
				}, 400);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/form", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<form id="login-form">
				<input id="email" type="email" placeholder="Email">
				<input id="password" type="password" placeholder="Password">
				<button id="submit" type="submit">Sign In</button>
			</form>
			<script>
				document.getElementById('login-form').addEventListener('submit', function(e) { e.preventDefault(); });
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/heavy-dom", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!DOCTYPE html><html><body><div id="root">`
		for i := 0; i < 200; i++ {
			html += fmt.Sprintf(`<div class="row" data-row="%d"><span class="cell">Cell %d</span></div>`, i, i)
		}
		html += `</div></body></html>`
		fmt.Fprint(w, html)
	})

	mux.HandleFunc("/mutation-storm", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="ticker"></div>
			<script>
				var count = 0;
				var iv = setInterval(function() {
					document.getElementById('ticker').textContent = 'Tick ' + (++count);
					if (count >= 10) clearInterval(iv);
				}, 100);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/ajax-poll", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="poll-status">Polling...</div>
			<script>
				var pollCount = 0;
				function poll() {
					setTimeout(function() {
						pollCount++;
						document.getElementById('poll-status').innerHTML = '<span id="poll-result">Data v' + pollCount + '</span>';
						if (pollCount < 5) poll();
					}, 150);
				}
				poll();
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/shadow-dom", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="host"></div>
			<script>
				var host = document.getElementById('host');
				var root = host.attachShadow({mode: 'open'});
				root.innerHTML = '<div id="shadow-content"><p>Shadow content</p></div>';
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/web-component", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<my-widget id="wc"></my-widget>
			<script>
				customElements.define('my-widget', class extends HTMLElement {
					connectedCallback() {
						this.innerHTML = '<div id="wc-content">Widget ready</div>';
					}
				});
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/infinite-scroll", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="height:200px;overflow:auto">
			<div id="feed"><div class="item">Item 1</div></div>
			<script>
				var feed = document.getElementById('feed');
				var obs = new IntersectionObserver(function(entries) {
					if (entries[0].isIntersecting) {
						var n = feed.children.length + 1;
						feed.appendChild(document.createElement('div')).className = 'item';
						feed.lastChild.textContent = 'Item ' + n;
					}
				}, {threshold: 0.5});
				setTimeout(function() {
					obs.observe(feed.lastChild);
					feed.appendChild(document.createElement('div')).className = 'item';
					feed.lastChild.textContent = 'Item 2';
				}, 300);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/auth-redirect", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="auth-state">Redirecting...</div>
			<script>
				setTimeout(function() {
					document.getElementById('auth-state').innerHTML = '<div id="logged-in">Welcome, user</div>';
				}, 200);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/d2l-module-list", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="d2l-modules" class="d2l-module-list">
				<div class="d2l-module" data-module="1"><h3>Module 1</h3><div class="d2l-content">Content 1</div></div>
				<div class="d2l-module" data-module="2"><h3>Module 2</h3><div class="d2l-content">Content 2</div></div>
			</div>
			<script>
				setTimeout(function() {
					document.querySelectorAll('.d2l-module').forEach(function(m) {
						m.classList.add('d2l-expanded');
					});
				}, 250);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/d2l-content-viewer", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="d2l-viewer" class="d2l-content-viewer">
				<div class="d2l-toolbar">Toolbar</div>
				<div class="d2l-frame-container"><div id="d2l-content-body">Loading content...</div></div>
			</div>
			<script>
				setTimeout(function() {
					document.getElementById('d2l-content-body').innerHTML = '<article><h1>Unit Content</h1><p>Body text.</p></article>';
				}, 180);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/woocommerce-product", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="product" class="product">
				<div id="gallery" class="woocommerce-product-gallery"></div>
				<div class="summary"><h1>Product Name</h1><p class="price">$29.99</p></div>
			</div>
			<script>
				setTimeout(function() {
					var g = document.getElementById('gallery');
					for (var i = 0; i < 3; i++) {
						var img = document.createElement('div');
						img.className = 'gallery-image';
						img.setAttribute('data-src', '/img' + i + '.jpg');
						g.appendChild(img);
					}
				}, 220);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/woocommerce-cart", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="cart" class="cart">
				<table><tbody id="cart-items"></tbody></table>
				<div id="cart-totals"><span id="total">$0.00</span></div>
			</div>
			<script>
				setTimeout(function() {
					var tbody = document.getElementById('cart-items');
					var tr = document.createElement('tr');
					tr.innerHTML = '<td>Item</td><td>$19.99</td>';
					tbody.appendChild(tr);
					document.getElementById('total').textContent = '$19.99';
				}, 190);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/react-like-app", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="root"></div>
			<script>
				var root = document.getElementById('root');
				function render(state) {
					root.innerHTML = '<div class="app"><div id="mounted">' + state + '</div></div>';
				}
				render('Loading');
				setTimeout(function() { render('Hydrating'); }, 50);
				setTimeout(function() { render('Ready'); }, 150);
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/websocket-page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="ws-feed"></div>
			<script>
				var feed = document.getElementById('ws-feed');
				var msgs = ['Connected', 'Msg 1', 'Msg 2', 'Msg 3'];
				msgs.forEach(function(msg, i) {
					setTimeout(function() {
						var div = document.createElement('div');
						div.className = 'ws-msg';
						div.textContent = msg;
						feed.appendChild(div);
					}, 80 * (i + 1));
				});
			</script>
		</body></html>`)
	})

	mux.HandleFunc("/error-recovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<div id="recovery-target">Loading...</div>
			<script>
				setTimeout(function() {
					try { throw new Error('Simulated error'); } catch (e) {}
					document.getElementById('recovery-target').innerHTML = '<div id="recovered">Recovered</div>';
				}, 100);
			</script>
		</body></html>`)
	})

	return httptest.NewServer(mux)
}

func TestPageWaiterBenchmark(t *testing.T) {
	requireBenchmark(t)

	ts := benchmarkFixtureServer()
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer browser.Close()

	pageTypes := []struct {
		name string
		path string
	}{
		{"static", "/static"},
		{"ssr", "/ssr"},
		{"delayed_content", "/delayed"},
		{"spa_multi_phase", "/spa"},
		{"lazy_images", "/lazy-images"},
		{"form_page", "/form"},
		{"heavy_dom", "/heavy-dom"},
		{"mutation_storm", "/mutation-storm"},
		{"ajax_poll", "/ajax-poll"},
		{"shadow_dom", "/shadow-dom"},
		{"web_component", "/web-component"},
		{"infinite_scroll", "/infinite-scroll"},
		{"auth_redirect", "/auth-redirect"},
		{"d2l_module_list", "/d2l-module-list"},
		{"d2l_content_viewer", "/d2l-content-viewer"},
		{"woocommerce_product", "/woocommerce-product"},
		{"woocommerce_cart", "/woocommerce-cart"},
		{"react_like_app", "/react-like-app"},
		{"websocket_page", "/websocket-page"},
		{"error_recovery", "/error-recovery"},
	}

	strategies := []struct {
		name     string
		strategy WaitStrategy
	}{
		{"network_idle_only", WaitNetworkIdle},
		{"dom_stable_only", WaitDOMStable},
		{"network_and_dom", WaitNetworkIdle | WaitDOMStable},
	}

	var allResults []PageWaiterBenchResult

	for _, pg := range pageTypes {
		for _, strategy := range strategies {
			waiter := NewPageWaiter(15*time.Second, strategy.strategy)
			url := ts.URL + pg.path

			start := time.Now()
			waitErr := waiter.NavigateAndWait(browser.ctx, url)
			dur := time.Since(start)

			var domLen int
			var domOK bool
			if html, err := browser.CaptureDOM(); err == nil {
				domLen = len(html)
				domOK = true
			}

			r := PageWaiterBenchResult{
				PageType:       pg.name,
				URL:            pg.path,
				WaitDuration:   dur,
				WaitDurationMs: float64(dur.Milliseconds()),
				DOMCaptured:    domOK,
				DOMLength:      domLen,
				Strategy:       strategy.name,
				Success:        waitErr == nil,
			}
			if waitErr != nil {
				r.Error = waitErr.Error()
			}
			allResults = append(allResults, r)

			t.Logf("page=%-20s strategy=%-20s duration=%-10v success=%v dom=%d",
				pg.name, strategy.name, dur.Truncate(time.Millisecond), waitErr == nil, domLen)
		}
	}

	// Build summary
	var totalMs float64
	var maxMs float64
	passed, failed := 0, 0
	for _, r := range allResults {
		if r.Success {
			passed++
		} else {
			failed++
		}
		ms := r.WaitDurationMs
		totalMs += ms
		if ms > maxMs {
			maxMs = ms
		}
	}

	total := len(allResults)
	var avgMs, successRate float64
	if total > 0 {
		avgMs = totalMs / float64(total)
		successRate = float64(passed) / float64(total)
	}

	report := PageWaiterBenchReport{
		Timestamp: time.Now(),
		Platform:  "chromedp",
		Results:   allResults,
		Summary: BenchSummary{
			TotalPages:  total,
			Passed:      passed,
			Failed:      failed,
			AvgWaitMs:   avgMs,
			MaxWaitMs:   maxMs,
			SuccessRate: successRate,
		},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	reportDir := filepath.Join(os.TempDir(), "uiauto-benchmarks")
	os.MkdirAll(reportDir, 0755)
	reportPath := filepath.Join(reportDir, fmt.Sprintf("pagewaiter_benchmark_%s.json", time.Now().Format("20060102_150405")))
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	t.Logf("\n--- Benchmark Summary ---\nTotal: %d  Passed: %d  Failed: %d\nAvg Wait: %.1fms  Max Wait: %.1fms\nSuccess Rate: %.1f%%\nReport: %s",
		total, passed, failed, avgMs, maxMs, successRate*100, reportPath)

	if successRate < 0.7 {
		t.Errorf("overall success rate too low: %.1f%%", successRate*100)
	}
}

func TestPageWaiterBenchmark_ComparisonBaseline(t *testing.T) {
	requireBenchmark(t)

	ts := benchmarkFixtureServer()
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("create browser: %v", err)
	}
	defer browser.Close()

	ctx, cancel := context.WithTimeout(browser.ctx, 30*time.Second)
	defer cancel()

	// Baseline: Navigate without PageWaiter (raw chromedp)
	pages := []string{"/static", "/ssr", "/delayed", "/spa"}
	for _, pg := range pages {
		// Raw navigate
		start := time.Now()
		rawErr := func() error {
			return fmt.Errorf("raw navigate not implemented in baseline")
		}()
		rawDur := time.Since(start)

		// PageWaiter navigate
		waiter := NewPageWaiter(10*time.Second, WaitNetworkIdle|WaitDOMStable)
		start = time.Now()
		pwErr := waiter.NavigateAndWait(ctx, ts.URL+pg)
		pwDur := time.Since(start)

		t.Logf("page=%-10s raw=%v(%v) pagewaiter=%v(%v)",
			pg, rawDur.Truncate(time.Millisecond), rawErr,
			pwDur.Truncate(time.Millisecond), pwErr)
	}
}
