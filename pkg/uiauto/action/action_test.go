package action

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSequenceHappyPath(t *testing.T) {
	var order []string
	seq := NewSequence("login-flow").
		Setup("open-browser", func(_ context.Context) error { order = append(order, "setup"); return nil }).
		Execute("fill-form", func(_ context.Context) error { order = append(order, "execute"); return nil }).
		Verify("check-redirect", func(_ context.Context) error { order = append(order, "verify"); return nil }).
		Teardown("close-browser", func(_ context.Context) error { order = append(order, "teardown"); return nil }).
		Build()

	if seq.State != StatePending {
		t.Errorf("initial state = %v, want pending", seq.State)
	}

	if err := seq.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if seq.State != StateCompleted {
		t.Errorf("final state = %v, want completed", seq.State)
	}

	expected := []string{"setup", "execute", "verify", "teardown"}
	if len(order) != len(expected) {
		t.Fatalf("step count = %d, want %d", len(order), len(expected))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("step %d = %q, want %q", i, order[i], v)
		}
	}

	if len(seq.Results) != 4 {
		t.Errorf("results = %d, want 4", len(seq.Results))
	}
	if seq.TotalDuration() <= 0 {
		t.Error("TotalDuration should be positive")
	}
}

func TestSequenceFailureAndRollback(t *testing.T) {
	rolled := false
	seq := NewSequence("failing-flow").
		SetupWithRollback("init",
			func(_ context.Context) error { return nil },
			func(_ context.Context) error { rolled = true; return nil },
		).
		Execute("fail-step", func(_ context.Context) error {
			return fmt.Errorf("element not found")
		}).
		Build()

	err := seq.Run(context.Background())
	if err == nil {
		t.Error("expected error")
	}
	if seq.State != StateRolledBack {
		t.Errorf("state = %v, want rolled_back", seq.State)
	}
	if !rolled {
		t.Error("rollback should have been called")
	}
	if len(seq.FailedSteps()) != 1 {
		t.Errorf("failed steps = %d, want 1", len(seq.FailedSteps()))
	}
}

func TestSequencePhaseOrdering(t *testing.T) {
	seq := NewSequence("ordered").
		Setup("s1", func(_ context.Context) error { return nil }).
		Execute("e1", func(_ context.Context) error { return nil }).
		Verify("v1", func(_ context.Context) error { return nil }).
		Teardown("t1", func(_ context.Context) error { return nil }).
		Build()

	expectedPhases := []Phase{PhaseSetup, PhaseExecute, PhaseVerify, PhaseTeardown}
	for i, step := range seq.Steps {
		if step.Phase != expectedPhases[i] {
			t.Errorf("step %d phase = %v, want %v", i, step.Phase, expectedPhases[i])
		}
	}
}

func TestSequenceTimeout(t *testing.T) {
	seq := NewSequence("timeout-test").
		ExecuteWithTimeout("slow", 10*time.Millisecond, func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
				return nil
			}
		}).
		Build()

	err := seq.Run(context.Background())
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestSequenceNoSteps(t *testing.T) {
	seq := NewSequence("empty").Build()
	if err := seq.Run(context.Background()); err != nil {
		t.Errorf("empty sequence should succeed: %v", err)
	}
	if seq.State != StateCompleted {
		t.Errorf("state = %v, want completed", seq.State)
	}
}

func TestSequenceMultipleRollbacks(t *testing.T) {
	rollbacks := []string{}
	seq := NewSequence("multi-rollback").
		SetupWithRollback("step-a",
			func(_ context.Context) error { return nil },
			func(_ context.Context) error { rollbacks = append(rollbacks, "a"); return nil },
		).
		ExecuteWithRollback("step-b",
			func(_ context.Context) error { return nil },
			func(_ context.Context) error { rollbacks = append(rollbacks, "b"); return nil },
		).
		Execute("step-c", func(_ context.Context) error { return fmt.Errorf("fail") }).
		Build()

	seq.Run(context.Background())

	// Rollbacks should run in reverse order from failure point
	if len(rollbacks) != 2 {
		t.Fatalf("rollback count = %d, want 2", len(rollbacks))
	}
	if rollbacks[0] != "b" || rollbacks[1] != "a" {
		t.Errorf("rollback order = %v, want [b, a]", rollbacks)
	}
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		p    Phase
		want string
	}{
		{PhaseSetup, "setup"},
		{PhaseExecute, "execute"},
		{PhaseVerify, "verify"},
		{PhaseTeardown, "teardown"},
		{Phase(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("Phase(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		s    State
		want string
	}{
		{StatePending, "pending"},
		{StateRunning, "running"},
		{StateCompleted, "completed"},
		{StateFailed, "failed"},
		{StateRolledBack, "rolled_back"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestBuilderWithLogger(t *testing.T) {
	seq := NewSequence("test").WithLogger(nil).Build()
	if seq.logger != nil {
		t.Error("logger should be nil when set to nil")
	}
}

func TestSequenceRollbackError(t *testing.T) {
	seq := NewSequence("rb-error").
		SetupWithRollback("init",
			func(_ context.Context) error { return nil },
			func(_ context.Context) error { return fmt.Errorf("rollback failed") },
		).
		Execute("fail", func(_ context.Context) error { return fmt.Errorf("fail") }).
		Build()

	err := seq.Run(context.Background())
	if err == nil {
		t.Error("expected error")
	}
	// State should still be rolled_back even if rollback had errors
	if seq.State != StateRolledBack {
		t.Errorf("state = %v, want rolled_back", seq.State)
	}
}
