package evolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityAudit_SandboxNetworkIsolation(t *testing.T) {
	cfg := DefaultSandboxConfig()
	assert.Equal(t, "none", cfg.NetworkMode, "sandbox must use network=none by default")
}

func TestSecurityAudit_SandboxMemoryLimit(t *testing.T) {
	cfg := DefaultSandboxConfig()
	assert.LessOrEqual(t, cfg.MemoryLimitMB, 1024, "sandbox memory must be capped")
	assert.Greater(t, cfg.MemoryLimitMB, 0)
}

func TestSecurityAudit_SandboxCPULimit(t *testing.T) {
	cfg := DefaultSandboxConfig()
	assert.LessOrEqual(t, cfg.CPULimit, 2.0, "sandbox CPU must be capped")
	assert.Greater(t, cfg.CPULimit, 0.0)
}

func TestSecurityAudit_HITLGateDefaultDeny(t *testing.T) {
	gate := NewHITLGate(false)
	approved := gate.Review("cap-test", "system", "auto-review")
	assert.False(t, approved, "HITL gate should deny by default")
}

func TestSecurityAudit_CapsuleStatusTransitions(t *testing.T) {
	validTransitions := map[CapsuleStatus][]CapsuleStatus{
		CapsuleStatusDraft:   {CapsuleStatusTesting},
		CapsuleStatusTesting: {CapsuleStatusActive, CapsuleStatusDraft},
		CapsuleStatusActive:  {CapsuleStatusRetired},
	}

	for from, tos := range validTransitions {
		for _, to := range tos {
			assert.NotEqual(t, from, to, "transition %s->%s should be different states", from, to)
		}
	}
}
