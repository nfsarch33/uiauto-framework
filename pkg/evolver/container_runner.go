package evolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// ContainerRunner abstracts Docker container lifecycle for testability.
type ContainerRunner interface {
	Run(ctx context.Context, cfg ContainerRunConfig) (ContainerRunResult, error)
	IsAvailable() bool
}

// ContainerRunConfig describes a single container execution.
type ContainerRunConfig struct {
	Image         string            `json:"image"`
	Command       []string          `json:"command"`
	Env           map[string]string `json:"env,omitempty"`
	MemoryLimitMB int               `json:"memory_limit_mb"`
	CPULimit      float64           `json:"cpu_limit"`
	NetworkMode   string            `json:"network_mode"`
	Timeout       time.Duration     `json:"timeout"`
	ReadOnly      bool              `json:"read_only"`
}

// ContainerRunResult captures Docker run output.
type ContainerRunResult struct {
	ExitCode  int           `json:"exit_code"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	Duration  time.Duration `json:"duration"`
	OOMKilled bool          `json:"oom_killed"`
}

// DockerRunner implements ContainerRunner using the docker CLI.
type DockerRunner struct {
	logger *slog.Logger
}

// NewDockerRunner creates a runner that shells out to docker.
func NewDockerRunner(logger *slog.Logger) *DockerRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &DockerRunner{logger: logger}
}

// IsAvailable checks if docker CLI is reachable.
func (d *DockerRunner) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	return cmd.Run() == nil
}

// Run executes a container with the given config and returns the result.
func (d *DockerRunner) Run(ctx context.Context, cfg ContainerRunConfig) (ContainerRunResult, error) {
	if cfg.Image == "" {
		return ContainerRunResult{ExitCode: 1}, fmt.Errorf("container runner: image is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := d.buildArgs(cfg)
	d.logger.Info("docker run", "image", cfg.Image, "args_count", len(args))

	start := time.Now()
	cmd := exec.CommandContext(runCtx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := ContainerRunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		if strings.Contains(stderr.String(), "OOMKilled") || strings.Contains(stderr.String(), "out of memory") {
			result.OOMKilled = true
		}
	}

	d.logger.Info("docker run complete",
		"image", cfg.Image,
		"exit_code", result.ExitCode,
		"duration_ms", result.Duration.Milliseconds(),
	)
	return result, nil
}

func (d *DockerRunner) buildArgs(cfg ContainerRunConfig) []string {
	args := []string{"run", "--rm"}

	if cfg.MemoryLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", cfg.MemoryLimitMB))
	}
	if cfg.CPULimit > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", cfg.CPULimit))
	}
	if cfg.NetworkMode != "" {
		args = append(args, "--network", cfg.NetworkMode)
	}
	if cfg.ReadOnly {
		args = append(args, "--read-only")
	}

	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, cfg.Image)
	args = append(args, cfg.Command...)
	return args
}

// InlineRunner executes mutations in-process (no Docker). Used for testing
// and environments where Docker is unavailable.
type InlineRunner struct{}

// NewInlineRunner creates an inline runner for testing.
func NewInlineRunner() *InlineRunner {
	return &InlineRunner{}
}

// IsAvailable always returns true for inline execution.
func (r *InlineRunner) IsAvailable() bool { return true }

// Run executes the command inline (interprets cfg.Command[0] as a Go test path).
func (r *InlineRunner) Run(ctx context.Context, cfg ContainerRunConfig) (ContainerRunResult, error) {
	start := time.Now()
	result := ContainerRunResult{}

	if ctx.Err() != nil {
		result.ExitCode = 1
		result.Duration = time.Since(start)
		return result, ctx.Err()
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		result.ExitCode = 1
		result.Duration = time.Since(start)
		return result, err
	}

	result.Stdout = string(data)
	result.Duration = time.Since(start)
	return result, nil
}
