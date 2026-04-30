package endurance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/signal"
)

func TestHarnessBasicRun(t *testing.T) {
	emitter := signal.NewEmitter()
	handler, getter := signal.CollectorHandler()
	emitter.On(handler)

	cfg := Config{MaxCycles: 10, CycleInterval: 0, ReportInterval: 1 * time.Minute}
	fn := func(ctx context.Context, cycle int) error { return nil }

	h := NewHarness(fn, cfg, emitter)
	result := h.Run(context.Background())

	if result.TotalCycles != 10 {
		t.Errorf("TotalCycles = %d, want 10", result.TotalCycles)
	}
	if result.Passed != 10 {
		t.Errorf("Passed = %d, want 10", result.Passed)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d, want 0", result.Failed)
	}
	if result.AvgCycleTime == 0 {
		t.Error("AvgCycleTime should be non-zero")
	}

	// Should have final test signal
	signals := getter()
	if len(signals) == 0 {
		t.Error("expected at least one signal")
	}
}

func TestHarnessWithFailures(t *testing.T) {
	cfg := Config{MaxCycles: 5, CycleInterval: 0}
	fn := func(ctx context.Context, cycle int) error {
		if cycle%2 == 0 {
			return fmt.Errorf("cycle %d failed", cycle)
		}
		return nil
	}

	h := NewHarness(fn, cfg, nil)
	result := h.Run(context.Background())

	if result.Passed != 2 {
		t.Errorf("Passed = %d, want 2", result.Passed)
	}
	if result.Failed != 3 {
		t.Errorf("Failed = %d, want 3", result.Failed)
	}
	if len(result.Errors) != 3 {
		t.Errorf("Errors = %d, want 3", len(result.Errors))
	}
}

func TestHarnessDurationTimeout(t *testing.T) {
	cfg := Config{
		Duration:      50 * time.Millisecond,
		CycleInterval: 10 * time.Millisecond,
	}
	fn := func(ctx context.Context, cycle int) error { return nil }

	h := NewHarness(fn, cfg, nil)
	result := h.Run(context.Background())

	if result.TotalCycles < 2 {
		t.Errorf("expected at least 2 cycles, got %d", result.TotalCycles)
	}
	if result.TotalDuration < 40*time.Millisecond {
		t.Errorf("duration = %s, want >= 40ms", result.TotalDuration)
	}
}

func TestHarnessContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	cfg := Config{CycleInterval: 5 * time.Millisecond}
	fn := func(ctx context.Context, cycle int) error { return nil }

	h := NewHarness(fn, cfg, nil)
	result := h.Run(ctx)

	if result.TotalCycles < 1 {
		t.Error("expected at least 1 cycle")
	}
}

func TestHarnessSummary(t *testing.T) {
	result := Result{
		TotalCycles:   100,
		Passed:        95,
		Failed:        5,
		TotalDuration: 10 * time.Second,
		AvgCycleTime:  100 * time.Millisecond,
		MinCycleTime:  50 * time.Millisecond,
		MaxCycleTime:  500 * time.Millisecond,
	}
	s := result.Summary()
	if s == "" {
		t.Error("empty summary")
	}
	t.Log(s)
}

// --- Benchmark tests ---

func TestRunBenchmark(t *testing.T) {
	fn := func(ctx context.Context) (time.Duration, error) {
		start := time.Now()
		time.Sleep(1 * time.Millisecond)
		return time.Since(start), nil
	}

	cfg := BenchmarkConfig{Iterations: 20, WarmUp: 2}
	result := RunBenchmark(context.Background(), "test-op", fn, cfg)

	if result.Iterations != 20 {
		t.Errorf("Iterations = %d, want 20", result.Iterations)
	}
	if result.P50 < 500*time.Microsecond {
		t.Errorf("P50 = %s, expected >= 500us", result.P50)
	}
	if result.P95 < result.P50 {
		t.Errorf("P95 (%s) < P50 (%s)", result.P95, result.P50)
	}
	if result.Min > result.Max {
		t.Errorf("Min (%s) > Max (%s)", result.Min, result.Max)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}

	t.Log(result.Summary())
}

func TestRunBenchmarkWithErrors(t *testing.T) {
	fn := func(ctx context.Context) (time.Duration, error) {
		return 0, fmt.Errorf("always fails")
	}

	result := RunBenchmark(context.Background(), "fail-op", fn, BenchmarkConfig{Iterations: 5, WarmUp: 0})
	if result.Errors != 5 {
		t.Errorf("Errors = %d, want 5", result.Errors)
	}
}

func TestBenchmarkSummary(t *testing.T) {
	result := BenchmarkResult{
		Name:       "test",
		Iterations: 100,
		P50:        10 * time.Millisecond,
		P95:        50 * time.Millisecond,
		P99:        100 * time.Millisecond,
		Min:        5 * time.Millisecond,
		Max:        150 * time.Millisecond,
		Avg:        20 * time.Millisecond,
		MemAllocs:  5000,
		MemBytes:   1024 * 100,
	}
	s := result.Summary()
	if s == "" {
		t.Error("empty summary")
	}
	t.Log(s)
}
