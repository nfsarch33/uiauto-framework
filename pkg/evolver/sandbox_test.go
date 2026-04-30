package evolver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvolutionSandbox_3PlusEvolutions(t *testing.T) {
	gate := NewHITLGate(true)
	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), gate)

	capsuleIDs := []string{"cap-001", "cap-002", "cap-003", "cap-004"}
	results := sandbox.RunBatch(context.Background(), capsuleIDs, func(id string) (string, error) {
		return fmt.Sprintf("evolved %s successfully", id), nil
	})

	require.Len(t, results, 4)
	for _, r := range results {
		assert.True(t, r.Success, "capsule %s should succeed", r.CapsuleID)
		assert.Equal(t, 1, r.Mutations)
		assert.Greater(t, r.Duration, time.Duration(0))
		assert.Contains(t, r.Output, "evolved")
	}

	approvals := gate.Approvals()
	assert.Len(t, approvals, 4)
	for _, a := range approvals {
		assert.True(t, a.Approved)
	}
}

func TestEvolutionSandbox_HITLGateRejects(t *testing.T) {
	gate := NewHITLGate(false)
	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), gate)

	capsuleIDs := []string{"cap-reject-1", "cap-reject-2"}
	results := sandbox.RunBatch(context.Background(), capsuleIDs, func(id string) (string, error) {
		return "mutation output", nil
	})

	for _, r := range results {
		assert.False(t, r.Success, "should be rejected by HITL gate")
		assert.Equal(t, "HITL gate rejected", r.Error)
	}
}

func TestEvolutionSandbox_MutationFailure(t *testing.T) {
	gate := NewHITLGate(true)
	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), gate)

	result := sandbox.RunEvolution(context.Background(), "cap-fail", func() (string, error) {
		return "", fmt.Errorf("mutation compilation failed")
	})

	assert.False(t, result.Success)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "compilation failed")
}

func TestEvolutionSandbox_ContextCancellation(t *testing.T) {
	gate := NewHITLGate(true)
	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), gate)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := sandbox.RunEvolution(ctx, "cap-cancelled", func() (string, error) {
		return "should not run", nil
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "context canceled")
}

func TestDefaultSandboxConfig(t *testing.T) {
	cfg := DefaultSandboxConfig()
	assert.Equal(t, "ironclaw-evolver:latest", cfg.Image)
	assert.Equal(t, 5*time.Minute, cfg.Timeout)
	assert.Equal(t, 512, cfg.MemoryLimitMB)
	assert.Equal(t, 1.0, cfg.CPULimit)
	assert.Equal(t, "none", cfg.NetworkMode)
}

func TestHITLGate_ManualReview(t *testing.T) {
	gate := NewHITLGate(false)
	approved := gate.Review("cap-test", "human-reviewer", "looks good")
	assert.False(t, approved)

	approvals := gate.Approvals()
	require.Len(t, approvals, 1)
	assert.Equal(t, "cap-test", approvals[0].CapsuleID)
	assert.Equal(t, "human-reviewer", approvals[0].Reviewer)
}
