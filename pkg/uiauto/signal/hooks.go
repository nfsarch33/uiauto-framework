package signal

import (
	"fmt"
	"time"
)

// TodoEvent represents a TODO item completion or status change.
type TodoEvent struct {
	ID     string
	Title  string
	Status string // "completed", "in_progress", "pending", "cancelled"
	Sprint string
}

// EmitTodoCompleted fires a signal when a sprint TODO is marked done.
func EmitTodoCompleted(e *Emitter, evt TodoEvent) {
	e.Emit(Signal{
		ID:        fmt.Sprintf("todo-%s-%d", evt.ID, time.Now().UnixMilli()),
		Timestamp: time.Now(),
		Severity:  SeveritySuccess,
		Category:  CategoryTodo,
		Title:     fmt.Sprintf("TODO completed: %s", evt.Title),
		Detail:    fmt.Sprintf("Sprint: %s, Status: %s", evt.Sprint, evt.Status),
		Tags:      map[string]string{"todo_id": evt.ID, "sprint": evt.Sprint},
		Source:    "todo-hook",
	})
}

// EmitTodoStarted fires a signal when a sprint TODO begins.
func EmitTodoStarted(e *Emitter, evt TodoEvent) {
	e.Emit(Signal{
		ID:        fmt.Sprintf("todo-start-%s-%d", evt.ID, time.Now().UnixMilli()),
		Timestamp: time.Now(),
		Severity:  SeverityInfo,
		Category:  CategoryTodo,
		Title:     fmt.Sprintf("TODO started: %s", evt.Title),
		Tags:      map[string]string{"todo_id": evt.ID, "sprint": evt.Sprint},
		Source:    "todo-hook",
	})
}

// HealEvent represents a self-healing operation result.
type HealEvent struct {
	TargetID string
	Method   string
	Success  bool
	Duration time.Duration
}

// EmitHealResult fires a signal for a self-heal operation outcome.
func EmitHealResult(e *Emitter, evt HealEvent) {
	sev := SeveritySuccess
	if !evt.Success {
		sev = SeverityWarning
	}
	status := "healed"
	if !evt.Success {
		status = "failed"
	}
	e.Emit(Signal{
		ID:        fmt.Sprintf("heal-%s-%d", evt.TargetID, time.Now().UnixMilli()),
		Timestamp: time.Now(),
		Severity:  sev,
		Category:  CategoryHeal,
		Title:     fmt.Sprintf("Self-heal %s: %s via %s", status, evt.TargetID, evt.Method),
		Detail:    fmt.Sprintf("Duration: %s", evt.Duration),
		Tags: map[string]string{
			"target":  evt.TargetID,
			"method":  evt.Method,
			"success": fmt.Sprintf("%t", evt.Success),
		},
		Source: "healer",
	})
}

// CircuitEvent represents a circuit breaker state change.
type CircuitEvent struct {
	StoreName string
	OldState  string
	NewState  string
	Failures  int
}

// EmitCircuitChange fires a signal on circuit breaker transitions.
func EmitCircuitChange(e *Emitter, evt CircuitEvent) {
	sev := SeverityInfo
	if evt.NewState == "open" {
		sev = SeverityWarning
	}
	e.Emit(Signal{
		ID:        fmt.Sprintf("circuit-%s-%d", evt.StoreName, time.Now().UnixMilli()),
		Timestamp: time.Now(),
		Severity:  sev,
		Category:  CategoryCircuit,
		Title:     fmt.Sprintf("Circuit %s: %s -> %s", evt.StoreName, evt.OldState, evt.NewState),
		Detail:    fmt.Sprintf("Consecutive failures: %d", evt.Failures),
		Tags: map[string]string{
			"store":     evt.StoreName,
			"old_state": evt.OldState,
			"new_state": evt.NewState,
		},
		Source: "circuit-breaker",
	})
}

// TestEvent represents a test suite result.
type TestEvent struct {
	Suite    string
	Passed   int
	Failed   int
	Skipped  int
	Duration time.Duration
}

// EmitTestResult fires a signal for a test suite outcome.
func EmitTestResult(e *Emitter, evt TestEvent) {
	sev := SeveritySuccess
	if evt.Failed > 0 {
		sev = SeverityError
	}
	e.Emit(Signal{
		ID:        fmt.Sprintf("test-%s-%d", evt.Suite, time.Now().UnixMilli()),
		Timestamp: time.Now(),
		Severity:  sev,
		Category:  CategoryTest,
		Title:     fmt.Sprintf("Tests %s: %d passed, %d failed, %d skipped (%s)", evt.Suite, evt.Passed, evt.Failed, evt.Skipped, evt.Duration),
		Tags: map[string]string{
			"suite":   evt.Suite,
			"passed":  fmt.Sprintf("%d", evt.Passed),
			"failed":  fmt.Sprintf("%d", evt.Failed),
			"skipped": fmt.Sprintf("%d", evt.Skipped),
		},
		Source: "test-runner",
	})
}
