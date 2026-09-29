package fusion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunCaptureBlocksUntilCtxDeadline: a ChromeDebug endpoint that never
// answers must NOT wedge the run past its own deadline — the executor
// returns promptly with grounding_reason=capture_failed and the ctx error
// recorded. Deleting the select-on-ctx.Done (or the goroutine) is exactly
// the mutant this kills: this test would hang to its own 5 s guard and
// fail instead.
func TestRunCaptureBlocksUntilCtxDeadline(t *testing.T) {
	blocking := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// Long enough that the executor's 300 ms ctx fires first; short
		// enough that the handler goroutine settles before goleak checks.
		time.Sleep(800 * time.Millisecond)
	}))
	defer blocking.Close()

	e := &Executor{
		BrowserUseURL: "http://unused",
		ChromeDebug:   blocking.URL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan Record, 1)
	go func() { done <- e.Run(ctx, Scenario{ID: "s", Task: "t"}, 1) }()

	select {
	case rec := <-done:
		if rec.Evidence["grounding_reason"] != string(ReasonCaptureFailed) {
			t.Fatalf("grounding_reason=%v, want capture_failed", rec.Evidence["grounding_reason"])
		}
		if errStr, _ := rec.Evidence["grounding_error"].(string); !strings.Contains(errStr, "context deadline exceeded") {
			t.Fatalf("grounding_error=%v, want the ctx deadline", rec.Evidence["grounding_error"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its ctx deadline — the capture goroutine wedged the run")
	}
}

// fakeAgent records which of its methods ran, so the ctx re-check tests
// can prove a LATE attach never Navigates and a late Navigate never
// screenshots the shared browser. Counters are atomic and closed
// signals goroutine exit: capture() may return via the already-fired
// ctx.Done() BEFORE the goroutine's writes land, so the tests wait for
// the barrier before reading.
type fakeAgent struct {
	navigated   atomic.Bool
	screenshots atomic.Int32
	onNavigate  func()
	closed      chan struct{}
}

func newFakeAgent() *fakeAgent {
	return &fakeAgent{closed: make(chan struct{})}
}

func (f *fakeAgent) Navigate(_ string) error {
	f.navigated.Store(true)
	if f.onNavigate != nil {
		f.onNavigate()
	}
	return nil
}

func (f *fakeAgent) CaptureScreenshot() ([]byte, error) {
	f.screenshots.Add(1)
	return []byte("png"), nil
}

func (f *fakeAgent) Close() { close(f.closed) }

// TestCaptureRecheckBeforeNavigate: the attach succeeds, but the ctx has
// already expired — the executor must return the ctx error WITHOUT
// navigating the shared CDP browser. Deleting the first ctx.Err()
// re-check is the mutant: Navigate would run.
func TestCaptureRecheckBeforeNavigate(t *testing.T) {
	agent := newFakeAgent()
	e := &Executor{
		BrowserUseURL: "http://unused",
		attach:        func() (cdpAgent, error) { return agent, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // expires BEFORE capture runs

	if _, err := e.capture(ctx, "http://example/"); err == nil {
		t.Fatal("want the ctx error surfaced, got nil")
	}
	<-agent.closed // the goroutine (and its deferred Close) has settled
	if agent.navigated.Load() {
		t.Fatal("Navigate ran after the ctx expired — the pre-Navigate re-check is gone")
	}
}

// TestCaptureRecheckBeforeScreenshot: Navigate succeeds, but cancels the
// ctx on its way out (the deadline lands mid-capture) — the executor
// must return the ctx error WITHOUT taking a screenshot. Deleting the
// second ctx.Err() re-check is the mutant: CaptureScreenshot would run.
func TestCaptureRecheckBeforeScreenshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	agent := newFakeAgent()
	agent.onNavigate = cancel
	e := &Executor{
		BrowserUseURL: "http://unused",
		attach:        func() (cdpAgent, error) { return agent, nil },
	}

	if _, err := e.capture(ctx, "http://example/"); err == nil {
		t.Fatal("want the ctx error surfaced, got nil")
	}
	<-agent.closed // the goroutine (and its deferred Close) has settled
	if agent.screenshots.Load() != 0 {
		t.Fatal("CaptureScreenshot ran after the ctx expired — the pre-screenshot re-check is gone")
	}
}
