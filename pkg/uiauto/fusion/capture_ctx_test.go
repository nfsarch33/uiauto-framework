package fusion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
