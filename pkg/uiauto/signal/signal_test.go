package signal

import (
	"strings"
	"testing"
	"time"
)

// --- Signal type tests ---

func TestSignalBrief(t *testing.T) {
	s := Signal{
		Severity: SeveritySuccess,
		Category: CategoryHeal,
		Title:    "Healed #login-btn via data-attribute",
	}
	got := s.Brief()
	want := "[OK] heal: Healed #login-btn via data-attribute"
	if got != want {
		t.Errorf("Brief() = %q, want %q", got, want)
	}
}

func TestSignalVerbose(t *testing.T) {
	s := Signal{
		Severity:  SeverityWarning,
		Category:  CategoryDrift,
		Title:     "Selector drift detected",
		Timestamp: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Source:    "drift-detector",
		Detail:    "3 elements drifted on /login",
		Tags:      map[string]string{"page": "/login"},
	}
	out := s.Verbose()
	if !strings.Contains(out, "[WARN] drift") {
		t.Errorf("Verbose() missing severity/category: %s", out)
	}
	if !strings.Contains(out, "2026-03-20T12:00:00Z") {
		t.Errorf("Verbose() missing timestamp: %s", out)
	}
	if !strings.Contains(out, "drift-detector") {
		t.Errorf("Verbose() missing source: %s", out)
	}
	if !strings.Contains(out, "3 elements drifted") {
		t.Errorf("Verbose() missing detail: %s", out)
	}
}

func TestSignalFormatBrief(t *testing.T) {
	s := Signal{Severity: SeverityInfo, Category: CategoryMetrics, Title: "test"}
	brief := s.Format(true)
	verbose := s.Format(false)
	if len(brief) >= len(verbose) {
		t.Error("brief should be shorter than verbose")
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityInfo, "INFO"},
		{SeveritySuccess, "OK"},
		{SeverityWarning, "WARN"},
		{SeverityError, "ERROR"},
		{Severity(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// --- Emitter tests ---

func TestEmitterBasicEmit(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	delivered := e.Emit(Signal{
		Severity: SeverityInfo,
		Category: CategoryTest,
		Title:    "hello",
	})

	if !delivered {
		t.Error("Emit returned false, want true")
	}

	signals := getter()
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	if signals[0].Title != "hello" {
		t.Errorf("Title = %q, want hello", signals[0].Title)
	}
	if signals[0].Timestamp.IsZero() {
		t.Error("Timestamp should be auto-set")
	}
}

func TestEmitterMultipleHandlers(t *testing.T) {
	e := NewEmitter()
	h1, g1 := CollectorHandler()
	h2, g2 := CollectorHandler()
	e.On(h1)
	e.On(h2)

	e.Emit(Signal{Category: CategoryPipeline, Title: "x"})

	if len(g1()) != 1 || len(g2()) != 1 {
		t.Error("both handlers should receive the signal")
	}
}

func TestEmitterDebounce(t *testing.T) {
	e := NewEmitter(WithDebounce(100 * time.Millisecond))
	handler, getter := CollectorHandler()
	e.On(handler)

	// First should deliver
	if !e.Emit(Signal{Category: CategoryHeal, Title: "first"}) {
		t.Error("first emit should deliver")
	}
	// Second should be suppressed (same category, within debounce window)
	if e.Emit(Signal{Category: CategoryHeal, Title: "second"}) {
		t.Error("second emit should be suppressed")
	}
	// Different category should deliver
	if !e.Emit(Signal{Category: CategoryTest, Title: "other"}) {
		t.Error("different category should deliver")
	}

	if len(getter()) != 2 {
		t.Errorf("got %d signals, want 2", len(getter()))
	}
	if e.Suppressed() != 1 {
		t.Errorf("Suppressed() = %d, want 1", e.Suppressed())
	}
}

func TestEmitterDebounceExpiry(t *testing.T) {
	e := NewEmitter(WithDebounce(20 * time.Millisecond))
	handler, getter := CollectorHandler()
	e.On(handler)

	e.Emit(Signal{Category: CategoryHeal, Title: "first"})
	time.Sleep(30 * time.Millisecond)
	// Should deliver after debounce window expires
	if !e.Emit(Signal{Category: CategoryHeal, Title: "after-debounce"}) {
		t.Error("should deliver after debounce window")
	}

	if len(getter()) != 2 {
		t.Errorf("got %d signals, want 2", len(getter()))
	}
}

func TestEmitterNoDebounce(t *testing.T) {
	e := NewEmitter() // debounce=0
	handler, getter := CollectorHandler()
	e.On(handler)

	for i := 0; i < 10; i++ {
		e.Emit(Signal{Category: CategoryHeal, Title: "rapid"})
	}

	if len(getter()) != 10 {
		t.Errorf("got %d signals, want 10 (no debounce)", len(getter()))
	}
}

// --- Hook tests ---

func TestEmitTodoCompleted(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitTodoCompleted(e, TodoEvent{
		ID:     "sprint11-sqlite-store",
		Title:  "SQLite fallback store",
		Status: "completed",
		Sprint: "sprint-11",
	})

	signals := getter()
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Severity != SeveritySuccess {
		t.Errorf("Severity = %v, want Success", s.Severity)
	}
	if s.Category != CategoryTodo {
		t.Errorf("Category = %v, want todo", s.Category)
	}
	if !strings.Contains(s.Title, "SQLite fallback store") {
		t.Errorf("Title = %q, missing task name", s.Title)
	}
}

func TestEmitTodoStarted(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitTodoStarted(e, TodoEvent{
		ID:     "sprint13",
		Title:  "Signal enhancement",
		Sprint: "sprint-13",
	})

	s := getter()[0]
	if s.Severity != SeverityInfo {
		t.Errorf("Severity = %v, want Info", s.Severity)
	}
	if !strings.Contains(s.Title, "started") {
		t.Errorf("Title = %q, missing 'started'", s.Title)
	}
}

func TestEmitHealResult(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitHealResult(e, HealEvent{
		TargetID: "#login-btn",
		Method:   "data-attribute",
		Success:  true,
		Duration: 150 * time.Millisecond,
	})

	s := getter()[0]
	if s.Severity != SeveritySuccess {
		t.Errorf("Severity = %v, want Success for successful heal", s.Severity)
	}
	if s.Tags["success"] != "true" {
		t.Errorf("tags[success] = %q, want true", s.Tags["success"])
	}
}

func TestEmitHealResultFailed(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitHealResult(e, HealEvent{
		TargetID: "#submit",
		Method:   "css",
		Success:  false,
		Duration: 2 * time.Second,
	})

	s := getter()[0]
	if s.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want Warning for failed heal", s.Severity)
	}
}

func TestEmitCircuitChange(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitCircuitChange(e, CircuitEvent{
		StoreName: "mem0-primary",
		OldState:  "closed",
		NewState:  "open",
		Failures:  3,
	})

	s := getter()[0]
	if s.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want Warning for circuit open", s.Severity)
	}
	if s.Category != CategoryCircuit {
		t.Errorf("Category = %v, want circuit", s.Category)
	}
	if !strings.Contains(s.Title, "closed -> open") {
		t.Errorf("Title = %q, missing state transition", s.Title)
	}
}

func TestEmitCircuitRecovery(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitCircuitChange(e, CircuitEvent{
		StoreName: "mem0-primary",
		OldState:  "half-open",
		NewState:  "closed",
		Failures:  0,
	})

	s := getter()[0]
	if s.Severity != SeverityInfo {
		t.Errorf("Severity = %v, want Info for circuit recovery", s.Severity)
	}
}

func TestEmitTestResult(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitTestResult(e, TestEvent{
		Suite:    "uiauto/store",
		Passed:   15,
		Failed:   0,
		Skipped:  0,
		Duration: 2 * time.Second,
	})

	s := getter()[0]
	if s.Severity != SeveritySuccess {
		t.Errorf("Severity = %v, want Success for all-pass", s.Severity)
	}
	if !strings.Contains(s.Title, "15 passed") {
		t.Errorf("Title = %q, missing pass count", s.Title)
	}
}

func TestEmitTestResultWithFailures(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	EmitTestResult(e, TestEvent{
		Suite:    "integration",
		Passed:   10,
		Failed:   2,
		Skipped:  1,
		Duration: 5 * time.Second,
	})

	s := getter()[0]
	if s.Severity != SeverityError {
		t.Errorf("Severity = %v, want Error for test failures", s.Severity)
	}
}

func TestCollectorHandlerThreadSafety(t *testing.T) {
	e := NewEmitter()
	handler, getter := CollectorHandler()
	e.On(handler)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			e.Emit(Signal{Category: CategoryTest, Title: "concurrent"})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	if len(getter()) != 10 {
		t.Errorf("got %d signals, want 10 concurrent", len(getter()))
	}
}
