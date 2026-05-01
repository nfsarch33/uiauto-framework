package uiauto

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// WithLatencyPrometheus / WithLatencyLogger / WithFallbackLogger / WithLogger
// are option setters. They were 0% covered. Each is verified by constructing
// a parent type and asserting the field updates.

func TestLatencyBudget_Options_Apply(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	lb := NewLatencyBudget(DefaultTierBudgets(),
		WithLatencyPrometheus(reg),
		WithLatencyLogger(logger),
	)

	if lb.violationCounter == nil {
		t.Error("WithLatencyPrometheus did not register violation counter")
	}
	if lb.latencyHist == nil {
		t.Error("WithLatencyPrometheus did not register histogram")
	}
	if lb.logger != logger {
		t.Error("WithLatencyLogger did not set logger")
	}

	// Recording a violation must hit both observers without panicking.
	lb.Record(TierLight, lb.budgets[TierLight]+1)
}

func TestFallbackChain_Options_Apply(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	chain := NewFallbackChain(DefaultFallbackChain(), nil, WithFallbackLogger(logger))
	if chain.logger != logger {
		t.Error("WithFallbackLogger did not set logger")
	}
}

func TestLightExecutor_WithLogger_Apply(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	dir := t.TempDir()
	store, err := NewPatternStore(dir + "/p.json")
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewPatternTrackerWithStore(store, dir+"/drift")

	exec := NewLightExecutor(tracker, &fakeOptBrowser{}, WithLogger(logger))
	if exec.logger != logger {
		t.Error("WithLogger did not set logger on LightExecutor")
	}
}

// fakeOptBrowser is a no-op Browser used only to construct LightExecutor.
type fakeOptBrowser struct{}

func (fakeOptBrowser) Navigate(string) error                       { return nil }
func (fakeOptBrowser) NavigateWithConfig(string, WaitConfig) error { return nil }
func (fakeOptBrowser) CurrentURL() (string, error)                 { return "", nil }
func (fakeOptBrowser) CaptureDOM() (string, error)                 { return "", nil }
func (fakeOptBrowser) CaptureScreenshot() ([]byte, error)          { return nil, nil }
func (fakeOptBrowser) Click(string) error                          { return nil }
func (fakeOptBrowser) Type(string, string) error                   { return nil }
func (fakeOptBrowser) Evaluate(string, interface{}) error          { return nil }
func (fakeOptBrowser) IsVisible(string) (bool, error)              { return true, nil }
func (fakeOptBrowser) SwitchToFrame(string) (func(), error)        { return func() {}, nil }
func (fakeOptBrowser) Close()                                      {}

func TestPtrCopy_AndErrStr(t *testing.T) {
	src := AggregatedMetrics{Degraded: true}
	cp := ptrCopy(src)
	if cp == nil {
		t.Fatal("ptrCopy returned nil")
	}
	if !cp.Degraded {
		t.Error("ptrCopy did not preserve Degraded")
	}
	cp.Degraded = false
	if !src.Degraded {
		t.Error("ptrCopy aliased source slice")
	}

	if errStr(nil) != "" {
		t.Error("errStr(nil) should be empty")
	}
	if errStr(errors.New("boom")) != "boom" {
		t.Error("errStr did not return wrapped error message")
	}
}

func TestDiscoveryMode_WithVLM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := NewDiscoveryMode(nil, nil, nil, DefaultDiscoveryConfig(), logger)
	if d.vlm != nil {
		t.Error("vlm should start nil")
	}
	bridge := &VLMBridge{}
	d.WithVLM(bridge)
	if d.vlm != bridge {
		t.Error("WithVLM did not attach bridge")
	}
}

func TestModelRouter_WithVLMBridge(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	store, err := NewPatternStore(dir + "/p.json")
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewPatternTrackerWithStore(store, dir+"/drift")
	exec := NewLightExecutor(tracker, fakeOptBrowser{}, WithLogger(logger))
	bridge := &VLMBridge{}
	router := NewModelRouter(exec, nil, tracker, nil, WithRouterLogger(logger), WithVLMBridge(bridge))
	if router.vlmBridge != bridge {
		t.Error("WithVLMBridge did not attach bridge")
	}
}
