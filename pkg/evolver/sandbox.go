package evolver

import (
	"context"
	"fmt"
	"time"
)

// SandboxConfig configures the evolution sandbox environment.
type SandboxConfig struct {
	Image         string        `json:"image"`
	Timeout       time.Duration `json:"timeout"`
	MemoryLimitMB int           `json:"memory_limit_mb"`
	CPULimit      float64       `json:"cpu_limit"`
	NetworkMode   string        `json:"network_mode"`
	UseDocker     bool          `json:"use_docker"`
}

// DefaultSandboxConfig returns production defaults for evolution sandboxes.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		Image:         "ironclaw-evolver:latest",
		Timeout:       5 * time.Minute,
		MemoryLimitMB: 512,
		CPULimit:      1.0,
		NetworkMode:   "none",
		UseDocker:     false,
	}
}

// SandboxResult captures the outcome of a sandboxed evolution run.
type SandboxResult struct {
	CapsuleID string        `json:"capsule_id"`
	Success   bool          `json:"success"`
	Duration  time.Duration `json:"duration"`
	ExitCode  int           `json:"exit_code"`
	Output    string        `json:"output"`
	Mutations int           `json:"mutations"`
	Error     string        `json:"error,omitempty"`
	Container bool          `json:"container"`
}

// HITLGate implements a human-in-the-loop gate for evolution promotions.
type HITLGate struct {
	autoApprove bool
	approvals   []HITLApproval
}

// HITLApproval records a single approval decision.
type HITLApproval struct {
	CapsuleID string    `json:"capsule_id"`
	Approved  bool      `json:"approved"`
	Reviewer  string    `json:"reviewer"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// NewHITLGate creates a new HITL gate. If autoApprove is true, all
// evolutions are auto-approved (for testing/CI).
func NewHITLGate(autoApprove bool) *HITLGate {
	return &HITLGate{autoApprove: autoApprove}
}

// Review submits a capsule for HITL review. Returns true if approved.
func (g *HITLGate) Review(capsuleID, reviewer, reason string) bool {
	approved := g.autoApprove
	g.approvals = append(g.approvals, HITLApproval{
		CapsuleID: capsuleID,
		Approved:  approved,
		Reviewer:  reviewer,
		Reason:    reason,
		Timestamp: time.Now(),
	})
	return approved
}

// Approvals returns all recorded approvals.
func (g *HITLGate) Approvals() []HITLApproval {
	return g.approvals
}

// EvolutionSandbox orchestrates sandboxed evolution runs with HITL gating.
// Supports both Docker container execution and inline execution.
type EvolutionSandbox struct {
	config  SandboxConfig
	gate    *HITLGate
	runner  ContainerRunner
	results []SandboxResult
}

// NewEvolutionSandbox creates a new sandbox with the given config and gate.
func NewEvolutionSandbox(cfg SandboxConfig, gate *HITLGate) *EvolutionSandbox {
	return &EvolutionSandbox{
		config: cfg,
		gate:   gate,
	}
}

// NewEvolutionSandboxWithRunner creates a sandbox backed by a real container runner.
func NewEvolutionSandboxWithRunner(cfg SandboxConfig, gate *HITLGate, runner ContainerRunner) *EvolutionSandbox {
	return &EvolutionSandbox{
		config: cfg,
		gate:   gate,
		runner: runner,
	}
}

// RunEvolution executes a sandboxed evolution cycle. If UseDocker is true and
// a ContainerRunner is available, the mutation runs in a Docker container;
// otherwise it executes inline.
func (s *EvolutionSandbox) RunEvolution(ctx context.Context, capsuleID string, mutationFn func() (string, error)) SandboxResult {
	start := time.Now()
	result := SandboxResult{CapsuleID: capsuleID}

	if ctx.Err() != nil {
		result.Error = ctx.Err().Error()
		result.Duration = time.Since(start)
		return result
	}

	if s.config.UseDocker && s.runner != nil && s.runner.IsAvailable() {
		return s.runInContainer(ctx, capsuleID)
	}

	output, err := mutationFn()
	result.Duration = time.Since(start)
	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 1
	} else {
		result.Success = true
		result.Output = output
		result.Mutations = 1
		result.ExitCode = 0
	}

	s.results = append(s.results, result)
	return result
}

func (s *EvolutionSandbox) runInContainer(ctx context.Context, capsuleID string) SandboxResult {
	start := time.Now()
	result := SandboxResult{CapsuleID: capsuleID, Container: true}

	runCfg := ContainerRunConfig{
		Image:         s.config.Image,
		Command:       []string{"evolve", "--capsule", capsuleID},
		MemoryLimitMB: s.config.MemoryLimitMB,
		CPULimit:      s.config.CPULimit,
		NetworkMode:   s.config.NetworkMode,
		Timeout:       s.config.Timeout,
		ReadOnly:      true,
	}

	cr, err := s.runner.Run(ctx, runCfg)
	result.Duration = time.Since(start)
	result.ExitCode = cr.ExitCode

	if err != nil {
		result.Error = err.Error()
		s.results = append(s.results, result)
		return result
	}

	if cr.ExitCode == 0 {
		result.Success = true
		result.Output = cr.Stdout
		result.Mutations = 1
	} else {
		result.Error = cr.Stderr
		if cr.OOMKilled {
			result.Error = "container OOM killed: " + cr.Stderr
		}
	}

	s.results = append(s.results, result)
	return result
}

// RunBatch executes multiple evolutions and gates each through HITL.
func (s *EvolutionSandbox) RunBatch(ctx context.Context, capsuleIDs []string, mutationFn func(id string) (string, error)) []SandboxResult {
	results := make([]SandboxResult, 0, len(capsuleIDs))
	for _, id := range capsuleIDs {
		localID := id
		result := s.RunEvolution(ctx, localID, func() (string, error) {
			return mutationFn(localID)
		})

		if result.Success && s.gate != nil {
			approved := s.gate.Review(localID, "sandbox-auto", fmt.Sprintf("auto-review for %s", localID))
			if !approved {
				result.Success = false
				result.Error = "HITL gate rejected"
			}
		}
		results = append(results, result)
	}
	return results
}

// Results returns all sandbox execution results.
func (s *EvolutionSandbox) Results() []SandboxResult {
	return s.results
}
