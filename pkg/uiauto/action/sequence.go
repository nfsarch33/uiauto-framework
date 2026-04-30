package action

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Phase represents a step's lifecycle phase.
type Phase int

const (
	PhaseSetup Phase = iota
	PhaseExecute
	PhaseVerify
	PhaseTeardown
)

func (p Phase) String() string {
	switch p {
	case PhaseSetup:
		return "setup"
	case PhaseExecute:
		return "execute"
	case PhaseVerify:
		return "verify"
	case PhaseTeardown:
		return "teardown"
	default:
		return "unknown"
	}
}

// State tracks the sequence's current execution state.
type State int

const (
	StatePending State = iota
	StateRunning
	StateCompleted
	StateFailed
	StateRolledBack
)

func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateCompleted:
		return "completed"
	case StateFailed:
		return "failed"
	case StateRolledBack:
		return "rolled_back"
	default:
		return "unknown"
	}
}

// StepFunc is a single action to execute.
type StepFunc func(ctx context.Context) error

// Step is a named, phased action with optional rollback.
type Step struct {
	Name     string
	Phase    Phase
	Action   StepFunc
	Rollback StepFunc
	Timeout  time.Duration
}

// StepResult records the outcome of a step.
type StepResult struct {
	Name     string
	Phase    Phase
	Duration time.Duration
	Err      error
}

// Sequence is an ordered list of steps with lifecycle management.
type Sequence struct {
	Name    string
	Steps   []Step
	Results []StepResult
	State   State
	logger  *slog.Logger
}

// Run executes all steps in order. On failure, runs rollbacks in reverse.
func (s *Sequence) Run(ctx context.Context) error {
	s.State = StateRunning
	s.Results = make([]StepResult, 0, len(s.Steps))

	for i, step := range s.Steps {
		stepCtx := ctx
		if step.Timeout > 0 {
			var cancel context.CancelFunc
			stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
			defer cancel()
		}

		start := time.Now()
		err := step.Action(stepCtx)
		result := StepResult{
			Name:     step.Name,
			Phase:    step.Phase,
			Duration: time.Since(start),
			Err:      err,
		}
		s.Results = append(s.Results, result)

		s.logger.Info("step executed",
			slog.String("sequence", s.Name),
			slog.String("step", step.Name),
			slog.String("phase", step.Phase.String()),
			slog.Duration("duration", result.Duration),
			slog.Bool("success", err == nil),
		)

		if err != nil {
			s.State = StateFailed
			s.rollback(ctx, i)
			return fmt.Errorf("sequence %q step %q failed: %w", s.Name, step.Name, err)
		}
	}

	s.State = StateCompleted
	return nil
}

func (s *Sequence) rollback(ctx context.Context, failedAt int) {
	for i := failedAt; i >= 0; i-- {
		step := s.Steps[i]
		if step.Rollback == nil {
			continue
		}
		if err := step.Rollback(ctx); err != nil {
			s.logger.Warn("rollback failed",
				slog.String("step", step.Name),
				slog.String("error", err.Error()),
			)
		}
	}
	s.State = StateRolledBack
}

// TotalDuration returns the sum of all step durations.
func (s *Sequence) TotalDuration() time.Duration {
	var total time.Duration
	for _, r := range s.Results {
		total += r.Duration
	}
	return total
}

// FailedSteps returns results for steps that failed.
func (s *Sequence) FailedSteps() []StepResult {
	var failed []StepResult
	for _, r := range s.Results {
		if r.Err != nil {
			failed = append(failed, r)
		}
	}
	return failed
}
