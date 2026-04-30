package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

func TestVisualVerifier_DOMMatch(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><button id="btn">Click</button></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	vv := NewVisualVerifier(browser, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyElement(ctx, "btn", "#btn", "click button")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !result.DOMMatch {
		t.Error("expected DOM match for #btn")
	}
	if !result.Combined {
		t.Error("expected combined match")
	}
	if result.Method != "dom" {
		t.Errorf("expected method=dom, got %s", result.Method)
	}
}

func TestVisualVerifier_DOMFail_VLMFallback(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><button id="btn">Click</button></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	mockVLM := &mockIntegrationLLM{
		response: `{"match": true, "confidence": 0.85, "reason": "element matches"}`,
	}
	vlm := NewVLMBridge(mockVLM, []string{"mock-model"})

	vv := NewVisualVerifier(browser, vlm, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyElement(ctx, "nonexistent", "#nonexistent", "some element")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.DOMMatch {
		t.Error("expected no DOM match for #nonexistent")
	}
	if result.Method != "vlm" {
		t.Errorf("expected method=vlm, got %s", result.Method)
	}
	if !result.VLMMatch {
		t.Error("expected VLM match from mock")
	}
}

func TestVisualVerifier_NoVLM(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><p>No button</p></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	vv := NewVisualVerifier(browser, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyElement(ctx, "btn", "#btn", "button")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Combined {
		t.Error("expected no combined match without VLM on missing element")
	}
	if result.Method != "dom_only" {
		t.Errorf("expected method=dom_only, got %s", result.Method)
	}
}

func TestVisualVerifier_VerifyWithJudge(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><button id="btn">Click</button></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	mockVLM := &mockIntegrationLLM{
		response: `{"present": true, "confidence": 0.9, "explanation": "Button visible", "suggested_selector": "#btn"}`,
	}
	vlm := NewVLMBridge(mockVLM, []string{"mock"})
	vv := NewVisualVerifier(browser, vlm, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyWithJudge(ctx, "click button")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !result.Passed {
		t.Error("expected passed=true")
	}
	if result.Confidence < 0.9 {
		t.Errorf("expected confidence >= 0.9, got %f", result.Confidence)
	}
	if result.Screenshot == "" {
		t.Error("expected non-empty screenshot base64")
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestVisualVerifier_VerifyWithJudge_NoVLM(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><p>No button</p></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	vv := NewVisualVerifier(browser, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyWithJudge(ctx, "button")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false when VLM not configured")
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0, got %f", result.Confidence)
	}
	if result.Explanation != "VLM not configured" {
		t.Errorf("expected VLM not configured message, got %s", result.Explanation)
	}
}

func TestVisualVerifier_VerifyWithJudge_NotPresent(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><p>Empty</p></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	mockVLM := &mockIntegrationLLM{
		response: `{"present": false, "confidence": 0.8, "explanation": "Element not found", "suggested_selector": ""}`,
	}
	vlm := NewVLMBridge(mockVLM, []string{"mock"})
	vv := NewVisualVerifier(browser, vlm, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.VerifyWithJudge(ctx, "nonexistent element")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.Passed {
		t.Error("expected passed=false when element not present")
	}
	if result.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", result.Confidence)
	}
}

func TestVisualVerifier_DetectAllElements(t *testing.T) {
	skipWithoutBrowser(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body><button id="a">A</button><input id="b"><a href="/">C</a></body></html>`)
	}))
	defer ts.Close()

	browser, err := NewBrowserAgent(true)
	if err != nil {
		t.Skipf("no browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	mockVLM := &mockIntegrationLLM{
		response: `[{"label":"A button","type":"button","bounding_box":[10,10,100,30],"confidence":0.9},{"label":"Input field","type":"input","bounding_box":[10,50,200,30],"confidence":0.85}]`,
	}
	vlm := NewVLMBridge(mockVLM, []string{"mock"})
	vv := NewVisualVerifier(browser, vlm, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := vv.DetectAllElements(ctx)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(result.Elements) < 2 {
		t.Errorf("expected at least 2 elements, got %d", len(result.Elements))
	}
}

func TestVisualVerifier_LiveVLM(t *testing.T) {
	if os.Getenv("UIAUTO_LIVE_VLM") != "1" {
		t.Skip("set UIAUTO_LIVE_VLM=1 to run live VLM integration tests")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
			<form><input id="user" type="text" placeholder="Username"><button id="login">Login</button></form>
		</body></html>`)
	}))
	defer ts.Close()

	var browser *BrowserAgent
	var err error
	if du := chromeDebugURL(); du != "" {
		browser, err = NewBrowserAgentWithRemote(du)
	} else {
		browser, err = NewBrowserAgent(true)
	}
	if err != nil {
		t.Fatalf("browser: %v", err)
	}
	defer browser.Close()

	if err := browser.Navigate(ts.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	routerURL := os.Getenv("LLM_ROUTER_URL")
	if routerURL == "" {
		routerURL = "http://localhost:18080"
	}

	provider := llm.NewClient(llm.Config{
		BaseURL: routerURL,
		Model:   "qwen3-vl",
		Timeout: 30 * time.Second,
	})
	vlm := NewVLMBridge(provider, []string{"qwen3-vl"}, WithVLMLogger(testDiscardLogger()))
	vv := NewVisualVerifier(browser, vlm, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := vv.VerifyElement(ctx, "login", "#login", "login button")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Logf("DOM=%v VLM=%v conf=%.2f method=%s", result.DOMMatch, result.VLMMatch, result.VLMConf, result.Method)
}

var _ llm.Provider = (*mockIntegrationLLM)(nil)
