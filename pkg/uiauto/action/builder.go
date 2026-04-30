package action

import (
	"log/slog"
	"time"
)

// Builder provides a fluent interface for constructing Sequences.
type Builder struct {
	name   string
	steps  []Step
	logger *slog.Logger
}

// NewSequence starts building a named action sequence.
func NewSequence(name string) *Builder {
	return &Builder{
		name:   name,
		logger: slog.Default(),
	}
}

// WithLogger sets a structured logger for execution tracing.
func (b *Builder) WithLogger(l *slog.Logger) *Builder {
	b.logger = l
	return b
}

// Setup adds a setup-phase step.
func (b *Builder) Setup(name string, action StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseSetup, Action: action})
	return b
}

// SetupWithRollback adds a setup step with a rollback handler.
func (b *Builder) SetupWithRollback(name string, action, rollback StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseSetup, Action: action, Rollback: rollback})
	return b
}

// Execute adds an execute-phase step.
func (b *Builder) Execute(name string, action StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseExecute, Action: action})
	return b
}

// ExecuteWithTimeout adds a time-limited execute step.
func (b *Builder) ExecuteWithTimeout(name string, timeout time.Duration, action StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseExecute, Action: action, Timeout: timeout})
	return b
}

// ExecuteWithRollback adds an execute step with rollback.
func (b *Builder) ExecuteWithRollback(name string, action, rollback StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseExecute, Action: action, Rollback: rollback})
	return b
}

// Verify adds a verification step.
func (b *Builder) Verify(name string, action StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseVerify, Action: action})
	return b
}

// Teardown adds a teardown step.
func (b *Builder) Teardown(name string, action StepFunc) *Builder {
	b.steps = append(b.steps, Step{Name: name, Phase: PhaseTeardown, Action: action})
	return b
}

// Build finalizes the sequence.
func (b *Builder) Build() *Sequence {
	return &Sequence{
		Name:   b.name,
		Steps:  b.steps,
		State:  StatePending,
		logger: b.logger,
	}
}
