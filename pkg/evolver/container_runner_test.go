package evolver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockContainerRunner struct {
	available bool
	exitCode  int
	stdout    string
	stderr    string
	oomKilled bool
	runErr    error
	calls     []ContainerRunConfig
}

func (m *mockContainerRunner) IsAvailable() bool { return m.available }

func (m *mockContainerRunner) Run(_ context.Context, cfg ContainerRunConfig) (ContainerRunResult, error) {
	m.calls = append(m.calls, cfg)
	if m.runErr != nil {
		return ContainerRunResult{ExitCode: 1}, m.runErr
	}
	return ContainerRunResult{
		ExitCode:  m.exitCode,
		Stdout:    m.stdout,
		Stderr:    m.stderr,
		Duration:  10 * time.Millisecond,
		OOMKilled: m.oomKilled,
	}, nil
}

func TestInlineRunner_IsAvailable(t *testing.T) {
	r := NewInlineRunner()
	assert.True(t, r.IsAvailable())
}

func TestInlineRunner_Run(t *testing.T) {
	r := NewInlineRunner()
	cfg := ContainerRunConfig{
		Image:   "test:latest",
		Command: []string{"echo", "hello"},
	}

	result, err := r.Run(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "test:latest")
}

func TestInlineRunner_ContextCancelled(t *testing.T) {
	r := NewInlineRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Run(ctx, ContainerRunConfig{Image: "x"})
	assert.Error(t, err)
}

func TestDockerRunner_BuildArgs(t *testing.T) {
	r := NewDockerRunner(nil)
	args := r.buildArgs(ContainerRunConfig{
		Image:         "test:latest",
		Command:       []string{"evolve", "--capsule", "c1"},
		MemoryLimitMB: 256,
		CPULimit:      0.5,
		NetworkMode:   "none",
		ReadOnly:      true,
		Env:           map[string]string{"FOO": "bar"},
	})

	assert.Contains(t, args, "run")
	assert.Contains(t, args, "--rm")
	assert.Contains(t, args, "--memory")
	assert.Contains(t, args, "256m")
	assert.Contains(t, args, "--cpus")
	assert.Contains(t, args, "--network")
	assert.Contains(t, args, "none")
	assert.Contains(t, args, "--read-only")
	assert.Contains(t, args, "test:latest")
	assert.Contains(t, args, "evolve")
}

func TestEvolutionSandbox_WithRunner_DockerEnabled(t *testing.T) {
	runner := &mockContainerRunner{
		available: true,
		exitCode:  0,
		stdout:    "mutation applied",
	}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	sandbox := NewEvolutionSandboxWithRunner(cfg, NewHITLGate(true), runner)

	result := sandbox.RunEvolution(context.Background(), "cap-docker", func() (string, error) {
		return "should not run inline", nil
	})

	assert.True(t, result.Success)
	assert.True(t, result.Container)
	assert.Equal(t, "mutation applied", result.Output)
	require.Len(t, runner.calls, 1)
	assert.Equal(t, cfg.Image, runner.calls[0].Image)
	assert.True(t, runner.calls[0].ReadOnly)
}

func TestEvolutionSandbox_WithRunner_DockerUnavailable(t *testing.T) {
	runner := &mockContainerRunner{available: false}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	sandbox := NewEvolutionSandboxWithRunner(cfg, NewHITLGate(true), runner)

	result := sandbox.RunEvolution(context.Background(), "cap-fallback", func() (string, error) {
		return "ran inline", nil
	})

	assert.True(t, result.Success)
	assert.False(t, result.Container)
	assert.Equal(t, "ran inline", result.Output)
	assert.Empty(t, runner.calls)
}

func TestEvolutionSandbox_WithRunner_ContainerFailure(t *testing.T) {
	runner := &mockContainerRunner{
		available: true,
		exitCode:  1,
		stderr:    "compilation error",
	}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	sandbox := NewEvolutionSandboxWithRunner(cfg, NewHITLGate(true), runner)

	result := sandbox.RunEvolution(context.Background(), "cap-fail", func() (string, error) {
		return "", nil
	})

	assert.False(t, result.Success)
	assert.True(t, result.Container)
	assert.Equal(t, 1, result.ExitCode)
	assert.Contains(t, result.Error, "compilation error")
}

func TestEvolutionSandbox_WithRunner_OOMKilled(t *testing.T) {
	runner := &mockContainerRunner{
		available: true,
		exitCode:  137,
		stderr:    "container OOMKilled",
		oomKilled: true,
	}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	sandbox := NewEvolutionSandboxWithRunner(cfg, NewHITLGate(true), runner)

	result := sandbox.RunEvolution(context.Background(), "cap-oom", func() (string, error) {
		return "", nil
	})

	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "OOM killed")
}

func TestEvolutionSandbox_WithRunner_RunError(t *testing.T) {
	runner := &mockContainerRunner{
		available: true,
		runErr:    fmt.Errorf("docker daemon not responding"),
	}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	sandbox := NewEvolutionSandboxWithRunner(cfg, NewHITLGate(true), runner)

	result := sandbox.RunEvolution(context.Background(), "cap-err", func() (string, error) {
		return "", nil
	})

	assert.False(t, result.Success)
	assert.True(t, result.Container)
	assert.Contains(t, result.Error, "docker daemon not responding")
}

func TestEvolutionSandbox_WithRunner_BatchDocker(t *testing.T) {
	runner := &mockContainerRunner{
		available: true,
		exitCode:  0,
		stdout:    "ok",
	}

	cfg := DefaultSandboxConfig()
	cfg.UseDocker = true
	gate := NewHITLGate(true)
	sandbox := NewEvolutionSandboxWithRunner(cfg, gate, runner)

	ids := []string{"c1", "c2", "c3"}
	results := sandbox.RunBatch(context.Background(), ids, func(id string) (string, error) {
		return "inline-" + id, nil
	})

	require.Len(t, results, 3)
	for _, r := range results {
		assert.True(t, r.Success)
		assert.True(t, r.Container)
	}
	assert.Len(t, runner.calls, 3)
}
